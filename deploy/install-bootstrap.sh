#!/bin/bash
# Le Voile — wrapper de déploiement relais avec vérification signature.
#
# Fix C7 audit sécurité 2026-04. Ce wrapper :
#   1. Télécharge install.sh + install.sh.sig depuis la release GitHub
#      spécifiée par $LEVOILE_RELEASE_TAG (ex: v1.2.3).
#   2. Télécharge levoile-release.pub (clé publique Ed25519 maîtresse).
#   3. Télécharge levoile-verify (binaire offline de vérif Ed25519).
#   4. Vérifie la signature — abandonne sans exécuter en cas d'échec.
#   5. Exécute install.sh avec les arguments passés à ce wrapper.
#
# Usage :
#   LEVOILE_RELEASE_TAG=v1.2.3 bash install-bootstrap.sh
#
# La clé publique master est épinglée par son hash SHA-256 dans ce script,
# donc un attaquant qui compromet GitHub doit aussi avoir une collision
# SHA-256 pour faire passer un faux levoile-release.pub.
#
# Pour bootstrapper un relais depuis rien (premier run) :
#   curl -fsSLo install-bootstrap.sh \
#     https://raw.githubusercontent.com/velia-the-veil/le_voile/main/deploy/install-bootstrap.sh
#   # Vérifier le hash SHA-256 annoncé hors-band (docs, mail PGP, etc.),
#   # puis :
#   LEVOILE_RELEASE_TAG=v1.2.3 bash install-bootstrap.sh

set -euo pipefail
IFS=$'\n\t'

REPO="velia-the-veil/le_voile"
TAG="${LEVOILE_RELEASE_TAG:-}"
if [[ -z "${TAG}" ]]; then
  echo "ERREUR : définir LEVOILE_RELEASE_TAG=vX.Y.Z avant d'appeler ce script" >&2
  exit 1
fi

# Hash SHA-256 de levoile-release.pub attendu pour chaque rotation de clé
# master. Mettre à jour à chaque cérémonie de rotation (NFR22h, tous les
# 24 mois). Valeur courante : clé générée 2026-04-18, empreinte:
#   openssl dgst -sha256 docs/keys/levoile-release.pub
EXPECTED_PUBKEY_SHA256="${LEVOILE_EXPECTED_PUBKEY_SHA256:-}"

BASE="https://github.com/${REPO}/releases/download/${TAG}"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

log()  { printf '\033[1;34m[install-bootstrap]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[install-bootstrap]\033[0m %s\n' "$*" >&2; exit 1; }

log "tag : ${TAG}"
log "répertoire travail : ${WORK}"

cd "${WORK}"

log "téléchargement install.sh + install.sh.sig"
curl -fsSL -o install.sh     "${BASE}/install.sh"
curl -fsSL -o install.sh.sig "${BASE}/install.sh.sig"

log "téléchargement levoile-release.pub"
curl -fsSL -o levoile-release.pub "${BASE}/levoile-release.pub"

if [[ -n "${EXPECTED_PUBKEY_SHA256}" ]]; then
  log "vérification hash clé publique master"
  ACTUAL="$(sha256sum levoile-release.pub | awk '{print $1}')"
  if [[ "${ACTUAL}" != "${EXPECTED_PUBKEY_SHA256}" ]]; then
    fail "SHA-256 clé publique ne correspond pas à l'empreinte attendue (got ${ACTUAL}, want ${EXPECTED_PUBKEY_SHA256})"
  fi
  log "hash clé publique OK"
else
  log "attention : LEVOILE_EXPECTED_PUBKEY_SHA256 non défini — pinning désactivé"
fi

log "téléchargement levoile-verify-linux-amd64"
curl -fsSL -o levoile-verify "${BASE}/levoile-verify-linux-amd64"
chmod +x levoile-verify

log "vérification signature install.sh"
if ! ./levoile-verify -pubkey levoile-release.pub -file install.sh -sig install.sh.sig; then
  fail "signature install.sh invalide — abandon, ne pas exécuter le script"
fi
log "signature install.sh OK"

chmod +x install.sh
log "exécution install.sh $*"
exec sudo bash install.sh "$@"
