# After Gitea is up: push elfisa-pharmacy + tags, then create Release via API (optional ZIP).
# Usage:
#   .\push-and-release.ps1 -Token <PAT> [-ZipPath path\to\release.zip]

param(
    [Parameter(Mandatory = $true)][string]$Token,
    [string]$GiteaUrl = "https://git.24pharmdata.ru",
    [string]$Owner = "pharmdata",
    [string]$RepoName = "elfisa-pharmacy",
    [string]$LocalRepo = "c:\Users\Exest\3D Objects\source\elfisa-pharmacy",
    [string]$Tag = "",
    [string]$ZipPath = ""
)

$ErrorActionPreference = "Stop"
$import = Join-Path $PSScriptRoot "import-elfisa-pharmacy.ps1"
& $import -GiteaUrl $GiteaUrl -Owner $Owner -Token $Token -RepoName $RepoName -LocalRepo $LocalRepo

if ([string]::IsNullOrWhiteSpace($Tag)) {
    Push-Location $LocalRepo
    try {
        $Tag = (git describe --tags --abbrev=0).Trim()
    } finally {
        Pop-Location
    }
}

Write-Host "Latest local tag: $Tag"
$GiteaUrl = $GiteaUrl.TrimEnd('/')
$api = "$GiteaUrl/api/v1"
$headers = @{ Authorization = "token $Token"; "Content-Type" = "application/json" }

# Ensure release exists.
$release = $null
try {
    $release = Invoke-RestMethod -Uri "$api/repos/$Owner/$RepoName/releases/tags/$Tag" -Headers $headers -Method GET
    Write-Host "Release already exists for $Tag"
} catch {
    $body = @{
        tag_name = $Tag
        name = $Tag
        body = "Desktop client $Tag"
        draft = $false
        prerelease = $false
    } | ConvertTo-Json
    $release = Invoke-RestMethod -Uri "$api/repos/$Owner/$RepoName/releases" -Headers $headers -Method POST -Body $body
    Write-Host "Created release $Tag"
}

if (-not [string]::IsNullOrWhiteSpace($ZipPath) -and (Test-Path $ZipPath)) {
    $uploadUrl = "$api/repos/$Owner/$RepoName/releases/$($release.id)/assets?name=$([Uri]::EscapeDataString((Split-Path $ZipPath -Leaf)))"
    $bytes = [System.IO.File]::ReadAllBytes((Resolve-Path $ZipPath))
    Invoke-RestMethod -Uri $uploadUrl -Headers @{ Authorization = "token $Token" } -Method POST -ContentType "application/octet-stream" -Body $bytes | Out-Null
    Write-Host "Uploaded asset: $ZipPath"
}

Write-Host "Done. Verify: $GiteaUrl/$Owner/$RepoName/releases"
