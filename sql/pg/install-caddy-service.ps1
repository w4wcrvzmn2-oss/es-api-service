# Caddy: служба Windows + HTTPS
# На сервере: PowerShell от Администратора
#   Set-ExecutionPolicy Bypass -Scope Process -Force
#   D:\es_api_service\install-caddy-service.ps1

$ErrorActionPreference = "Stop"
$Root = "D:\es_api_service"
$CaddyExe = Join-Path $Root "caddy\caddy.exe"
$Caddyfile = Join-Path $Root "Caddyfile"

if (-not (Test-Path $CaddyExe)) { throw "Не найден $CaddyExe" }
if (-not (Test-Path $Caddyfile)) { throw "Не найден $Caddyfile — скопируй обновлённый Caddyfile из sql\pg\" }

Write-Host "== Firewall =="
foreach ($port in 80, 443) {
    $name = "Caddy TCP $port"
    if (-not (Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName $name -Direction Inbound -Protocol TCP -LocalPort $port -Action Allow | Out-Null
        Write-Host "открыт порт $port"
    } else {
        Write-Host "порт $port уже открыт"
    }
}

Write-Host "== Stop manual caddy =="
Get-Process -Name caddy -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host "== Recreate service =="
$svc = Get-Service -Name caddy -ErrorAction SilentlyContinue
if ($svc) {
    if ($svc.Status -eq "Running") { Stop-Service caddy -Force -ErrorAction SilentlyContinue }
    sc.exe delete caddy | Out-Null
    Start-Sleep -Seconds 2
}

# BinaryPathName: exe + аргументы (как одна строка для SCM)
$binPath = "`"$CaddyExe`" run --config `"$Caddyfile`" --adapter caddyfile"
New-Service -Name "caddy" `
    -BinaryPathName $binPath `
    -DisplayName "Caddy Reverse Proxy" `
    -Description "PharmData: 24pharmdata.ru → 8080, phd → 8081, HTTPS" `
    -StartupType Automatic | Out-Null

sc.exe failure caddy reset= 60 actions= restart/5000/restart/5000/restart/5000 | Out-Null

Write-Host "== Start =="
Start-Service caddy
Start-Sleep -Seconds 3
Get-Service caddy | Format-List Name, Status, StartType

Write-Host ""
Write-Host "Проверь в браузере (через 10-60 сек на сертификат):"
Write-Host "  https://24pharmdata.ru/"
Write-Host "  https://phd.24pharmdata.ru/"
Write-Host ""
Write-Host "Если служба падает — в консоли:"
Write-Host "  & `"$CaddyExe`" run --config `"$Caddyfile`""
Write-Host "там будет текст ошибки (часто порт 80/443 занят или DNS/проброс 443)."
