#Requires -Version 5.1
<#
.SYNOPSIS
  Daily backup: PostgreSQL (elfisa + eplus_work) + project folder to F:\Backups.
  Old day folders are deleted; last KeepDays are kept.

.EXAMPLE
  .\backup-daily.ps1
  .\backup-daily.ps1 -KeepDays 14 -BackupRoot F:\Backups
#>
[CmdletBinding()]
param(
    [string]$BackupRoot = (Join-Path "F:" "Backups"),
    [int]$KeepDays = 14,
    [string]$ProjectDir = (Join-Path "D:" "es_api_service"),
    [string]$PgBin = "",
    [string]$CfgPath = (Join-Path "D:" "es_api_service\es_api_service.cfg"),
    [string]$PgPassword = "",
    [string]$PgUser = "",
    [string]$PgHost = "127.0.0.1",
    [int]$PgPort = 5432,
    [string[]]$Databases = @("elfisa", "eplus_work")
)

$ErrorActionPreference = "Stop"
$stamp = Get-Date -Format "yyyy-MM-dd"
$dayDir = Join-Path $BackupRoot $stamp
$logDir = Join-Path $BackupRoot "logs"
$logFile = Join-Path $logDir ("backup_{0}.log" -f (Get-Date -Format "yyyyMMdd_HHmmss"))

function Write-Log([string]$msg) {
    $line = "[{0}] {1}" -f (Get-Date -Format "yyyy-MM-dd HH:mm:ss"), $msg
    Write-Host $line
    Add-Content -Path $logFile -Value $line -Encoding UTF8
}

function Get-YamlSimpleValue([string]$text, [string]$section, [string]$key) {
    $inSection = $false
    foreach ($raw in ($text -split "`r?`n")) {
        $line = $raw
        if ($line -match ("^\s*{0}\s*:\s*$" -f [regex]::Escape($section))) {
            $inSection = $true
            continue
        }
        if ($inSection -and $line -match '^\S') {
            $inSection = $false
        }
        if ($inSection -and $line -match ("^\s*{0}\s*:\s*(.+)\s*$" -f [regex]::Escape($key))) {
            $val = $Matches[1].Trim().Trim('"').Trim("'")
            return $val
        }
    }
    return $null
}

New-Item -ItemType Directory -Force -Path $dayDir, $logDir | Out-Null
Write-Log ("=== Backup start -> {0} (keep {1} days) ===" -f $dayDir, $KeepDays)

if (-not $PgBin) {
    $candidates = New-Object System.Collections.Generic.List[string]
    foreach ($root in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, "C:\Program Files", "D:\Program Files")) {
        if ([string]::IsNullOrWhiteSpace($root)) { continue }
        foreach ($ver in @("17", "16", "15", "14")) {
            $candidates.Add((Join-Path $root ("PostgreSQL\{0}\bin" -f $ver)))
        }
    }
    # EDB / custom installs sometimes leave pg_dump on PATH
    $fromPath = Get-Command pg_dump.exe -ErrorAction SilentlyContinue
    if ($fromPath) {
        $candidates.Insert(0, (Split-Path -Parent $fromPath.Source))
    }
    foreach ($c in $candidates) {
        $tryDump = Join-Path $c "pg_dump.exe"
        if (Test-Path -LiteralPath $tryDump) {
            $PgBin = $c
            break
        }
    }
}

if ([string]::IsNullOrWhiteSpace($PgBin)) {
    throw @"
PostgreSQL pg_dump.exe not found.
Install client tools or pass:
  -PgBin 'C:\Program Files\PostgreSQL\17\bin'
"@
}

$pgDump = Join-Path $PgBin "pg_dump.exe"
if (-not (Test-Path -LiteralPath $pgDump)) {
    throw ("pg_dump.exe not found in: {0}" -f $PgBin)
}
Write-Log ("pg_dump: {0}" -f $pgDump)

