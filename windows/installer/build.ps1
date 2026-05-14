# Build Le Voile Windows installer.
# Prerequisites: goreleaser, makensis, windows\internal\tun\wintun\wintun.dll
# (run `bash windows/scripts/fetch-wintun.sh` if missing).
# IMPORTANT: Inject the relay Ed25519 public key in config-default.toml before distribution builds.
param(
    [string]$Version = "0.0.0-dev"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
# ScriptDir is windows\installer\ ; WindowsRoot is one level up ; ProjectRoot is two levels up.
$WindowsRoot = Split-Path -Parent $ScriptDir
$ProjectRoot = Split-Path -Parent $WindowsRoot
$BuildDir = Join-Path $ScriptDir "build"
$WintunSrc = Join-Path $WindowsRoot "internal\tun\wintun\wintun.dll"

Write-Host "=== Le Voile Installer Build (v$Version) ==="

# Check prerequisites
foreach ($cmd in @("goreleaser", "makensis")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Error "ERROR: $cmd is not installed."
        exit 1
    }
}

# Story 7.1 — wintun.dll must be present for NSIS to bundle it into Program Files.
if (-not (Test-Path $WintunSrc)) {
    Write-Error "ERROR: $WintunSrc missing. Run 'bash windows/scripts/fetch-wintun.sh' first."
    exit 1
}

# Step 1: Build binaries with GoReleaser
# --single-target restricts to the host OS/arch so a Windows developer box
# doesn't try to cross-compile the Linux ui/service targets (CGO webview)
# without a Linux toolchain. This installer only needs the Windows binaries
# anyway (service + ui + ctl + verify).
#
# Note : ajout Git\usr\bin au PATH pour les before.hooks goreleaser (cp, mkdir -p)
# qui sont POSIX et absents du PATH PowerShell natif. Le hook tente d'invoquer
# `cp ../LICENSE LICENSE` (workaround bug glob ../LICENSE sur drive Windows).
$gitUsrBin = "C:\Program Files\Git\usr\bin"
if ((Test-Path $gitUsrBin) -and ($env:PATH -notlike "*$gitUsrBin*")) {
    $env:PATH = "$gitUsrBin;$env:PATH"
}

# Note 2 : goreleaser build (sans `release`) n'invoque PAS les before.hooks
# par défaut → il faut les exécuter manuellement, OU passer par `goreleaser release --snapshot`
# qui les invoque. On reste sur `build` (simple binaires Windows pour NSIS)
# et on copie LICENSE + clés à la main.
Write-Host "--- Building binaries with GoReleaser ---"
Push-Location $WindowsRoot
goreleaser build --snapshot --clean --single-target --config .goreleaser.yaml
Pop-Location

# Step 2: Prepare build directory
Write-Host "--- Preparing build directory ---"
if (Test-Path $BuildDir) { Remove-Item -Recurse -Force $BuildDir }
New-Item -ItemType Directory -Path $BuildDir -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $BuildDir "icons") -Force | Out-Null

# Copy binaries from GoReleaser output (Story 7.1 — preserve canonical names).
Copy-Item (Join-Path $WindowsRoot "dist\service_windows_amd64_v1\levoile-service.exe") $BuildDir
Copy-Item (Join-Path $WindowsRoot "dist\ui_windows_amd64_v1\levoile-ui.exe") $BuildDir
Copy-Item (Join-Path $WindowsRoot "dist\ctl_windows_amd64_v1\levoile-ctl.exe") $BuildDir

# Copy Wintun DLL (Story 7.1 — bundled into Program Files for auditability).
Copy-Item $WintunSrc (Join-Path $BuildDir "wintun.dll")

# Copy status icons (Story 5.x — UI tray icons live next to the UI source).
Copy-Item (Join-Path $WindowsRoot "internal\ui\icons\connected.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $WindowsRoot "internal\ui\icons\connecting.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $WindowsRoot "internal\ui\icons\disconnected.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $ScriptDir "config-default.toml") $BuildDir

# Step 3: Compile NSIS installer
Write-Host "--- Compiling NSIS installer ---"
Push-Location $ScriptDir
makensis "/DAPP_VERSION=$Version" levoile.nsi
Pop-Location

Write-Host "=== Build complete: installer\LeVoile-Setup.exe ==="
