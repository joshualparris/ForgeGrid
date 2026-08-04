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

$privTmp = [System.IO.Path]::GetTempFileName()
[System.IO.File]::WriteAllBytes($privTmp, $privateKeyBytes)
[System.Array]::Clear($privateKeyBytes, 0, $privateKeyBytes.Length)

$cryptoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$goUtil = Join-Path $cryptoDir "bootstrap-crypto\bootstrap-crypto.exe"

Write-Host "Decrypting bundle..."
$output = & $goUtil decrypt $privTmp $EncryptedBundlePath 2>&1

# Safe temp cleanup
Remove-Item $privTmp -Force

if ($LASTEXITCODE -ne 0) {
    Write-Error "Decryption or validation failed: $output"
    exit 1
}

# Only delete persistent material on success
Remove-Item $privateKeyPath -Force
Remove-Item $EncryptedBundlePath -Force

$bundle = $output | ConvertFrom-Json

Write-Host "Bundle decrypted and validated successfully."
Write-Host "Agent Name: $($bundle.agent_name)"
Write-Host "Relay URL : $($bundle.relay_url)"
Write-Host "TLS Fingerprint: $($bundle.fingerprint)"
Write-Host "Expiry    : $($bundle.expiry)"
Write-Host "Bootstrap ID: $($bundle.bootstrap_id)"

# Pass the token to ForgeGrid through the new secure stdin or protected-file setup flow once Fedora implements it.
Write-Host "`nTODO: Feed token to ForgeGrid secure configuration flow here."

Write-Host "Secure bootstrap complete."
