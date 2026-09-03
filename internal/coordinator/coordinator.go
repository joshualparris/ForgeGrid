package coordinator

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"forgegrid/internal/models"
	"forgegrid/internal/network"
	"forgegrid/internal/store"
	"forgegrid/internal/ui"
)

type Coordinator struct {
	Store            *store.Store
	IP               string
	Insecure         bool
	Fingerprint      string
	AdminToken       string
	Listener         net.Listener
	MessagingGateway MessagingGateway
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func New(s *store.Store, insecure bool) *Coordinator {
	return &Coordinator{
		Store:    s,
		IP:       getOutboundIP(),
		Insecure: insecure,
	}
}

func (c *Coordinator) Start(port string) error {
	mux := http.NewServeMux()

	// Ensure identity, AdminToken and TLS cert
	c.Store.Mu.Lock()
	if c.Store.CoordinatorCfg.Identity == "" {
		c.Store.CoordinatorCfg.Identity = fmt.Sprintf("ForgeGrid-%d", time.Now().UnixNano()) // fallback if rand fails later
		c.Store.Save()
	}
	if c.Store.CoordinatorCfg.AdminToken == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			c.Store.Mu.Unlock()
			return fmt.Errorf("failed to generate secure admin token: %w", err)
		}
		c.Store.CoordinatorCfg.AdminToken = hex.EncodeToString(b)
		c.Store.Save()
	}

	if len(c.Store.CoordinatorCfg.CertPEM) == 0 {
		certPEM, keyPEM, fp, err := network.GenerateSelfSignedCert()
		if err != nil {
			c.Store.Mu.Unlock()
			return fmt.Errorf("failed to generate TLS cert: %w", err)
		}
		c.Store.CoordinatorCfg.CertPEM = certPEM
		c.Store.CoordinatorCfg.KeyPEM = keyPEM
		c.Fingerprint = fp
		c.Store.Save()
	} else {
		fp, err := network.FingerprintFromPEM(c.Store.CoordinatorCfg.CertPEM)
		if err != nil {
			c.Store.Mu.Unlock()
			return err
		}
		c.Fingerprint = fp
	}
	adminToken := c.Store.CoordinatorCfg.AdminToken
	c.AdminToken = adminToken
	c.Store.Mu.Unlock()

	if c.MessagingGateway == nil {
		gw, err := NewLiveMessagingGateway()
		if err == nil {
			c.MessagingGateway = gw
		}
		// If err != nil, c.MessagingGateway remains nil (Messaging unavailable)
	}

	adminAuth := func(next http.HandlerFunc) http.HandlerFunc { return c.requireAdmin(next) }

	mux.HandleFunc("/api/coordinator/start", adminAuth(c.handleStart))
	mux.HandleFunc("/api/coordinator/status", adminAuth(c.handleStatus))
	mux.HandleFunc("/api/session/start", adminAuth(c.handleSessionStart))
	mux.HandleFunc("/api/pairing/code", adminAuth(c.handleGenerateCode))
	mux.HandleFunc("/api/projects", adminAuth(c.handleProjects))
	mux.HandleFunc("/api/projects/refresh", adminAuth(c.handleProjectsRefresh))
	mux.HandleFunc("/api/projects/favorite", adminAuth(c.handleProjectFavorite))
	mux.HandleFunc("/api/projects/inspect", adminAuth(c.handleProjectInspect))

	// Messaging API
	mux.HandleFunc("/api/dashboard/messaging/status", adminAuth(c.handleMessagingStatus))
	mux.HandleFunc("/api/dashboard/messaging/agents", adminAuth(c.handleMessagingAgents))
	mux.HandleFunc("/api/dashboard/messaging/repair", adminAuth(c.handleMessagingRepair))
	mux.HandleFunc("/api/dashboard/messages", adminAuth(c.handleMessages))
	mux.HandleFunc("/api/dashboard/messages/", adminAuth(c.handleMessageDeliveryOrAck))

	mux.HandleFunc("/api/workers/pair", c.handlePair)
	mux.HandleFunc("/api/workers/heartbeat", c.handleHeartbeat)
	mux.HandleFunc("/api/workers/disconnect", adminAuth(c.handleDisconnectWorker))
	mux.HandleFunc("/api/workers/policy", adminAuth(c.handleWorkerPolicy))
	mux.HandleFunc("/api/workers", adminAuth(c.handleListWorkers))
	mux.HandleFunc("/api/updates/status", adminAuth(c.handleUpdateStatus))
	mux.HandleFunc("/api/updates/workers", adminAuth(c.handleQueueWorkerUpdates))
	mux.HandleFunc("/api/updates/worker", c.handleWorkerUpdatePoll)
	mux.HandleFunc("/api/updates/report", c.handleWorkerUpdateReport)
	mux.HandleFunc("/api/jobs/test", adminAuth(c.handleTestJob))
	mux.HandleFunc("/api/jobs/batch-compute-test", adminAuth(c.handleBatchComputeTest))
	mux.HandleFunc("/api/jobs", c.handleListJobs)
	mux.HandleFunc("/api/jobs/", c.handleJobAction)
	mux.HandleFunc("/api/jobs/manifest", adminAuth(c.handleManifest))

	// Serve UI
	dashboardFS, err := fs.Sub(ui.DashboardFS, "dashboard")
	if err != nil {
		return fmt.Errorf("failed to prepare dashboard filesystem: %w", err)
	}
	dashboardHandler := http.FileServer(http.FS(dashboardFS))
	mux.Handle("/", http.HandlerFunc(adminAuth(func(w http.ResponseWriter, r *http.Request) {
		dashboardHandler.ServeHTTP(w, r)
	})))

	var addr string
	if c.Listener != nil {
		addr = c.Listener.Addr().String()
	} else {
		addr = fmt.Sprintf("0.0.0.0:%s", port)
	}

	go c.checkWorkerStatus()

	scheme := "https"
	if c.Insecure {
		scheme = "http"
	}
	uiURL := fmt.Sprintf("%s://%s:%s", scheme, c.IP, port)
	loginPath, loginErr := c.writeDashboardLoginFile(uiURL, adminToken)
	c.printDashboardLogin(addr, uiURL, adminToken, loginPath, loginErr)

	ui.OpenBrowser(uiURL)

	if c.Listener != nil {
		if c.Insecure {
			server := &http.Server{
				Handler:      mux,
				ReadTimeout:  15 * time.Second,
				WriteTimeout: 15 * time.Second,
				IdleTimeout:  60 * time.Second,
			}
			return server.Serve(c.Listener)
		}
		cert, err := tls.X509KeyPair(c.Store.CoordinatorCfg.CertPEM, c.Store.CoordinatorCfg.KeyPEM)
		if err != nil {
			return err
		}
		server := &http.Server{
			Handler: mux,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		return server.ServeTLS(c.Listener, "", "")
	}

	if c.Insecure {
		server := &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		return server.ListenAndServe()
	}

	// Create tls cert from PEM
	cert, err := tls.X509KeyPair(c.Store.CoordinatorCfg.CertPEM, c.Store.CoordinatorCfg.KeyPEM)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return server.ListenAndServeTLS("", "")
}

