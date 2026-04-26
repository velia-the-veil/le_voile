#!/usr/bin/env bash
# Build Le Voile Windows installer.
# Prerequisites: goreleaser, makensis, windows/internal/tun/wintun/wintun.dll
# (run `bash windows/scripts/fetch-wintun.sh` if missing).
# IMPORTANT: Inject the relay Ed25519 public key in config-default.toml before distribution builds.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# SCRIPT_DIR is windows/installer/ ; WINDOWS_ROOT is one level up.
WINDOWS_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"
VERSION="${1:-0.0.0-dev}"
WINTUN_SRC="$WINDOWS_ROOT/internal/tun/wintun/wintun.dll"

echo "=== Le Voile Installer Build (v$VERSION) ==="

# Check prerequisites
for cmd in goreleaser makensis; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "ERROR: $cmd is not installed." >&2
    exit 1
  fi
done

# Story 7.1 — wintun.dll must be present for NSIS to bundle it into Program Files.
if [[ ! -f "$WINTUN_SRC" ]]; then
  echo "ERROR: $WINTUN_SRC missing. Run 'bash windows/scripts/fetch-wintun.sh' first." >&2
  exit 1
fi

# Step 1: Build binaries with GoReleaser
# --single-target restricts to the host OS/arch so a Windows developer box
# doesn't try to cross-compile the Linux ui/service targets (CGO webview)
# without a Linux toolchain. This installer only needs the Windows binaries
# anyway (service + ui + ctl + verify).
echo "--- Building binaries with GoReleaser ---"
cd "$WINDOWS_ROOT"
goreleaser build --snapshot --clean --single-target --config .goreleaser.yaml

# Step 2: Prepare build directory
echo "--- Preparing build directory ---"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/icons"

# Copy binaries from GoReleaser output (Story 7.1 — preserve canonical names).
cp dist/service_windows_amd64_v1/levoile-service.exe "$BUILD_DIR/"
cp dist/ui_windows_amd64_v1/levoile-ui.exe "$BUILD_DIR/"
cp dist/ctl_windows_amd64_v1/levoile-ctl.exe "$BUILD_DIR/"

# Copy Wintun DLL (Story 7.1 — bundled into Program Files for auditability).
cp "$WINTUN_SRC" "$BUILD_DIR/wintun.dll"

# Copy status icons (Story 5.x — UI tray icons live next to the UI source).
cp "$WINDOWS_ROOT/internal/ui/icons/connected.ico" "$BUILD_DIR/icons/"
cp "$WINDOWS_ROOT/internal/ui/icons/connecting.ico" "$BUILD_DIR/icons/"
cp "$WINDOWS_ROOT/internal/ui/icons/disconnected.ico" "$BUILD_DIR/icons/"
cp "$SCRIPT_DIR/config-default.toml" "$BUILD_DIR/"

# Step 3: Compile NSIS installer
echo "--- Compiling NSIS installer ---"
cd "$SCRIPT_DIR"
makensis -DAPP_VERSION="$VERSION" levoile.nsi

echo "=== Build complete: windows/installer/LeVoile-Setup.exe ==="
