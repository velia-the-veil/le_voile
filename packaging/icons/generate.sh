#!/bin/sh
# Regénère les icônes hicolor multi-tailles depuis build/appicon.png (1024×1024).
# À lancer ponctuellement si l'appicon source change — PAS lancé en build CI.
# Les 6 PNG générés (16/32/48/64/128/256) sont commit dans le repo pour éviter
# une dépendance build sur ImageMagick / Pillow.
#
# Usage :
#     bash packaging/icons/generate.sh
#
# Prérequis : ImageMagick 7+ (`magick`) OU Python 3 avec Pillow.

set -eu

SRC="${SRC:-build/appicon.png}"
DEST_BASE="${DEST_BASE:-packaging/icons/hicolor}"
SIZES="16 32 48 64 128 256"

if [ ! -f "$SRC" ]; then
    echo "ERREUR : source $SRC introuvable." >&2
    exit 1
fi

if command -v magick >/dev/null 2>&1; then
    echo "Utilisation d'ImageMagick 7 (magick)."
    for s in $SIZES; do
        dir="$DEST_BASE/${s}x${s}/apps"
        mkdir -p "$dir"
        magick "$SRC" -resize "${s}x${s}" -strip "$dir/levoile.png"
        echo "  ${s}x${s} -> $dir/levoile.png"
    done
elif command -v python3 >/dev/null 2>&1 && python3 -c "import PIL" 2>/dev/null; then
    echo "Utilisation de Python + Pillow."
    python3 <<PYEOF
from PIL import Image
import os
img = Image.open("$SRC").convert('RGBA')
for s in [int(x) for x in "$SIZES".split()]:
    d = f"$DEST_BASE/{s}x{s}/apps"
    os.makedirs(d, exist_ok=True)
    img.resize((s, s), Image.Resampling.LANCZOS).save(
        f"{d}/levoile.png", "PNG", optimize=True
    )
    print(f"  {s}x{s} -> {d}/levoile.png")
PYEOF
else
    echo "ERREUR : ni 'magick' ni 'python3+Pillow' disponibles." >&2
    echo "  sudo apt install imagemagick     # Debian/Ubuntu" >&2
    echo "  python3 -m pip install Pillow    # alternative" >&2
    exit 1
fi

echo "OK — 6 tailles générées dans $DEST_BASE."
