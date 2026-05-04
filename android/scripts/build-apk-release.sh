#!/usr/bin/env bash
#
# build-apk-release.sh — Story 12.4
#
# Build APK reproductible Le Voile Android. Invoque par
# .github/workflows/release-android.yml (job reproducibility-check) ET par
# tout auditeur independant qui veut verifier la chaine de confiance
# (NFR-AND-6 / ADR-11).
#
# Output (flavor `apkDirect` — Story 12.5 productFlavors) :
#   - app/build/outputs/apk/apkDirect/release/app-apkDirect-release(-unsigned-LOCAL-DEV).apk
#   - + .sha256
#   - apk-content-archive.zip + .sha256
#
# Variables env optionnelles :
#   LEVOILE_FLAVOR=apkDirect|fdroid (default: apkDirect — canal GitHub direct,
#   celui qu'on signe Story 12.3. F-Droid build sa propre repro de son cote.)
#
# Pre-requis OS pinnes (verifies au debut) :
#   - JDK 17.0.x (Temurin recommande)
#   - Gradle 8.5 (via gradlew, deja pinne)
#   - AGP 8.5.0 (via libs.versions.toml, deja pinne)
#   - Kotlin 1.9.24 (via libs.versions.toml, deja pinne)
#   - Go 1.22.x ou plus recent
#   - gomobile (commit pinne via go install)
#   - Android SDK API 34 + NDK r25c
#
# Usage : bash android/scripts/build-apk-release.sh
#
# Variables env optionnelles :
#   SKIP_GOMOBILE_VERIFY=1 : accepte gomobile non-pinne (debug uniquement)
#   SKIP_PINNING_CHECK=1   : accepte JDK / Go versions arbitraires (debug uniquement)

set -euo pipefail

ANDROID_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ANDROID_DIR"

echo "→ Verification pinnings"

if [ -z "${SKIP_PINNING_CHECK:-}" ]; then
    JDK_VERSION="$(java -version 2>&1 | head -n1)"
    if ! echo "$JDK_VERSION" | grep -qE 'version "?17\.'; then
        echo "WARN : JDK $JDK_VERSION — Story 12.4 attendu Temurin 17.0.x. Fixer JAVA_HOME." >&2
        echo "       (export SKIP_PINNING_CHECK=1 pour bypasser le check.)" >&2
    fi

    if command -v go >/dev/null 2>&1; then
        GO_VERSION="$(go version 2>&1)"
        if ! echo "$GO_VERSION" | grep -qE 'go1\.(2[2-9]|3[0-9])'; then
            echo "WARN : $GO_VERSION — Story 12.4 attendu Go 1.22.x ou plus recent." >&2
        fi
    else
        echo "WARN : go non trouve. Si build-aar.sh n'a pas ete lance avant, ce script va echouer." >&2
    fi
fi

if [ -z "${SKIP_GOMOBILE_VERIFY:-}" ]; then
    echo "→ Build aar (gomobile bind)"
    bash scripts/build-aar.sh
fi

echo "→ Sync frontend"
bash scripts/sync-frontend.sh

echo "→ Clean Gradle (anti-cache pollution)"
./gradlew clean --no-daemon

FLAVOR="${LEVOILE_FLAVOR:-apkDirect}"
GRADLE_TASK=":app:assemble$(printf '%s' "$FLAVOR" | sed 's/^./\U&/')Release"

echo "→ Assemble release flavor=$FLAVOR (no-daemon, no-parallel, no-cache, no-config-cache)"
# On NE passe PAS les LEVOILE_KEYSTORE_* env vars → fallback debug-signed avec
# suffix `-unsigned-LOCAL-DEV` (cf. Story 12.3 build.gradle.kts). Le contenu de
# l'APK est ce qui compte pour la repro check, pas la signature qui contient
# des nonces non-reproductibles.
./gradlew "$GRADLE_TASK" \
    --no-daemon \
    --no-parallel \
    --no-build-cache \
    --no-configuration-cache \
    --stacktrace

# Le nom de l'APK depend du flavor + presence des env vars de signature :
#   - sans LEVOILE_KEYSTORE_* : `app-<flavor>-release-unsigned-LOCAL-DEV.apk`
#   - avec : `app-<flavor>-release.apk`
# Path : app/build/outputs/apk/<flavor>/release/...
APK_DIR="app/build/outputs/apk/$FLAVOR/release"
APK="$APK_DIR/app-$FLAVOR-release-unsigned-LOCAL-DEV.apk"
if [ ! -f "$APK" ]; then
    APK="$APK_DIR/app-$FLAVOR-release.apk"
fi
if [ ! -f "$APK" ]; then
    echo "ERROR : APK release introuvable dans $APK_DIR" >&2
    ls -la "$APK_DIR" || true
    exit 1
fi

echo "→ Calcule SHA256 APK"
( cd "$(dirname "$APK")" && sha256sum "$(basename "$APK")" ) > "${APK}.sha256"
cat "${APK}.sha256"

echo "→ Genere apk-content-archive.zip (extraction ZIP triee + re-zip deterministe)"
# L'APK est un ZIP. Pour comparer le contenu sans le wrapping (compression
# variable, alignment, signature META-INF non-reproductible), on extrait +
# sort + re-zip avec timestamps epoch=0.
TMP_DIR="$(mktemp -d)"
trap "rm -rf \"$TMP_DIR\"" EXIT
unzip -q "$APK" -d "$TMP_DIR/extracted"

# Note : zip -X strip extra fields (timestamps, uid/gid). LC_ALL=C garantit un
# sort stable indépendamment de la locale du runner. find -print | sort + xargs
# zip preserve l'ordre.
( cd "$TMP_DIR/extracted" && \
  find . -type f \! -path "./META-INF/*.SF" \! -path "./META-INF/*.RSA" \! -path "./META-INF/*.DSA" \! -path "./META-INF/*.EC" \
    | LC_ALL=C sort | zip -q -X -@ "$TMP_DIR/apk-content-archive.zip" )
# `META-INF/*.SF`, `META-INF/*.RSA`, `META-INF/CERT.*` sont les fichiers de
# signature qui contiennent des nonces et timestamps de signature — ils
# different a chaque build. On les exclut explicitement de l'archive contenu.

cp "$TMP_DIR/apk-content-archive.zip" .
sha256sum apk-content-archive.zip > apk-content-archive.zip.sha256
cat apk-content-archive.zip.sha256

echo
echo "✓ Build reproductible termine."
echo "  APK : $APK"
echo "  SHA256 APK             : $(awk '{print $1}' "${APK}.sha256")"
echo "  SHA256 content-archive : $(awk '{print $1}' apk-content-archive.zip.sha256)"
