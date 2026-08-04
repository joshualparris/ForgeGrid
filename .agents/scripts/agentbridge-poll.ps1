$ErrorActionPreference = "Stop"

$workspaceDir = "C:\dev\ForgeGrid"
$exePath = "$workspaceDir\dist\ForgeGrid-USB\Windows\ForgeGrid.exe"

if (!(Test-Path $exePath)) {
    Write-Host "ForgeGrid executable not found at $exePath"
    exit 0
}

Write-Host "Checking Inbox..."
try {
    $inboxJson = & $exePath agent-bridge inbox
} catch {
    Write-Host "Failed to fetch inbox."
    exit 0
}

if ($LASTEXITCODE -ne 0) {
    Write-Host "Failed to fetch inbox: $inboxJson"
    exit 0
}

$inbox = $inboxJson | ConvertFrom-Json
if ($inbox -eq $null -or $inbox.Length -eq 0) {
    Write-Host "Inbox is empty. Nothing to do."
    exit 0
}

# Process only one message per invocation
$msg = $inbox[0]

# Safety validations
if ($msg.type -ne "instruction") {
    Write-Host "Ignoring message of unsupported type: $($msg.type)"
    exit 0
}

Write-Host "Pending message ID: $($msg.id)"
Write-Host "Sender: $($msg.sender), Task ID: $($msg.task_id), Type: $($msg.type)"

# Never acknowledge, complete, fail, or execute content here.
Write-Host "Message found. Antigravity should handle acknowledgment, execution, and completion."
exit 0
