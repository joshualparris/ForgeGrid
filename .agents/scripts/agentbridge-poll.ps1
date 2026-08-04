$ErrorActionPreference = "Stop"

$workspaceDir = "C:\dev\ForgeGrid"
$exePath = "$workspaceDir\dist\ForgeGrid-USB\Windows\ForgeGrid.exe"

if (!(Test-Path $exePath)) {
    Write-Error "ForgeGrid executable not found at $exePath"
    exit 1
}

Write-Host "Checking Inbox for windows-test..."
$inboxJson = & $exePath agent-bridge inbox
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to fetch inbox: $inboxJson"
    exit 1
}

$inbox = $inboxJson | ConvertFrom-Json
if ($inbox.Length -eq 0) {
    Write-Host "Inbox is empty. Nothing to do."
    exit 0
}

# Process only one message per invocation
$msg = $inbox[0]
Write-Host "Processing message ID: $($msg.id)"
Write-Host "Sender: $($msg.sender), Task ID: $($msg.task_id), Type: $($msg.type)"

# Acknowledge before acting
Write-Host "Acknowledging message..."
$ackResult = & $exePath agent-bridge ack --message-id $msg.id
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to acknowledge message: $ackResult"
    exit 1
}

# Safety validations
if ($msg.type -ne "instruction") {
    Write-Warning "Ignoring message of unsupported type: $($msg.type)"
    exit 0
}

# We do not execute arbitrary shell commands.
# This script is meant to be called by Antigravity, so the polling script 
# simply leaves the acknowledged message for Antigravity's attention.
# When Antigravity runs the scheduled task, it will handle the instruction.
Write-Host "Message acknowledged. Waiting for Antigravity to act."
