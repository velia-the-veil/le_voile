#!/usr/bin/env bash
#
# build-aar.sh — Compile le noyau Go partage en .aar consommable Gradle.
#
# Story 9.2 — invoque `gomobile bind` sur les 5 packages exposes :
#   - android/shims/protocol  (shim — version + framing constants)
#   - android/shims/auth      (shim — token TTL + refresh threshold)
#   - android/shims/crypto    (shim — wrap internal/crypto, Ed25519 helpers)
#   - android/shims/registry  (shim — wrap internal/registry, country meta)
#   - android/shims/leakcheck (shim — wrap internal/leakcheck, transitif tunnel + quic-go)
#
# Tous les shims vivent sous android/shims/ — pas android/internal/ — car la
# regle Go "internal" interdit l'import depuis le package gobind genere par
# gomobile dans son work dir temporaire.
#
# Sortie : android/app/libs/levoile-core.aar (gitignore).
# Le .aar est consomme directement par :app via
# `implementation(files("libs/levoile-core.aar"))` (cf. app/build.gradle.kts).
# AGP interdit qu'un module library (:levoile-core) bundle un .aar local.
#
# Pre-requis :
#   - Go >= 1.26 (matche la directive `go 1.26` du go.mod racine)
#   - gomobile + gobind installes :
#       go install golang.org/x/mobile/cmd/gomobile@latest
#       go install golang.org/x/mobile/cmd/gobind@latest
#       gomobile init  (telecharge le NDK ~1.5 GB la premiere fois)
#   - Android NDK installe (via Android Studio ou sdkmanager)
#
# Voir : ADR-08 (isolation OS), ADR-09 (gomobile noyau partage),
#        architecture.md l. 296-302, story 9-2-script-build-aar-sh-*.md.

set -euo pipefail

# ---- Mode --stub-only (Story 12.2) ----------------------------------
#
# Story 12.2 livre 4 jobs CI Android (lint, unit-tests, permission-audit,
# proguard-syntax) qui ont besoin que `app/libs/levoile-core.aar` existe pour
# que Gradle resolve `implementation(files("libs/levoile-core.aar"))`. Mais
# faire tourner gomobile en CI ajoute ~10 min de cold-start (telechargement
# NDK ~1.5 GB) sans valeur fonctionnelle pour un job lint ou un audit
# permissions (qui n'a pas besoin du code Go reel).
#
# `--stub-only` produit un .aar minimal : ZIP contenant AndroidManifest.xml +
# classes.jar vide + R.txt vide. Gradle accepte ce format, les classes Go ne
# sont pas accessibles a l'execution (mais en lint/permission-audit elles ne
# sont pas non plus invoquees).
#
# Pour les jobs CI qui ont besoin du vrai .aar (release, instrumented tests
# Story 12.6, reproductibilite Story 12.4), invoquer ce script SANS
# --stub-only — il appellera gomobile bind comme avant.
if [ "${1:-}" = "--stub-only" ]; then
  shift
  REPO_ROOT="$(git rev-parse --show-toplevel)"
  OUTPUT_DIR="${REPO_ROOT}/android/app/libs"
  OUTPUT="${OUTPUT_DIR}/levoile-core.aar"
  mkdir -p "$OUTPUT_DIR"

  TMP="$(mktemp -d)"
  trap "rm -rf \"$TMP\"" EXIT

  cat > "$TMP/AndroidManifest.xml" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package="fr.plateformeliberte.levoile.core" />
EOF
  : > "$TMP/R.txt"
  # classes.jar = empty ZIP (22-byte EOCD record).
  printf 'PK\005\006\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000\000' > "$TMP/classes.jar"

  rm -f "$OUTPUT"
  ( cd "$TMP" && zip -q -X "$OUTPUT" AndroidManifest.xml classes.jar R.txt )

  SIZE="$(stat -c '%s' "$OUTPUT" 2>/dev/null || stat -f '%z' "$OUTPUT" 2>/dev/null || wc -c < "$OUTPUT")"
  echo "[build-aar --stub-only] OK"
  echo "  Artefact : $OUTPUT"
  echo "  Taille   : $SIZE octets"
  echo
  echo "ATTENTION : ce .aar ne contient AUCUN code Go. Utilisez-le uniquement"
  echo "pour les jobs CI qui ne necessitent pas le runtime gomobile (lint,"
  echo "permission-audit, proguard-syntax). Pour les builds release et tests"
  echo "instrumentes, relancer ce script SANS --stub-only."
  exit 0
fi

# ---- Localisation repo root (script invocable depuis n'importe ou) ----
if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git non trouve dans le PATH (requis pour resoudre le repo root)." >&2
  exit 1
fi
REPO_ROOT="$(git rev-parse --show-toplevel)"

# ---- Pre-check : gomobile dans le PATH ----
if ! command -v gomobile >/dev/null 2>&1; then
  cat >&2 <<'EOF'
ERROR: gomobile non trouve dans le PATH.

Installation :
  go install golang.org/x/mobile/cmd/gomobile@latest
  go install golang.org/x/mobile/cmd/gobind@latest
  gomobile init    # telecharge l'Android NDK (~1.5 GB, 5-15 min premiere fois)

Verifier que $GOPATH/bin (ou $HOME/go/bin) est dans le PATH.
EOF
  exit 1
