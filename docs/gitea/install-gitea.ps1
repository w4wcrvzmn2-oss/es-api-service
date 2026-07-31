# Install / repair Gitea under D:\gitea
# Run as Administrator on the application server.

param(
    [string]$InstallRoot = "D:\gitea",
    [string]$Version = "1.22.6",
    [string]$SetupSource = $PSScriptRoot
)

$ErrorActionPreference = "Stop"
$MinBytes = 50MB

New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot "data") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot "repositories") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot "log") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot "custom\conf") | Out-Null

$exeName = "gitea-$Version-windows-4.0-amd64.exe"
$downloadUrl = "https://dl.gitea.com/gitea/$Version/$exeName"
$exePath = Join-Path $InstallRoot "gitea.exe"
$bundled = Join-Path $SetupSource "gitea.exe"
$tmp = Join-Path $env:TEMP $exeName

function Test-GiteaBinary([string]$Path) {
    if (-not (Test-Path $Path)) { return $false }
    $len = (Get-Item $Path).Length
    if ($len -lt $MinBytes) {
        Write-Host "Rejecting tiny/corrupt file: $Path ($len bytes)"
        return $false
    }
    return $true
}

# Prefer pre-copied binary from share (gitea_setup\gitea.exe)
if (Test-GiteaBinary $bundled) {
    Write-Host "Copying bundled binary from $bundled ..."
    Copy-Item $bundled $exePath -Force
} elseif (-not (Test-GiteaBinary $exePath)) {
    Write-Host "Downloading $downloadUrl ..."
    if (Test-Path $tmp) { Remove-Item $tmp -Force }
    # curl is more reliable than Invoke-WebRequest for large files on Server
    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if ($curl) {
        & curl.exe -L --fail --retry 3 -o $tmp $downloadUrl
        if ($LASTEXITCODE -ne 0) { throw "curl download failed: $LASTEXITCODE" }
    } else {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tmp -UseBasicParsing
    }
    if (-not (Test-GiteaBinary $tmp)) {
        throw "Downloaded file is too small / corrupt. Check firewall/proxy and retry."
    }
    Copy-Item $tmp $exePath -Force
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
}

if (-not (Test-GiteaBinary $exePath)) {
    throw "gitea.exe missing or corrupt at $exePath"
}

Write-Host ("gitea.exe OK: {0:N0} bytes" -f (Get-Item $exePath).Length)

$iniSource = Join-Path $SetupSource "app.ini"
$iniTarget = Join-Path $InstallRoot "custom\conf\app.ini"
if (-not (Test-Path $iniSource)) {
    throw "Missing app.ini next to installer: $iniSource"
}
Copy-Item $iniSource $iniTarget -Force
Write-Host "Wrote $iniTarget"

# Ensure Caddyfile has git site
$caddySrc = Join-Path (Split-Path $SetupSource -Parent) "Caddyfile"
if (-not (Test-Path $caddySrc)) { $caddySrc = "D:\es_api_service\Caddyfile" }
$caddyDst = "D:\Caddy\Caddyfile"
if ((Test-Path $caddySrc) -and (Test-Path (Split-Path $caddyDst -Parent))) {
    Copy-Item $caddySrc $caddyDst -Force
    Write-Host "Updated $caddyDst"
    Restart-Service caddy -ErrorAction SilentlyContinue
}

# Register autostart
Get-Process gitea -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

$nssm = Get-Command nssm -ErrorAction SilentlyContinue
if ($nssm) {
    $svc = Get-Service Gitea -ErrorAction SilentlyContinue
    if (-not $svc) {
        & nssm install Gitea $exePath web
        & nssm set Gitea AppDirectory $InstallRoot
        & nssm set Gitea Start SERVICE_AUTO_START
    }
    & nssm start Gitea
    Write-Host "Started NSSM service Gitea"
} else {
    Write-Host "nssm not found; using Scheduled Task Gitea"
    $action = New-ScheduledTaskAction -Execute $exePath -Argument "web" -WorkingDirectory $InstallRoot
    $trigger = New-ScheduledTaskTrigger -AtStartup
    Register-ScheduledTask -TaskName "Gitea" -Action $action -Trigger $trigger -RunLevel Highest -Force | Out-Null
    Start-ScheduledTask -TaskName "Gitea"
}

Write-Host "Waiting for http://127.0.0.1:3000 ..."
$ok = $false
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Seconds 1
    try {
        $r = Invoke-WebRequest -Uri "http://127.0.0.1:3000/" -UseBasicParsing -TimeoutSec 2
        Write-Host "OK HTTP $($r.StatusCode)"
        $ok = $true
        break
    } catch {
        Write-Host ("  try {0}/30 ..." -f ($i + 1))
    }
}

if (-not $ok) {
    Write-Host "Port 3000 still down. Run manually:"
    Write-Host "  cd D:\gitea"
    Write-Host "  .\gitea.exe web"
    exit 1
}

Write-Host "Open https://git.24pharmdata.ru and create the admin account."
