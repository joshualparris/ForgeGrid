package ui

import (
	"embed"
	"fmt"
	"os/exec"
	"runtime"
)

//go:embed dashboard/*
var DashboardFS embed.FS

var DisableBrowser bool

func OpenBrowser(url string) {
	if DisableBrowser {
		return
	}
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		fmt.Println("Could not open browser:", err)
	}
}