fi

# ---- Pre-check : version Go >= 1.26 (matche la directive go.mod racine) ----
GO_VERSION="$(go version | awk '{print $3}' | sed 's/^go//')"
GO_MAJOR="$(echo "$GO_VERSION" | cut -d. -f1)"
GO_MINOR="$(echo "$GO_VERSION" | cut -d. -f2)"
if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 26 ]; }; then
  echo "ERROR: Go $GO_VERSION trop ancien (requis >= 1.26 — directive go.mod racine)." >&2
  exit 1
fi

# ---- Pre-check : NDK ----
NDK_PATH="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
if [ -z "$NDK_PATH" ]; then
  echo "WARN: ANDROID_NDK_HOME / ANDROID_NDK_ROOT non defini ; gomobile tentera l'auto-detection." >&2
elif [ ! -d "$NDK_PATH" ]; then
  echo "ERROR: ANDROID_NDK_HOME=$NDK_PATH n'existe pas." >&2
  exit 1
fi

# ---- Pre-check : javac (requis par gomobile pour produire le .aar) ----
# Sur Windows / Git Bash, JAVA_HOME peut etre persiste user-scope mais
# absent de la session bash. Si javac n'est pas dans le PATH mais
# JAVA_HOME existe, on prepend automatiquement $JAVA_HOME/bin.
if ! command -v javac >/dev/null 2>&1; then
  if [ -n "${JAVA_HOME:-}" ] && { [ -x "${JAVA_HOME}/bin/javac" ] || [ -x "${JAVA_HOME}/bin/javac.exe" ]; }; then
    # Sur Git Bash/MSYS2 (Windows), gomobile invoque javac via exec.LookPath
    # qui utilise %PATH% Windows-style. Si JAVA_HOME contient des forward
    # slashes ou un format mingw, on convertit via cygpath quand dispo.
    JH_NATIVE="$JAVA_HOME"
    if command -v cygpath >/dev/null 2>&1; then
      JH_NATIVE="$(cygpath -u "$JAVA_HOME")"
    fi
    PATH="${JH_NATIVE}/bin:${PATH}"
    export PATH
    echo "[build-aar] PATH prepended with JAVA_HOME/bin = ${JH_NATIVE}/bin"
  else
    cat >&2 <<'EOF'
ERROR: javac non trouve dans le PATH.

Ce script (build-aar.sh) est concu pour Linux / macOS. Sur Windows,
preferer build-aar.ps1 depuis PowerShell qui lit les variables user-scope
automatiquement.

Sur Linux/macOS : verifier qu'un JDK 17+ est installe et que JAVA_HOME
pointe sur sa racine (ou que javac est directement dans le PATH).

Sur Windows/Git Bash (degrade) :
  export JAVA_HOME="$LOCALAPPDATA/Programs/Microsoft/jdk-17.0.10.7-hotspot"
  bash android/scripts/build-aar.sh
EOF
    exit 1
  fi
fi

# ---- Output path + cleanup trap ----
# gomobile bind exige que le -o se termine par ".aar", impossible d'utiliser
# un suffixe ".tmp" intermediaire. On ecrit directement vers le chemin final
# et on retire le fichier en cas d'erreur (trap conditionnel via $BUILD_OK).
OUTPUT_DIR="${REPO_ROOT}/android/app/libs"
OUTPUT="${OUTPUT_DIR}/levoile-core.aar"
mkdir -p "$OUTPUT_DIR"
BUILD_OK=0
# shellcheck disable=SC2064  # OUTPUT est volontairement expanse a la pose du trap
trap "if [ \"\$BUILD_OK\" -ne 1 ]; then rm -f \"$OUTPUT\"; fi" EXIT INT TERM

# ---- gomobile bind ----
cd "$REPO_ROOT"
echo "[build-aar] Invocation gomobile bind ..."
echo "[build-aar]   target=android androidapi=29 javapkg=fr.plateformeliberte.levoile.core"
echo "[build-aar]   packages=android/shims/{protocol,auth,crypto,registry,leakcheck}"

gomobile bind \
  -target=android \
  -androidapi=29 \
  -javapkg=fr.plateformeliberte.levoile.core \
  -o "$OUTPUT" \
  ./android/shims/protocol \
  ./android/shims/auth \
  ./android/shims/crypto \
  ./android/shims/registry \
  ./android/shims/leakcheck

BUILD_OK=1

# ---- Rapport ----
# stat existe partout (GNU sur Linux, BSD sur macOS) ; wc -c reste un dernier
# fallback ultra-portable au cas ou stat manquerait sur un environnement minimal.
SIZE="$(stat -c '%s' "$OUTPUT" 2>/dev/null || stat -f '%z' "$OUTPUT" 2>/dev/null || wc -c < "$OUTPUT")"
HASH="$(sha256sum "$OUTPUT" 2>/dev/null | awk '{print $1}' || shasum -a 256 "$OUTPUT" | awk '{print $1}')"

cat <<EOF

[build-aar] OK
  Artefact : ${OUTPUT}
  Taille   : ${SIZE} octets
  SHA256   : ${HASH}

Verifier la frontiere ADR-09 (imports cross-OS) :
  bash android/scripts/verify-shared-imports.sh
EOF
