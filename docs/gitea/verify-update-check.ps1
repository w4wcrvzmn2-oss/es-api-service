# Verify forced-update version logic (no Gitea required).
# Also probes live API when git.24pharmdata.ru is reachable.

$ErrorActionPreference = "Stop"
$failed = 0

function Assert-True([bool]$cond, [string]$msg) {
    if ($cond) {
        Write-Host "OK  $msg"
    } else {
        Write-Host "FAIL $msg"
        $script:failed++
    }
}

# Mirror of UpdateChecker.IsRemoteNewer / NormalizeVersion (PowerShell).
function Normalize-Version([string]$value) {
    if ([string]::IsNullOrWhiteSpace($value)) { return [Version]"0.0.0.0" }
    $value = $value.Trim()
    if ($value.StartsWith("v") -or $value.StartsWith("V")) { $value = $value.Substring(1) }
    $dash = $value.IndexOf("-")
    if ($dash -gt 0) { $value = $value.Substring(0, $dash) }
    $parts = $value.Split(".")
    while ($parts.Count -lt 4) { $value = "$value.0"; $parts = $value.Split(".") }
    return [Version]::Parse(($parts[0..3] -join "."))
}

function Is-RemoteNewer([string]$remote, [string]$local) {
    return (Normalize-Version $remote) -gt (Normalize-Version $local)
}

Assert-True (Is-RemoteNewer "v1.0.9" "1.0.3.0") "v1.0.9 > 1.0.3.0 blocks old build"
Assert-True (-not (Is-RemoteNewer "v1.0.9" "1.0.9.0")) "same version does not block"
Assert-True (-not (Is-RemoteNewer "v1.0.8" "1.0.9.0")) "older remote does not block"
Assert-True (Is-RemoteNewer "1.0.10" "1.0.9") "1.0.10 > 1.0.9"
Assert-True (Is-RemoteNewer "v2.0.0" "1.9.9.0") "major bump blocks"

$base = "https://git.24pharmdata.ru"
$repo = "pharmdata/elfisa-pharmacy"
$url = "$base/api/v1/repos/$repo/releases/latest"
try {
    $resp = Invoke-RestMethod -Uri $url -Method GET -TimeoutSec 8
    $tag = $resp.tag_name
    Write-Host "Live latest release: $tag"
    Assert-True (Is-RemoteNewer $tag "1.0.3.0") "live release newer than 1.0.3.0"
    Assert-True (-not (Is-RemoteNewer $tag "99.0.0.0")) "future local not blocked"
} catch {
    Write-Host "SKIP live API (Gitea not reachable yet): $($_.Exception.Message)"
}

if ($failed -gt 0) {
    Write-Error "$failed assertion(s) failed"
    exit 1
}

Write-Host "All update-check assertions passed."
