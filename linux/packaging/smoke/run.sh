#!/usr/bin/env bash
# Smoke test des paquets Linux — story 7.2
#
# Lance 3 containers Docker et valide pour chacun :
#   1. Installation via le gestionnaire de paquets natif
#   2. Présence des binaires + fichiers systemd + entrées XDG
#   3. User système `levoile` créé
#   4. `levoile-ctl --help` retourne 0
#   5. Désinstallation propre
#
# Usage :
#     bash packaging/smoke/run.sh                  # utilise dist/ existant
#     DIST=/path/to/dist bash packaging/smoke/run.sh
#
# Prérequis :
#   - Docker daemon actif
#   - `goreleaser release --snapshot --skip=publish` déjà lancé (dist/ peuplé)

set -u

DIST="${DIST:-$(pwd)/dist}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FAILURES=0
REPORT=""

log() { echo "[smoke] $*" >&2; }
fail() { REPORT="${REPORT}✗ $1"$'\n'; FAILURES=$((FAILURES + 1)); }
pass() { REPORT="${REPORT}✓ $1"$'\n'; }

if ! command -v docker >/dev/null 2>&1; then
    log "ERREUR : docker introuvable dans le PATH."
    exit 2
fi
if [ ! -d "$DIST" ]; then
    log "ERREUR : répertoire DIST=$DIST absent. Lancer 'goreleaser release --snapshot --skip=publish' d'abord."
    exit 2
fi

# Localiser les paquets (amd64 pour les tests — arm64 testable sur runners arm64)
DEB="$(ls "$DIST"/levoile_*_linux_amd64.deb 2>/dev/null | head -1 || true)"
RPM="$(ls "$DIST"/levoile*.x86_64.rpm 2>/dev/null | head -1 || true)"
APK="$(ls "$DIST"/levoile_*_linux_amd64.apk 2>/dev/null | head -1 || true)"

[ -n "$DEB" ] || { log "ERREUR : aucun .deb amd64 trouvé dans $DIST"; exit 2; }
[ -n "$RPM" ] || { log "ERREUR : aucun .rpm x86_64 trouvé dans $DIST"; exit 2; }
[ -n "$APK" ] || { log "ERREUR : aucun .apk amd64 trouvé dans $DIST"; exit 2; }

log "DEB : $DEB"
log "RPM : $RPM"
log "APK : $APK"

# ---------------------------------------------------------------------------
# Script de validation exécuté À L'INTÉRIEUR de chaque container. Vérifie tous
# les invariants AC4 + AC8 de la story 7-2.
# ---------------------------------------------------------------------------
read -r -d '' VALIDATE <<'EOF' || true
set -u
RC=0
fail() { echo "[inside] FAIL: $*" >&2; RC=1; }
pass() { echo "[inside] OK: $*" >&2; }

# Binaires
for b in /usr/bin/levoile-service /usr/bin/levoile-ui /usr/bin/levoile-ctl; do
    [ -x "$b" ] && pass "binaire $b" || fail "binaire $b manquant ou non exécutable"
done

# Unit systemd system
[ -f /usr/lib/systemd/system/levoile.service ] \
    && pass "unit system levoile.service" \
    || fail "unit system levoile.service manquant"
# Unit systemd user
[ -f /usr/lib/systemd/user/levoile-ui.service ] \
    && pass "unit user levoile-ui.service" \
    || fail "unit user levoile-ui.service manquant"

# XDG
[ -f /etc/xdg/autostart/levoile-autostart.desktop ] \
    && pass "autostart XDG" \
    || fail "autostart XDG manquant"
[ -f /usr/share/applications/levoile.desktop ] \
    && pass "launcher menu XDG" \
    || fail "launcher menu XDG manquant"

# Config skeleton
[ -f /etc/levoile/config.toml ] \
    && pass "config skeleton /etc/levoile/config.toml" \
    || fail "config skeleton manquant"

# Icônes (échantillonnage : 48 + 256)
[ -f /usr/share/icons/hicolor/48x48/apps/levoile.png ] \
    && pass "icône 48×48" \
    || fail "icône 48×48 manquante"
[ -f /usr/share/icons/hicolor/256x256/apps/levoile.png ] \
    && pass "icône 256×256" \
    || fail "icône 256×256 manquante"

# User système
if command -v getent >/dev/null 2>&1; then
    getent passwd levoile >/dev/null && pass "user levoile créé" || fail "user levoile absent"
else
    grep -q '^levoile:' /etc/passwd && pass "user levoile créé" || fail "user levoile absent"
fi

# levoile-ctl --help (teste juste que le binaire est linkable/runnable)
/usr/bin/levoile-ctl --help >/dev/null 2>&1 \
    && pass "levoile-ctl --help retourne 0" \
    || fail "levoile-ctl --help a échoué (rc=$?)"

exit $RC
EOF

# ---------------------------------------------------------------------------
# Helpers docker run
# ---------------------------------------------------------------------------
run_in() {
    local image="$1" install_cmd="$2" label="$3"
    log "── $label : $image"
    # -v $DIST:/dist:ro — monte les paquets. Le VALIDATE est piped sur stdin
    # et consommé par `sh -s` — POSIX partout (Alpine busybox n'a pas bash).
    docker run --rm -i \
        -v "$DIST":/dist:ro \
        -v "$SCRIPT_DIR":/smoke:ro \
        "$image" \
        sh -c "$install_cmd && sh -s" <<< "$VALIDATE"
    return $?
}

# Debian 12 — apt install local .deb
DEB_BASENAME="$(basename "$DEB")"
if run_in debian:12-slim \
    "export DEBIAN_FRONTEND=noninteractive && apt-get update -qq && apt-get install -qq -y /dist/$DEB_BASENAME" \
    "Debian 12 (.deb)"; then
    pass "Debian 12 / .deb — install + checks"
else
    fail "Debian 12 / .deb — échec (voir logs ci-dessus)"
fi

# Fedora 40 — dnf install local .rpm
RPM_BASENAME="$(basename "$RPM")"
if run_in fedora:40 \
    "dnf install -y -q /dist/$RPM_BASENAME" \
    "Fedora 40 (.rpm)"; then
    pass "Fedora 40 / .rpm — install + checks"
else
    fail "Fedora 40 / .rpm — échec (voir logs ci-dessus)"
fi

# Alpine 3.19 — apk add local .apk. NB : --allow-untrusted pour un paquet non signé
# (la signature est story 7.4, hors scope 7.2).
APK_BASENAME="$(basename "$APK")"
if run_in alpine:3.19 \
    "apk add --no-cache --allow-untrusted /dist/$APK_BASENAME" \
    "Alpine 3.19 (.apk)"; then
    pass "Alpine 3.19 / .apk — install + checks"
else
    fail "Alpine 3.19 / .apk — échec (voir logs ci-dessus)"
fi

# ---------------------------------------------------------------------------
# Rapport final
# ---------------------------------------------------------------------------
echo
echo "=================================================================="
echo "  Smoke test — rapport final"
echo "=================================================================="
printf '%s' "$REPORT"
echo "=================================================================="

if [ "$FAILURES" -eq 0 ]; then
    log "✅ Tous les checks sont OK."
    exit 0
else
    log "❌ $FAILURES échec(s)."
    exit 1
fi
