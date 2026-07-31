#Requires -Version 5.1
<#
  Импорт es_ef2.txt (tab-separated, из MSSQL) в PostgreSQL elfisa.

  1) На MSSQL: sql\mssql\export_es_ef2.bat  ->  D:\es_api_service\data\es_ef2.txt
  2) На сервере PG:
       .\scripts\import-es-ef2-from-txt.ps1
       .\scripts\import-es-ef2-from-txt.ps1 -TxtPath "C:\Users\Exest\Downloads\es_ef2.txt"
#>
[CmdletBinding()]
param(
    [string]$TxtPath = (Join-Path "D:" "es_api_service\data\es_ef2.txt"),
    [switch]$SkipHeader,
    [string]$PgHost = "127.0.0.1",
    [int]$PgPort = 5432,
    [string]$PgUser = "es_api",
    [string]$PgPassword = "",
    [string]$PgDatabase = "elfisa",
    [string]$CfgPath = (Join-Path "D:" "es_api_service\es_api_service.cfg")
)

$ErrorActionPreference = "Stop"

function Find-Psql {
    $cmd = Get-Command psql.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, "C:\Program Files", "D:\Program Files")) {
        if (-not $root) { continue }
        foreach ($ver in @("17", "16", "15", "14", "13")) {
            $p = Join-Path $root "PostgreSQL\$ver\bin\psql.exe"
            if (Test-Path $p) { return $p }
        }
    }
    throw "psql.exe не найден. Укажите путь: `$env:Path += ';C:\Program Files\PostgreSQL\16\bin'"
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
    $srv = Get-YamlValue $cfg "db" "server"
    if ($srv) { $PgHost = $srv }
}
if (-not $PgPassword) { $PgPassword = "24PharmData" }

if (-not (Test-Path -LiteralPath $TxtPath)) {
    throw "Файл не найден: $TxtPath`nСначала экспортируйте es_ef2 из MSSQL (export_es_ef2.bat)."
}

$importFile = $TxtPath
if ($SkipHeader) {
    $importFile = Join-Path $env:TEMP ("es_ef2_nohdr_{0}.txt" -f (Get-Date -Format "yyyyMMddHHmmss"))
    Get-Content -LiteralPath $TxtPath -Encoding UTF8 | Select-Object -Skip 1 | Set-Content -LiteralPath $importFile -Encoding UTF8
    Write-Host "Пропущена первая строка (заголовок SSMS)"
}

$sizeMb = [math]::Round((Get-Item -LiteralPath $importFile).Length / 1MB, 2)
Write-Host "Файл: $importFile ($sizeMb MB)"

$psql = Find-Psql
Write-Host "psql: $psql"
$env:PGPASSWORD = $PgPassword

# psql на Windows понимает пути с /
$pgPath = ($importFile -replace '\\', '/')

$tmpSql = Join-Path $env:TEMP ("import_es_ef2_{0}.sql" -f (Get-Date -Format "yyyyMMddHHmmss"))
@(
    "BEGIN;"
    ""
    "DROP TABLE IF EXISTS _es_ef2_import;"
    "CREATE TEMP TABLE _es_ef2_import ("
    "    guid_es text, name text, barcode text, cureform_cod text, cureform_name text,"
    "    inn_name_rus text, inn_name_lat text, producer_cod text, trn_name_rus text, trn_name_lat text,"
    "    upak_cod text, data_an text, data_reg text, dosage text, kod_es text, nds_rate text,"
    "    id_es text, discribe text, rating text, updated text"
    ");"
    ""
    "\copy _es_ef2_import FROM '$pgPath' WITH (FORMAT text, DELIMITER E'\t', NULL '', ENCODING 'UTF8')"
    ""
    "TRUNCATE TABLE `"ES_EF2`";"
    ""
    'INSERT INTO "ES_EF2" ('
    '    "GUID_ES", "NAME", "BARCODE", "CUREFORM_COD", "CUREFORM_NAME",'
    '    "INN_NAME_RUS", "INN_NAME_LAT", "PRODUCER_COD", "TRN_NAME_RUS", "TRN_NAME_LAT",'
    '    "UPAK_COD", "DATA_AN", "DATA_REG", "DOSAGE",'
    '    "KOD_ES", "NDS_RATE", "ID_ES", "DISCRIBE", "RATING", "UPDATED",'
    '    "TS", "created_at", "updated_at", "is_active"'
    ')'
    "SELECT"
    "    CAST(NULLIF(TRIM(guid_es), '') AS uuid),"
    "    COALESCE(NULLIF(TRIM(name), ''), '(без названия)'),"
    "    NULLIF(TRIM(barcode), ''),"
    "    NULLIF(TRIM(cureform_cod), ''),"
    "    NULLIF(TRIM(cureform_name), ''),"
    "    NULLIF(TRIM(inn_name_rus), ''),"
    "    NULLIF(TRIM(inn_name_lat), ''),"
    "    NULLIF(TRIM(producer_cod), '')::bigint,"
    "    NULLIF(TRIM(trn_name_rus), ''),"
    "    NULLIF(TRIM(trn_name_lat), ''),"
    "    NULLIF(TRIM(upak_cod), '')::bigint,"
    "    NULLIF(TRIM(data_an), '')::timestamptz,"
    "    NULLIF(TRIM(data_reg), '')::timestamptz,"
    "    NULLIF(TRIM(dosage), ''),"
    "    COALESCE(NULLIF(TRIM(kod_es), '')::bigint, 0),"
    "    COALESCE(NULLIF(TRIM(nds_rate), '')::numeric, 0),"
    "    COALESCE(NULLIF(TRIM(id_es), '')::bigint, 0),"
    "    NULLIF(TRIM(discribe), ''),"
    "    COALESCE(NULLIF(TRIM(rating), '')::integer, 0),"
    "    COALESCE(NULLIF(TRIM(updated), '')::timestamptz, (NOW() AT TIME ZONE 'utc')),"
    "    '\x00'::bytea,"
    "    (NOW() AT TIME ZONE 'utc'),"
    "    (NOW() AT TIME ZONE 'utc'),"
    "    TRUE"
    "FROM _es_ef2_import"
    "WHERE NULLIF(TRIM(guid_es), '') IS NOT NULL;"
    ""
    'DO $$ BEGIN'
    '  IF to_regclass(''public."ES_EF2"'') IS NOT NULL'
    '     AND to_regclass(''public."es_ef2"'') IS NULL THEN'
    '    EXECUTE ''CREATE OR REPLACE VIEW "es_ef2" AS SELECT * FROM "ES_EF2"'';'
    '  END IF;'
    'END $$;'
    ""
    'SELECT COUNT(*)::bigint AS es_ef2_rows FROM "ES_EF2";'
    ""
    "COMMIT;"
) | Set-Content -LiteralPath $tmpSql -Encoding UTF8

Write-Host "Импорт (может занять несколько минут)..."
& $psql -h $PgHost -p $PgPort -U $PgUser -d $PgDatabase -v ON_ERROR_STOP=1 -f $tmpSql
if ($LASTEXITCODE -ne 0) {
    Remove-Item -LiteralPath $tmpSql -Force -ErrorAction SilentlyContinue
    throw "Импорт завершился с ошибкой (код $LASTEXITCODE)"
}

Remove-Item -LiteralPath $tmpSql -Force -ErrorAction SilentlyContinue
if ($SkipHeader -and $importFile -ne $TxtPath) {
    Remove-Item -LiteralPath $importFile -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "Готово. Нажмите «Обновить» у прайса Katren для сопоставления." -ForegroundColor Green
