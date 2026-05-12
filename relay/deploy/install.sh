#!/bin/bash
# Le Voile - Script de deploiement du relais sur VPS
#
# ATTENTION (fix C7 audit securite 2026-04) :
# Ce script est publie comme asset de release GitHub avec sa signature
# detachee Ed25519 (install.sh.sig). Avant de l'executer en prod, le
# verifier hors-band :
#
#   TAG=vX.Y.Z
#   BASE=https://github.com/velia-the-veil/le_voile/releases/download/${TAG}
#   curl -fLO ${BASE}/install.sh
#   curl -fLO ${BASE}/install.sh.sig
#   curl -fLO ${BASE}/levoile-release.pub
#   # Outil de verification publie dans la meme release :
#   curl -fLO ${BASE}/levoile-verify-linux-amd64
#   chmod +x levoile-verify-linux-amd64
#   ./levoile-verify-linux-amd64 -pubkey levoile-release.pub -file install.sh -sig install.sh.sig
#   # Si "signature OK", alors :
#   sudo bash ./install.sh
#
# Ne jamais executer via "curl | bash" direct sans la verif signature —
# un MITM sur le VPS ou sur GitHub.com romprait toute la chaine d'integrite.
#
# Usage ancien (encore valide apres verif) :
#   scp relay cert.pem key.pem signing.key relay-registry.json deploy/install.sh deploy/levoile-relay.service user@vps:/tmp/
#   ssh user@vps 'sudo bash /tmp/install.sh'
#
# Prerequis:
#   - Binaire relay compile pour linux/amd64
#   - Certificat TLS (cert.pem) et cle privee (key.pem)
#   - Cle de signature Ed25519 (signing.key, base64, 64 octets decodes)
#   - Registry signe (relay-registry.json)
#   - Acces root sur le VPS cible

set -euo pipefail

INSTALL_DIR="/opt/levoile"
SERVICE_USER="levoile"
SERVICE_NAME="levoile-relay"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Verifier la presence des fichiers requis (AC6)
# signing.key et relay-registry.json sont referenes par l'ExecStart du .service
# donc obligatoires pour que le relais demarre sans crash loop.
REQUIRED_FILES=(
    relay
    cert.pem
    key.pem
    signing.key
    relay-registry.json
    "${SERVICE_NAME}.service"
)
for f in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "${SCRIPT_DIR}/${f}" ]; then
        echo "ERREUR: fichier requis manquant: ${SCRIPT_DIR}/${f}" >&2
        echo "Prerequis: placer relay, cert.pem, key.pem, signing.key, relay-registry.json et ${SERVICE_NAME}.service dans le meme repertoire que install.sh" >&2
        exit 1
    fi
done

# Creer utilisateur systeme levoile (sans shell, sans home)
if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

# Creer le repertoire d'installation
mkdir -p "${INSTALL_DIR}"

# Copier le binaire
cp "${SCRIPT_DIR}/relay" "${INSTALL_DIR}/relay"
chmod 755 "${INSTALL_DIR}/relay"

# Copier les certificats TLS avec permissions restrictives dès la copie
install -m 600 "${SCRIPT_DIR}/cert.pem" "${INSTALL_DIR}/cert.pem"
install -m 600 "${SCRIPT_DIR}/key.pem" "${INSTALL_DIR}/key.pem"

# Cle de signature Ed25519 (0600 — secret)
install -m 600 "${SCRIPT_DIR}/signing.key" "${INSTALL_DIR}/signing.key"

# Registry signe (0644 — lu par le handler HTTP, lisible public)
install -m 644 "${SCRIPT_DIR}/relay-registry.json" "${INSTALL_DIR}/relay-registry.json"

# Ajuster les permissions du repertoire
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}"

# Installer le service systemd
cp "${SCRIPT_DIR}/${SERVICE_NAME}.service" "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

# Activer et demarrer le service (AC3: enable --now)
systemctl enable --now "${SERVICE_NAME}"

# ---
# Ops auxiliaires (audit-fixes-relais-2026-04-21) : cert-expiry watchdog +
# deploy hook certbot. Installation OPPORTUNISTE : si les artefacts sont
# presents dans le repertoire de staging, on les pose ; sinon on saute
# pour rester compatible avec les packages de release plus anciens.
if [ -f "${SCRIPT_DIR}/cert-expiry-check.sh" ] \
   && [ -f "${SCRIPT_DIR}/levoile-cert-check.service" ] \
   && [ -f "${SCRIPT_DIR}/levoile-cert-check.timer" ]; then
    install -m 0755 "${SCRIPT_DIR}/cert-expiry-check.sh" "${INSTALL_DIR}/cert-expiry-check.sh"
    install -m 0644 "${SCRIPT_DIR}/levoile-cert-check.service" /etc/systemd/system/levoile-cert-check.service
    install -m 0644 "${SCRIPT_DIR}/levoile-cert-check.timer"   /etc/systemd/system/levoile-cert-check.timer
    systemctl daemon-reload
    systemctl enable --now levoile-cert-check.timer
    echo "Installed: cert expiry watchdog (journalctl -t levoile-cert-expiry)"
fi

if [ -f "${SCRIPT_DIR}/renewal-hook-restart-relay.sh" ]; then
    install -m 0755 -D "${SCRIPT_DIR}/renewal-hook-restart-relay.sh" \
        /etc/letsencrypt/renewal-hooks/deploy/restart-levoile-relay.sh
    echo "Installed: certbot deploy hook (restart levoile-relay on renewal)"
fi

echo "Le Voile relay installed and started successfully."
echo "Check status: systemctl status ${SERVICE_NAME}"
echo "View health:  curl -k https://localhost/health"
