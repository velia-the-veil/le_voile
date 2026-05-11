#!/usr/bin/env bash
# Story 7.4 smoke test — adapté à la nouvelle architecture OS-isolation 2026-04.
#
# Avant le refactor OS-isolation (2026-04) :
#   - .goreleaser.yaml unique à la racine
#   - script `scripts/test-release-signing.sh` couvrait 3 phases :
#       Phase 1 : signpkg/verifypkg round-trip (cross-platform)
#       Phase 2 : `goreleaser release --snapshot` + signs hook (Linux CGO)
#       Phase 3 : verifypkg avec maintainer key embedded (skip si absent)
#
# Après le refactor (commit 2c3386e) :
#   - .goreleaser.yaml a été déplacé sous linux/, relay/, windows/, android/
#   - Le script smoke a été supprimé sans remplacement
#   - Le CI release.yml référençait toujours scripts/test-release-signing.sh
#     → fail systématique avec exit 127 "no such file or directory"
#
# Cette version restaurée garde Phase 1 + Phase 3 (cross-platform, validate
# la chaîne tools/signpkg ↔ tools/verifypkg ↔ tools/genkey). Phase 2
# (goreleaser) est désactivée par défaut — le maintainer la lance per-OS
# (`linux/scripts/release-sign.sh`, `relay/scripts/release-sign.sh`, etc.)
# au moment de la vraie release. Le smoke CI vérifie juste que les outils de
# signature/vérification fonctionnent — c'est suffisant pour bloquer toute
# régression sur la chaîne crypto.
#
# Usage :
#   scripts/test-release-signing.sh           # Phase 1 + Phase 3 (CI mode)
#   scripts/test-release-signing.sh --fast    # alias (équivalent par défaut)
#   scripts/test-release-signing.sh --full    # tente Phase 2 (skip si pas
#                                              # de .goreleaser.yaml à la racine)

set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

MODE="fast"
for arg in "$@"; do
  case "${arg}" in
    --fast)     MODE="fast" ;;
    --full)     MODE="full" ;;
    --snapshot) MODE="full" ;;  # legacy alias
    *)          : ;;
  esac
done

log()  { printf '\033[1;34m[smoke-sign]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[smoke-sign]\033[0m %s\n' "$*" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

KEY_BASE="${WORK}/smoke"
log "generating ephemeral signing key: ${KEY_BASE}.{key,pub,pub.pem}"
go run ./tools/genkey -out "${KEY_BASE}" -pem -force >/dev/null
PUB_B64="$(tr -d '\n\r' < "${KEY_BASE}.pub")"
log "ephemeral public key: ${PUB_B64}"

BIN_DIR="${WORK}/bin"
mkdir -p "${BIN_DIR}"
log "pre-building signpkg + verifypkg -> ${BIN_DIR}/"
go build -o "${BIN_DIR}/signpkg" ./tools/signpkg
go build -o "${BIN_DIR}/verifypkg" ./tools/verifypkg
export PATH="${BIN_DIR}:${PATH}"
export LEVOILE_SIGNING_KEY_PATH="${KEY_BASE}.key"

# --- Phase 1 : dummy artifact round-trip (cross-platform) ---------------------

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

# --- Phase 3 : maintainer-key path (skip if no key) ---------------------------

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

# --- Phase 2 : goreleaser snapshot (full mode only) ---------------------------

if [[ "${MODE}" == "fast" ]]; then
  log "phase 2 SKIP (--fast — default in CI post-OS-isolation)"
  log "smoke OK (fast mode)"
  exit 0
fi

# Architecture post-2026-04 : .goreleaser.yaml a été déplacé per-OS. On
# n'a plus un goreleaser unique à la racine. Soit on lance les 3-4
# configs séparément (long + cross-compile cgo complexe), soit on skip
# en CI et on fait confiance aux `linux/scripts/release-sign.sh` etc.
# pour valider la chaîne complète au moment de la vraie release locale.
if [[ ! -f ".goreleaser.yaml" ]] && [[ ! -f ".goreleaser.yml" ]]; then
  log "phase 2 SKIP: no .goreleaser.yaml at repo root (OS-isolation refactor)"
  log "  → run per-OS scripts for full coverage : linux/scripts/release-sign.sh,"
  log "    relay/scripts/release-sign.sh, windows/scripts/release-sign.sh"
  log "smoke OK (full mode, goreleaser phase skipped)"
  exit 0
fi

log "phase 2 : goreleaser snapshot build + sign all artifacts"
goreleaser release --snapshot --skip=publish --clean >"${WORK}/goreleaser.log" 2>&1 || {
  tail -60 "${WORK}/goreleaser.log" >&2
  fail "goreleaser failed — see log above"
}
log "goreleaser snapshot OK"

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
