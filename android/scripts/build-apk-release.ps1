#Requires -Version 5.1
<#
.SYNOPSIS
  Story 12.4 — Build APK reproductible Le Voile Android (variante PowerShell).

.DESCRIPTION
  Variante PowerShell de scripts/build-apk-release.sh — pour developpeurs
  Windows. Cohérent ADR-08 (dual-script OS).

  Output (flavor `apkDirect` Story 12.5 productFlavors) :
    - app/build/outputs/apk/apkDirect/release/app-apkDirect-release(-unsigned-LOCAL-DEV).apk
    - + .sha256
    - apk-content-archive.zip + .sha256

.PARAMETER SkipGomobileVerify
  Saute le `bash scripts/build-aar.sh` (suppose que le .aar est deja a jour).

.PARAMETER SkipPinningCheck
  Saute la verification des versions JDK / Go pinnees.

.PARAMETER Flavor
  Flavor productFlavors (default: apkDirect — canal GitHub direct, celui qu'on
  signe Story 12.3. F-Droid build sa propre repro de son cote).
#>

[CmdletBinding()]
param(
  [switch] $SkipGomobileVerify,
  [switch] $SkipPinningCheck,
  [string] $Flavor = 'apkDirect'
)

$ErrorActionPreference = 'Stop'

$androidDir = Resolve-Path (Join-Path $PSScriptRoot '..')
Set-Location $androidDir

Write-Host "→ Verification pinnings"

if (-not $SkipPinningCheck) {
  $jdkVersion = (& java -version 2>&1) | Select-Object -First 1
  if ($jdkVersion -notmatch 'version "?17\.') {
    Write-Warning "JDK $jdkVersion — Story 12.4 attendu Temurin 17.0.x. Fixer JAVA_HOME ou utiliser -SkipPinningCheck."
  }
  if (Get-Command go -ErrorAction SilentlyContinue) {
    $goVersion = (& go version) -join ''
    if ($goVersion -notmatch 'go1\.(2[2-9]|3[0-9])') {
      Write-Warning "$goVersion — Story 12.4 attendu Go 1.22.x ou plus recent."
    }
  }
}

if (-not $SkipGomobileVerify) {
  Write-Host "→ Build aar (gomobile bind)"
  & bash scripts/build-aar.sh
  if ($LASTEXITCODE -ne 0) { throw "build-aar.sh a echoue" }
}

Write-Host "→ Sync frontend"
& bash scripts/sync-frontend.sh
if ($LASTEXITCODE -ne 0) { throw "sync-frontend.sh a echoue" }

Write-Host "→ Clean Gradle (anti-cache pollution)"
& ./gradlew clean --no-daemon
if ($LASTEXITCODE -ne 0) { throw "gradlew clean a echoue" }

$gradleTask = ":app:assemble" + ($Flavor.Substring(0,1).ToUpper() + $Flavor.Substring(1)) + "Release"

Write-Host "→ Assemble release flavor=$Flavor (no-daemon, no-parallel, no-cache, no-config-cache)"
& ./gradlew $gradleTask `
    --no-daemon `
    --no-parallel `
    --no-build-cache `
    --no-configuration-cache `
    --stacktrace
if ($LASTEXITCODE -ne 0) { throw "gradlew $gradleTask a echoue" }

$apkDir = "app/build/outputs/apk/$Flavor/release"
$apk = "$apkDir/app-$Flavor-release-unsigned-LOCAL-DEV.apk"
if (-not (Test-Path $apk)) {
  $apk = "$apkDir/app-$Flavor-release.apk"
}
if (-not (Test-Path $apk)) {
  throw "APK release introuvable dans $apkDir"
}

Write-Host "→ Calcule SHA256 APK"
$apkHash = (Get-FileHash $apk -Algorithm SHA256).Hash.ToLower()
"$apkHash  $(Split-Path $apk -Leaf)" | Set-Content "$apk.sha256" -Encoding utf8
Write-Host "$apkHash  $(Split-Path $apk -Leaf)"

Write-Host "→ Genere apk-content-archive.zip (extraction ZIP triee + re-zip deterministe)"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  $extracted = Join-Path $tmp 'extracted'
  Expand-Archive -Path $apk -DestinationPath $extracted -Force

  # Exclure les fichiers de signature (META-INF/*.SF, *.RSA, *.DSA, *.EC) qui
  # contiennent des nonces et timestamps non-reproductibles.
  $files = Get-ChildItem -Path $extracted -Recurse -File |
    Where-Object {
      $rel = $_.FullName.Substring($extracted.Length + 1).Replace('\', '/')
      -not ($rel -match '^META-INF/.*\.(SF|RSA|DSA|EC)$')
    } |
    Sort-Object @{ Expression = { $_.FullName.Substring($extracted.Length + 1).Replace('\', '/') } }

  $zip = Join-Path $tmp 'apk-content-archive.zip'
  if (Test-Path $zip) { Remove-Item $zip -Force }

  Add-Type -AssemblyName System.IO.Compression
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $archive = [System.IO.Compression.ZipFile]::Open($zip, [System.IO.Compression.ZipArchiveMode]::Create)
  try {
    foreach ($f in $files) {
      $rel = $f.FullName.Substring($extracted.Length + 1).Replace('\', '/')
      $entry = $archive.CreateEntry($rel, [System.IO.Compression.CompressionLevel]::NoCompression)
      $entry.LastWriteTime = [System.DateTimeOffset]::FromUnixTimeSeconds(0)
      $stream = $entry.Open()
      try {
        $bytes = [System.IO.File]::ReadAllBytes($f.FullName)
        $stream.Write($bytes, 0, $bytes.Length)
      } finally { $stream.Dispose() }
    }
  } finally { $archive.Dispose() }

  Copy-Item $zip 'apk-content-archive.zip' -Force
  $contentHash = (Get-FileHash 'apk-content-archive.zip' -Algorithm SHA256).Hash.ToLower()
  "$contentHash  apk-content-archive.zip" | Set-Content 'apk-content-archive.zip.sha256' -Encoding utf8
  Write-Host "$contentHash  apk-content-archive.zip"
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host "✓ Build reproductible termine."
Write-Host "  APK : $apk"
Write-Host "  SHA256 APK             : $apkHash"
Write-Host "  SHA256 content-archive : $contentHash"
