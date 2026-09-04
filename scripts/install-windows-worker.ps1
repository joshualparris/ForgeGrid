<#
.SYNOPSIS
Installs and configures the ForgeGrid Windows Worker service.

.DESCRIPTION
This script registers ForgeGrid as a Windows Service, configures it to auto-start,
sets up auto-restart recovery on crash, unblocks it in the Windows Firewall, and
establishes a log rotation scheduled task for the worker logs.

.PARAMETER Coordinator
The IP or hostname of the ForgeGrid coordinator.

.PARAMETER Code
The 6-digit pairing code generated from the dashboard.

.PARAMETER Fingerprint
The TLS fingerprint of the coordinator.

.PARAMETER AllowedRepos
Comma-separated list of allowed repository URLs.

.PARAMETER AllowPush
Switch to allow the worker to push changes.
#>

param (
    [Parameter(Mandatory=$false)]
    [string]$Coordinator,

    [Parameter(Mandatory=$false)]
    [string]$Code,

    [Parameter(Mandatory=$false)]
    [string]$Fingerprint,

    [Parameter(Mandatory=$false)]
    [string]$AllowedRepos,

    [switch]$AllowPush,

    [Parameter(Mandatory=$false)]
    [string]$Name
)

$ErrorActionPreference = "Stop"

# 1. Verify executable exists
$ExePath = Join-Path $PSScriptRoot "..\forgegrid.exe" | Resolve-Path -ErrorAction SilentlyContinue
if (-not $ExePath) {
    # Check current directory
    $ExePath = Join-Path $PWD "forgegrid.exe" | Resolve-Path -ErrorAction SilentlyContinue
    if (-not $ExePath) {
        Write-Error "forgegrid.exe not found. Please place this script in the same directory as the executable."
        exit 1
    }
}

Write-Host "Found ForgeGrid executable at: $($ExePath.Path)" -ForegroundColor Green

# 2. Build install arguments
$ArgsList = @("-install-service")
if ($Coordinator) { $ArgsList += "-coordinator", $Coordinator }
if ($Code) { $ArgsList += "-code", $Code }
if ($Fingerprint) { $ArgsList += "-fingerprint", $Fingerprint }
if ($AllowedRepos) { $ArgsList += "-allowed-repos", $AllowedRepos }
if ($AllowPush) { $ArgsList += "-allow-push" }
if ($Name) { $ArgsList += "-name", $Name }

# 3. Stop and Uninstall existing service if present
Write-Host "Ensuring clean slate..."
& $ExePath.Path -stop-service 2>$null
& $ExePath.Path -uninstall-service 2>$null
Start-Sleep -Seconds 2

# 4. Install Service
Write-Host "Installing Windows Service..."
$process = Start-Process -FilePath $ExePath.Path -ArgumentList $ArgsList -Wait -NoNewWindow -PassThru
if ($process.ExitCode -ne 0) {
    Write-Error "Failed to install service (Exit Code: $($process.ExitCode)). Ensure you are running as Administrator."
    exit 1
}

# 5. Configure Firewall
Write-Host "Configuring Windows Firewall..."
$RuleName = "ForgeGrid Worker"
$existingRule = Get-NetFirewallRule -DisplayName $RuleName -ErrorAction SilentlyContinue
if ($existingRule) {
    Remove-NetFirewallRule -DisplayName $RuleName
}
New-NetFirewallRule -DisplayName $RuleName -Direction Inbound -Program $ExePath.Path -Action Allow -Profile Any | Out-Null
New-NetFirewallRule -DisplayName $RuleName -Direction Outbound -Program $ExePath.Path -Action Allow -Profile Any | Out-Null
Write-Host "Firewall rules created successfully." -ForegroundColor Green

# 6. Setup Log Rotation
# Windows doesn't have logrotate natively, so we set up a Scheduled Task to compress/clean old logs
Write-Host "Setting up Log Rotation task..."
$LogDir = Join-Path $env:USERPROFILE ".config\forgegrid\worker\logs"
if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Path $LogDir | Out-Null
}
$TaskScript = @"
`$LogDir = '$LogDir'
Get-ChildItem `$LogDir -Filter '*.log' | Where-Object { `$_.LastWriteTime -lt (Get-Date).AddDays(-7) } | Remove-Item -Force
"@
$TaskScriptPath = Join-Path $LogDir "logrotate.ps1"
Set-Content -Path $TaskScriptPath -Value $TaskScript

$Action = New-ScheduledTaskAction -Execute "PowerShell.exe" -Argument "-WindowStyle Hidden -NonInteractive -ExecutionPolicy Bypass -File `"$TaskScriptPath`""
$Trigger = New-ScheduledTaskTrigger -Daily -At 3:00AM
$Principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$Task = New-ScheduledTask -Action $Action -Trigger $Trigger -Principal $Principal
Register-ScheduledTask -TaskName "ForgeGridLogRotation" -InputObject $Task -Force | Out-Null
Write-Host "Log rotation scheduled task created." -ForegroundColor Green

# 7. Start Service
Write-Host "Starting ForgeGrid Service..."
& $ExePath.Path -start-service
Start-Sleep -Seconds 2

# 8. Run Health Check
Write-Host "Running Health Check Diagnostics..."
& $ExePath.Path -health-check

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "ForgeGrid Worker installed and running!" -ForegroundColor Cyan
Write-Host "Logs are located at: $LogDir" -ForegroundColor Cyan
Write-Host "Service is configured to auto-start on boot and auto-restart on crash." -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
