@echo off
chcp 65001 >nul
setlocal

REM Экспорт es_ef2 из MSSQL в D:\es_api_service\data\es_ef2.txt
REM Запуск на машине с MSSQL (SSMS / sqlcmd / bcp).

set "OUT_DIR=D:\es_api_service\data"
set "OUT_FILE=%OUT_DIR%\es_ef2.txt"

REM === Настройте подключение ===
if "%MSSQL_SERVER%"=="" set "MSSQL_SERVER=localhost"
if "%MSSQL_DB%"=="" set "MSSQL_DB=elfisa"
if "%MSSQL_USER%"=="" set "MSSQL_USER=sa"
if not "%MSSQL_PASSWORD%"=="" goto :have_pass
echo.
echo Укажите пароль MSSQL:
echo   set MSSQL_PASSWORD=ваш_пароль
echo   export_es_ef2.bat
echo.
echo Или одной строкой:
echo   set MSSQL_PASSWORD=*** ^& export_es_ef2.bat
pause
exit /b 1

:have_pass
if not exist "%OUT_DIR%" mkdir "%OUT_DIR%"

set "QUERY=SELECT CAST(GUID_ES AS varchar(36)), REPLACE(REPLACE(REPLACE(ISNULL(NAME,''), CHAR(9),' '), CHAR(10),' '), CHAR(13),' '), REPLACE(REPLACE(ISNULL(BARCODE,''), CHAR(9),' '), CHAR(10),' '), ISNULL(CUREFORM_COD,''), REPLACE(REPLACE(ISNULL(CUREFORM_NAME,''), CHAR(9),' '), CHAR(10),' '), REPLACE(REPLACE(ISNULL(INN_NAME_RUS,''), CHAR(9),' '), CHAR(10),' '), REPLACE(REPLACE(ISNULL(INN_NAME_LAT,''), CHAR(9),' '), CHAR(10),' '), ISNULL(CAST(PRODUCER_COD AS varchar(20)),''), REPLACE(REPLACE(ISNULL(TRN_NAME_RUS,''), CHAR(9),' '), CHAR(10),' '), REPLACE(REPLACE(ISNULL(TRN_NAME_LAT,''), CHAR(9),' '), CHAR(10),' '), ISNULL(CAST(UPAK_COD AS varchar(20)),''), CONVERT(varchar(30), DATA_AN, 126), CONVERT(varchar(30), DATA_REG, 126), REPLACE(REPLACE(ISNULL(DOSAGE,''), CHAR(9),' '), CHAR(10),' '), ISNULL(CAST(KOD_ES AS varchar(20)),'0'), ISNULL(CAST(NDS_RATE AS varchar(20)),'0'), ISNULL(CAST(ID_ES AS varchar(20)),'0'), REPLACE(REPLACE(ISNULL(DISCRIBE,''), CHAR(9),' '), CHAR(10),' '), ISNULL(CAST(RATING AS varchar(10)),'0'), CONVERT(varchar(30), ISNULL(UPDATED, GETDATE()), 126) FROM %MSSQL_DB%.dbo.es_ef2 WHERE DELETED IS NULL"

echo Экспорт es_ef2 -> %OUT_FILE%
echo Сервер: %MSSQL_SERVER%  База: %MSSQL_DB%

bcp "%QUERY%" queryout "%OUT_FILE%" -S %MSSQL_SERVER% -d %MSSQL_DB% -U %MSSQL_USER% -P %MSSQL_PASSWORD% -c -t 0x09 -C 65001 -q
if errorlevel 1 (
  echo.
  echo bcp не сработал. Альтернатива: откройте export_es_ef2.sql в SSMS,
  echo Query - Results To - Results to File, выполните SELECT.
  pause
  exit /b 1
)

for %%A in ("%OUT_FILE%") do echo OK: %%~zA bytes
echo.
echo Скопируйте файл на сервер PostgreSQL:
echo   %OUT_FILE%
echo Затем запустите import-es-ef2-from-txt.ps1
pause
