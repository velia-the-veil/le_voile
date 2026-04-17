#!/bin/bash
# Le Voile - Script de deploiement du relais sur VPS
#
# Usage:
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

echo "Le Voile relay installed and started successfully."
echo "Check status: systemctl status ${SERVICE_NAME}"
echo "View health:  curl -k https://localhost/health"
