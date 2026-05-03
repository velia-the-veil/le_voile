#Requires -Version 5.1
<#
.SYNOPSIS
  Compile le noyau Go partage en .aar consommable Gradle (Story 9.2).

.DESCRIPTION
  Variante PowerShell de scripts/build-aar.sh — pour developpeurs Windows.
  Resout les variables d'environnement user-scope (JAVA_HOME, ANDROID_HOME,
  ANDROID_NDK_HOME) automatiquement via [System.Environment] (.NET) car
  les sessions PowerShell ne les heritent pas toujours.

  Invoque gomobile bind sur les 5 packages Story 9.2 :
    - android/shims/protocol  (shim — version + framing constants)
    - android/shims/auth      (shim — token TTL + refresh threshold)
    - android/shims/crypto    (shim — wrap internal/crypto, Ed25519 helpers)
    - android/shims/registry  (shim — wrap internal/registry, country meta)
    - android/shims/leakcheck (shim — wrap internal/leakcheck, transitif tunnel + quic-go)

  Sortie : android/levoile-core/libs/levoile-core.aar (gitignore).

.NOTES
  Pre-requis :
    - Go >= 1.26 dans le PATH
    - gomobile + gobind installes :
        go install golang.org/x/mobile/cmd/gomobile@latest
        go install golang.org/x/mobile/cmd/gobind@latest
        gomobile init    # telecharge le NDK ~1.5 GB la premiere fois
    - JDK 17+ (JAVA_HOME persiste user-scope)
    - Android SDK + NDK (ANDROID_HOME / ANDROID_NDK_HOME persistes user-scope)

  Voir : ADR-08, ADR-09, architecture.md l. 296-302,
         story 9-2-script-build-aar-sh-*.md.
#>

$ErrorActionPreference = "Stop"

# ---- Resolution variables user-scope (.NET) ----
function Get-UserEnv($name) {
  return [System.Environment]::GetEnvironmentVariable($name, 'User')
}

$javaHome     = if ($env:JAVA_HOME)        { $env:JAVA_HOME }        else { Get-UserEnv 'JAVA_HOME' }
$androidHome  = if ($env:ANDROID_HOME)     { $env:ANDROID_HOME }     else { Get-UserEnv 'ANDROID_HOME' }
$androidNdk   = if ($env:ANDROID_NDK_HOME) { $env:ANDROID_NDK_HOME } else { Get-UserEnv 'ANDROID_NDK_HOME' }
$userPath     = Get-UserEnv 'Path'

if ($javaHome)    { $env:JAVA_HOME = $javaHome }
if ($androidHome) {
  $env:ANDROID_HOME = $androidHome
  $env:ANDROID_SDK_ROOT = $androidHome
}
if ($androidNdk)  { $env:ANDROID_NDK_HOME = $androidNdk }

# Prepend le PATH user-scope au PATH session pour exposer gomobile.exe,
# javac.exe, sdkmanager.bat, etc.
if ($userPath) {
  $env:Path = $userPath + ';' + $env:Path
}

# ---- Localisation repo root ----
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
  Write-Error "git non trouve dans le PATH (requis pour resoudre le repo root)."
  exit 1
}
$repoRoot = (& git rev-parse --show-toplevel).Trim()

# ---- Pre-check : gomobile dans le PATH ----
if (-not (Get-Command gomobile -ErrorAction SilentlyContinue)) {
  Write-Error @"
gomobile non trouve dans le PATH.

Installation :
  go install golang.org/x/mobile/cmd/gomobile@latest
  go install golang.org/x/mobile/cmd/gobind@latest
  gomobile init    # telecharge l'Android NDK (~1.5 GB, 5-15 min premiere fois)

Verifier que `$env:GOPATH\bin (ou `$env:USERPROFILE\go\bin) est dans le PATH.
"@
  exit 1
}

# ---- Pre-check : version Go ----
$goVersionLine = (& go version) -join ''
if ($goVersionLine -match 'go(\d+)\.(\d+)') {
  $goMajor = [int]$Matches[1]
  $goMinor = [int]$Matches[2]
  if ($goMajor -lt 1 -or ($goMajor -eq 1 -and $goMinor -lt 26)) {
    Write-Error "Go $goMajor.$goMinor trop ancien (requis >= 1.26 - directive go.mod racine)."
    exit 1
  }
} else {
  Write-Warning "Impossible de parser la version Go depuis : $goVersionLine"
}

# ---- Pre-check : javac (gomobile l'invoque pour produire le .aar) ----
if (-not (Get-Command javac -ErrorAction SilentlyContinue)) {
  if ($env:JAVA_HOME -and (Test-Path "$env:JAVA_HOME\bin\javac.exe")) {
    $env:Path = "$env:JAVA_HOME\bin;" + $env:Path
    Write-Host "[build-aar] PATH prepended with `$env:JAVA_HOME\bin = $env:JAVA_HOME\bin"
  } else {
    Write-Error @"
javac non trouve dans le PATH.

Verifier qu'un JDK 17+ est installe (Microsoft OpenJDK, Adoptium, etc.)
et que JAVA_HOME est persiste user-scope :
  setx JAVA_HOME "C:\Users\<user>\AppData\Local\Programs\Microsoft\jdk-17.0.10.7-hotspot"
puis ouvrir une nouvelle session PowerShell.
"@
    exit 1
  }
}

# ---- Pre-check : NDK ----
if (-not $env:ANDROID_NDK_HOME) {
  Write-Warning "ANDROID_NDK_HOME non defini ; gomobile tentera l'auto-detection."
} elseif (-not (Test-Path $env:ANDROID_NDK_HOME)) {
  Write-Error "ANDROID_NDK_HOME=$env:ANDROID_NDK_HOME n'existe pas."
  exit 1
}

# ---- Output path + cleanup en cas d'erreur ----
$outputDir = Join-Path $repoRoot "android/app/libs"
$output    = Join-Path $outputDir "levoile-core.aar"
New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
$buildOk = $false

try {
  # ---- gomobile bind ----
  Push-Location $repoRoot
  Write-Host "[build-aar] Invocation gomobile bind ..."
  Write-Host "[build-aar]   target=android androidapi=29 javapkg=fr.plateformeliberte.levoile.core"
  Write-Host "[build-aar]   packages=android/shims/{protocol,auth,crypto,registry,leakcheck}"

  $gomobileArgs = @(
    'bind',
    '-target=android',
    '-androidapi=29',
    '-javapkg=fr.plateformeliberte.levoile.core',
    '-o', $output,
    './android/shims/protocol',
    './android/shims/auth',
    './android/shims/crypto',
    './android/shims/registry',
    './android/shims/leakcheck'
  )
  & gomobile.exe @gomobileArgs
  if ($LASTEXITCODE -ne 0) {
    throw "gomobile bind a echoue (exit code $LASTEXITCODE)"
  }
  $buildOk = $true
} finally {
  Pop-Location
  if (-not $buildOk -and (Test-Path $output)) {
    Remove-Item $output -Force -ErrorAction SilentlyContinue
  }
}

# ---- Rapport ----
$item = Get-Item $output
$hash = (Get-FileHash $output -Algorithm SHA256).Hash

Write-Host ""
Write-Host "[build-aar] OK"
Write-Host "  Artefact : $output"
Write-Host "  Taille   : $($item.Length) octets"
Write-Host "  SHA256   : $hash"
Write-Host ""
Write-Host "Verifier la frontiere ADR-09 (imports cross-OS) :"
Write-Host "  bash android/scripts/verify-shared-imports.sh"
