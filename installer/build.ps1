# Build Le Voile Windows installer.
# Prerequisites: goreleaser, makensis, internal\tun\wintun\wintun.dll (run `make wintun` if missing).
# IMPORTANT: Inject the relay Ed25519 public key in config-default.toml before distribution builds.
param(
    [string]$Version = "0.0.0-dev"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$BuildDir = Join-Path $ScriptDir "build"
$WintunSrc = Join-Path $ProjectRoot "internal\tun\wintun\wintun.dll"

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
    Write-Error "ERROR: $WintunSrc missing. Run 'make wintun' (or bash scripts/fetch-wintun.sh) first."
    exit 1
}

# Step 1: Build binaries with GoReleaser
Write-Host "--- Building binaries with GoReleaser ---"
Push-Location $ProjectRoot
goreleaser build --snapshot --clean
Pop-Location

# Step 2: Prepare build directory
Write-Host "--- Preparing build directory ---"
if (Test-Path $BuildDir) { Remove-Item -Recurse -Force $BuildDir }
New-Item -ItemType Directory -Path $BuildDir -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $BuildDir "icons") -Force | Out-Null

# Copy binaries from GoReleaser output (Story 7.1 — preserve canonical names).
Copy-Item (Join-Path $ProjectRoot "dist\service_windows_amd64_v1\levoile-service.exe") $BuildDir
Copy-Item (Join-Path $ProjectRoot "dist\ui_windows_amd64_v1\levoile-ui.exe") $BuildDir
Copy-Item (Join-Path $ProjectRoot "dist\ctl-windows_windows_amd64_v1\levoile-ctl.exe") $BuildDir

# Copy Wintun DLL (Story 7.1 — bundled into Program Files for auditability).
Copy-Item $WintunSrc (Join-Path $BuildDir "wintun.dll")

# Copy status icons (Story 5.x — UI tray icons live next to the UI source).
Copy-Item (Join-Path $ProjectRoot "internal\ui\icons\connected.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $ProjectRoot "internal\ui\icons\connecting.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $ProjectRoot "internal\ui\icons\disconnected.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $ScriptDir "config-default.toml") $BuildDir

# Step 3: Compile NSIS installer
Write-Host "--- Compiling NSIS installer ---"
Push-Location $ScriptDir
makensis "/DAPP_VERSION=$Version" levoile.nsi
Pop-Location

Write-Host "=== Build complete: installer\LeVoile-Setup.exe ==="
