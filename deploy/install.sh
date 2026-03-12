#!/bin/bash
# Le Voile - Script de deploiement du relais sur VPS
#
# Usage:
#   scp relay cert.pem key.pem deploy/install.sh user@vps:/tmp/
#   ssh user@vps 'sudo bash /tmp/install.sh'
#
# Prerequis:
#   - Binaire relay compile pour linux/amd64
#   - Certificat TLS (cert.pem) et cle privee (key.pem)
#   - Acces root sur le VPS cible

set -euo pipefail

INSTALL_DIR="/opt/levoile"
SERVICE_USER="levoile"
SERVICE_NAME="levoile-relay"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Creer utilisateur systeme levoile (sans shell, sans home)
if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

# Creer le repertoire d'installation
mkdir -p "${INSTALL_DIR}"

# Copier le binaire
cp "${SCRIPT_DIR}/relay" "${INSTALL_DIR}/relay"
chmod 755 "${INSTALL_DIR}/relay"

# Copier les certificats TLS
cp "${SCRIPT_DIR}/cert.pem" "${INSTALL_DIR}/cert.pem"
cp "${SCRIPT_DIR}/key.pem" "${INSTALL_DIR}/key.pem"
chmod 600 "${INSTALL_DIR}/cert.pem" "${INSTALL_DIR}/key.pem"

# Ajuster les permissions du repertoire
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}"

# Installer le service systemd
cp "${SCRIPT_DIR}/${SERVICE_NAME}.service" "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

# Activer et demarrer le service
systemctl enable "${SERVICE_NAME}"
systemctl start "${SERVICE_NAME}"

echo "Le Voile relay installed and started successfully."
echo "Check status: systemctl status ${SERVICE_NAME}"
echo "View health:  curl -k https://localhost/health"
