$ErrorActionPreference = "Stop"

$cryptoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$goUtil = Join-Path $cryptoDir "bootstrap-crypto"

Write-Host "Compiling Go crypto utility..."
Push-Location $goUtil
go build -o bootstrap-crypto.exe .
Pop-Location

$dir = "$env:LOCALAPPDATA\ForgeGrid\bootstrap"
if (!(Test-Path $dir)) { 
    $null = New-Item -ItemType Directory -Path $dir -Force 
}

# The Go utility secureBootstrapDirectory will do the ACLs when it runs, 
# but it's safe to do them here or rely on the utility if it creates it.
# Actually, the Go utility will secure it, so we just run the utility.
$blobPath = "$dir\private.blob"
$pubPath = Join-Path $PWD "bootstrap-public.pem"

Write-Host "Generating 3072-bit RSA key pair..."
$output = & "$goUtil\bootstrap-crypto.exe" generate-protected $blobPath $pubPath

if ($LASTEXITCODE -ne 0) {
    Write-Error "Key generation failed"
    exit 1
}

$fingerprint = ($output -join "").Trim()

Write-Host "`nBootstrap material generated."
Write-Host "Public Key File : $pubPath"
Write-Host "Fingerprint     : $fingerprint"
Write-Host "`nSend bootstrap-public.pem to the Fedora coordinator."
