#!/usr/bin/env bash
# Test E2E d'installation AUR — story 7.3
#
# Simule localement ce qu'un utilisateur Arch ferait avec `yay -S levoile` :
#   1. Spin up un container archlinux:base-devel.
#   2. Injecter un PKGBUILD avec une version locale (celle du dist/ actuel) +
#      des sha256sums calculés depuis les .deb locaux.
#   3. `makepkg -s --noconfirm --skipinteg` (builds + installe).
#   4. Valider les mêmes invariants que packaging/smoke/run.sh (binaires, units,
#      user système, entrées XDG).
#   5. `pacman -R` et vérifier un retrait propre.
#
# Le container tourne `sh`, pas systemd — on ne peut pas tester `systemctl
# enable --now` fonctionnellement, mais on valide le unit file via
# `systemd-analyze verify` et la présence des fichiers.
#
# Usage :
#     bash scripts/test-aur-install.sh                 # utilise dist/ existant
#     DIST=/path/to/dist bash scripts/test-aur-install.sh
#
# Prérequis :
#   - docker actif (podman fonctionne aussi si alias `docker`)
#   - `goreleaser release --snapshot --skip=publish` déjà exécuté (dist/ peuplé)

set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DIST="${DIST:-$REPO_ROOT/dist}"
PKGBUILD_DIR="$REPO_ROOT/packaging/arch"

log()  { echo "[aur-test] $*" >&2; }
fail() { echo "[aur-test] ✗ $*" >&2; exit 1; }
ok()   { echo "[aur-test] ✓ $*" >&2; }

command -v docker >/dev/null 2>&1 || fail "docker introuvable dans le PATH"
[ -d "$DIST" ] || fail "DIST=$DIST absent — lancer 'goreleaser release --snapshot --skip=publish'"
[ -f "$PKGBUILD_DIR/PKGBUILD" ] || fail "$PKGBUILD_DIR/PKGBUILD absent"
[ -f "$PKGBUILD_DIR/levoile.install" ] || fail "$PKGBUILD_DIR/levoile.install absent"

# GoReleaser nomme les .deb `levoile_<pkgver>_amd64.deb`. Récupérer le plus
# récent (un snapshot donne une version 0.0.0-next-<sha>).
DEB_AMD64="$(ls -t "$DIST"/levoile_*_amd64.deb 2>/dev/null | head -1 || true)"
[ -n "$DEB_AMD64" ] || fail "aucun levoile_*_amd64.deb trouvé dans $DIST"

log "Paquet .deb testé : $(basename "$DEB_AMD64")"

# Extraire la version depuis le nom du fichier pour l'injecter dans le PKGBUILD.
# Format : levoile_<version>_amd64.deb → version avec possibles tirets (0.0.0-next-...).
DEB_BASENAME="$(basename "$DEB_AMD64")"
PKGVER="${DEB_BASENAME#levoile_}"
PKGVER="${PKGVER%_amd64.deb}"
# pacman n'accepte pas les tirets dans pkgver → les remplacer par des points.
# La version officielle release (0.1.0) n'a pas de tirets ; seul le snapshot en a.
PKGVER_SAFE="${PKGVER//-/.}"
log "Version injectée dans le PKGBUILD de test : $PKGVER_SAFE"

SHA_AMD64="$(sha256sum "$DEB_AMD64" | awk '{print $1}')"
log "SHA256 amd64 : $SHA_AMD64"

# Prépare un répertoire de travail avec PKGBUILD modifié + levoile.install +
# .deb local. Le PKGBUILD pointe vers `file://` pour éviter la résolution GitHub.
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cp "$PKGBUILD_DIR/levoile.install" "$WORK/levoile.install"
cp "$DEB_AMD64" "$WORK/levoile-${PKGVER_SAFE}-x86_64.deb"

# PKGBUILD de test — pkgver remplacé, source pointe vers le fichier local
# injecté dans le container, SHA256 injecté, .sig skippée (pas de story 7.4 en
# test local).
cat > "$WORK/PKGBUILD" <<EOF
pkgname=levoile
pkgver=${PKGVER_SAFE}
pkgrel=1
pkgdesc="VPN — Le Voile (test local story 7.3)"
arch=('x86_64')
url="https://github.com/velia-the-veil/le_voile"
license=('MIT')
depends=('webkit2gtk-4.1' 'libayatana-appindicator' 'nftables' 'iproute2')
makedepends=('binutils')
install=levoile.install
options=('!strip' '!debug')

source_x86_64=("levoile-\${pkgver}-x86_64.deb")
sha256sums_x86_64=('${SHA_AMD64}')

prepare() {
    cd "\$srcdir"
    bsdtar -xf "levoile-\${pkgver}-x86_64.deb"
}

package() {
    bsdtar -xpf "\$srcdir"/data.tar.* -C "\$pkgdir"
    install -d "\$pkgdir/usr/share/licenses/\$pkgname"
    if [ -f "\$pkgdir/usr/share/doc/\$pkgname/LICENSE" ]; then
        mv "\$pkgdir/usr/share/doc/\$pkgname/LICENSE" \\
           "\$pkgdir/usr/share/licenses/\$pkgname/LICENSE"
    fi
}
EOF

