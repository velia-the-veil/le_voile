#!/bin/sh
# preinstall — Le Voile VPN
# Créé le groupe + user système `levoile` (idempotents) avant que les fichiers
# ne soient posés : `postinstall` et le service systemd s'attendent à ce que
# user ET groupe existent (unit : User=levoile Group=levoile).
# Lancé avec root privileges par apt/dnf/apk.

set -eu

USER_NAME="levoile"
GROUP_NAME="levoile"
USER_SHELL="/usr/sbin/nologin"
# Fallback pour Alpine (busybox n'a pas /usr/sbin/nologin par défaut)
if [ ! -x "$USER_SHELL" ] && [ -x "/sbin/nologin" ]; then
    USER_SHELL="/sbin/nologin"
fi
if [ ! -x "$USER_SHELL" ] && [ -x "/bin/false" ]; then
    USER_SHELL="/bin/false"
fi

# getent est présent sur glibc (Debian/Fedora) mais pas sur musl (Alpine pur).
# Fallback sur grep si getent absent.
user_exists() {
    if command -v getent >/dev/null 2>&1; then
        getent passwd "$1" >/dev/null 2>&1
    else
        grep -q "^$1:" /etc/passwd 2>/dev/null
    fi
}
group_exists() {
    if command -v getent >/dev/null 2>&1; then
        getent group "$1" >/dev/null 2>&1
    else
        grep -q "^$1:" /etc/group 2>/dev/null
    fi
}

# 1. Groupe d'abord — sur Alpine, adduser -S sans -G mettrait le user dans
# `nogroup`, ce qui casse le `Group=levoile` du unit systemd.
if group_exists "$GROUP_NAME"; then
    echo "[levoile] groupe '$GROUP_NAME' déjà présent." >&2
else
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system "$GROUP_NAME"
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup -S "$GROUP_NAME"
    else
        echo "[levoile] ERREUR : ni groupadd ni addgroup trouvé — groupe '$GROUP_NAME' non créé." >&2
        exit 1
    fi
    echo "[levoile] groupe système '$GROUP_NAME' créé." >&2
fi

# 2. User — explicite sur le groupe primaire sur TOUTES les distros.
if user_exists "$USER_NAME"; then
    echo "[levoile] user '$USER_NAME' déjà présent." >&2
else
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --shell "$USER_SHELL" \
            --gid "$GROUP_NAME" \
            --comment "Le Voile VPN service" "$USER_NAME"
    elif command -v adduser >/dev/null 2>&1; then
        # busybox adduser (Alpine) — -G force le groupe primaire
        adduser -S -D -H -G "$GROUP_NAME" -s "$USER_SHELL" \
            -g "Le Voile VPN service" "$USER_NAME"
    else
        echo "[levoile] ERREUR : ni useradd ni adduser trouvé — user '$USER_NAME' non créé." >&2
        echo "[levoile] Le service ne pourra pas démarrer. Créez le user manuellement." >&2
        exit 1
    fi
    echo "[levoile] user système '$USER_NAME' créé (groupe primaire $GROUP_NAME)." >&2
fi

exit 0
