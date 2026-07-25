# Убрать EDB Apache с порта 8080 и закрепить es_api_service.
# PowerShell от Администратора.

$ErrorActionPreference = "Continue"

Write-Host "=== Stop httpd / PEMHTTPD ===" -ForegroundColor Cyan
Get-Process httpd -ErrorAction SilentlyContinue | Stop-Process -Force
$pem = Get-Service PEMHTTPD-x64 -ErrorAction SilentlyContinue
if ($pem) {
    Stop-Service PEMHTTPD-x64 -Force -ErrorAction SilentlyContinue
    Set-Service PEMHTTPD-x64 -StartupType Disabled
    Write-Host "[+] PEMHTTPD-x64: Stopped + Disabled"
}

# На всякий случай другие EDB HTTP
Get-Service | Where-Object {
    $_.Name -match "pem|httpd|apache|edb" -or
    $_.DisplayName -match "PEM|Apache|EDB HTTP"
} | ForEach-Object {
    if ($_.Name -eq "PEMHTTPD-x64") { return }
    Write-Host "[?] Найдена: $($_.Name) / $($_.DisplayName) Status=$($_.Status) Start=$($_.StartType)"
}

Write-Host "=== Restart ES API ===" -ForegroundColor Cyan
Restart-Service ESAPIService -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 3

Write-Host "=== Who owns 8080/8081 ===" -ForegroundColor Cyan
Get-NetTCPConnection -LocalPort 8080,8081 -State Listen -ErrorAction SilentlyContinue |
  ForEach-Object {
    $p = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue
    [PSCustomObject]@{ Port = $_.LocalPort; PID = $_.OwningProcess; Name = $p.ProcessName }
  } | Format-Table -AutoSize

Write-Host "=== Local title check ===" -ForegroundColor Cyan
curl.exe -s http://127.0.0.1:8080/ | Select-String -Pattern "<title>" | ForEach-Object { $_.Line.Trim() }
curl.exe -s http://127.0.0.1:8081/ | Select-String -Pattern "<title>" | ForEach-Object { $_.Line.Trim() }

Write-Host ""
Write-Host "На 8080 должен быть ТОЛЬКО es_api_service. Затем открой https://24pharmdata.ru/"
