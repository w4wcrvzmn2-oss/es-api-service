# Force-start Gitea + refresh Caddyfile. Run as Administrator.

$ErrorActionPreference = "Stop"
$InstallRoot = "D:\gitea"
$exe = Join-Path $InstallRoot "gitea.exe"
$caddyfile = "D:\Caddy\Caddyfile"
$shareCaddy = "D:\es_api_service\Caddyfile"
$MinBytes = 50MB

if (-not (Test-Path $exe) -or ((Get-Item $exe).Length -lt $MinBytes)) {
    throw "Gitea binary missing/corrupt: $exe - run install-gitea.ps1 first"
}

if (Test-Path $shareCaddy) {
    Copy-Item $shareCaddy $caddyfile -Force
    Write-Host "Updated $caddyfile"
}

Get-Process gitea -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

$task = Get-ScheduledTask -TaskName Gitea -ErrorAction SilentlyContinue
if ($task) {
    Start-ScheduledTask -TaskName Gitea
    Write-Host "Started scheduled task Gitea"
} elseif (Get-Service Gitea -ErrorAction SilentlyContinue) {
    Start-Service Gitea
    Write-Host "Started service Gitea"
} else {
    Start-Process -FilePath $exe -ArgumentList "web" -WorkingDirectory $InstallRoot -WindowStyle Hidden
    Write-Host "Started gitea.exe web"
}

$ok = $false
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Seconds 1
    try {
        $r = Invoke-WebRequest -Uri "http://127.0.0.1:3000/" -UseBasicParsing -TimeoutSec 2
        Write-Host "Port 3000 OK: HTTP $($r.StatusCode)"
        $ok = $true
        break
    } catch {
        Write-Host "waiting for :3000 ..."
    }
}

if (-not $ok) {
    Write-Host "Still down. Check D:\gitea\log and run: D:\gitea\gitea.exe web"
    exit 1
}

Restart-Service caddy -ErrorAction SilentlyContinue
Write-Host "Done. Open https://git.24pharmdata.ru"
