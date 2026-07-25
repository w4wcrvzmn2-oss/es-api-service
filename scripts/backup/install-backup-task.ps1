#Requires -Version 5.1
<#
.SYNOPSIS
  Registers a daily Windows task: backup at 07:00 to F:\Backups

.EXAMPLE
  Run as Administrator:
  .\install-backup-task.ps1
#>
[CmdletBinding()]
param(
    [string]$TaskName = "PharmDataDailyBackup",
    [string]$ScriptPath = "",
    [string]$BackupRoot = (Join-Path "F:" "Backups"),
    [int]$KeepDays = 14,
    [string]$ProjectDir = (Join-Path "D:" "es_api_service"),
    [string]$Time = "07:00"
)

$ErrorActionPreference = "Stop"

if (-not $ScriptPath) {
    $ScriptPath = Join-Path $PSScriptRoot "backup-daily.ps1"
}
if (-not (Test-Path -LiteralPath $ScriptPath)) {
    throw "Script not found: $ScriptPath"
}

# Do not write paths like 'F:\' or "F:\" — PowerShell treats \ before quote as broken string
if (-not (Test-Path -LiteralPath "F:")) {
    Write-Warning "Drive F: is not visible now. Task will still be created; check the volume before 07:00."
}

New-Item -ItemType Directory -Force -Path $BackupRoot | Out-Null

$arg = '-NoProfile -ExecutionPolicy Bypass -File "{0}" -BackupRoot "{1}" -KeepDays {2} -ProjectDir "{3}"' -f `
    $ScriptPath, $BackupRoot, $KeepDays, $ProjectDir

$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $arg
$trigger = New-ScheduledTaskTrigger -Daily -At $Time
$settings = New-ScheduledTaskSettingsSet `
    -StartWhenAvailable `
    -DontStopIfGoingOnBatteries `
    -AllowStartIfOnBatteries `
    -ExecutionTimeLimit (New-TimeSpan -Hours 4)

$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger $trigger `
    -Settings $settings `
    -Principal $principal `
    -Description ("Daily PG + es_api_service backup to {0}, keep {1} days" -f $BackupRoot, $KeepDays) `
    -Force | Out-Null

Write-Host ("OK: task '{0}' every day at {1}" -f $TaskName, $Time)
Write-Host ("Script: {0}" -f $ScriptPath)
Write-Host ("Target: {0} (keep {1} days)" -f $BackupRoot, $KeepDays)
Write-Host ""
Write-Host "Test now:"
Write-Host ("  Start-ScheduledTask -TaskName '{0}'" -f $TaskName)
Write-Host ("  Get-ScheduledTaskInfo -TaskName '{0}'" -f $TaskName)
Write-Host ("  Get-ChildItem -LiteralPath '{0}'" -f $BackupRoot)
