#!/usr/bin/env bash
# Story 7.4 — end-to-end smoke test for the release signing pipeline.
#
# Generates an ephemeral Ed25519 keypair, exercises the signpkg <-> verifypkg
# round-trip, and (when --fast is NOT set) also runs `goreleaser release
# --snapshot` with the key wired into the signs: hook to prove the full
# pipeline end-to-end.
#
# Usage:
#   scripts/test-release-signing.sh               # full smoke (needs CGO Linux toolchain)
#   scripts/test-release-signing.sh --snapshot    # alias
#   scripts/test-release-signing.sh --fast        # skip goreleaser cross-compile
#                                                 # (for Windows dev / cross-compile-less envs)
#
# The --fast mode is sufficient to validate signpkg/verifypkg correctness.
# The full mode additionally proves goreleaser integration. CI runs the full
# mode on ubuntu-latest (.github/workflows/release.yml snapshot-signing job).
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

MODE="full"
for arg in "$@"; do
  case "${arg}" in
    --fast)     MODE="fast" ;;
    --snapshot) MODE="full" ;;
    *)          : ;;
  esac
done

log()  { printf '\033[1;34m[smoke-sign]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[smoke-sign]\033[0m %s\n' "$*" >&2; exit 1; }

# Ephemeral workspace.
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

KEY_BASE="${WORK}/smoke"
log "generating ephemeral signing key: ${KEY_BASE}.{key,pub,pub.pem}"
go run ./cmd/genkey -out "${KEY_BASE}" -pem -force >/dev/null
PUB_B64="$(tr -d '\n\r' < "${KEY_BASE}.pub")"
log "ephemeral public key: ${PUB_B64}"

# Pre-build signpkg and verifypkg.
BIN_DIR="${WORK}/bin"
mkdir -p "${BIN_DIR}"
log "pre-building signpkg + verifypkg -> ${BIN_DIR}/"
go build -o "${BIN_DIR}/signpkg" ./cmd/signpkg
go build -o "${BIN_DIR}/verifypkg" ./cmd/verifypkg
export PATH="${BIN_DIR}:${PATH}"
export LEVOILE_SIGNING_KEY_PATH="${KEY_BASE}.key"

# --- Phase 1 : dummy artifact round-trip (works everywhere) --------------------

log "phase 1 : dummy artifact round-trip"
FIXTURE_DIR="${WORK}/fixtures"
mkdir -p "${FIXTURE_DIR}"
for name in fake-1.0.0.deb fake-1.0.0.rpm fake-1.0.0.apk LeVoile-Setup.exe checksums.txt; do
  printf 'artifact content for %s\n' "${name}" > "${FIXTURE_DIR}/${name}"
done

signpkg -signing-key "${KEY_BASE}.key" \
    -checksums "${FIXTURE_DIR}/checksums.txt" \
    "${FIXTURE_DIR}/fake-1.0.0.deb" \
    "${FIXTURE_DIR}/fake-1.0.0.rpm" \
    "${FIXTURE_DIR}/fake-1.0.0.apk" \
    "${FIXTURE_DIR}/LeVoile-Setup.exe" >/dev/null

PHASE1_OK=0
PHASE1_FAIL=0
for sig in "${FIXTURE_DIR}"/*.sig; do
  artifact="${sig%.sig}"
  if verifypkg -pubkey "${PUB_B64}" "${artifact}" "${sig}" >/dev/null 2>&1; then
    log "  ✓ ${artifact##*/}"
    PHASE1_OK=$((PHASE1_OK + 1))
  else
    log "  ✗ ${artifact##*/} (signature mismatch)"
    PHASE1_FAIL=$((PHASE1_FAIL + 1))
  fi
done

# Negative test : tampered artifact must be rejected.
TAMPERED="${FIXTURE_DIR}/fake-1.0.0.deb"
printf 'tampered content\n' > "${TAMPERED}"
if verifypkg -pubkey "${PUB_B64}" "${TAMPERED}" "${TAMPERED}.sig" >/dev/null 2>&1; then
  fail "phase 1 negative test FAILED: tampered artifact was accepted"
