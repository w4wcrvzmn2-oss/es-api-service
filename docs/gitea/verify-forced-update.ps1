# End-to-end: mock Gitea releases/latest -> UpdateChecker must require update for old local version.

$ErrorActionPreference = "Stop"
$root = "c:\Users\Exest\3D Objects\source\elfisa-pharmacy"

$exeDir = Join-Path $root "src\ElectronicPharmacy\bin\Release"
if (-not (Test-Path $exeDir)) {
    throw "Missing build dir: $exeDir"
}
$exe = (Get-ChildItem -LiteralPath $exeDir -File | Where-Object { $_.Extension -eq ".exe" } | Select-Object -First 1).FullName
if (-not $exe) {
    throw "Build Release first. Missing exe in: $exeDir"
}
Write-Host "Using exe: $exe"

# Rebuild first so UpdateChecker overloads are present
& "C:\Program Files\Microsoft Visual Studio\18\Community\MSBuild\Current\Bin\MSBuild.exe" `
    (Join-Path $root "src\ElectronicPharmacy\ElectronicPharmacy.csproj") `
    /p:Configuration=Release /v:q
if ($LASTEXITCODE -ne 0) { throw "MSBuild failed" }

$port = 18765
$prefix = "http://127.0.0.1:$port/"
$listener = [System.Net.HttpListener]::new()
$listener.Prefixes.Add($prefix)
try {
    $listener.Start()
} catch {
    throw "Cannot bind $prefix (run as admin once or free the port): $($_.Exception.Message)"
}

$json = @'
{
  "tag_name": "v1.0.10",
  "html_url": "http://127.0.0.1:18765/pharmdata/elfisa-pharmacy/releases/tag/v1.0.10",
  "assets": [
    {
      "name": "ElectronicPharmacy-v1.0.10.zip",
      "browser_download_url": "http://127.0.0.1:18765/files/ElectronicPharmacy-v1.0.10.zip"
    }
  ]
}
'@

$csc = (Get-ChildItem "C:\Program Files*\Microsoft Visual Studio" -Recurse -Filter csc.exe -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match "\\Roslyn\\csc\.exe$" } |
    Select-Object -First 1).FullName
if (-not $csc) { throw "csc.exe not found" }

$outDir = Join-Path $root "tools\UpdateCheckSmoke\bin"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$smokeExe = Join-Path $outDir "UpdateCheckSmoke.exe"
$program = Join-Path $root "tools\UpdateCheckSmoke\Program.cs"

$stj = Get-ChildItem (Join-Path $root "packages") -Recurse -Filter "System.Text.Json.dll" -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match "net462|netstandard2" } |
    Select-Object -First 1 -ExpandProperty FullName
if (-not $stj) {
    $stj = Get-ChildItem $exeDir -Filter "System.Text.Json.dll" -ErrorAction SilentlyContinue |
        Select-Object -First 1 -ExpandProperty FullName
}

$refArgs = @(
    "/r:$exe",
    "/r:$(Join-Path $exeDir 'Elfisa.UI.dll')",
    "/r:System.dll",
    "/r:System.Core.dll",
    "/r:System.Net.Http.dll",
    "/r:System.Configuration.dll",
    "/r:System.Windows.Forms.dll",
    "/r:System.Drawing.dll"
)
if ($stj) { $refArgs += "/r:$stj" }

& $csc /nologo /target:exe /out:$smokeExe /langversion:latest @refArgs $program
if ($LASTEXITCODE -ne 0) { throw "Smoke compile failed" }

Copy-Item (Join-Path $exeDir "*") $outDir -Force -ErrorAction SilentlyContinue

# Start smoke client (will hit listener), then serve one response on this thread.
$proc = Start-Process -FilePath $smokeExe -ArgumentList @(
    "http://127.0.0.1:$port",
    "pharmdata/elfisa-pharmacy",
    "1.0.3.0"
) -PassThru -WorkingDirectory $outDir -WindowStyle Hidden

try {
    $ar = $listener.BeginGetContext($null, $null)
    if (-not $ar.AsyncWaitHandle.WaitOne(15000)) {
        throw "Timed out waiting for UpdateChecker HTTP request"
    }
    $ctx = $listener.EndGetContext($ar)
    $buf = [Text.Encoding]::UTF8.GetBytes($json)
    if ($ctx.Request.Url.AbsolutePath -like "*/releases/latest") {
        $ctx.Response.StatusCode = 200
        $ctx.Response.ContentType = "application/json; charset=utf-8"
        $ctx.Response.OutputStream.Write($buf, 0, $buf.Length)
    } else {
        $ctx.Response.StatusCode = 404
    }
    $ctx.Response.Close()

    if (-not $proc.WaitForExit(15000)) {
        $proc.Kill()
        throw "Smoke process did not exit"
    }

    if ($proc.ExitCode -ne 0) {
        throw "Expected UpdateRequired for local 1.0.3 vs remote v1.0.10, exit=$($proc.ExitCode)"
    }

    Write-Host "PASS: old build (1.0.3) is blocked by newer release v1.0.10 (mock Gitea)."
} finally {
    try { if (-not $proc.HasExited) { $proc.Kill() } } catch {}
    try { $listener.Stop(); $listener.Close() } catch {}
}
