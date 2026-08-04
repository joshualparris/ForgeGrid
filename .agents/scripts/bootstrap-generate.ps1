param(
    [Parameter(Mandatory=$true)]
    [string]$PublicKeyPath,

    [Parameter(Mandatory=$true)]
    [string]$PlaintextJSONPath,

    [Parameter(Mandatory=$true)]
    [string]$SignerPrivateKeyPath,

    [Parameter(Mandatory=$true)]
    [string]$OutputPath
)

$ErrorActionPreference = "Stop"

$cryptoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$goUtil = Join-Path $cryptoDir "bootstrap-crypto"

Write-Host "Compiling Go crypto utility..."
Push-Location $goUtil
go build -o bootstrap-crypto .
Pop-Location

Write-Host "Generating signed and encrypted bootstrap bundle..."
$exePath = Join-Path $goUtil "bootstrap-crypto"
if ($IsWindows) {
    $exePath += ".exe"
}

& $exePath encrypt $PublicKeyPath $PlaintextJSONPath $SignerPrivateKeyPath $OutputPath

if ($LASTEXITCODE -ne 0) {
    Write-Error "Bundle generation failed."
    exit 1
}

Write-Host "`nBootstrap bundle generated at $OutputPath"
