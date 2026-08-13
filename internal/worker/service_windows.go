//go:build windows

package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const svcName = "ForgeGridWorker"
const svcDesc = "ForgeGrid LAN Worker Service"

type forgeGridService struct {
	runFunc func()
}

func (m *forgeGridService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	go m.runFunc()

loop:
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			break loop
		default:
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func RunService(runFunc func()) error {
	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		return err
	}
	if isInteractive {
		runFunc()
		return nil
	}

	// Running as service, redirect output to a log file
	logDir := filepath.Join(os.Getenv("USERPROFILE"), ".config", "forgegrid", "worker", "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, fmt.Sprintf("forgegrid_service_%d.log", time.Now().Unix()))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		os.Stdout = logFile
		os.Stderr = logFile
	}

	return svc.Run(svcName, &forgeGridService{runFunc: runFunc})
}

func ControlService(cmd string, args []string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	switch cmd {
	case "install":
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		exePath, _ = filepath.Abs(exePath)

		s, err := m.OpenService(svcName)
		if err == nil {
			s.Close()
			return fmt.Errorf("service %s already exists", svcName)
		}

		// Add -mode worker to args
		svcArgs := append([]string{"-mode", "worker"}, args...)

		s, err = m.CreateService(svcName, exePath, mgr.Config{
			StartType:        mgr.StartAutomatic,
			DisplayName:      "ForgeGrid Worker",
			Description:      svcDesc,
			DelayedAutoStart: true,
		}, svcArgs...)
		if err != nil {
			return fmt.Errorf("failed to create service: %w", err)
		}
		defer s.Close()

		// Set recovery actions (Auto-restart on crash)
		recoveryActions := []mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 1 * time.Minute},
			{Type: mgr.ServiceRestart, Delay: 5 * time.Minute},
		}
		if err := s.SetRecoveryActions(recoveryActions, 86400); err != nil {
			fmt.Printf("Warning: failed to set recovery actions: %v\n", err)
		}

		fmt.Println("Service installed successfully")

	case "uninstall":
		s, err := m.OpenService(svcName)
		if err != nil {
			return fmt.Errorf("could not open service: %w", err)
		}
		defer s.Close()
		err = s.Delete()
		if err != nil {
			return fmt.Errorf("could not delete service: %w", err)
		}
		fmt.Println("Service uninstalled successfully")

	case "start":
		s, err := m.OpenService(svcName)
		if err != nil {
			return fmt.Errorf("could not open service: %w", err)
		}
		defer s.Close()
		if err := s.Start(); err != nil {
			return fmt.Errorf("could not start service: %w", err)
		}
		fmt.Println("Service started successfully")

	case "stop":
		s, err := m.OpenService(svcName)
		if err != nil {
			return fmt.Errorf("could not open service: %w", err)
		}
		defer s.Close()
		status, err := s.Control(svc.Stop)
		if err != nil {
			return fmt.Errorf("could not stop service: %w", err)
		}
		fmt.Printf("Service stop requested, state: %v\n", status.State)

	case "status":
		status, err := ServiceStatus()
		if err != nil {
			return err
		}
		fmt.Println(status)

	default:
		return fmt.Errorf("unknown service command: %s", cmd)
	}

	return nil
}

func ServiceStatus() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(svcName)
	if err != nil {
		return "", fmt.Errorf("could not open service: %w", err)
	}
	defer s.Close()
	status, err := s.Query()
	if err != nil {
		return "", fmt.Errorf("could not query service status: %w", err)
	}
	return fmt.Sprintf("Service status: %v", status.State), nil
}
