# Test manuel 5.4-5.6 : Installation extension via politiques d'entreprise
# DOIT ETRE EXECUTE EN ADMINISTRATEUR
# Usage: .\scripts\test-extension-install.ps1 [-Clean]

param(
    [switch]$Clean
)

$ErrorActionPreference = "Stop"
$geckoId = "levoile@plateformeliberte.fr"
$deployDir = "C:\ProgramData\LeVoile\extension"
$chromeDir = "$deployDir\chrome"
$projectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not (Test-Path "$projectRoot\extension\background.js")) {
    $projectRoot = "D:\AI\Bmad\bmad_vpn_le_voile"
}

# Derive extension ID from crxgen (same PEM key used at build time).
$crxgenOutput = & go run "$projectRoot\tools\crxgen" -key "$projectRoot\extension\levoile.pem" -src "$projectRoot\extension" -out "$env:TEMP\levoile-test.crx" 2>&1
$extId = ($crxgenOutput | Select-String "Extension ID: (\w+)").Matches.Groups[1].Value
Remove-Item "$env:TEMP\levoile-test.crx" -ErrorAction SilentlyContinue
if (-not $extId) {
    Write-Host "ERREUR: Impossible de deriver l'extension ID depuis crxgen" -ForegroundColor Red
    exit 1
}
Write-Host "Extension ID derive: $extId" -ForegroundColor Green

function Install-Extension {
    Write-Host "`n=== Deploiement des fichiers extension ===" -ForegroundColor Cyan

    # Creer les repertoires
    New-Item -Path $chromeDir -ItemType Directory -Force | Out-Null
    Write-Host "  Repertoire cree: $chromeDir"

    # Copier le CRX
    $crxSrc = "$projectRoot\internal\browser\extension_assets\build\levoile.crx"
    if (-not (Test-Path $crxSrc)) {
        Write-Host "  ERREUR: CRX introuvable: $crxSrc" -ForegroundColor Red
        Write-Host "  Execute d'abord: go run ./tools/crxgen" -ForegroundColor Yellow
        return
    }
    Copy-Item $crxSrc "$chromeDir\levoile.crx" -Force
    Write-Host "  CRX copie: $chromeDir\levoile.crx"

    # Generer updates.xml
    $updatesXml = @"
<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='$extId'>
    <updatecheck codebase='file:///C:/ProgramData/LeVoile/extension/chrome/levoile.crx' version='1.0.0' />
  </app>
</gupdate>
"@
    $utf8NoBom = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText("$chromeDir\updates.xml", $updatesXml, $utf8NoBom)
    Write-Host "  updates.xml genere: $chromeDir\updates.xml"

    # Generer le XPI Firefox (ZIP avec manifest_firefox.json renomme en manifest.json)
    $xpiPath = "$deployDir\levoile.xpi"
    $tempZipDir = "$env:TEMP\levoile-xpi-build"
    if (Test-Path $tempZipDir) { Remove-Item -Recurse -Force $tempZipDir }
    New-Item -Path $tempZipDir -ItemType Directory -Force | Out-Null

    # Copier les fichiers source
    Copy-Item "$projectRoot\extension\background.js" "$tempZipDir\background.js"
    Copy-Item "$projectRoot\extension\manifest_firefox.json" "$tempZipDir\manifest.json"
    New-Item -Path "$tempZipDir\icons" -ItemType Directory -Force | Out-Null
    Copy-Item "$projectRoot\extension\icons\*" "$tempZipDir\icons\" -Force

    # Creer le ZIP puis renommer en .xpi (Compress-Archive n'accepte que .zip)
    $zipPath = "$deployDir\levoile.zip"
    if (Test-Path $xpiPath) { Remove-Item $xpiPath -Force }
    if (Test-Path $zipPath) { Remove-Item $zipPath -Force }
    Compress-Archive -Path "$tempZipDir\*" -DestinationPath $zipPath -Force
    Rename-Item $zipPath $xpiPath -Force
    Remove-Item -Recurse -Force $tempZipDir
    Write-Host "  XPI genere: $xpiPath"

    Write-Host "`n=== Ecriture des politiques registre ===" -ForegroundColor Cyan

    # Chrome ExtensionSettings
    $chromeJson = '{"' + $extId + '":{"installation_mode":"force_installed","update_url":"file:///C:/ProgramData/LeVoile/extension/chrome/updates.xml"}}'

    # Ecrire pour chaque navigateur Chromium
    $chromiumPaths = @(
        "HKLM:\SOFTWARE\Policies\Google\Chrome",
        "HKLM:\SOFTWARE\Policies\Microsoft\Edge"
    )

    foreach ($path in $chromiumPaths) {
        New-Item -Path $path -Force -ErrorAction SilentlyContinue | Out-Null
        Set-ItemProperty -Path $path -Name "ExtensionSettings" -Value $chromeJson -Type String
        Write-Host "  Ecrit: $path\ExtensionSettings"
    }

    # Firefox ExtensionSettings
    $firefoxJson = '{"' + $geckoId + '":{"installation_mode":"force_installed","install_url":"file:///C:/ProgramData/LeVoile/extension/levoile.xpi"}}'
    $firefoxPath = "HKLM:\SOFTWARE\Policies\Mozilla\Firefox"
    New-Item -Path $firefoxPath -Force -ErrorAction SilentlyContinue | Out-Null
    Set-ItemProperty -Path $firefoxPath -Name "ExtensionSettings" -Value $firefoxJson -Type String
    Write-Host "  Ecrit: $firefoxPath\ExtensionSettings"

    Write-Host "`n=== Verification ===" -ForegroundColor Cyan
    Write-Host "  Fichiers deployes:"
    Get-ChildItem -Recurse $deployDir | ForEach-Object { Write-Host "    $($_.FullName)" }

    Write-Host "`n  Politiques registre:"
    foreach ($path in $chromiumPaths) {
        $val = Get-ItemProperty -Path $path -Name "ExtensionSettings" -ErrorAction SilentlyContinue
        if ($val) { Write-Host "    $path = $($val.ExtensionSettings.Substring(0, [Math]::Min(80, $val.ExtensionSettings.Length)))..." }
    }
    $val = Get-ItemProperty -Path $firefoxPath -Name "ExtensionSettings" -ErrorAction SilentlyContinue
    if ($val) { Write-Host "    $firefoxPath = $($val.ExtensionSettings.Substring(0, [Math]::Min(80, $val.ExtensionSettings.Length)))..." }

    Write-Host "`n=== INSTRUCTIONS ===" -ForegroundColor Green
    Write-Host "  1. FERME completement Chrome/Edge/Firefox (verifier dans le Gestionnaire des taches)"
    Write-Host "  2. Rouvre le navigateur"
    Write-Host "  3. Chrome/Edge : va sur chrome://extensions (ou edge://extensions)"
    Write-Host "     -> 'Le Voile' devrait apparaitre avec 'Installe par une regle d'entreprise'"
    Write-Host "  4. Chrome/Edge : va sur chrome://policy (ou edge://policy)"
    Write-Host "     -> ExtensionSettings devrait apparaitre"
    Write-Host "  5. Firefox : va sur about:addons"
    Write-Host "     -> 'Le Voile' devrait apparaitre"
    Write-Host "  6. Firefox : va sur about:policies"
    Write-Host "     -> ExtensionSettings devrait apparaitre"
    Write-Host ""
    Write-Host "  Pour nettoyer : .\scripts\test-extension-install.ps1 -Clean" -ForegroundColor Yellow
}

