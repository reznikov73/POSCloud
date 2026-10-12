# Сборка приложений POSCloud и установщика.
#
#   .\build.ps1                 собрать оба приложения и установщик
#   .\build.ps1 -SkipApps       только установщик (использовать уже собранные exe)
#   .\build.ps1 -StopRunning    перед сборкой закрыть запущенные POSCloud

param(
    [switch]$SkipApps,
    [switch]$StopRunning,
    [string]$Version,   # своя версия установщика; по умолчанию — +1 к version.txt
    [switch]$NoBump     # не увеличивать версию
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$serverDir = Join-Path $root 'poscloud-server'
$clientDir = Join-Path $root 'poscloud-client'

function Find-Iscc {
    $cmd = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $candidates = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe')
    )
    foreach ($c in $candidates) { if ($c -and (Test-Path $c)) { return $c } }
    throw 'Не найден ISCC.exe (компилятор Inno Setup 6). Установите Inno Setup: https://jrsoftware.org/isinfo.php'
}

function Stop-IfRunning([string]$name) {
    $procs = Get-Process -Name $name -ErrorAction SilentlyContinue
    if ($procs) {
        if (-not $StopRunning) {
            throw "$name запущен. Закройте программу (или запустите сборку с -StopRunning) и повторите."
        }
        $procs | Stop-Process -Force
        Start-Sleep -Seconds 1
    }
}

if (-not $SkipApps) {
    Stop-IfRunning 'poscloud-server'
    Stop-IfRunning 'poscloud-client'
    Write-Host 'Сборка poscloud-server...'
    Push-Location $serverDir
    try {
        wails build -s -skipbindings -webview2 browser
        if ($LASTEXITCODE -ne 0) { throw "wails build (server) завершился с кодом $LASTEXITCODE" }
    } finally { Pop-Location }
    Write-Host 'Сборка poscloud-client...'
    Push-Location $clientDir
    try {
        wails build -s -skipbindings -webview2 browser
        if ($LASTEXITCODE -ne 0) { throw "wails build (client) завершился с кодом $LASTEXITCODE" }
    } finally { Pop-Location }
}

$versionFile = Join-Path $PSScriptRoot 'version.txt'
if (-not $Version) {
    $cur = '1.0.0'
    if (Test-Path $versionFile) { $cur = (Get-Content $versionFile -Raw).Trim() }
    if ($NoBump) {
        $Version = $cur
    } else {
        $parts = $cur.Split('.')
        if ($parts.Count -lt 2) { throw "version.txt: ожидается версия вида x.y.z, сейчас '$cur'" }
        $parts[$parts.Count - 1] = [string]([int]$parts[$parts.Count - 1] + 1)
        $Version = ($parts -join '.')
    }
}
Set-Content -Path $versionFile -Value $Version -NoNewline -Encoding ASCII
Write-Host "Версия установщика: $Version"

$iscc = Find-Iscc
Write-Host 'Компиляция установщика...'
& $iscc (Join-Path $PSScriptRoot 'poscloud-setup.iss')
if ($LASTEXITCODE -ne 0) { throw "ISCC завершился с кодом $LASTEXITCODE" }
Get-ChildItem (Join-Path $PSScriptRoot 'out') | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
