$ErrorActionPreference = "Stop"

$taskName = "ForgeGrid_AgentBridge_Poll"
$scriptPath = "C:\dev\ForgeGrid\.agents\scripts\agentbridge-poll.ps1"

Write-Host "Registering scheduled task $taskName (Disabled by default)..."

$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$scriptPath`""
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 5)
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RunOnlyIfNetworkAvailable
$principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive

# Register disabled task
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Description "Polls AgentBridge inbox every 5 minutes" -Force
Disable-ScheduledTask -TaskName $taskName | Out-Null

Write-Host "Task registered and disabled successfully."
