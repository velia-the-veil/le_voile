#!/usr/bin/env bash
#
# generate-master-keystore.sh — Story 12.3
#
# Genere le keystore master Le Voile pour la signature APK release direct
# (canal GitHub releases). F-Droid signe avec sa propre cle (cf.
# docs/key-management-android.md, ADR-11) — ce script ne concerne que le
# canal APK direct.
#
# **A EXECUTER MANUELLEMENT** sur la machine air-gapped / HSM / YubiKey
# qui detient la master key. JAMAIS sur un runner CI, JAMAIS sur une machine
# en production connectee a Internet.
#
# Output : android/keystore/levoile-release.jks (PKCS12) — a encoder en
# base64 puis stocker comme GitHub Actions secret LEVOILE_KEYSTORE_BASE64.
# Le .jks lui-meme n'est JAMAIS commite (cf. android/keystore/.gitignore).
#
# Usage : bash android/scripts/generate-master-keystore.sh
#
# Pre-requis : JDK 17+ avec keytool, openssl ou base64 (pour encodage),
# shred ou srm (pour suppression securisee post-upload — facultatif mais
# recommande sur machine non air-gapped permanent).

set -euo pipefail

DEFAULT_DN="CN=Le Voile, O=Plateforme Liberte, C=FR"
DN="${LEVOILE_DN:-$DEFAULT_DN}"

# Suffixe annee = traceabilite rotation 24 mois (NFR22g/h prd.md).
ALIAS_YEAR="${LEVOILE_KEY_YEAR:-2026}"
ALIAS="levoile-master-${ALIAS_YEAR}"

# Decision implem (cf. Dev Notes Story 12.3) :
#  - Tentative 1 : Ed25519 (-keyalg Ed25519 -keysize implicit) — Java 17+ native.
#  - Si apksigner verify --min-sdk-version 29 fail (PackageManager Android 10
#    ne valide pas Ed25519 nativement), fallback RSA 4096 cohérent avec
#    Mullvad / ProtonVPN / Calyx / Orbot.
#
# Le mainteneur teste en local avec un mini APK avant export base64.
KEYALG="${LEVOILE_KEY_ALG:-RSA}"
KEYSIZE="${LEVOILE_KEY_SIZE:-4096}"

if [ -z "${LEVOILE_KEYSTORE_PATH:-}" ]; then
  if ! command -v git >/dev/null 2>&1; then
    echo "ERROR : git introuvable (requis pour resoudre repo root)." >&2
    exit 1
  fi
  REPO_ROOT="$(git rev-parse --show-toplevel)"
  KEYSTORE="${REPO_ROOT}/android/keystore/levoile-release.jks"
else
  KEYSTORE="$LEVOILE_KEYSTORE_PATH"
fi

if [ -f "$KEYSTORE" ]; then
  cat >&2 <<EOF
ERROR : $KEYSTORE existe deja.

Procedure rotation keystore : voir docs/key-management-android.md
section "Rotation 24 mois". Ne PAS ecraser le keystore existant — la
chaine de confiance v3 lineage exigerait une migration documentee.

Si vous voulez vraiment regenerer (test, mauvaise generation initiale) :
  shred -u "$KEYSTORE"
puis relancer ce script.
EOF
  exit 1
fi

mkdir -p "$(dirname "$KEYSTORE")"

if [ ! -t 0 ]; then
  echo "ERROR : ce script exige un terminal interactif (read password)." >&2
  exit 1
fi

read -r -s -p "Password keystore (>= 16 chars, ne sera pas affiche) : " STOREPASS
echo
read -r -s -p "Confirmer password : " STOREPASS_CONFIRM
echo
if [ "$STOREPASS" != "$STOREPASS_CONFIRM" ]; then
  echo "ERROR : passwords ne matchent pas." >&2
  exit 1
