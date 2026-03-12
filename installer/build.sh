#!/usr/bin/env bash
# Build Le Voile Windows installer.
# Prerequisites: goreleaser, makensis
# IMPORTANT: Inject the relay Ed25519 public key in config-default.toml before distribution builds.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"
VERSION="${1:-0.0.0-dev}"

echo "=== Le Voile Installer Build (v$VERSION) ==="

# Check prerequisites
for cmd in goreleaser makensis; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: $cmd is not installed." >&2
    exit 1
  fi
done

# Step 1: Build binaries with GoReleaser
echo "--- Building binaries with GoReleaser ---"
cd "$PROJECT_ROOT"
goreleaser build --snapshot --clean

# Step 2: Prepare build directory
echo "--- Preparing build directory ---"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/icons"

# Copy binaries from GoReleaser output
cp dist/service_windows_amd64_v1/levoile-service.exe "$BUILD_DIR/"
cp dist/tray_windows_amd64_v1/levoile-tray.exe "$BUILD_DIR/"

# Copy assets
cp "$PROJECT_ROOT/assets/icons/"*.ico "$BUILD_DIR/icons/"
cp "$SCRIPT_DIR/config-default.toml" "$BUILD_DIR/"

# Step 3: Compile NSIS installer
echo "--- Compiling NSIS installer ---"
cd "$SCRIPT_DIR"
makensis /DAPP_VERSION="$VERSION" levoile.nsi

echo "=== Build complete: installer/LeVoile-Setup.exe ==="
