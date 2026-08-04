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

Write-Host "[Stage 2] Preparing isolated test environment..."
$Guid = [guid]::NewGuid().ToString()
$TempDir = Join-Path $env:TEMP "ForgeGrid-Test-$Guid"
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
    Write-Host "FAILED at Stage 3: Failed to launch ForgeGrid.exe"
    exit 1
}

if (-not $Process -or -not $Process.Id) {
    Write-Host "FAILED at Stage 3: Failed to capture PID"
    exit 1
}

$PIDToMonitor = $Process.Id
Write-Host "Captured ForgeGrid PID: $PIDToMonitor"

$ExitCode = 0
$Ready = $false

Write-Host "[Stage 4] Waiting for HTTPS server and verifying identity..."

for ($i = 1; $i -le 10; $i++) {
    Start-Sleep -Seconds 1

    # Check if process died early
    $p = Get-Process -Id $PIDToMonitor -ErrorAction SilentlyContinue
    if (-not $p -or $p.HasExited) {
        Write-Host "FAILED at Stage 4: Process $PIDToMonitor exited prematurely before becoming ready."
        $ExitCode = 1
        break
    }

    # Check executable path if accessible
    try {
        if ($p.Path -and $p.Path -ne $ExePath) {
            Write-Host "FAILED at Stage 4: PID $PIDToMonitor path mismatch. Expected '$ExePath', got '$($p.Path)'"
            $ExitCode = 1
            break
        }
    } catch {}

    # Query HTTPS endpoint using curl to avoid PowerShell connection pooling (Keep-Alive TIME_WAIT issues)
    try {
        $CurlOutput = curl.exe -k -sS -f "https://127.0.0.1:$Port/api/coordinator/status" 2>$null
        if ($LASTEXITCODE -eq 0 -and $CurlOutput) {
            $Response = $CurlOutput | ConvertFrom-Json
            if ($Response.identity -eq $ExpectedIdentity) {
                $Ready = $true
                break
            }
        }
    } catch {
        # Ignore errors and keep polling
    }
}

if (-not $Ready -and $ExitCode -eq 0) {
    Write-Host "FAILED at Stage 4: Coordinator HTTPS Status check timed out or returned mismatched identity."
    $ExitCode = 1
} elseif ($Ready) {
    Write-Host "SUCCESS: Windows runtime HTTPS endpoint and exact identity verified successfully."
    $ExitCode = 0
}

Write-Host "[Stage 5] Cleaning up isolated test process..."
try {
    $p = Get-Process -Id $PIDToMonitor -ErrorAction SilentlyContinue
    if ($p -and -not $p.HasExited) {
        Stop-Process -Id $PIDToMonitor -Force -ErrorAction Stop
    }
} catch {
    Write-Host "FAILED: Cleanup could not safely terminate process $PIDToMonitor"
    $ExitCode = 1
}

try {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force -ErrorAction Stop
    }
} catch {
    Write-Host "FAILED: Could not remove temporary test directory $TempDir"
    $ExitCode = 1
}

exit $ExitCode
