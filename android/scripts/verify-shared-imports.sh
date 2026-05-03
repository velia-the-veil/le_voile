#!/usr/bin/env bash
#
# verify-shared-imports.sh — Lint frontiere ADR-09.
#
# Story 9.2 — verifie que les packages exposes a Android via gomobile
# (les 5 packages du build-aar.sh + leurs dependances directes critiques
# internal/{tunnel,crypto}) n'importent AUCUN package OS-specifique
# (internal/tun, internal/firewall, internal/routing, internal/ui,
# internal/ipc, internal/wfp, internal/nftables, internal/wintun, etc.).
#
# Periodicite recommandee : avant chaque PR touchant a un package partage,
# et en CI Story 12.2.
#
# Voir : architecture.md l. 1707-1712 (Frontiere noyau partage),
#        ADR-08 (isolation OS), ADR-09 (gomobile noyau partage).

set -euo pipefail

# ---- Localisation repo root ----
if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git non trouve dans le PATH (requis pour resoudre le repo root)." >&2
  exit 2
fi
REPO_ROOT="$(git rev-parse --show-toplevel)"

# ---- Pre-check : go dans le PATH ----
if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go non trouve dans le PATH (requis pour 'go list' qui resout les imports)." >&2
  exit 2
fi

# ---- Module Go racine ----
GO_MODULE="$(awk '/^module /{print $2}' "$REPO_ROOT/go.mod")"
if [ -z "$GO_MODULE" ]; then
  echo "ERROR: impossible de lire 'module' depuis $REPO_ROOT/go.mod." >&2
  exit 2
fi

# ---- Packages a verifier ----
# Les 5 shims gomobile bind (Story 9.2) + les packages racine sources de
# verite. protocol et auth sont inclus defensivement : meme s'ils ne
# re-exportent rien d'internal/ aujourd'hui, Story 9.7 enrichira leur
# surface — la check doit attraper les violations futures sans modif du
# script.
SHARED_PACKAGES=(
  "android/shims/protocol"
  "android/shims/auth"
  "android/shims/crypto"
  "android/shims/registry"
  "android/shims/leakcheck"
  "internal/crypto"
  "internal/registry"
  "internal/leakcheck"
  "internal/tunnel"   # importe transitivement par leakcheck
)

# ---- Patterns d'imports OS-specifiques interdits ----
# Liste fermee de prefixes considere "OS-specifique desktop" — voir
# architecture.md l. 1710. Si un package partage importe un de ces
# prefixes, c'est un bug de frontiere ADR-09 a corriger.
FORBIDDEN_PATTERNS=(
  "${GO_MODULE}/internal/tun"
  "${GO_MODULE}/internal/firewall"
  "${GO_MODULE}/internal/routing"
  "${GO_MODULE}/internal/ui"
  "${GO_MODULE}/internal/ipc"
  "${GO_MODULE}/internal/wfp"
  "${GO_MODULE}/internal/nftables"
  "${GO_MODULE}/internal/wintun"
  "${GO_MODULE}/internal/captive"
  "${GO_MODULE}/internal/httpproxy"
  "${GO_MODULE}/internal/watchdog"
  "${GO_MODULE}/cmd/ui"
  "${GO_MODULE}/cmd/client"
  "${GO_MODULE}/cmd/relay"
  "${GO_MODULE}/cmd/ctl"
  "${GO_MODULE}/cmd/genregistry"
  "${GO_MODULE}/windows/"
  "${GO_MODULE}/linux/"
)

# ---- Verification ----
cd "$REPO_ROOT"
FAIL=0
TOTAL=0

for pkg in "${SHARED_PACKAGES[@]}"; do
  if [ ! -d "$pkg" ]; then
    echo "WARN: package $pkg introuvable (skip)" >&2
    continue
  fi
  TOTAL=$((TOTAL + 1))

  # `.Deps` capture les imports TRANSITIFS (tout l'arbre, stdlib + module).
  # Les FORBIDDEN_PATTERNS commencent tous par ${GO_MODULE}/ donc la stdlib
  # ne genere pas de faux positif. Critique : sans transitive, un package
  # "clean" qui importe un package "clean" qui importe internal/tun
  # passerait entre les mailles.
  IMPORTS="$(go list -f '{{range .Deps}}{{.}}{{"\n"}}{{end}}' "./$pkg" 2>/dev/null || true)"
  if [ -z "$IMPORTS" ]; then
    # Cas legitime : un shim sans aucun import (protocol/auth) — n'importe
    # rien d'externe. Ce n'est pas une erreur.
    echo "INFO: aucun import transitif pour $pkg (package autonome)" >&2
    continue
  fi

  while IFS= read -r imp; do
    [ -z "$imp" ] && continue
    for forbidden in "${FORBIDDEN_PATTERNS[@]}"; do
      case "$imp" in
        "$forbidden"|"$forbidden"/*)
          echo "FAIL  $pkg  ->  $imp  (pattern interdit: $forbidden)" >&2
          FAIL=$((FAIL + 1))
          ;;
      esac
    done
  done <<< "$IMPORTS"
done

echo ""
if [ "$FAIL" -eq 0 ]; then
  echo "[verify-shared-imports] OK"
  echo "  $TOTAL packages partages verifies (frontiere ADR-09)"
  echo "  Aucun import OS-specifique detecte."
  exit 0
else
  echo "[verify-shared-imports] FAIL"
  echo "  $FAIL import(s) interdit(s) detecte(s) sur $TOTAL packages."
  echo "  Corriger les imports avant tout build .aar / merge."
  exit 1
fi
