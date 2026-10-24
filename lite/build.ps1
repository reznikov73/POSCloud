# Build POSCloud Lite for Windows 7 (x64 and x86) with Go 1.20 toolchain.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$env:GOTOOLCHAIN = 'go1.20.14'
New-Item -ItemType Directory -Force -Path 'out' | Out-Null
$env:GOARCH = 'amd64'
go build -trimpath -ldflags "-s -w -H=windowsgui" -o out/POSCloud-Lite-x64.exe .
$env:GOARCH = '386'
go build -trimpath -ldflags "-s -w -H=windowsgui" -o out/POSCloud-Lite-x86.exe .
Remove-Item Env:GOTOOLCHAIN, Env:GOARCH
Write-Host 'done: lite/out/POSCloud-Lite-x64.exe and lite/out/POSCloud-Lite-x86.exe'