function Remove-Extension {
    Write-Host "`n=== Nettoyage des politiques ===" -ForegroundColor Cyan

    $chromiumPaths = @(
        "HKLM:\SOFTWARE\Policies\Google\Chrome",
        "HKLM:\SOFTWARE\Policies\Microsoft\Edge"
    )

    foreach ($path in $chromiumPaths) {
        Remove-ItemProperty -Path $path -Name "ExtensionSettings" -ErrorAction SilentlyContinue
        Write-Host "  Supprime: $path\ExtensionSettings"
    }

    Remove-ItemProperty -Path "HKLM:\SOFTWARE\Policies\Mozilla\Firefox" -Name "ExtensionSettings" -ErrorAction SilentlyContinue
    Write-Host "  Supprime: HKLM:\SOFTWARE\Policies\Mozilla\Firefox\ExtensionSettings"

    Write-Host "`n=== Nettoyage des fichiers ===" -ForegroundColor Cyan
    if (Test-Path $deployDir) {
        Remove-Item -Recurse -Force $deployDir
        Write-Host "  Supprime: $deployDir"
    } else {
        Write-Host "  Deja supprime: $deployDir"
    }

    Write-Host "`n=== INSTRUCTIONS ===" -ForegroundColor Green
    Write-Host "  1. FERME completement Chrome/Edge/Firefox"
    Write-Host "  2. Rouvre le navigateur"
    Write-Host "  3. Verifie que 'Le Voile' a DISPARU de chrome://extensions et about:addons"
    Write-Host "  4. Verifie que C:\ProgramData\LeVoile\extension\ n'existe plus"
}

# Point d'entree
if ($Clean) {
    Remove-Extension
} else {
    Install-Extension
}
