# Install Git for Windows (needed by Gitea). Run as Administrator.
# Silent install + add to machine PATH, then restart Gitea.

$ErrorActionPreference = "Stop"
$Setup = Join-Path $PSScriptRoot "Git-64-bit.exe"
if (-not (Test-Path $Setup)) { throw "Missing $Setup" }

Write-Host "Installing Git for Windows (silent)..."
$args = @(
  "/VERYSILENT",
  "/NORESTART",
  "/NOCANCEL",
  "/SP-",
  "/CLOSEAPPLICATIONS",
  "/RESTARTAPPLICATIONS",
  "/COMPONENTS=icons,ext\reg\shellhere,assoc,assoc_sh",
  '/o:PathOption=CmdTools',
  '/o:BashTerminalOption=ConHost',
  '/o:EditorOption=VIM',
  '/o:DefaultBranchOption=master'
)
$p = Start-Process -FilePath $Setup -ArgumentList $args -Wait -PassThru
if ($p.ExitCode -ne 0 -and $p.ExitCode -ne 3010) {
  throw "Git installer exit code $($p.ExitCode)"
}

$gitCmd = @(
  "C:\Program Files\Git\cmd\git.exe",
  "C:\Program Files (x86)\Git\cmd\git.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1

if (-not $gitCmd) { throw "git.exe not found after install" }
Write-Host "git OK: $(& $gitCmd --version)"

# Ensure Machine PATH contains Git\cmd
$gitDir = Split-Path $gitCmd -Parent
$machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine")
if ($machinePath -notlike "*$gitDir*") {
  [Environment]::SetEnvironmentVariable("Path", "$machinePath;$gitDir", "Machine")
  Write-Host "Added to Machine PATH: $gitDir"
}

# Restart Gitea so it sees new PATH
Get-Process gitea -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
$task = Get-ScheduledTask -TaskName Gitea -ErrorAction SilentlyContinue
if ($task) {
  Start-ScheduledTask -TaskName Gitea
  Write-Host "Restarted scheduled task Gitea"
} elseif (Get-Service Gitea -ErrorAction SilentlyContinue) {
  Restart-Service Gitea -Force
  Write-Host "Restarted service Gitea"
} else {
  Start-Process -FilePath "D:\gitea\gitea.exe" -ArgumentList "web" -WorkingDirectory "D:\gitea" -WindowStyle Hidden
  Write-Host "Started gitea.exe web"
}

# Wait
for ($i = 0; $i -lt 20; $i++) {
  Start-Sleep -Seconds 1
  try {
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:3000/" -UseBasicParsing -TimeoutSec 2
    Write-Host "Gitea HTTP $($r.StatusCode)"
    break
  } catch { Write-Host "waiting..." }
}

Write-Host "Done. Refresh https://git.24pharmdata.ru and continue setup."
Write-Host "Optional: set Git path in installer UI to: $gitCmd"
