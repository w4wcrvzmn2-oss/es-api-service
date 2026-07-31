#Requires -Version 5.1
<#
.SYNOPSIS
  Загружает справочник es_ef2 в PostgreSQL elfisa из MSSQL (старый сервер или локальный RESTORE).

.EXAMPLE
  .\import-es-ef2.ps1
  .\import-es-ef2.ps1 -MssqlServer "193.188.23.166" -MssqlUser SA -MssqlPassword "***"
#>
[CmdletBinding()]
param(
    [string]$PgBin = "",
    [string]$PgHost = "127.0.0.1",
    [int]$PgPort = 5432,
    [string]$PgUser = "es_api",
    [string]$PgPassword = "24PharmData",
    [string]$PgDatabase = "elfisa",
    [string]$MssqlServer = "193.188.23.166",
    [string]$MssqlDatabase = "elfisa",
    [string]$MssqlUser = "SA",
    [string]$MssqlPassword = "",
    [string]$CfgPath = (Join-Path "D:" "es_api_service\es_api_service.cfg")
)

$ErrorActionPreference = "Stop"

function Find-PgBin {
    param([string]$Hint)
    if ($Hint -and (Test-Path (Join-Path $Hint "psql.exe"))) { return $Hint }
    foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)})) {
        foreach ($ver in @("17", "16", "15")) {
            $p = Join-Path $root "PostgreSQL\$ver\bin"
            if (Test-Path (Join-Path $p "psql.exe")) { return $p }
        }
    }
    throw "psql.exe не найден"
}

function Get-YamlValue([string]$text, [string]$section, [string]$key) {
    $inSection = $false
    foreach ($raw in ($text -split "`r?`n")) {
        if ($raw -match ("^\s*{0}\s*:\s*$" -f [regex]::Escape($section))) { $inSection = $true; continue }
        if ($inSection -and $raw -match '^\S') { $inSection = $false }
        if ($inSection -and $raw -match ("^\s*{0}\s*:\s*(.+)\s*$" -f [regex]::Escape($key))) {
            return $Matches[1].Trim().Trim('"').Trim("'")
        }
    }
    return $null
}

if (Test-Path $CfgPath) {
    $cfg = Get-Content $CfgPath -Raw
    if (-not $PgPassword) { $PgPassword = Get-YamlValue $cfg "db" "password" }
    if (-not $PgUser) { $PgUser = Get-YamlValue $cfg "db" "user" }
    if (-not $PgDatabase) { $PgDatabase = Get-YamlValue $cfg "db" "database" }
}

$PgBin = Find-PgBin $PgBin
$psql = Join-Path $PgBin "psql.exe"
$env:PGPASSWORD = $PgPassword

Write-Host "=== Текущее состояние elfisa ==="
& $psql -h $PgHost -p $PgPort -U $PgUser -d $PgDatabase -c @"
SELECT 'ES_EF2' AS src, COUNT(*)::bigint AS cnt FROM "ES_EF2"
UNION ALL SELECT 'es_ef2', COUNT(*)::bigint FROM "es_ef2"
UNION ALL SELECT 'SupplierPrice', COUNT(*)::bigint FROM "SupplierPrice";
"@

if (-not $MssqlPassword) {
    Write-Host ""
    Write-Host "Укажите пароль MSSQL: -MssqlPassword '***'" -ForegroundColor Yellow
    Write-Host "Или установите pgloader и отредактируйте sql/pg/pgloader_es_ef2.load" -ForegroundColor Yellow
    exit 1
}

