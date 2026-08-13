param(
    [string]$Root = (Get-Location).Path,
    [switch]$InstallService
)

$ErrorActionPreference = "Stop"

$script = Join-Path $Root "scripts\start-windows-runner.ps1"
if (-not (Test-Path $script)) {
    Write-Error "Missing $script"
    exit 1
}

$desktop = [Environment]::GetFolderPath("Desktop")
$shortcutPath = Join-Path $desktop "Start ForgeGrid Runner.lnk"
$ws = New-Object -ComObject WScript.Shell
$shortcut = $ws.CreateShortcut($shortcutPath)
$shortcut.TargetPath = "powershell.exe"
$shortcut.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$script`" -Root `"$Root`""
$shortcut.WorkingDirectory = $Root
$shortcut.IconLocation = "powershell.exe,0"
$shortcut.Save()

Write-Host "Created desktop shortcut: $shortcutPath" -ForegroundColor Green

if ($InstallService) {
    $exe = Join-Path $Root "ForgeGrid.exe"
    if (-not (Test-Path $exe)) {
        $exe = Join-Path $Root "dist\ForgeGrid-USB\Windows\ForgeGrid.exe"
    }
    if (-not (Test-Path $exe)) {
        Write-Error "ForgeGrid.exe not found; cannot install service."
        exit 1
    }
    Write-Host "Installing ForgeGrid worker service. This requires an elevated Administrator shell." -ForegroundColor Yellow
    & $exe service install -name $env:COMPUTERNAME
    & $exe service start
    & $exe service status
}

