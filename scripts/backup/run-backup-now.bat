@echo off
REM Manual backup test (no scheduler)
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0backup-daily.ps1" -KeepDays 14 -BackupRoot "F:\Backups"
echo ExitCode=%ERRORLEVEL%
pause
