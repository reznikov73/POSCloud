# Сборка POSCloud Lite под Windows 7 (x64 и x86) инструментарием Go 1.20.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$env:GOTOOLCHAIN = 'go1.20.14'
New-Item -ItemType Directory -Force -Path 'out' | Out-Null
$env:GOARCH = 'amd64'
go build -trimpath -ldflags "-s -w -H=windowsgui" -o out/POSCloud-Lite-x64.exe .
$env:GOARCH = '386'
go build -trimpath -ldflags "-s -w -H=windowsgui" -o out/POSCloud-Lite-x86.exe .
Remove-Item Env:GOTOOLCHAIN, Env:GOARCH
Write-Host 'готово: lite/out/POSCloud-Lite-x64.exe и lite/out/POSCloud-Lite-x86.exe'
