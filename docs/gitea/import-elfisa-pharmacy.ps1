# Push local elfisa-pharmacy clone (with tags) to Gitea.
# Requires: git, and a Gitea personal access token with repo write.

param(
    [Parameter(Mandatory = $true)][string]$GiteaUrl,
    [Parameter(Mandatory = $true)][string]$Owner,
    [Parameter(Mandatory = $true)][string]$Token,
    [string]$RepoName = "elfisa-pharmacy",
    [string]$LocalRepo = ""
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($LocalRepo)) {
    $LocalRepo = Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) "elfisa-pharmacy"
    if (-not (Test-Path $LocalRepo)) {
        $LocalRepo = "c:\Users\Exest\3D Objects\source\elfisa-pharmacy"
    }
}

$GiteaUrl = $GiteaUrl.TrimEnd('/')
$api = "$GiteaUrl/api/v1"
$headers = @{ Authorization = "token $Token"; "Content-Type" = "application/json" }

# Ensure repo exists (private).
try {
    Invoke-RestMethod -Uri "$api/repos/$Owner/$RepoName" -Headers $headers -Method GET | Out-Null
    Write-Host "Repo exists: $Owner/$RepoName"
} catch {
    $body = @{
        name = $RepoName
        private = $true
        auto_init = $false
        description = "Desktop client Electronic Pharmacy"
    } | ConvertTo-Json
    Invoke-RestMethod -Uri "$api/orgs/$Owner/repos" -Headers $headers -Method POST -Body $body | Out-Null
    Write-Host "Created org repo $Owner/$RepoName (if org missing, create user repo manually)."
}

$remoteUrl = $GiteaUrl.Replace("https://", "https://oauth2:$Token@").Replace("http://", "http://oauth2:$Token@")
$remoteUrl = "$remoteUrl/$Owner/$RepoName.git"

Push-Location $LocalRepo
try {
    $existing = git remote | Where-Object { $_ -eq "gitea" }
    if ($existing) {
        git remote set-url gitea $remoteUrl
    } else {
        git remote add gitea $remoteUrl
    }
    git push gitea master
    git push gitea --tags
    Write-Host "Pushed master + tags to gitea."
} finally {
    Pop-Location
}

Write-Host "Next: create Release vX.Y.Z in UI and attach ZIP from dist/build-release.ps1"
