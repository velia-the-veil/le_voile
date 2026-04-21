#!/bin/bash
# Le Voile - Deploy hook invoqué par certbot après un renouvellement réussi.
#
# Le relais appelle tls.LoadX509KeyPair UNE fois au démarrage (internal/relay/server.go)
# et garde la tls.Certificate parsée en mémoire — donc il ne repère pas un cert
# renouvelé sans restart. Ce hook force le restart après renewal.
#
# Déployé dans : /etc/letsencrypt/renewal-hooks/deploy/restart-levoile-relay.sh
# Exécuté par  : certbot, seulement quand un cert a été effectivement renouvelé.
# Downtime     : ~1-2 s TCP/443 toutes les ~90 jours.
logger -t levoile-cert-deploy "restarting levoile-relay after renewal of ${RENEWED_LINEAGE:-unknown}"
systemctl restart levoile-relay
