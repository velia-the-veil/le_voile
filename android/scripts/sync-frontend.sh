#!/usr/bin/env bash
# Story 11.1 — Sync idempotent des assets web Le Voile vers app/src/main/assets/web/.
#
# DÉCISION DEV (Option 2 documentée Story 11.1) : ne pas synchroniser depuis
# `windows/frontend/` (qui est lourdement Windows-spécifique : titlebar custom,
# sidebar pays, /api/* HTTP server, modals desktop). Les assets Android sont
# minimalistes et auto-portés (cohérent ADR-08 isolation OS, mémoire user).
#
# Le script sert d'idempotency check + structure pour Story 12.2 CI : il vérifie
# la présence des fichiers attendus dans web/ et exit 0 si tout est OK.
# Si une story future décide d'introduire un vrai sync depuis windows/frontend/,
# ce script sera enrichi (toolkit déjà en place : detect repo root, paths).

set -euo pipefail
IFS=$'\n\t'

# Fallback robuste : on calcule à partir du chemin du script. Évite git
# rev-parse qui renvoie un Windows path avec séparateurs mixtes sous
# Git-for-Windows / MSYS, cassant le concat suivant.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DEST="${REPO_ROOT}/android/app/src/main/assets/web"

start_ms=$(date +%s%3N 2>/dev/null || date +%s)

echo "[sync-frontend] Repo root: ${REPO_ROOT}"
echo "[sync-frontend] Destination: ${DEST}"

mkdir -p "${DEST}"

# Vérification idempotente : fichiers Android-natifs versionnés (cf. .gitignore).
required=(index.html style.css app.js style-android.css)
missing=0
for f in "${required[@]}"; do
  if [[ ! -f "${DEST}/${f}" ]]; then
    echo "[sync-frontend] MANQUANT : web/${f}" >&2
    missing=1
  fi
done

if [[ ${missing} -ne 0 ]]; then
  echo "[sync-frontend] Au moins un fichier requis est absent — vérifier le commit Story 11.1." >&2
  exit 2
fi

end_ms=$(date +%s%3N 2>/dev/null || date +%s)
echo "[sync-frontend] OK — ${#required[@]} fichiers présents, idempotent re-run safe"
echo "[sync-frontend] Elapsed: $((end_ms - start_ms)) ms"
exit 0
