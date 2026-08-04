param(
    [Parameter(Mandatory=$true)]
    [string]$EncryptedBundlePath
)

$ErrorActionPreference = "Stop"

$dir = "$env:LOCALAPPDATA\ForgeGrid\bootstrap"
$privateKeyPath = "$dir\private.blob"

if (!(Test-Path $privateKeyPath)) {
    Write-Error "Bootstrap private key not found at $privateKeyPath"
    exit 1
}

if (!(Test-Path $EncryptedBundlePath)) {
    Write-Error "Encrypted bundle not found at $EncryptedBundlePath"
    exit 1
}

Write-Host "Unprotecting private key with DPAPI..."
Add-Type -AssemblyName System.Security
$protectedBytes = [System.IO.File]::ReadAllBytes($privateKeyPath)
$privateKeyBytes = [System.Security.Cryptography.ProtectedData]::Unprotect($protectedBytes, $null, [System.Security.Cryptography.DataProtectionScope]::CurrentUser)

# Documentation:
# - The DPAPI-protected key is decrypted in memory
# - A plaintext private-key temporary file is currently created
# - That file is access-restricted to the current user (via GetTempFileName) and best-effort overwritten/deleted
# - This filesystem overwrite is a best-effort approach and not a guaranteed secure erasure
$privTmp = [System.IO.Path]::GetTempFileName()
[System.IO.File]::WriteAllBytes($privTmp, $privateKeyBytes)
[System.Array]::Clear($privateKeyBytes, 0, $privateKeyBytes.Length)

$cryptoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$goUtil = Join-Path $cryptoDir "bootstrap-crypto\bootstrap-crypto.exe"
$forgegridExe = Join-Path $cryptoDir "..\..\dist\ForgeGrid-USB\Windows\ForgeGrid.exe"

Write-Host "Decrypting bundle and applying config..."
$output = & $goUtil decrypt-and-apply $privTmp $EncryptedBundlePath $forgegridExe 2>&1

$exitCode = $LASTEXITCODE

# Safe temp cleanup
$zeroes = New-Object byte[] (Get-Item $privTmp).Length
[System.IO.File]::WriteAllBytes($privTmp, $zeroes)
Remove-Item $privTmp -Force

if ($exitCode -ne 0) {
    Write-Error "Decryption or validation failed: $output"
    exit 1
}

# Only delete persistent material on success
Remove-Item $privateKeyPath -Force
# The go tool already deletes the bundle if successful, but we can try removing just in case
if (Test-Path $EncryptedBundlePath) {
    Remove-Item $EncryptedBundlePath -Force
}

Write-Host $output
Write-Host "Secure bootstrap complete."
