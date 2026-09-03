//go:build !windows

package worker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const systemdUnitName = "forgegrid-worker.service"

func RunService(runFunc func()) error {
	runFunc()
	return nil
}

func ControlService(cmd string, args []string) error {
	switch cmd {
	case "install":
		return installUserService(args)
	case "uninstall":
		if err := runSystemctl("disable", "--now", systemdUnitName); err != nil {
			fmt.Printf("Warning: failed to disable service: %v\n", err)
		}
		unitPath, err := userServicePath()
		if err != nil {
			return err
		}
		if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = runSystemctl("daemon-reload")
		fmt.Println("User service uninstalled successfully")
		return nil
	case "start":
		return runSystemctl("start", systemdUnitName)
	case "stop":
		return runSystemctl("stop", systemdUnitName)
	case "status":
		status, err := ServiceStatus()
		if status != "" {
			fmt.Println(status)
		}
		return err
	default:
		return fmt.Errorf("unknown service command: %s", cmd)
	}
}

func ServiceStatus() (string, error) {
	cmd := exec.Command("systemctl", "--user", "status", "--no-pager", systemdUnitName)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func installUserService(args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}
	unitPath, err := userServicePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return err
	}

	workerArgs := append([]string{"-mode", "worker"}, args...)
	unit := `[Unit]
Description=ForgeGrid Worker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + systemdQuote(exePath, workerArgs) + `
Restart=always
RestartSec=10
WorkingDirectory=` + shellQuote(filepath.Dir(exePath)) + `

[Install]
WantedBy=default.target
`
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl("enable", systemdUnitName); err != nil {
		return err
	}
	fmt.Printf("User service installed at %s\n", unitPath)
	fmt.Printf("Start it with: systemctl --user start %s\n", systemdUnitName)
	return nil
}

func userServicePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "systemd", "user", systemdUnitName), nil
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func systemdQuote(exe string, args []string) string {
	parts := []string{shellQuote(exe)}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(v string) string {
	if v == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
}
