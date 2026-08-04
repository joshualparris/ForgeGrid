$ErrorActionPreference = "Stop"

$cryptoDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$goUtil = Join-Path $cryptoDir "bootstrap-crypto"

Write-Host "Compiling Go crypto utility..."
Push-Location $goUtil
go build -o bootstrap-crypto.exe main.go
Pop-Location

$privTmp = [System.IO.Path]::GetTempFileName()
$pubPath = Join-Path $PWD "bootstrap-public.pem"

Write-Host "Generating 3072-bit RSA key pair..."
$output = & "$goUtil\bootstrap-crypto.exe" generate $privTmp $pubPath

if ($LASTEXITCODE -ne 0) {
    Remove-Item $privTmp -Force
    Write-Error "Key generation failed"
    exit 1
}

$fingerprint = ($output -join "").Trim()

Write-Host "Protecting private key with DPAPI (CurrentUser)..."
$privateKeyBytes = [System.IO.File]::ReadAllBytes($privTmp)
Remove-Item $privTmp -Force

Add-Type -AssemblyName System.Security
$protectedBytes = [System.Security.Cryptography.ProtectedData]::Protect($privateKeyBytes, $null, [System.Security.Cryptography.DataProtectionScope]::CurrentUser)
[System.Array]::Clear($privateKeyBytes, 0, $privateKeyBytes.Length)

$dir = "$env:LOCALAPPDATA\ForgeGrid\bootstrap"
if (!(Test-Path $dir)) { 
    $null = New-Item -ItemType Directory -Path $dir -Force 
}

Write-Host "Securing directory ACLs..."
$acl = Get-Acl $dir
$acl.SetAccessRuleProtection($true, $false)
$rule = New-Object System.Security.AccessControl.FileSystemAccessRule([System.Security.Principal.WindowsIdentity]::GetCurrent().Name, "FullControl", "ContainerInherit,ObjectInherit", "None", "Allow")
$acl.AddAccessRule($rule)
Set-Acl $dir $acl

$blobPath = "$dir\private.blob"
[System.IO.File]::WriteAllBytes($blobPath, $protectedBytes)

Write-Host "`nBootstrap material generated."
Write-Host "Public Key File : $pubPath"
Write-Host "Fingerprint     : $fingerprint"
Write-Host "`nSend bootstrap-public.pem to the Fedora coordinator."
