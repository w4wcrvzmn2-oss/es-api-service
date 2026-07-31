@echo off
set PGPASSWORD=24PharmData
echo === Applying 006_supplier_price_columns.sql ===
"C:\Program Files\PostgreSQL\17\bin\psql.exe" -h 127.0.0.1 -U es_api -d elfisa -f "%~dp0..\006_supplier_price_columns.sql"
if errorlevel 1 (
  echo FAILED
  exit /b 1
)
echo OK
pause
