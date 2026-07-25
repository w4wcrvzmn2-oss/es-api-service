# Restore MSSQL .bak on a temporary SQL Server (Express/Developer), then migrate to PG with pgloader.
# .bak CANNOT be loaded directly into PostgreSQL.
#
# Prerequisites:
# - SQL Server Express/Developer installed locally OR use live VPS DB instead of .bak
# - sqlcmd in PATH
# - File access: SQL Server service account must read the .bak path

param(
    [Parameter(Mandatory = $true)]
    [string]$BakPath,

    [string]$ServerInstance = "localhost",
    [string]$Database = "elfisa",
    [string]$DataDir = "C:\SQLData",
    [string]$User = "",
    [string]$Password = ""
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $BakPath)) {
    throw "Файл не найден: $BakPath"
}

New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

$mdf = Join-Path $DataDir "$Database.mdf"
$ldf = Join-Path $DataDir "${Database}_log.ldf"

$auth = @("-E")
if ($User) {
    $auth = @("-U", $User, "-P", $Password)
}

Write-Host "1) Смотрим логические имена файлов внутри .bak ..."
& sqlcmd -S $ServerInstance @auth -Q "RESTORE FILELISTONLY FROM DISK = N'$BakPath'" -W

Write-Host ""
Write-Host "Если Logical Name для data/log другие — поправьте MOVE ниже вручную."
Write-Host "2) RESTORE DATABASE $Database ..."

$restore = @"
IF DB_ID(N'$Database') IS NOT NULL
BEGIN
  ALTER DATABASE [$Database] SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
  DROP DATABASE [$Database];
END
RESTORE DATABASE [$Database]
FROM DISK = N'$BakPath'
WITH MOVE N'elfisa' TO N'$mdf',
     MOVE N'elfisa_log' TO N'$ldf',
     REPLACE,
     STATS = 10;
"@

# Если FILELISTONLY показал другие logical names — замените elfisa / elfisa_log.
& sqlcmd -S $ServerInstance @auth -Q $restore
if ($LASTEXITCODE -ne 0) { throw "RESTORE failed" }

Write-Host "OK: база $Database восстановлена из .bak"
Write-Host "Дальше: pgloader sql/pg/pgloader_elfisa.load  (FROM mssql://...@localhost/$Database)"
