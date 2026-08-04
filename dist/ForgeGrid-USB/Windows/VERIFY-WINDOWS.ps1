$ErrorActionPreference = "Stop"

$ScriptDir = $PSScriptRoot
$ExePath = Join-Path $ScriptDir "ForgeGrid.exe"
$Port = 48192

Write-Host "Verifying ForgeGrid Windows Runtime..."
Write-Host "[Stage 1] Checking executable existence..."

if (-not (Test-Path $ExePath -PathType Leaf)) {
    Write-Host "FAILED at Stage 1: '$ExePath' not found"
    exit 1
}

$Process = $null
$TempDir = $null
$Verified = $false
$FailureMessage = $null
$CleanupSucceeded = $true

Write-Host "[Stage 2] Preparing isolated test environment..."
$Guid = [guid]::NewGuid().ToString()
$TempDir = Join-Path $env:TEMP "ForgeGrid-Test-$Guid"

try {
    $DataDir = Join-Path $TempDir "forgegrid-data"
    New-Item -ItemType Directory -Path $DataDir -Force | Out-Null

    $ExpectedIdentity = "ForgeGrid-Verifier-$Guid"
    $CoordJsonPath = Join-Path $DataDir "coordinator.json"
    $CoordJsonContent = @{
        identity = $ExpectedIdentity
    } | ConvertTo-Json

    Set-Content -Path $CoordJsonPath -Value $CoordJsonContent

    Write-Host "[Stage 3] Starting isolated ForgeGrid Coordinator..."
    try {
        $Process = Start-Process -FilePath $ExePath -ArgumentList "-mode", "coordinator", "-port", "$Port" -WorkingDirectory $TempDir -PassThru -WindowStyle Hidden
    } catch {
        $FailureMessage = "FAILED at Stage 3: Failed to launch ForgeGrid.exe"
        throw
    }

    if (-not $Process -or -not $Process.Id) {
        $FailureMessage = "FAILED at Stage 3: Failed to capture PID"
        throw
    }

    $PIDToMonitor = $Process.Id
    Write-Host "Captured ForgeGrid PID: $PIDToMonitor"

    Write-Host "[Stage 4] Waiting for HTTPS server and verifying identity..."

    for ($i = 1; $i -le 10; $i++) {
        Start-Sleep -Seconds 1

        # Check if process died early
        $p = Get-Process -Id $PIDToMonitor -ErrorAction SilentlyContinue
        if (-not $p -or $p.HasExited) {
            $FailureMessage = "FAILED at Stage 4: Process $PIDToMonitor exited prematurely before becoming ready."
            break
        }

        # Check executable path if accessible
        try {
            if ($p.Path -and $p.Path -ne $ExePath) {
                $FailureMessage = "FAILED at Stage 4: PID $PIDToMonitor path mismatch. Expected '$ExePath', got '$($p.Path)'"
                break
            }
        } catch {}

        # Query HTTPS endpoint using curl
        try {
            $CurlOutput = curl.exe -k -sS -f "https://127.0.0.1:$Port/api/coordinator/status" 2>$null
            if ($LASTEXITCODE -eq 0 -and $CurlOutput) {
                $Response = $CurlOutput | ConvertFrom-Json
                if ($Response.identity -eq $ExpectedIdentity) {
                    $Verified = $true
                    break
                }
            }
        } catch {
            # Ignore errors and keep polling
        }
    }

    if (-not $Verified -and -not $FailureMessage) {
        $FailureMessage = "FAILED at Stage 4: Coordinator HTTPS Status check timed out or returned mismatched identity."
    }

} catch {
    if (-not $FailureMessage) {
        $FailureMessage = "FAILED: An unexpected error occurred: $_"
    }
} finally {
    Write-Host "[Stage 5] Cleaning up isolated test process..."
    
    if ($Process -and $Process.Id) {
        try {
            $p = Get-Process -Id $Process.Id -ErrorAction SilentlyContinue
            if ($p -and -not $p.HasExited) {
                Stop-Process -Id $Process.Id -Force -ErrorAction Stop
            }
            # Wait briefly to let OS fully release file handles from the process's working directory
            try { $Process.WaitForExit(1000) | Out-Null } catch {}

            # Confirm process is no longer running
            $pCheck = Get-Process -Id $Process.Id -ErrorAction SilentlyContinue
            if ($pCheck -and -not $pCheck.HasExited) {
                Write-Host "FAILED: Process $($Process.Id) is still running after attempted stop."
                $CleanupSucceeded = $false
            }
        } catch {
            Write-Host "FAILED: Cleanup could not safely terminate process $($Process.Id)"
            $CleanupSucceeded = $false
        }
    }

    if ($TempDir) {
        try {
            if (Test-Path $TempDir) {
                # Attempt to remove with retries since Windows may hold transient locks
                $removed = $false
                for ($retry = 0; $retry -lt 3; $retry++) {
                    try {
                        Remove-Item -Path $TempDir -Recurse -Force -ErrorAction Stop
                        $removed = $true
                        break
                    } catch {
                        Start-Sleep -Milliseconds 500
                    }
                }
                if (-not $removed) {
                    # Final attempt that will throw if it fails
                    Remove-Item -Path $TempDir -Recurse -Force -ErrorAction Stop
                }
            }
            # Confirm directory no longer exists
            if (Test-Path $TempDir) {
                Write-Host "FAILED: Directory $TempDir still exists after attempted removal."
                $CleanupSucceeded = $false
            }
        } catch {
            Write-Host "FAILED: Could not remove temporary test directory $TempDir"
            $CleanupSucceeded = $false
        }
    }
}

if ($Verified -and $CleanupSucceeded) {
    Write-Host "SUCCESS: Windows runtime HTTPS endpoint and exact identity verified successfully."
    exit 0
} else {
    if ($FailureMessage) {
        Write-Host $FailureMessage
    }
    exit 1
}