func (c *Coordinator) writeDashboardLoginFile(uiURL, adminToken string) (string, error) {
	path := filepath.Join(c.Store.Dir(), "dashboard-login.txt")
	body := fmt.Sprintf("ForgeGrid Dashboard Login\n\nURL: %s\nUsername: admin\nPassword: %s\n\nKeep this file private. Anyone with this password can control the local ForgeGrid coordinator.\n", uiURL, adminToken)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		return path, err
	}
	return path, nil
}

func (c *Coordinator) printDashboardLogin(addr, uiURL, adminToken, loginPath string, loginErr error) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" ForgeGrid Dashboard Login")
	fmt.Println("========================================")
	fmt.Printf(" Coordinator: %s (LAN IP: %s)\n", addr, c.IP)
	fmt.Printf(" URL:         %s\n", uiURL)
	fmt.Println(" Username:    admin")
	fmt.Printf(" Password:    %s\n", adminToken)
	if !c.Insecure {
		fmt.Printf(" TLS FP:      %s\n", c.Fingerprint)
	}
	if loginErr != nil {
		fmt.Printf(" Login file:  unavailable (%v)\n", loginErr)
	} else {
		fmt.Printf(" Login file:  %s\n", loginPath)
	}
	fmt.Println("========================================")
	fmt.Println()
}

func (c *Coordinator) checkWorkerStatus() {
	for {
		time.Sleep(5 * time.Second)
		c.Store.Mu.Lock()
		changed := false
		for _, w := range c.Store.Workers {
			if w.Status == "online" && time.Since(w.LastSeen) > 15*time.Second {
				w.Status = "offline"
				changed = true
				if c.requeueWorkerJobsLocked(w.ID) {
					changed = true
				}
			}
		}
		if changed {
			c.Store.Save()
		}
		c.Store.Mu.Unlock()
	}
}

func (c *Coordinator) requeueWorkerJobsLocked(workerID string) bool {
	changed := false
	for _, job := range c.Store.Jobs {
		if job.WorkerID != workerID {
			continue
		}
		if job.Status != models.StatusClaimed && job.Status != models.StatusRunning {
			continue
		}
		job.Status = models.StatusFailed
		job.Result = "worker offline"
		now := time.Now()
		job.EndTime = &now
		changed = true
		if job.RetryCount >= job.MaxRetries {
			continue
		}
		retry := *job
		retry.ID = "job-" + cryptoRandomHex(16)
		retry.AttemptID = ""
		retry.Status = models.StatusPending
		retry.StartTime = nil
		retry.EndTime = nil
		retry.Result = ""
		retry.Logs = nil
		retry.LogSeq = 0
		retry.Artifacts = nil
		retry.PushedBranch = ""
		retry.PRURL = ""
		retry.RetryOf = job.ID
		retry.RetryCount = job.RetryCount + 1
		c.Store.Jobs[retry.ID] = &retry
	}
	return changed
}
