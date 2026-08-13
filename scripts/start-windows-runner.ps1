param(
    [string]$Root = $PSScriptRoot
)

$ErrorActionPreference = "Stop"

if ((Split-Path -Leaf $Root) -eq "scripts") {
    $Root = Split-Path -Parent $Root
}

$exe = Join-Path $Root "ForgeGrid.exe"
if (-not (Test-Path $exe)) {
    $distExe = Join-Path $Root "dist\ForgeGrid-USB\Windows\ForgeGrid.exe"
    if (Test-Path $distExe) {
        $exe = $distExe
    } else {
        Write-Error "ForgeGrid.exe not found in $Root or dist\ForgeGrid-USB\Windows"
        exit 1
    }
}

$logDir = Join-Path $env:LOCALAPPDATA "ForgeGrid\logs"
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$log = Join-Path $logDir ("runner_{0:yyyyMMdd_HHmmss}.log" -f (Get-Date))

Write-Host "Starting ForgeGrid runner..."
Write-Host "Executable: $exe"
Write-Host "Log: $log"

Start-Process -FilePath $exe -ArgumentList "-mode worker" -WorkingDirectory (Split-Path -Parent $exe) -RedirectStandardOutput $log -RedirectStandardError $log -WindowStyle Minimized

Start-Sleep -Seconds 2
Get-Process ForgeGrid -ErrorAction SilentlyContinue | Select-Object Id, ProcessName, Path

