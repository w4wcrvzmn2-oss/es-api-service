# Полный бэкап MSSQL (все пользовательские БД или одна).
# Требуется: sqlcmd (идёт с SSMS / SQL Server Command Line Utilities).
#
# Примеры:
#   .\backup_full_mssql.ps1
#   .\backup_full_mssql.ps1 -ServerInstance "localhost\SQLEXPRESS" -Database "PharmData"
#   .\backup_full_mssql.ps1 -ServerInstance "193.188.23.166" -BackupRoot "D:\SQLBackups" -User "sa" -Password "****"

param(
    [string]$ServerInstance = "localhost",
    [string]$BackupRoot = "C:\SQLBackups",
    [string]$Database = "",          # пусто = все пользовательские БД
    [string]$User = "",              # пусто = Windows-аутентификация
    [string]$Password = "",
    [switch]$Verify
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command sqlcmd -ErrorAction SilentlyContinue)) {
    throw "sqlcmd не найден. Установите SQL Server Command Line Utilities или SSMS."
}

New-Item -ItemType Directory -Force -Path $BackupRoot | Out-Null

$stamp = Get-Date -Format "yyyyMMdd_HHmmss"
$authArgs = @()
if ([string]::IsNullOrWhiteSpace($User)) {
    $authArgs = @("-E")
    Write-Host "Аутентификация: Windows (текущий пользователь)"
} else {
    $authArgs = @("-U", $User, "-P", $Password)
    Write-Host "Аутентификация: SQL ($User)"
}

function Invoke-Sql([string]$Query) {
    $out = & sqlcmd -S $ServerInstance @authArgs -h -1 -W -Q $Query 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "sqlcmd failed: $out"
    }
    return ($out | ForEach-Object { "$_".Trim() } | Where-Object { $_ -and $_ -notmatch "^-+$" })
}

# Список БД
if ([string]::IsNullOrWhiteSpace($Database)) {
    $listQuery = @"
SET NOCOUNT ON;
SELECT name FROM sys.databases
WHERE state_desc = N'ONLINE'
  AND name NOT IN (N'master', N'model', N'msdb', N'tempdb')
ORDER BY name;
"@
    $databases = @(Invoke-Sql $listQuery)
} else {
    $databases = @($Database)
}

if ($databases.Count -eq 0) {
    throw "Нет баз для бэкапа."
}

Write-Host "Инстанс: $ServerInstance"
Write-Host "Папка:   $BackupRoot"
Write-Host "БД:      $($databases -join ', ')"
Write-Host ""

$ok = 0
$fail = 0

foreach ($db in $databases) {
    $safeName = ($db -replace '[\\/:*?"<>|]', '_')
    $bak = Join-Path $BackupRoot "${safeName}_FULL_${stamp}.bak"
    $bakSql = $bak.Replace("'", "''")
    $dbSql = $db.Replace("'", "''")

    $backupSql = @"
SET NOCOUNT ON;
BACKUP DATABASE [$db]
TO DISK = N'$bakSql'
WITH COMPRESSION, CHECKSUM, INIT, STATS = 10,
     NAME = N'$dbSql-Full Database Backup';
"@

    Write-Host "=== FULL: $db ==="
    Write-Host "→ $bak"
    try {
        & sqlcmd -S $ServerInstance @authArgs -Q $backupSql
        if ($LASTEXITCODE -ne 0) { throw "BACKUP exit code $LASTEXITCODE" }

        if ($Verify) {
            $verifySql = "RESTORE VERIFYONLY FROM DISK = N'$bakSql' WITH CHECKSUM;"
            & sqlcmd -S $ServerInstance @authArgs -Q $verifySql
            if ($LASTEXITCODE -ne 0) { throw "VERIFY failed" }
            Write-Host "VERIFY OK"
        }

        $sizeMb = [math]::Round((Get-Item $bak).Length / 1MB, 1)
        Write-Host "OK ($sizeMb MB)"
        $ok++
    }
    catch {
        Write-Host "ERROR: $_" -ForegroundColor Red
        $fail++
    }
    Write-Host ""
}

Write-Host "Готово. Успешно: $ok, ошибок: $fail"
if ($fail -gt 0) { exit 1 }
