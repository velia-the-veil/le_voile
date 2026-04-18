# Build Le Voile Windows installer.
# Prerequisites: goreleaser, makensis
# IMPORTANT: Inject the relay Ed25519 public key in config-default.toml before distribution builds.
param(
    [string]$Version = "0.0.0-dev"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$BuildDir = Join-Path $ScriptDir "build"

Write-Host "=== Le Voile Installer Build (v$Version) ==="

# Check prerequisites
foreach ($cmd in @("goreleaser", "makensis")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Error "ERROR: $cmd is not installed."
        exit 1
    }
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

# Copy binaries from GoReleaser output
Copy-Item (Join-Path $ProjectRoot "dist\service_windows_amd64_v1\levoile-service.exe") $BuildDir
Copy-Item (Join-Path $ProjectRoot "dist\ui_windows_amd64_v1\levoile-ui.exe") (Join-Path $BuildDir "levoile-desktop.exe")

# Copy assets
Copy-Item (Join-Path $ProjectRoot "internal\ui\icons\*.ico") (Join-Path $BuildDir "icons\")
Copy-Item (Join-Path $ScriptDir "config-default.toml") $BuildDir

# Step 3: Compile NSIS installer
Write-Host "--- Compiling NSIS installer ---"
Push-Location $ScriptDir
makensis "/DAPP_VERSION=$Version" levoile.nsi
Pop-Location

Write-Host "=== Build complete: installer\LeVoile-Setup.exe ==="
