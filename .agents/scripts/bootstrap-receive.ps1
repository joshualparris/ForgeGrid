param(
    [Parameter(Mandatory=$true)]
    [string]$EncryptedBundlePath,

    [Parameter(Mandatory=$true)]
    [string]$SignerPublicKeyPath,

    [Parameter(Mandatory=$true)]
    [string]$ExpectedSignerFingerprint
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

if (!(Test-Path $SignerPublicKeyPath)) {
    Write-Error "Signer public key not found at $SignerPublicKeyPath"
    exit 1
}

$cryptoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$goUtil = Join-Path $cryptoDir "bootstrap-crypto\bootstrap-crypto.exe"
$forgegridExe = Join-Path $cryptoDir "..\..\dist\ForgeGrid-USB\Windows\ForgeGrid.exe"

Write-Host "Decrypting bundle and applying config using protected DPAPI blob..."
$output = & $goUtil decrypt-protected-and-apply $privateKeyPath $EncryptedBundlePath $SignerPublicKeyPath $ExpectedSignerFingerprint $forgegridExe 2>&1

$exitCode = $LASTEXITCODE

if ($exitCode -ne 0) {
    Write-Error "Decryption or validation failed: $output"
    exit 1
}

# Only delete persistent material on success
if (Test-Path $privateKeyPath) {
    Remove-Item $privateKeyPath -Force
}
if (Test-Path $EncryptedBundlePath) {
    Remove-Item $EncryptedBundlePath -Force
}

Write-Host $output
Write-Host "Secure bootstrap complete."
