#!/usr/bin/env bash
# Story 7.4 — helper interactif pour sauvegarder la master key Ed25519.
#
# Ce script n'est PAS automatisable : il demande la passphrase GPG et le
# découpage SSSS à la main, par design (un attaquant qui automatise ton
# script peut forger une sauvegarde qu'il contrôle). Exécuter physiquement
# sur la machine mainteneur, de préférence hors-ligne.
#
# Sorties :
#   ./backup/signing.key.gpg         — sauvegarde chiffrée GPG symétrique
#   ./backup/signing.key.ssss.part{1,2,3}  — 3 parts Shamir's Secret Sharing
#                                            (2 parts suffisent à reconstruire)
#
# Voir docs/release-signing.md §3 pour la procédure complète et les lieux
# de stockage recommandés.
set -euo pipefail
IFS=$'\n\t'

KEY_PATH="${LEVOILE_SIGNING_KEY_PATH:-${HOME}/.levoile/signing.key}"
BACKUP_DIR="${BACKUP_DIR:-./backup}"

log()  { printf '\033[1;34m[backup]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[backup]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[backup]\033[0m %s\n' "$*" >&2; exit 1; }

if [[ ! -f "${KEY_PATH}" ]]; then
  fail "signing key not found: ${KEY_PATH}"
fi

mkdir -p "${BACKUP_DIR}"
chmod 0700 "${BACKUP_DIR}" 2>/dev/null || true

# --- Étape 1 : GPG symmetric ---------------------------------------------------

if ! command -v gpg >/dev/null 2>&1; then
  warn "gpg introuvable — saute l'étape GPG. Installer GnuPG (https://gnupg.org) pour activer."
else
  log "étape 1/2 : sauvegarde chiffrée GPG AES256"
  log "  choisir une passphrase forte ≥ 20 caractères, à stocker dans un gestionnaire offline"
  if [[ -f "${BACKUP_DIR}/signing.key.gpg" ]]; then
    warn "signing.key.gpg existe déjà — supprimer d'abord pour regénérer"
  else
    gpg --symmetric --cipher-algo AES256 \
        --output "${BACKUP_DIR}/signing.key.gpg" \
        "${KEY_PATH}"
    log "  ✓ ${BACKUP_DIR}/signing.key.gpg ($(wc -c < "${BACKUP_DIR}/signing.key.gpg") octets)"
  fi
fi

# --- Étape 2 : Shamir's Secret Sharing -----------------------------------------

if ! command -v ssss-split >/dev/null 2>&1; then
  warn "ssss-split introuvable — saute l'étape SSSS. Installer via :"
  warn "  apt install ssss     (Debian/Ubuntu)"
  warn "  dnf install ssss     (Fedora)"
  warn "  pacman -S ssss       (Arch)"
  warn "  brew install ssss    (macOS)"
  warn "  Windows : WSL ou skip (GPG seul suffit pour MVP)"
else
  log "étape 2/2 : découpage SSSS 2-of-3 (2 parts nécessaires pour reconstruire)"
  log "  label utilisé : levoile-master-$(date +%Y)"
  LABEL="levoile-master-$(date +%Y)"
  # ssss-split prend le secret sur stdin. La clé Ed25519 en base64 fait 88 octets.
  ssss-split -t 2 -n 3 -w "${LABEL}" -Q < "${KEY_PATH}" \
      > "${BACKUP_DIR}/signing.key.ssss.parts"
  # Sépare les 3 parts en fichiers individuels (1 par ligne). Le base path
  # est passé via -v pour éviter tout soucis de quoting si BACKUP_DIR
  # contient des espaces ou caractères spéciaux.
  awk -v base="${BACKUP_DIR}" 'NF {print > (base "/signing.key.ssss.part" NR)}' "${BACKUP_DIR}/signing.key.ssss.parts"
  rm "${BACKUP_DIR}/signing.key.ssss.parts"
  log "  ✓ ${BACKUP_DIR}/signing.key.ssss.part{1,2,3}"
  log "  répartir les 3 parts dans 3 lieux physiques distincts (coffre, ami scellé,"
  log "  boîte postale louée). 2 parts reconstruisent la clé via :"
  log "    cat part1 part2 | ssss-combine -t 2"
fi

log ""
log "✅ sauvegardes prêtes dans ${BACKUP_DIR}/"
log ""
log "ACTIONS manuelles à faire MAINTENANT :"
log "  1. Distribuer les parts SSSS physiquement (NE PAS les laisser sur disque)"
log "  2. Copier signing.key.gpg sur clé USB chiffrée (2 copies, 2 lieux)"
log "  3. Tester la restauration sur une VM éphémère (docs/release-signing.md §3)"
log "  4. shred -u ${BACKUP_DIR}/signing.key.ssss.part* après distribution"
log ""
log "⚠️  Ne JAMAIS committer ${BACKUP_DIR}/ — le .gitignore racine bloque déjà"
log "    tout chemin contenant /backup/ mais vérifier avant chaque commit."
