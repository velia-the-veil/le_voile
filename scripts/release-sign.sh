#!/usr/bin/env bash
# Story 7.4 — release signing from maintainer machine.
#
# This script is the authoritative entry point for real releases. Unlike
# scripts/test-release-signing.sh (smoke test with ephemeral key), this
# expects the maintainer's long-term Ed25519 master key at
# $LEVOILE_SIGNING_KEY_PATH (mode 0600, never in CI secrets — NFR22g).
#
# Pre-flight checks: git clean, tag matches version, tests green, security
# gates green (NFR22d/e/f). Then invokes goreleaser release with the signing
# hook wired in .goreleaser.yaml (signs: ed25519-master).
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

LEVOILE_SIGNING_KEY_PATH="${LEVOILE_SIGNING_KEY_PATH:-${HOME}/.levoile/signing.key}"
export LEVOILE_SIGNING_KEY_PATH

log()  { printf '\033[1;34m[release-sign]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[release-sign]\033[0m %s\n' "$*" >&2; exit 1; }

log "pre-flight checks"

# 1. Git state.
if ! git diff-index --quiet HEAD --; then
  fail "working tree is not clean — commit or stash before releasing"
fi

# 2. Tag / version.
TAG="$(git describe --tags --exact-match HEAD 2>/dev/null || true)"
if [[ -z "${TAG}" ]]; then
  fail "HEAD is not tagged — create an annotated tag vX.Y.Z before releasing"
fi
log "releasing tag: ${TAG}"

# 3. Signing key present + mode 0600 (Unix) / note-only (Windows).
if [[ ! -f "${LEVOILE_SIGNING_KEY_PATH}" ]]; then
  fail "signing key not found: ${LEVOILE_SIGNING_KEY_PATH}
         generate one with: go run ./cmd/genkey -out \"\$HOME/.levoile/signing\" -pem"
fi
PERM="$(stat -c %a "${LEVOILE_SIGNING_KEY_PATH}" 2>/dev/null || stat -f %Lp "${LEVOILE_SIGNING_KEY_PATH}" 2>/dev/null || echo "unknown")"
case "${PERM}" in
  600|0600) log "signing key mode OK (0600)" ;;
  unknown)  log "warning: cannot stat permissions — verify ACLs manually (Windows?)" ;;
  *)        fail "signing key must be mode 0600 (got ${PERM})" ;;
esac

# 4. Tests green.
log "go test -race -count=1 ./..."
go test -race -count=1 ./cmd/... ./internal/... >/dev/null

# 5. Security gates (NFR22d/e/f).
log "go vet ./..."
go vet ./...
if command -v govulncheck >/dev/null 2>&1; then
  log "govulncheck ./..."
  govulncheck ./... >/dev/null
else
  log "warning: govulncheck not installed — skipping (install: go install golang.org/x/vuln/cmd/govulncheck@latest)"
fi
if command -v gosec >/dev/null 2>&1; then
  log "gosec -severity medium ./..."
  gosec -severity medium -quiet ./... >/dev/null
else
  log "warning: gosec not installed — skipping (install: go install github.com/securego/gosec/v2/cmd/gosec@latest)"
fi

# 6. Build signpkg first — goreleaser will invoke it via cmd: in signs:.
# This ensures it exists in PATH for the duration of the release.
log "installing signpkg to local PATH"
TMP_BIN="$(mktemp -d)"
trap 'rm -rf "${TMP_BIN}"' EXIT
go build -o "${TMP_BIN}/signpkg" ./cmd/signpkg
export PATH="${TMP_BIN}:${PATH}"

log "reminder: if this machine is online, consider disconnecting network before release"
log "running: goreleaser release --clean"
goreleaser release --clean

log "release ${TAG} signed and published"
