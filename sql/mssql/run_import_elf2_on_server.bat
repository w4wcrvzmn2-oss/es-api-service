@echo off
chcp 65001 >nul
cd /d D:\es_api_service
echo Импорт es_ef2 из elef2.txt ...
import_elf2.exe -file "D:\es_api_service\data\elef2.txt" -cfg "D:\es_api_service\es_api_service.cfg"
if errorlevel 1 pause
echo.
echo После импорта: restart-service.bat и Обновить прайс Katren
pause
