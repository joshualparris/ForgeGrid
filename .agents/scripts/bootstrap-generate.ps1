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

$isWindowsPlatform = [System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform(
	[System.Runtime.InteropServices.OSPlatform]::Windows
)

$exeName = if ($isWindowsPlatform) {
	"bootstrap-crypto.exe"
} else {
	"bootstrap-crypto"
}

$exePath = Join-Path $goUtil $exeName

Write-Host "Compiling Go crypto utility..."
try {
	Push-Location $goUtil
	go build -o $exeName .
	if ($LASTEXITCODE -ne 0) {
		Write-Error "Go build failed."
		exit 1
	}
} finally {
	Pop-Location
}

if (-not (Test-Path -Path $exePath -PathType Leaf)) {
	Write-Error "Expected executable was not built: $exePath"
	exit 1
}

Write-Host "Generating signed and encrypted bootstrap bundle..."
& $exePath encrypt $PublicKeyPath $PlaintextJSONPath $SignerPrivateKeyPath $OutputPath
if ($LASTEXITCODE -ne 0) {
	Write-Error "Bundle generation failed."
	exit 1
}

if (-not (Test-Path -Path $OutputPath -PathType Leaf)) {
	Write-Error "Expected output bundle was not created: $OutputPath"
	exit 1
}

Write-Host "`nBootstrap bundle generated at $OutputPath"
