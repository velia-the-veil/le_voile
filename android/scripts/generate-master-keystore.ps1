#Requires -Version 5.1
<#
.SYNOPSIS
  Story 12.3 — Genere le keystore master Le Voile (variante PowerShell).

.DESCRIPTION
  A EXECUTER MANUELLEMENT sur la machine air-gapped / YubiKey qui detient la
  master key. JAMAIS sur un runner CI, JAMAIS sur une machine en production
  connectee a Internet.

  Output : android/keystore/levoile-release.jks (PKCS12). A encoder en base64
  puis stocker comme GitHub Actions secret LEVOILE_KEYSTORE_BASE64. Le .jks
  lui-meme n'est JAMAIS commite (cf. android/keystore/.gitignore).

  Variante PowerShell de generate-master-keystore.sh — pour mainteneurs Windows.

.PARAMETER KeyAlg
  Algorithme de cle. RSA 4096 par defaut (cohérent Mullvad / ProtonVPN /
  PackageManager Android API 29+). Ed25519 reserve pour la master key
  desktop (cf. docs/key-management-android.md).

.PARAMETER KeySize
  Taille de cle. 4096 par defaut pour RSA. Ignore si KeyAlg = Ed25519.

.PARAMETER KeyYear
  Suffixe annee de l'alias keystore (traceabilite rotation 24 mois NFR22g/h).
  Defaut = annee courante.
#>

[CmdletBinding()]
param(
  [string] $KeyAlg = 'RSA',
  [int]    $KeySize = 4096,
  [string] $KeyYear = ([datetime]::Now.Year.ToString()),
  [string] $DName = 'CN=Le Voile, O=Plateforme Liberte, C=FR'
)

$ErrorActionPreference = 'Stop'

# Resolution repo root.
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
  throw 'git introuvable dans le PATH (requis pour resoudre le repo root).'
}
$repoRoot = (& git rev-parse --show-toplevel).Trim()
$keystoreDir = Join-Path $repoRoot 'android/keystore'
$keystore = Join-Path $keystoreDir 'levoile-release.jks'
$alias = "levoile-master-$KeyYear"

if (Test-Path $keystore) {
  throw @"
$keystore existe deja.

Procedure rotation keystore : voir docs/key-management-android.md section
"Rotation 24 mois". Ne PAS ecraser le keystore existant — la chaine de
confiance v3 lineage exigerait une migration documentee.

Pour regenerer (test, mauvaise generation initiale) : supprimer manuellement
le fichier puis relancer ce script.
"@
}

New-Item -ItemType Directory -Path $keystoreDir -Force | Out-Null

if (-not (Get-Command keytool -ErrorAction SilentlyContinue)) {
  throw 'keytool introuvable. Installer un JDK 17+ et le mettre dans le PATH.'
}

# Read password (SecureString — non afficher dans console history).
$securePass = Read-Host -Prompt 'Password keystore (>= 16 chars)' -AsSecureString
$securePassConfirm = Read-Host -Prompt 'Confirmer password' -AsSecureString

# Conversion securisee SecureString -> plain pour passer a keytool (qui ne
# supporte pas les input pipes pour le password store).
function ConvertFrom-Secure([System.Security.SecureString]$s) {
  $bstr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($s)
  try {
    return [System.Runtime.InteropServices.Marshal]::PtrToStringAuto($bstr)
  } finally {
    [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
  }
}

$pass = ConvertFrom-Secure $securePass
$passConfirm = ConvertFrom-Secure $securePassConfirm

if ($pass -ne $passConfirm) {
  throw 'Passwords ne matchent pas.'
}
if ($pass.Length -lt 16) {
  throw 'Password < 16 chars (NFR securite — entropie ~96 bits requise).'
}

Write-Host ''
Write-Host 'Generation keystore PKCS12 :'
Write-Host "  Algorithme : $KeyAlg $KeySize bits"
Write-Host "  Alias      : $alias"
Write-Host "  DN         : $DName"
Write-Host '  Validite   : 7300 jours (~20 ans, couvre rotation 24 mois x 10)'
Write-Host ''

$keytoolArgs = @(
  '-genkeypair', '-v',
  '-keystore', $keystore,
  '-storetype', 'PKCS12',
  '-storepass', $pass,
  '-alias', $alias,
  '-keypass', $pass,
  '-dname', $DName,
  '-validity', '7300',
  '-keyalg', $KeyAlg
)
if ($KeyAlg -eq 'RSA' -or $KeyAlg -eq 'DSA') {
  $keytoolArgs += '-keysize'
  $keytoolArgs += "$KeySize"
}

& keytool @keytoolArgs
if ($LASTEXITCODE -ne 0) {
  throw "keytool a echoue (exit code $LASTEXITCODE)"
}

# Cleanup variables locales (best effort — la JVM keytool a vu le password,
# il vit dans son heap pendant le run mais est libere a sa fin).
$pass = $null
$passConfirm = $null
[System.GC]::Collect()

Write-Host ''
Write-Host '================================================================'
Write-Host "Keystore genere : $keystore"
Write-Host "  Alias : $alias"
Write-Host ''
Write-Host 'Etape suivante 1/3 : tester apksigner localement (Story 12.3 AC #8).'
Write-Host '  cd $(git rev-parse --show-toplevel)/android'
Write-Host "  `$env:LEVOILE_KEYSTORE_PATH = `"$keystore`""
Write-Host '  $env:LEVOILE_KEYSTORE_PASSWORD = "<password>"'
Write-Host "  `$env:LEVOILE_KEY_ALIAS = `"$alias`""
Write-Host '  $env:LEVOILE_KEY_PASSWORD = "<password>"'
Write-Host '  ./gradlew :app:assembleRelease --no-daemon'
Write-Host '  apksigner verify --verbose --print-certs --min-sdk-version 29 `'
Write-Host '      app/build/outputs/apk/release/app-release.apk'
Write-Host ''
Write-Host 'Etape suivante 2/3 : encoder en base64 + provisionner les secrets GitHub :'
Write-Host "  [Convert]::ToBase64String([IO.File]::ReadAllBytes(`"$keystore`")) | Set-Clipboard"
Write-Host '  # Coller dans GitHub > Settings > Secrets > LEVOILE_KEYSTORE_BASE64.'
Write-Host 'Secrets a ajouter dans le meme ecran :'
Write-Host '  - LEVOILE_KEYSTORE_BASE64 (le base64 ci-dessus)'
Write-Host '  - LEVOILE_KEYSTORE_PASSWORD (le password saisi)'
Write-Host "  - LEVOILE_KEY_ALIAS = $alias"
Write-Host '  - LEVOILE_KEY_PASSWORD (= LEVOILE_KEYSTORE_PASSWORD pour l''instant)'
Write-Host ''
Write-Host 'Etape suivante 3/3 : recuperer le SHA256 fingerprint :'
Write-Host "  keytool -list -v -keystore `"$keystore`" -alias $alias -storepass <password> | Select-String 'SHA256:'"
Write-Host ''
Write-Host 'PUIS supprimer le keystore local apres verification (sauf machine air-gapped'
Write-Host 'permanente). Sur Windows :'
Write-Host "  Remove-Item `"$keystore`""
Write-Host "  cipher /w:`"$keystoreDir`"     # ecrase l'espace libere du disque"
Write-Host '================================================================'