# Script exécuté DANS le container — build + install + assertions + remove.
cat > "$WORK/run-in-container.sh" <<'EOF'
#!/bin/bash
set -uo pipefail

fail() { echo "[inside] ✗ $*"; exit 1; }
ok()   { echo "[inside] ✓ $*"; }

# 1. Deps de base (nftables tiré comme dépendance du paquet, mais makepkg
# refuse sans tooling)
pacman -Sy --noconfirm --needed base-devel binutils systemd >/dev/null
ok "tooling installé"

# 2. Créer un user non-root (makepkg refuse EUID=0)
useradd -m -G wheel aurtest 2>/dev/null || true
echo 'aurtest ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/aurtest
cp -r /work /home/aurtest/pkg
chown -R aurtest: /home/aurtest/pkg

# 3. Valider la syntaxe PKGBUILD via namcap (si installable) + printsrcinfo
sudo -u aurtest bash -c 'cd /home/aurtest/pkg && makepkg --printsrcinfo > /tmp/srcinfo.txt'
[ -s /tmp/srcinfo.txt ] && ok "makepkg --printsrcinfo OK" || fail "printsrcinfo vide"

# 4. Build + install — pacman -Syu d'abord pour résoudre webkit2gtk-4.1 etc.
#    On installe les dépendances séparément pour isoler les erreurs paquet.
pacman -S --noconfirm webkit2gtk-4.1 libayatana-appindicator nftables iproute2 >/dev/null 2>&1 \
    || echo "[inside] WARN : dépendances runtime partielles — on continue"

# On vérifie l'intégrité — les SHA256 ont été calculés et injectés dans le
# PKGBUILD par le script host, donc makepkg doit VALIDER qu'on a bien
# reproduit le fichier. `--skipinteg` bypasserait précisément le check qu'on
# veut effectuer (cf. review M13).
sudo -u aurtest bash -c 'cd /home/aurtest/pkg && makepkg -s --noconfirm' \
    || fail "makepkg a échoué (intégrité SHA256 ou build)"
ok "makepkg build + check SHA256 OK"

BUILT_PKG=$(ls /home/aurtest/pkg/levoile-*.pkg.tar.* 2>/dev/null | head -1)
[ -n "$BUILT_PKG" ] || fail "aucun .pkg.tar.* généré"
ok "paquet construit : $(basename "$BUILT_PKG")"

# 5. Installer le paquet
pacman -U --noconfirm "$BUILT_PKG" >/dev/null || fail "pacman -U a échoué"
ok "pacman -U réussi"

# 6. Assertions — mêmes invariants que packaging/smoke/run.sh
for b in /usr/bin/levoile-service /usr/bin/levoile-ui /usr/bin/levoile-ctl; do
    [ -x "$b" ] && ok "binaire $b présent" || fail "binaire $b manquant"
done

for f in \
    /usr/lib/systemd/system/levoile.service \
    /usr/lib/systemd/user/levoile-ui.service \
    /etc/xdg/autostart/levoile-autostart.desktop \
    /usr/share/applications/levoile.desktop \
    /etc/levoile/config.toml \
    /usr/share/icons/hicolor/48x48/apps/levoile.png \
    /usr/share/icons/hicolor/256x256/apps/levoile.png \
    /usr/share/licenses/levoile/LICENSE \
; do
    [ -f "$f" ] && ok "fichier $f installé" || fail "fichier $f manquant"
done

# 7. User système créé par le scriptlet post_install
if getent passwd levoile >/dev/null 2>&1; then
    ok "user système levoile créé"
else
    fail "user système levoile absent (post_install non exécuté ?)"
fi

# 8. Validation du unit systemd via systemd-analyze (ne nécessite pas systemd
# en tant qu'init — la commande vérifie la syntaxe).
if command -v systemd-analyze >/dev/null 2>&1; then
    if systemd-analyze verify /usr/lib/systemd/system/levoile.service 2>&1; then
        ok "systemd-analyze verify OK"
    else
        echo "[inside] WARN : systemd-analyze verify a remonté des avertissements (peut être normal en container)."
    fi
fi

# 9. levoile-ctl --help doit tourner (binaire linkable)
/usr/bin/levoile-ctl --help >/dev/null 2>&1 && ok "levoile-ctl --help OK" \
    || fail "levoile-ctl --help a échoué"

# 10. Retrait propre
pacman -R --noconfirm levoile >/dev/null || fail "pacman -R a échoué"
ok "pacman -R réussi"

# Convention Arch : /etc/levoile et user levoile sont CONSERVÉS (post_remove :).
# On vérifie au moins que les binaires ont disparu.
[ ! -e /usr/bin/levoile-service ] && ok "binaires retirés" \
    || fail "binaires encore présents après -R"

echo "[inside] TOUS LES CHECKS OK"
EOF

chmod +x "$WORK/run-in-container.sh"

log "Lancement du container archlinux:base-devel..."
if docker run --rm \
    -v "$WORK":/work:ro \
    archlinux:base-devel \
    bash /work/run-in-container.sh; then
    ok "Container Arch : tous les checks passés."
    exit 0
else
    fail "Container Arch : échec (voir logs ci-dessus)"
fi
