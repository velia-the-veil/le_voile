#!/usr/bin/env bash
# Release-sign for the Windows tier (story 7.4, post per-tier isolation refacto).
#
# Self-contained release flow for the Windows binaries :
#   1. Pre-flight checks (git clean, current tag, signing key, security gates).
#   2. Builds tools/signpkg into a tmpdir, adds it to PATH so the goreleaser
#      `signs:` block can invoke it without depending on a pre-installed binary.
#   3. Invokes `goreleaser release --clean --config .goreleaser.yaml` from
#      windows/ — produces dist/ binaries, archive, signatures, plus the
#      docs/keys assets.
#
# Run from repo root or windows/ — both work; the script chdirs to windows/.
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WINDOWS_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${WINDOWS_ROOT}/.." && pwd)"
cd "${WINDOWS_ROOT}"

LEVOILE_SIGNING_KEY_PATH="${LEVOILE_SIGNING_KEY_PATH:-${HOME}/.levoile/signing.key}"
export LEVOILE_SIGNING_KEY_PATH

log()  { printf '\033[1;34m[windows-release]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[windows-release]\033[0m %s\n' "$*" >&2; exit 1; }

log "pre-flight checks (Windows tier)"

# 1. Git state.
if ! git diff-index --quiet HEAD --; then
  fail "working tree is not clean — commit or stash before releasing"
fi

# 2. Tag / version.
TAG="$(git describe --tags --exact-match HEAD 2>/dev/null || true)"
if [[ -z "${TAG}" ]]; then
  fail "HEAD is not tagged — create an annotated tag (windows-vX.Y.Z) before releasing"
fi
log "releasing tag: ${TAG}"

# 3. Signing key present + mode 0600 (Unix) / note-only (Windows ACL).
if [[ ! -f "${LEVOILE_SIGNING_KEY_PATH}" ]]; then
  fail "signing key not found: ${LEVOILE_SIGNING_KEY_PATH}
         generate one with: go run ./tools/genkey -out \"\$HOME/.levoile/signing\" -pem"
fi
OSTYPE_LOWER="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OSTYPE_LOWER}" in
  *mingw*|*msys*|*cygwin*)
    log "running on ${OSTYPE_LOWER} — POSIX mode unreliable, trusting NTFS ACL (verify with icacls if unsure)"
    ;;
  *)
    PERM="$(stat -c %a "${LEVOILE_SIGNING_KEY_PATH}" 2>/dev/null || stat -f %Lp "${LEVOILE_SIGNING_KEY_PATH}" 2>/dev/null || echo "unknown")"
    case "${PERM}" in
      600|0600) log "signing key mode OK (0600)" ;;
      unknown)  log "warning: cannot stat permissions — verify ACLs manually" ;;
      *)        fail "signing key must be mode 0600 (got ${PERM})" ;;
    esac
    ;;
esac

# 4. Wintun DLL must be present (embedded into levoile-service via go:embed).
log "ensuring wintun.dll is present..."
bash "${SCRIPT_DIR}/fetch-wintun.sh"

# 5. Tests green (Windows tier).
# Escape hatch: LEVOILE_SKIP_LOCAL_TESTS=1 if CI on the same commit is green.
if [[ "${LEVOILE_SKIP_LOCAL_TESTS:-0}" = "1" ]]; then
  log "skipping local tests (LEVOILE_SKIP_LOCAL_TESTS=1)"
else
  log "go test -race -count=1 ./..."
  go test -race -count=1 ./... >/dev/null
fi

# 6. Security gates (NFR22d/e/f).
log "go vet ./..."
go vet ./...
if command -v govulncheck >/dev/null 2>&1; then
  log "govulncheck ./..."
  govulncheck ./... >/dev/null
else
  log "warning: govulncheck not installed — skipping (install: go install golang.org/x/vuln/cmd/govulncheck@latest)"
fi
if command -v gosec >/dev/null 2>&1; then
  log "gosec -severity medium -quiet ./..."
  gosec -severity medium -quiet ./... >/dev/null
else
  log "warning: gosec not installed — skipping (install: go install github.com/securego/gosec/v2/cmd/gosec@latest)"
fi

# 7. Build signpkg from tools/ (repo root).
log "installing signpkg to local PATH from ${REPO_ROOT}/tools/signpkg"
TMP_BIN="$(mktemp -d)"
trap 'rm -rf "${TMP_BIN}"' EXIT
( cd "${REPO_ROOT}" && go build -o "${TMP_BIN}/signpkg" ./tools/signpkg )
export PATH="${TMP_BIN}:${PATH}"

log "reminder: if this machine is online, consider disconnecting network before release"
log "running: goreleaser release --clean --config .goreleaser.yaml"
goreleaser release --clean --config .goreleaser.yaml

log "Windows release ${TAG} signed and published"