if (Test-Path $CfgPath) {
    $cfgText = Get-Content $CfgPath -Raw -Encoding UTF8
    if (-not $PgUser) { $PgUser = Get-YamlSimpleValue $cfgText "db" "user" }
    if (-not $PgPassword) { $PgPassword = Get-YamlSimpleValue $cfgText "db" "password" }
    $cfgHost = Get-YamlSimpleValue $cfgText "db" "server"
    $cfgPort = Get-YamlSimpleValue $cfgText "db" "port"
    if ($cfgHost) { $PgHost = $cfgHost }
    if ($cfgPort) { $PgPort = [int]$cfgPort }
}

if (-not $PgUser) { $PgUser = "postgres" }
if (-not $PgPassword) {
    throw ("PG password missing. Set db.password in {0} or pass -PgPassword" -f $CfgPath)
}

$env:PGPASSWORD = $PgPassword
try {
    foreach ($dbName in $Databases) {
        $outFile = Join-Path $dayDir ("{0}_{1}.dump" -f $dbName, $stamp)
        Write-Log ("pg_dump {0} -> {1}" -f $dbName, $outFile)
        & $pgDump `
            --host=$PgHost `
            --port=$PgPort `
            --username=$PgUser `
            --format=custom `
            --blobs `
            --file=$outFile `
            --dbname=$dbName
        if ($LASTEXITCODE -ne 0) {
            throw ("pg_dump failed for {0} (exit {1})" -f $dbName, $LASTEXITCODE)
        }
        $sizeMb = [math]::Round((Get-Item $outFile).Length / 1MB, 1)
        Write-Log ("OK {0} ({1} MB)" -f $dbName, $sizeMb)
    }
}
finally {
    Remove-Item Env:PGPASSWORD -ErrorAction SilentlyContinue
}

if (-not (Test-Path $ProjectDir)) {
    Write-Log ("WARN: project not found: {0}" -f $ProjectDir)
}
else {
    $projZip = Join-Path $dayDir ("es_api_service_{0}.zip" -f $stamp)
    $stage = Join-Path $env:TEMP ("es_backup_stage_{0}" -f $stamp)
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $stage | Out-Null

    Write-Log ("Copy project {0} -> stage (skip logs, price_cache)" -f $ProjectDir)
    $robolog = Join-Path $logDir ("robocopy_{0}.log" -f $stamp)
    & robocopy $ProjectDir $stage /E /R:2 /W:3 /NFL /NDL /NJH /NJS /XD `
        "logs" `
        "Log" `
        ".git" `
        "node_modules" `
        "price_cache" `
        | Out-File $robolog -Encoding UTF8
    if ($LASTEXITCODE -ge 8) {
        throw ("robocopy failed (exit {0}), see {1}" -f $LASTEXITCODE, $robolog)
    }

    Write-Log ("Zip -> {0}" -f $projZip)
    if (Test-Path $projZip) { Remove-Item $projZip -Force }
    Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $projZip -CompressionLevel Optimal
    Remove-Item $stage -Recurse -Force
    $zipMb = [math]::Round((Get-Item $projZip).Length / 1MB, 1)
    Write-Log ("OK project ({0} MB)" -f $zipMb)
}

Write-Log ("Rotation: delete folders older than {0} days in {1}" -f $KeepDays, $BackupRoot)
$cutoff = (Get-Date).Date.AddDays(-$KeepDays)
Get-ChildItem $BackupRoot -Directory -ErrorAction SilentlyContinue |
    Where-Object {
        $_.Name -match '^\d{4}-\d{2}-\d{2}$' -and
        ($_.Name -as [datetime]) -lt $cutoff
    } |
    ForEach-Object {
        Write-Log ("Delete old backup: {0}" -f $_.FullName)
        Remove-Item $_.FullName -Recurse -Force
    }

Get-ChildItem $logDir -File -Filter "backup_*.log" -ErrorAction SilentlyContinue |
    Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-($KeepDays * 2)) } |
    Remove-Item -Force

Write-Log "=== Backup done ==="
exit 0
