package coordinator

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"forgegrid/internal/network"
	"forgegrid/internal/store"
	"forgegrid/internal/ui"
)

type Coordinator struct {
	Store            *store.Store
	IP               string
	Insecure         bool
	Fingerprint      string
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
	c.Store.Mu.Unlock()

	if c.MessagingGateway == nil {
		gw, err := NewLiveMessagingGateway()
		if err == nil {
			c.MessagingGateway = gw
		}
		// If err != nil, c.MessagingGateway remains nil (Messaging unavailable)
	}

	adminAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "admin" || pass != adminToken {
				w.Header().Set("WWW-Authenticate", `Basic realm="ForgeGrid Dashboard"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		}
	}

	mux.HandleFunc("/api/coordinator/start", adminAuth(c.handleStart))
	mux.HandleFunc("/api/coordinator/status", adminAuth(c.handleStatus))
	mux.HandleFunc("/api/pairing/code", adminAuth(c.handleGenerateCode))

	// Messaging API
	mux.HandleFunc("/api/dashboard/messaging/status", adminAuth(c.handleMessagingStatus))
	mux.HandleFunc("/api/dashboard/messaging/agents", adminAuth(c.handleMessagingAgents))
	mux.HandleFunc("/api/dashboard/messages", adminAuth(c.handleMessages))
	mux.HandleFunc("/api/dashboard/messages/", adminAuth(c.handleMessageDeliveryOrAck))

	mux.HandleFunc("/api/workers/pair", c.handlePair)
	mux.HandleFunc("/api/workers/heartbeat", c.handleHeartbeat)
	mux.HandleFunc("/api/workers/disconnect", adminAuth(c.handleDisconnectWorker))
	mux.HandleFunc("/api/workers", c.handleListWorkers) // Dashboard uses this, but let's let workers use it too or add auth inside handler
	mux.HandleFunc("/api/jobs/test", adminAuth(c.handleTestJob))
	mux.HandleFunc("/api/jobs", c.handleListJobs)
	mux.HandleFunc("/api/jobs/", c.handleJobAction)
	mux.HandleFunc("/api/jobs/manifest", adminAuth(c.handleManifest))

	// Serve UI
	mux.Handle("/", http.HandlerFunc(adminAuth(func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(http.FS(ui.DashboardFS)).ServeHTTP(w, r)
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
	fmt.Printf("Coordinator starting on %s (LAN IP: %s)\n", addr, c.IP)
	if !c.Insecure {
		fmt.Printf("TLS Fingerprint: %s\n", c.Fingerprint)
	}
	fmt.Printf("Admin Password (Dashboard): %s\n", adminToken)

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

func (c *Coordinator) checkWorkerStatus() {
	for {
		time.Sleep(5 * time.Second)
		c.Store.Mu.Lock()
		changed := false
		for _, w := range c.Store.Workers {
			if w.Status == "online" && time.Since(w.LastSeen) > 15*time.Second {
				w.Status = "offline"
				changed = true
			}
		}
		if changed {
			c.Store.Save()
		}
		c.Store.Mu.Unlock()
	}
}
