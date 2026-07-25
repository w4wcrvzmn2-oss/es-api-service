# Автозапуск всего стека PharmData после перезагрузки.
# Запуск: PowerShell от Администратора
#   Set-ExecutionPolicy Bypass -Scope Process -Force
#   D:\es_api_service\ensure-autostart.ps1

$ErrorActionPreference = "Stop"

$services = @(
    @{ Name = "postgresql-x64-17"; Display = "PostgreSQL 17" },
    @{ Name = "ESAPIService";      Display = "ES API Service" },
    @{ Name = "caddy";             Display = "Caddy Reverse Proxy" }
)

Write-Host "=== Автозапуск служб ===" -ForegroundColor Cyan

foreach ($s in $services) {
    $svc = Get-Service -Name $s.Name -ErrorAction SilentlyContinue
    if (-not $svc) {
        Write-Host "[!] $($s.Name) не найдена — пропуск ($($s.Display))" -ForegroundColor Yellow
        continue
    }

    Set-Service -Name $s.Name -StartupType Automatic
    # перезапуск при сбое: через 5с, 5с, 5с
    sc.exe failure $s.Name reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null
    sc.exe failureflag $s.Name 1 | Out-Null

    if ($svc.Status -ne "Running") {
        Start-Service -Name $s.Name
        Write-Host "[+] $($s.Name): Automatic + запущена" -ForegroundColor Green
    } else {
        Write-Host "[+] $($s.Name): Automatic (уже Running)" -ForegroundColor Green
    }
}

# Caddy лучше стартует после API (мягкая зависимость через delayed start не обязательна)
# Delayed auto для caddy — дать Postgres/API подняться
sc.exe config caddy start= delayed-auto | Out-Null
Write-Host "[+] caddy: Delayed Automatic (чуть позже после boot)" -ForegroundColor Green

Write-Host ""
Write-Host "=== Текущий статус ===" -ForegroundColor Cyan
Get-Service postgresql-x64-17, ESAPIService, caddy -ErrorAction SilentlyContinue |
    Format-Table Name, Status, StartType -AutoSize

Write-Host "Готово. После перезагрузки сервера эти службы поднимутся сами."
Write-Host "Проверка: https://24pharmdata.ru/  и  https://phd.24pharmdata.ru/"