fi
log "  ✓ tampered artifact correctly rejected"

if [[ ${PHASE1_FAIL} -gt 0 ]]; then
  fail "phase 1: ${PHASE1_FAIL} signatures failed verification"
fi
log "phase 1 OK (${PHASE1_OK} round-trips)"

# --- Phase 3 (runs in both fast & full modes when maintainer key is available) --
# Exercises the DEFAULT (embedded-key) verification path — the one end users
# actually hit with `levoile-verify artifact artifact.sig`. We can only do this
# if the caller-supplied key matches the constant baked into the binary.
MAINTAINER_KEY="${LEVOILE_MAINTAINER_KEY_PATH:-${HOME}/.levoile/signing.key}"
if [[ -f "${MAINTAINER_KEY}" ]]; then
  PROBE_FILE="${WORK}/phase3-probe.bin"
  echo "phase3 probe: exercises default (embedded key) verification path" > "${PROBE_FILE}"
  if "${BIN_DIR}/signpkg" -signing-key "${MAINTAINER_KEY}" "${PROBE_FILE}" >/dev/null 2>&1; then
    if "${BIN_DIR}/verifypkg" "${PROBE_FILE}" "${PROBE_FILE}.sig" >/dev/null 2>&1; then
      log "phase 3 OK (maintainer key ${MAINTAINER_KEY} matches embedded current)"
    else
      log "phase 3 SKIP: ${MAINTAINER_KEY} does not match embedded ReleasePublicKeyCurrent"
      log "  (fine for contributor/CI runs with throwaway keys; release-sign.sh would refuse)"
    fi
  else
    log "phase 3 SKIP: signpkg rejected ${MAINTAINER_KEY} (malformed or unreadable)"
  fi
else
  log "phase 3 SKIP: no maintainer key at ${MAINTAINER_KEY} (expected for CI / contributors)"
fi

if [[ "${MODE}" == "fast" ]]; then
  log "skipping goreleaser phase (--fast)"
  log "smoke OK (fast mode)"
  exit 0
fi

# --- Phase 2 : goreleaser snapshot build (needs CGO cross-compile) ------------

log "phase 2 : goreleaser snapshot build + sign all artifacts"
log "running: goreleaser release --snapshot --skip=publish --clean"
goreleaser release --snapshot --skip=publish --clean >"${WORK}/goreleaser.log" 2>&1 || {
  tail -60 "${WORK}/goreleaser.log" >&2
  fail "goreleaser failed — see log above (may need CGO Linux toolchain; retry with --fast on Windows)"
}
log "goreleaser snapshot OK"

# Verify every .sig in dist/ against the ephemeral pubkey.
SIG_COUNT=0
FAIL_COUNT=0
while IFS= read -r -d '' sig; do
  artifact="${sig%.sig}"
  if [[ ! -f "${artifact}" ]]; then
    continue
  fi
  SIG_COUNT=$((SIG_COUNT + 1))
  if verifypkg -pubkey "${PUB_B64}" "${artifact}" "${sig}" >/dev/null 2>&1; then
    log "  ✓ ${artifact##${REPO_ROOT}/}"
  else
    log "  ✗ ${artifact##${REPO_ROOT}/} (signature mismatch)"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
done < <(find dist -name '*.sig' -print0 2>/dev/null || true)

if [[ ${SIG_COUNT} -eq 0 ]]; then
  fail "no .sig files produced — goreleaser signs: block not wired correctly"
fi
if [[ ${FAIL_COUNT} -gt 0 ]]; then
  fail "${FAIL_COUNT}/${SIG_COUNT} signatures failed verification"
fi

log "phase 2 OK (${SIG_COUNT} real artifacts signed + verified)"

log "smoke OK (full mode)"