fi
if [ ${#STOREPASS} -lt 16 ]; then
  echo "ERROR : password < 16 chars (NFR securite — entropie ~96 bits requise)." >&2
  echo "Astuce : openssl rand -base64 16  produit un password adapte." >&2
  exit 1
fi

echo
echo "Generation keystore PKCS12 :"
echo "  Algorithme : $KEYALG ${KEYSIZE}bits"
echo "  Alias      : $ALIAS"
echo "  DN         : $DN"
echo "  Validite   : 7300 jours (~20 ans, couvre rotation 24 mois x 10)"
echo

KEYTOOL_ARGS=(
  -genkeypair -v
  -keystore "$KEYSTORE"
  -storetype PKCS12
  -storepass "$STOREPASS"
  -alias "$ALIAS"
  -keypass "$STOREPASS"
  -dname "$DN"
  -validity 7300
  -keyalg "$KEYALG"
)
if [ "$KEYALG" = "RSA" ] || [ "$KEYALG" = "DSA" ]; then
  KEYTOOL_ARGS+=(-keysize "$KEYSIZE")
fi

# Resolution keytool : PATH d'abord, puis fallback JAVA_HOME (cas Windows
# typique où JDK est installé mais bash MSYS n'a pas $PATH étendu).
KEYTOOL_BIN=""
if command -v keytool >/dev/null 2>&1; then
  KEYTOOL_BIN="keytool"
elif [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/keytool" ]; then
  KEYTOOL_BIN="$JAVA_HOME/bin/keytool"
elif [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/keytool.exe" ]; then
  KEYTOOL_BIN="$JAVA_HOME/bin/keytool.exe"
fi
if [ -z "$KEYTOOL_BIN" ]; then
  echo "ERROR : keytool introuvable. Installer un JDK 17+ et le mettre dans le PATH (ou définir JAVA_HOME)." >&2
  exit 1
fi

"$KEYTOOL_BIN" "${KEYTOOL_ARGS[@]}"

unset STOREPASS STOREPASS_CONFIRM

cat <<EOF

================================================================
Keystore genere : $KEYSTORE
  Alias : $ALIAS

Etape suivante 1/3 : tester apksigner localement (cf. Story 12.3 AC #8).
  cd \$(git rev-parse --show-toplevel)/android
  export LEVOILE_KEYSTORE_PATH="$KEYSTORE"
  export LEVOILE_KEYSTORE_PASSWORD=...    # password saisi ci-dessus
  export LEVOILE_KEY_ALIAS=$ALIAS
  export LEVOILE_KEY_PASSWORD=...          # idem
  ./gradlew :app:assembleRelease --no-daemon
  apksigner verify --verbose --print-certs --min-sdk-version 29 \\
      app/build/outputs/apk/release/app-release.apk

Si apksigner verify echoue avec "scheme v3 ed25519 not supported on
min-sdk-version 29" : relancer ce script avec
  LEVOILE_KEY_ALG=RSA LEVOILE_KEY_SIZE=4096 bash $0

Etape suivante 2/3 : encoder le keystore en base64 puis ajouter aux
GitHub Actions secrets du repo le_voile :
  base64 -w 0 < "$KEYSTORE" | xclip -selection clipboard
  # Linux        ; ou : base64 < "$KEYSTORE" | tr -d '\n' | pbcopy   # macOS
  # Coller dans GitHub > Settings > Secrets > LEVOILE_KEYSTORE_BASE64.
Secrets a ajouter dans le meme ecran :
  - LEVOILE_KEYSTORE_BASE64 (le base64 ci-dessus)
  - LEVOILE_KEYSTORE_PASSWORD (le password saisi ci-dessus)
  - LEVOILE_KEY_ALIAS = $ALIAS
  - LEVOILE_KEY_PASSWORD (= LEVOILE_KEYSTORE_PASSWORD pour l'instant)

Etape suivante 3/3 : recuperer le SHA256 fingerprint (a publier dans
docs/key-management-android.md + sur https://plateformeliberte.fr/le-voile/keys) :
  keytool -list -v -keystore "$KEYSTORE" -alias $ALIAS \\
      -storepass <password> | grep "SHA256:"

PUIS supprimer le keystore local apres verification (sauf si machine
air-gapped permanente avec stockage hardware) :
  shred -u "$KEYSTORE"      # Linux — efface definitivement
  # macOS : srm -m "$KEYSTORE"  ou  rm -P "$KEYSTORE"
  # Windows : utiliser cipher /w:<dossier> apres rm classique
================================================================
EOF