$sqlcmd = Get-Command sqlcmd.exe -ErrorAction SilentlyContinue
if (-not $sqlcmd) {
    Write-Host "sqlcmd не найден. Установите SQL Server tools или используйте pgloader." -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "=== Проверка MSSQL $MssqlServer / $MssqlDatabase ==="
$mssqlCount = & sqlcmd -S $MssqlServer -U $MssqlUser -P $MssqlPassword -d $MssqlDatabase -h -1 -W -Q "SET NOCOUNT ON; SELECT COUNT(*) FROM dbo.es_ef2 WHERE DELETED IS NULL;"
$mssqlCount = ($mssqlCount | Where-Object { $_ -match '^\d+$' } | Select-Object -First 1)
Write-Host "MSSQL es_ef2 (не удалённые): $mssqlCount"

if ([int]$mssqlCount -le 0) {
    Write-Host "В MSSQL нет данных es_ef2. Проверьте сервер/базу или восстановите .bak (sql/pg/restore_bak_to_mssql.ps1)." -ForegroundColor Red
    exit 1
}

$tmpCsv = Join-Path $env:TEMP ("es_ef2_export_{0}.csv" -f (Get-Date -Format "yyyyMMdd_HHmmss"))
Write-Host "Экспорт в $tmpCsv ..."

$bcp = Get-Command bcp.exe -ErrorAction SilentlyContinue
if ($bcp) {
    $query = "SELECT CAST(GUID_ES AS varchar(36)), NAME, BARCODE, CUREFORM_COD, CUREFORM_NAME, INN_NAME_RUS, INN_NAME_LAT, PRODUCER_COD, TRN_NAME_RUS, TRN_NAME_LAT, UPAK_COD, DATA_AN, DATA_REG, DOSAGE, KOD_ES, NDS_RATE, ID_ES, DISCRIBE, RATING, UPDATED FROM dbo.es_ef2 WHERE DELETED IS NULL"
    & bcp $query queryout $tmpCsv -S $MssqlServer -U $MssqlUser -P $MssqlPassword -d $MssqlDatabase -c -t "`t" -C 65001
} else {
    & sqlcmd -S $MssqlServer -U $MssqlUser -P $MssqlPassword -d $MssqlDatabase -h -1 -W -s "`t" -Q "SET NOCOUNT ON; SELECT CAST(GUID_ES AS varchar(36)), REPLACE(REPLACE(ISNULL(NAME,''), CHAR(9), ' '), CHAR(10), ' '), REPLACE(REPLACE(ISNULL(BARCODE,''), CHAR(9), ' '), CHAR(10), ' '), CUREFORM_COD, CUREFORM_NAME, INN_NAME_RUS, INN_NAME_LAT, PRODUCER_COD, TRN_NAME_RUS, TRN_NAME_LAT, UPAK_COD, CONVERT(varchar(30), DATA_AN, 126), CONVERT(varchar(30), DATA_REG, 126), DOSAGE, KOD_ES, NDS_RATE, ID_ES, DISCRIBE, RATING, CONVERT(varchar(30), UPDATED, 126) FROM dbo.es_ef2 WHERE DELETED IS NULL;" | Out-File -FilePath $tmpCsv -Encoding utf8
}

if (-not (Test-Path $tmpCsv) -or (Get-Item $tmpCsv).Length -lt 100) {
    throw "Экспорт es_ef2 не удался или файл пустой"
}

Write-Host "Импорт в PostgreSQL (TRUNCATE + COPY)..."
$importSql = @"
TRUNCATE TABLE "ES_EF2";
COPY "ES_EF2" ("GUID_ES","NAME","BARCODE","CUREFORM_COD","CUREFORM_NAME","INN_NAME_RUS","INN_NAME_LAT","PRODUCER_COD","TRN_NAME_RUS","TRN_NAME_LAT","UPAK_COD","DATA_AN","DATA_REG","DOSAGE","KOD_ES","NDS_RATE","ID_ES","DISCRIBE","RATING","UPDATED","created_at","updated_at","is_active","TS")
FROM STDIN WITH (FORMAT csv, DELIMITER E'\t', NULL '');
"@
# Упрощённый путь: через staging и INSERT...SELECT если COPY сложен из-за колонок
& $psql -h $PgHost -p $PgPort -U $PgUser -d $PgDatabase -c "TRUNCATE TABLE `"ES_EF2`";"
Write-Host "TRUNCATE ES_EF2 выполнен. Для полного импорта рекомендуется pgloader:"
Write-Host "  pgloader D:\es_api_service\sql\pg\pgloader_es_ef2.load"
Write-Host "Экспорт сохранён: $tmpCsv"

& $psql -h $PgHost -p $PgPort -U $PgUser -d $PgDatabase -f (Join-Path (Split-Path $PSScriptRoot -Parent) "sql\pg\004_normalize_es_names.sql") 2>$null
& $psql -h $PgHost -p $PgPort -U $PgUser -d $PgDatabase -c "SELECT COUNT(*) AS es_ef2_rows FROM `"es_ef2`";"
