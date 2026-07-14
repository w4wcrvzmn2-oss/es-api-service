# Скрипт запуска веб-клиента ClienElf2 (ЭльФиСА)

Write-Host "Запуск веб-клиента ClienElf2..." -ForegroundColor Cyan

# Проверяем существование login.html
if (-not (Test-Path "login.html")) {
    Write-Host "Ошибка: файл login.html не найден!" -ForegroundColor Red
    Write-Host "Убедитесь, что вы запускаете скрипт из директории ClienElf2" -ForegroundColor Yellow
    exit 1
}

# Получаем полный путь к файлу
$loginPath = Resolve-Path "login.html"

Write-Host "Открываем: $loginPath" -ForegroundColor Green
Write-Host ""
Write-Host "ВАЖНО: Убедитесь, что API сервер запущен на порту 8082!" -ForegroundColor Yellow
Write-Host "   API должен быть доступен по адресу: http://localhost:8082" -ForegroundColor Yellow
Write-Host ""

# Открываем в браузере по умолчанию
Start-Process $loginPath

Write-Host "Веб-клиент открыт в браузере" -ForegroundColor Green
Write-Host ""
Write-Host "Для входа используйте:" -ForegroundColor Cyan
Write-Host "   Username: admin" -ForegroundColor White
Write-Host "   Password: admin123" -ForegroundColor White
Write-Host "   API URL: http://localhost:8082" -ForegroundColor White
