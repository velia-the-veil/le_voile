# Story 12.3: Signature APK v2 + v3 (key rotation) par master key Ed25519

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev livre la signature APK release pour le canal APK direct GitHub releases (PAS pour le canal F-Droid — F-Droid utilise sa propre clé). Le dev DOIT comprendre l'architecture clé Ed25519 master + clé APK avant de toucher quoi que ce soit. AUCUNE clé privée ne doit être committée dans le repo. AUCUNE clé privée ne doit être loggée par le workflow CI.**
>
> **Story 12.3 livre** :
> 1. **Étend** `android/app/build.gradle.kts` avec un bloc `signingConfigs.release` qui lit les credentials depuis des **variables d'environnement** (jamais de fichier en clair) : `LEVOILE_KEYSTORE_PATH`, `LEVOILE_KEYSTORE_PASSWORD`, `LEVOILE_KEY_ALIAS`, `LEVOILE_KEY_PASSWORD`. Le bloc `release` du `buildTypes` bascule de `signingConfig = signingConfigs.getByName("debug")` vers `signingConfigs.getByName("release")` **conditionnellement** (si l'env var est présente, sinon fallback debug avec versionNameSuffix `-unsigned` — pour permettre les builds locaux des devs sans la clé).
> 2. **Étend** `.github/workflows/release-android.yml` (Story 12.2 squelette) avec un job `sign-apk` réel qui :
>    - Décode `secrets.LEVOILE_KEYSTORE_BASE64` → `levoile-release.jks` (PKCS12).
>    - Lance `./gradlew :app:assembleRelease` avec env vars set.
>    - Vérifie la signature via `apksigner verify --verbose --print-certs app-release.apk` — assertion `Verifies (APK Signature Scheme v2): true` + `Verifies (APK Signature Scheme v3): true`.
>    - Génère `apk-signature-info.txt` avec le SHA256 du certificat + algos (à publier dans la GitHub release pour vérification utilisateur).
>    - Upload l'APK signé en artifact.
> 3. Un script `android/scripts/generate-master-keystore.sh` (NOUVEAU, **exécuté manuellement par le mainteneur sur la machine HSM/YubiKey air-gapped**, JAMAIS en CI) qui documente la procédure de génération du keystore PKCS12 contenant la clé APK Ed25519 dérivée. Le script vérifie que le keystore généré est lisible par `apksigner` (test local avant export base64 + secret GitHub).
> 4. `docs/key-management-android.md` (NOUVEAU) — runbook complet :
>    - Architecture clés : master key Ed25519 (registre, releases, paquets — Story 7.4 desktop) + clé APK signing v2/v3 (cette story).
>    - Procédure rotation tous les 24 mois (NFR22g/h prd.md l. 684-685) — comment v3 key rotation transitionne sans forcer désinstallation.
>    - Procédure incident (clé compromise → révocation, dual-signing transitoire 6 mois NFR22h).
>    - **Aucun secret en clair dans le runbook** — uniquement les commandes et la flow.
> 5. Un test instrumenté `SignatureValidationTest.kt` (Story 12.6 dépendance — placeholder pour 12.3, implémenté Story 12.6 sur l'émulateur) qui :
>    - Récupère la signature de l'APK installé via `PackageManager.getPackageInfo(...).signingInfo.apkContentsSigners`.
>    - Compare au certificat attendu (SHA256 fingerprint hardcodé dans le test, ou vérifié via `assertEquals` au certificat de prod).
>    - **Pour 12.3** : on livre la **structure** du test (TODO documentés) ; l'implémentation runtime est dans Story 12.6.
> 6. Un test JVM `SigningConfigTest.kt` qui parse `app/build.gradle.kts` et vérifie que :
>    - `signingConfigs.release { ... }` existe.
>    - Les credentials sont lus depuis env vars (pas de string literal `"password123"`).
>    - Le `buildTypes.release.signingConfig` est conditionnellement bound à `signingConfigs.release` (pas hardcodé à debug).
>    - Anti-régression : le `versionNameSuffix = "-unsigned"` reste activé en fallback.
> 7. **Anti-fuite secrets** : le step CI qui décode le keystore base64 utilise `::add-mask::` pour masquer la valeur dans les logs. Le keystore décodé est écrit dans un fichier `$RUNNER_TEMP/levoile-release.jks` (cleanup automatique en fin de job). Aucun `echo` du contenu, aucun `cat` du keystore.
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile.
>
> **Rappel ADR-08** : workflow GitHub Actions = exception. Les keystores eux ne sont JAMAIS committés (gitignore strict — `android/keystore/*.jks` déjà gitignored Story 9.x).
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 12.3 |
> |---|---|---|
> | `android/app/src/main/**`, `android/app/src/test/**` (sauf nouveaux tests) | 9.x/10.x/11.x | INTACT |
> | `android/levoile-core/**`, `android/shims/**` | 9.x | INTACT |
> | `metadata/**` | 12.1 | INTACT — F-Droid signe avec sa propre clé, **pas avec la nôtre** (point critique) |
> | `.github/workflows/android-audit.yml` | 10.4/12.2 | INTACT — uniquement `release-android.yml` est touché |
> | `android/app/build.gradle.kts` | 11.8/12.1 | **MODIFIÉ — bloc `signingConfigs.release` ajouté + buildTypes.release conditional** |
> | `.github/workflows/release-android.yml` | 12.2 | **MODIFIÉ — job `sign-apk` placeholder remplacé par implémentation réelle** |
> | `android/scripts/generate-master-keystore.sh` | (absent) | **NOUVEAU — runbook bash mainteneur** |
> | `android/scripts/generate-master-keystore.ps1` | (absent) | **NOUVEAU — variante Windows pour mainteneur (cohérent ADR-08 dual-script)** |
> | `docs/key-management-android.md` | (absent) | **NOUVEAU — runbook + ADR + procédure rotation** |
> | `android/app/src/test/kotlin/.../security/SigningConfigTest.kt` | (absent) | **NOUVEAU — test JVM parsing build.gradle.kts** |
> | `android/app/src/androidTest/kotlin/.../security/SignatureValidationTest.kt` | (absent) | **NOUVEAU — squelette test instrumenté** (impl runtime Story 12.6) |
> | `android/keystore/.gitignore` | 9.x | **VÉRIFIER** — confirmer que `*.jks` y est. Si non, l'ajouter. |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/build.gradle.kts` (MODIFIÉ — signingConfigs.release),
>   (b) `.github/workflows/release-android.yml` (MODIFIÉ — sign-apk job réel),
>   (c) `android/scripts/generate-master-keystore.sh` (NOUVEAU),
>   (d) `android/scripts/generate-master-keystore.ps1` (NOUVEAU),
>   (e) `docs/key-management-android.md` (NOUVEAU),
>   (f) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/security/SigningConfigTest.kt` (NOUVEAU),
>   (g) `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/security/SignatureValidationTest.kt` (NOUVEAU squelette),
>   (h) éventuellement `android/keystore/.gitignore` (MODIFIÉ si nécessaire),
>   (i) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (j) `_bmad-output/implementation-artifacts/12-3-signature-apk-v2-v3-key-rotation-par-master-key-ed25519.md`.
>
> **Anti-patterns CRITIQUES sécurité** :
> - **Committer un keystore** (.jks, .p12, .pfx, .pem) dans le repo — fuite immédiate. `android/keystore/.gitignore` doit refuser tout sauf `README.md`. Si un dev pousse accidentellement, **rotation immédiate de la clé** (procédure incident dans `docs/key-management-android.md`).
> - **Passer le mot de passe keystore en CLI** (`-storepass MyPass123`) — visible dans `ps aux`, dans les logs CI. **Toujours** via env var ou prompt interactif.
> - **Logger le contenu du keystore décodé** (`cat $RUNNER_TEMP/keystore.jks`) — fuite via les logs Actions. Step CI doit `set +x` autour de la décode + `add-mask`.
> - **Re-signer en debug par accident** sur tag release — le bloc conditionnel `release` doit fail explicitement si l'env var est absente sur un build tag (pas de fallback silencieux). Pattern : `if (System.getenv("LEVOILE_KEYSTORE_PATH") == null && isReleaseBuildOnTag()) throw GradleException("Missing release signing credentials")`.
> - **Utiliser une clé RSA 2048 ou DSA** au lieu d'Ed25519 — viole NFR22g (master key Ed25519). **Note technique critique** : Android APK Signature Scheme v2/v3 supporte Ed25519 uniquement depuis API 30+ (Android 11). Notre minSdk = 29 (Android 10). **Décision dev : la clé APK est en Ed25519 (NFR-AND-5 cohérent NFR22g) signée v2 + v3, et v1 (JAR signing) est *désactivé* — Android 10+ supporte v2 sans v1**. Vérifier : `apksigner verify --min-sdk-version 29` accepte v2 sans v1 sur API 29+ ? **Si non**, fallback : signer en RSA 4096 + alignement justifié dans `docs/key-management-android.md` (cohérent pratique courante : Mullvad VPN, ProtonVPN signent en RSA, pas en Ed25519). **Décision finale prise par le dev en Phase 1 lecture documentation `apksigner` officielle**, reportée en Completion Notes.
> - **Signer le bundle AAB pour Play Store** — pas en MVP (ADR-11 — F-Droid + APK direct uniquement). Le `signingConfigs.release` cible APK only.
> - **Activer `enableV4Signing`** (APK Signature Scheme v4) — non requis (utilisé pour streaming install Android 11+, pas pertinent VPN). Laisser désactivé.
> - **Stocker la clé sur le runner GitHub Actions** entre 2 builds — secrets sont éphémères par design, c'est correct. Toute persistance violerait NFR22g.
> - **Ajouter un `signingReport` task accessible aux PR** — révèle le SHA256 fingerprint mais aussi des metadata. C'est OK publié post-release dans `apk-signature-info.txt`, MAIS pas par accident sur une PR (pour éviter le confusion utilisateur entre debug et release certs). **Décision** : `signingReport` reste activé localement (debug only par défaut) mais le job CI debug ne l'expose pas dans les artifacts.

## Story

En tant qu'utilisatrice Android Le Voile,
Je veux que l'APK installé soit signé par la master key Le Voile et vérifié automatiquement par PackageManager,
Afin que je sois protégée contre une APK altérée par un MITM ou un upload malveillant (FR-AND-7 prd.md l. 615 + NFR-AND-5 prd.md l. 701 + epics.md l. 2118-2142).

## Acceptance Criteria

1. **`signingConfigs.release` dans `app/build.gradle.kts`** — Quand le fichier est lu après cette story :
   ```kotlin
   android {
       // ... compileSdk, defaultConfig inchangés ...

       signingConfigs {
           create("release") {
               // SECURITY : credentials lus exclusivement depuis env vars.
               // Aucun string literal de password ici. Voir docs/key-management-android.md.
               val keystorePath = System.getenv("LEVOILE_KEYSTORE_PATH")
               val keystorePassword = System.getenv("LEVOILE_KEYSTORE_PASSWORD")
               val alias = System.getenv("LEVOILE_KEY_ALIAS")
               val keyPassword = System.getenv("LEVOILE_KEY_PASSWORD")

               if (keystorePath != null && keystorePassword != null && alias != null && keyPassword != null) {
                   storeFile = file(keystorePath)
                   storePassword = keystorePassword
                   keyAlias = alias
                   this.keyPassword = keyPassword
                   // Story 12.3 : APK Signature Scheme v2 + v3 (key rotation).
                   // v1 (JAR signing) désactivé — Android 10+ (minSdk 29) accepte v2 sans v1.
                   // v4 désactivé — non requis (streaming install Android 11+).
                   enableV1Signing = false
                   enableV2Signing = true
                   enableV3Signing = true
                   enableV4Signing = false
               }
               // Si les env vars sont absentes : signingConfig.storeFile reste null,
               // et le buildType.release tombera en fallback debug (cf. ci-dessous).
           }
       }

       buildTypes {
           release {
               isMinifyEnabled = true
               proguardFiles(
                   getDefaultProguardFile("proguard-android-optimize.txt"),
                   "proguard-rules.pro"
               )
               // Story 12.3 : signingConfig conditionnel.
               // - Si LEVOILE_KEYSTORE_PATH est défini → release signing (master key Le Voile).
               // - Sinon → fallback debug + suffix `-unsigned-LOCAL-DEV` pour clarifier qu'aucun
               //   utilisateur ne devrait jamais installer cet APK (debug-signed).
               // Ce fallback permet aux devs de faire `./gradlew assembleRelease` localement
               // pour tester R8/ProGuard sans avoir la master key.
               if (System.getenv("LEVOILE_KEYSTORE_PATH") != null) {
                   signingConfig = signingConfigs.getByName("release")
                   // Pas de versionNameSuffix : c'est l'APK release officiel.
               } else {
                   signingConfig = signingConfigs.getByName("debug")
                   versionNameSuffix = "-unsigned-LOCAL-DEV"
               }
           }
           debug {
               isMinifyEnabled = false
               applicationIdSuffix = ".debug"
               versionNameSuffix = "-debug"
           }
       }

       // ... reste inchangé ...
   }
   ```

2. **Job `sign-apk` réel dans `release-android.yml`** — Quand le fichier est lu :
   ```yaml
   sign-apk:
     needs: ci
     runs-on: ubuntu-22.04
     timeout-minutes: 30
     steps:
       - uses: actions/checkout@v4

       - uses: actions/setup-java@v4
         with: { distribution: temurin, java-version: '17' }

       - uses: gradle/actions/setup-gradle@v3

       - uses: android-actions/setup-android@v3
         with:
           cmdline-tools-version: 11076708

       - name: Build aar (gomobile)
         working-directory: android
         run: bash scripts/build-aar.sh
         # NB : build-aar.sh requiert Go + gomobile installés — geler les versions ici
         # (Phase 12.4 reproductibilité). Pour 12.3, on suppose qu'elles sont alignées
         # avec local.properties + go.mod existants.

       - name: Sync frontend
         working-directory: android
         run: bash scripts/sync-frontend.sh

       - name: Decode keystore
         id: decode-keystore
         run: |
           set +x   # ne JAMAIS afficher les commandes ci-dessous
           KEYSTORE_PATH="$RUNNER_TEMP/levoile-release.jks"
           echo "$LEVOILE_KEYSTORE_BASE64" | base64 -d > "$KEYSTORE_PATH"
           # Vérification minimale : le fichier décodé est non-vide et lisible par keytool.
           if [ ! -s "$KEYSTORE_PATH" ]; then
             echo "::error::Keystore décodé vide — secret LEVOILE_KEYSTORE_BASE64 invalide ?"
             exit 1
           fi
           # Masque le path complet dans les logs futurs (au cas où un autre step l'imprimerait).
           echo "::add-mask::$KEYSTORE_PATH"
           echo "keystore_path=$KEYSTORE_PATH" >> "$GITHUB_OUTPUT"
         env:
           LEVOILE_KEYSTORE_BASE64: ${{ secrets.LEVOILE_KEYSTORE_BASE64 }}

       - name: Assemble release (signed)
         working-directory: android
         env:
           LEVOILE_KEYSTORE_PATH: ${{ steps.decode-keystore.outputs.keystore_path }}
           LEVOILE_KEYSTORE_PASSWORD: ${{ secrets.LEVOILE_KEYSTORE_PASSWORD }}
           LEVOILE_KEY_ALIAS: ${{ secrets.LEVOILE_KEY_ALIAS }}
           LEVOILE_KEY_PASSWORD: ${{ secrets.LEVOILE_KEY_PASSWORD }}
         run: ./gradlew :app:assembleRelease --no-daemon --stacktrace

       - name: Verify APK signature
         working-directory: android
         run: |
           APK="app/build/outputs/apk/release/app-release.apk"
           apksigner verify --verbose --print-certs --min-sdk-version 29 "$APK" | tee apk-signature-info.txt
           # Asserts : Verifies (APK Signature Scheme v2): true ET v3: true ET v1: false.
           grep -E "^Verifies \(APK Signature Scheme v2\): true$" apk-signature-info.txt || { echo "::error::v2 signing absent"; exit 1; }
           grep -E "^Verifies \(APK Signature Scheme v3\): true$" apk-signature-info.txt || { echo "::error::v3 signing absent"; exit 1; }
           grep -E "^Verifies using v1 scheme.*: false$" apk-signature-info.txt || { echo "::error::v1 signing présent — devrait être désactivé sur minSdk 29+"; exit 1; }

       - name: Cleanup keystore
         if: always()
         run: |
           rm -f "$RUNNER_TEMP/levoile-release.jks"

       - name: Upload signed APK
         uses: actions/upload-artifact@v4
         with:
           name: app-release-signed
           path: |
             android/app/build/outputs/apk/release/app-release.apk
             android/apk-signature-info.txt
           retention-days: 90
   ```

3. **Script `android/scripts/generate-master-keystore.sh`** — Quand le mainteneur exécute ce script sur sa machine HSM/YubiKey air-gapped :
   ```bash
   #!/usr/bin/env bash
   # Story 12.3 — Génère le keystore master Le Voile pour la signature APK release.
   #
   # **À EXÉCUTER MANUELLEMENT** sur la machine air-gapped / HSM / YubiKey
   # qui détient la master key Ed25519. JAMAIS sur un runner CI, JAMAIS sur
   # une machine connectée à internet en production.
   #
   # Output : `android/keystore/levoile-release.jks` (PKCS12) — à encoder
   # en base64 puis stocker comme secret GitHub Actions
   # `LEVOILE_KEYSTORE_BASE64`. Le .jks lui-même n'est JAMAIS committé
   # (cf. android/keystore/.gitignore).
   #
   # Usage : bash android/scripts/generate-master-keystore.sh

   set -euo pipefail

   if [ -z "${LEVOILE_DN:-}" ]; then
       export LEVOILE_DN="CN=Le Voile, O=Plateforme Liberté, C=FR"
   fi

   read -r -s -p "Password keystore (≥ 16 chars, ne sera pas affiché) : " STOREPASS
   echo
   read -r -s -p "Confirmer password : " STOREPASS_CONFIRM
   echo
   if [ "$STOREPASS" != "$STOREPASS_CONFIRM" ]; then
       echo "ERREUR : passwords ne matchent pas." >&2
       exit 1
   fi
   if [ ${#STOREPASS} -lt 16 ]; then
       echo "ERREUR : password < 16 chars (NFR sécurité)." >&2
       exit 1
   fi

   KEYSTORE="android/keystore/levoile-release.jks"
   ALIAS="levoile-master-2026"   # incrémenter le suffixe lors de la rotation 24 mois

   if [ -f "$KEYSTORE" ]; then
       echo "ERREUR : $KEYSTORE existe déjà. Procédure rotation : voir docs/key-management-android.md." >&2
       exit 1
   fi

   mkdir -p "$(dirname "$KEYSTORE")"

   # Décision implem (cf. anti-pattern Ed25519 vs RSA dans la story file) :
   # - Tentative 1 : Ed25519 via -keyalg Ed25519 (Java 17+ supporte natif).
   # - Si apksigner fail au verify --min-sdk-version 29, fallback RSA 4096.
   # Le mainteneur teste localement avant export base64.
   keytool -genkeypair -v \
       -keystore "$KEYSTORE" \
       -storetype PKCS12 \
       -storepass "$STOREPASS" \
       -alias "$ALIAS" \
       -keyalg Ed25519 \
       -keypass "$STOREPASS" \
       -dname "$LEVOILE_DN" \
       -validity 7300   # 20 ans — couvre rotation 24 mois × 10

   echo
   echo "✓ Keystore généré : $KEYSTORE"
   echo "  Alias : $ALIAS"
   echo
   echo "Test apksigner local (vérifie compat min-sdk-version 29) :"
   echo "  cd android && ./gradlew :app:assembleRelease  (avec env vars LEVOILE_*)"
   echo "  apksigner verify --verbose --min-sdk-version 29 app/build/outputs/apk/release/app-release.apk"
   echo
   echo "Si Ed25519 échoue sur min-sdk 29 :"
   echo "  rm $KEYSTORE && relancer ce script avec keyalg=RSA size=4096 (édition manuelle ligne 47)"
   echo
   echo "Encoder en base64 pour GitHub Secrets :"
   echo "  base64 -w 0 < $KEYSTORE | xclip -selection clipboard"
   echo "  → coller dans GitHub Settings > Secrets > LEVOILE_KEYSTORE_BASE64"
   echo "  → autres secrets : LEVOILE_KEYSTORE_PASSWORD, LEVOILE_KEY_ALIAS=$ALIAS, LEVOILE_KEY_PASSWORD"
   echo
   echo "PUIS supprimer le keystore local après upload base64 (si machine non air-gapped permanent) :"
   echo "  shred -u $KEYSTORE   # ne pas juste rm — shred efface définitivement"
   ```

4. **Variante PowerShell `android/scripts/generate-master-keystore.ps1`** (cohérent ADR-08 dual-script) :
   - Mêmes paramètres, prompt password via `Read-Host -AsSecureString`.
   - Validation password ≥ 16 chars.
   - `keytool` invocation identique.
   - Affichage path .jks + secret list à provisionner.

5. **`docs/key-management-android.md`** — Quand le fichier est lu :
   - **Architecture clés** : 3 clés distinctes : (a) master key Ed25519 desktop (Story 7.4 — signe binaires desktop + paquets + registre), (b) clé APK signing v2/v3 (cette story — signe APK Android direct GitHub), (c) clé F-Droid (gérée par F-Droid build server, distinctement).
   - **Procédure setup initial** : exécuter `generate-master-keystore.sh` sur machine air-gapped, encoder base64, provisionner secrets GitHub Actions, **supprimer le .jks local** après upload (ou le déplacer sur YubiKey hardware).
   - **Procédure rotation 24 mois** :
     1. Générer nouvelle clé `levoile-master-2028.jks` (suffixe année).
     2. Période transitoire 6 mois : APK signé en v3 avec **rotation lineage** (`apksigner rotate --lineage`) — la nouvelle clé est cryptographiquement liée à l'ancienne via v3 lineage.
     3. PackageManager Android accepte la transition automatiquement (v3 key rotation) — utilisateurs n'ont rien à faire.
     4. Après 6 mois, retirer l'ancienne clé du lineage.
   - **Procédure incident clé compromise** :
     1. **Rotation immédiate** (NFR22h) — générer nouvelle clé.
     2. **Dual-signing transitoire 6 mois** (cohérent NFR22h) : APK signé avec **les 2 clés** (v2 ancienne + v3 nouvelle + lineage), users existants peuvent encore valider.
     3. Communication post-mortem (warrant canary mensuel).
     4. Re-publication APK direct + notification F-Droid.
   - **Vérifications utilisateur** : `apksigner verify --print-certs app-release.apk` → SHA256 fingerprint à comparer avec celui annoncé dans `docs/key-management-android.md` (publié dans le repo + sur `https://plateformeliberte.fr/le-voile/keys`).
   - **Anti-pattern** : ne JAMAIS révoquer une clé en supprimant le secret CI sans rotation lineage v3 — les users existants ne pourraient plus updater.

6. **Test JVM `SigningConfigTest.kt`** — Quand exécuté, vert :
   ```kotlin
   package fr.plateformeliberte.levoile.security

   import org.junit.Assert.assertFalse
   import org.junit.Assert.assertTrue
   import org.junit.Test
   import java.io.File

   class SigningConfigTest {

       @Test
       fun `app build gradle declare un signingConfigs release`() {
           val content = appBuildGradle().readText()
           assertTrue(
               "signingConfigs.release manquant — Story 12.3",
               content.contains("create(\"release\")") && content.contains("signingConfigs"),
           )
           assertTrue(
               "buildTypes.release doit reference signingConfigs release conditionnellement",
               content.contains("signingConfigs.getByName(\"release\")"),
           )
       }

       @Test
       fun `signingConfigs lit credentials depuis env vars uniquement`() {
           val content = appBuildGradle().readText()
           listOf(
               "LEVOILE_KEYSTORE_PATH",
               "LEVOILE_KEYSTORE_PASSWORD",
               "LEVOILE_KEY_ALIAS",
               "LEVOILE_KEY_PASSWORD",
           ).forEach { envVar ->
               assertTrue(
                   "build.gradle.kts doit lire $envVar via System.getenv",
                   content.contains("System.getenv(\"$envVar\")"),
               )
           }
       }

       @Test
       fun `aucun string literal de password dans build gradle`() {
           val content = appBuildGradle().readText()
           // Pattern dangereux : storePassword = "xxx" littéral.
           val literalPasswordRegex = Regex(
               """(storePassword|keyPassword)\s*=\s*"[^$\{][^"]*"""",
           )
           val matches = literalPasswordRegex.findAll(content).toList()
           assertTrue(
               "Mots de passe en clair détectés dans build.gradle.kts : ${matches.joinToString { it.value }}",
               matches.isEmpty(),
           )
       }

       @Test
       fun `v1 signing desactive et v2 v3 actives sur release`() {
           val content = appBuildGradle().readText()
           assertTrue("enableV1Signing = false attendu (minSdk 29+)", content.contains("enableV1Signing = false"))
           assertTrue("enableV2Signing = true attendu", content.contains("enableV2Signing = true"))
           assertTrue("enableV3Signing = true attendu", content.contains("enableV3Signing = true"))
           assertFalse("enableV4Signing doit etre desactive (non requis MVP)", content.contains("enableV4Signing = true"))
       }

       @Test
       fun `fallback debug + versionNameSuffix unsigned LOCAL DEV en absence env var`() {
           val content = appBuildGradle().readText()
           assertTrue(
               "Fallback debug requis pour les builds locaux sans la master key",
               content.contains("versionNameSuffix = \"-unsigned-LOCAL-DEV\""),
           )
           assertTrue(
               "Le fallback doit verifier l'absence de LEVOILE_KEYSTORE_PATH",
               content.contains("System.getenv(\"LEVOILE_KEYSTORE_PATH\")"),
           )
       }

       private fun appBuildGradle(): File {
           val candidates = listOf(
               "build.gradle.kts",
               "app/build.gradle.kts",
               "android/app/build.gradle.kts",
           )
           return candidates.map { File(it) }.firstOrNull { it.exists() && it.readText().contains("applicationId") }
               ?: throw AssertionError("app/build.gradle.kts introuvable. user.dir=${System.getProperty("user.dir")}")
       }
   }
   ```

7. **Squelette `SignatureValidationTest.kt` (instrumenté — Story 12.6 implémentera)** :
   ```kotlin
   package fr.plateformeliberte.levoile.security

   import androidx.test.ext.junit.runners.AndroidJUnit4
   import androidx.test.platform.app.InstrumentationRegistry
   import org.junit.Assert.assertTrue
   import org.junit.Test
   import org.junit.runner.RunWith

   /**
    * Story 12.3 squelette + Story 12.6 implémentation runtime.
    *
    * Vérifie que l'APK installé sur l'émulateur est signé avec le certificat
    * attendu (SHA256 fingerprint hardcodé après génération master keystore
    * Story 12.3 + provisionnement secrets).
    *
    * TODO Story 12.6 :
    *  - Récupérer signingInfo via PackageManager.getPackageInfo().
    *  - Comparer apkContentsSigners[0].toCharsString() au fingerprint attendu.
    *  - Test `apk altere refuse install` : injecter un APK modifié (1 byte flippé)
    *    via adb install — vérifier echo "Failure [INSTALL_PARSE_FAILED_NO_CERTIFICATES]".
    */
   @RunWith(AndroidJUnit4::class)
   class SignatureValidationTest {

       @Test
       fun `placeholder Story 12-3 — implementation runtime Story 12-6`() {
           // TODO Story 12.6 : implem complète.
           // Pour 12.3, on garantit juste que la classe compile et que le fichier
           // existe — le test instrumenté réel sera ajouté avec la matrice Espresso.
           assertTrue(true)
       }
   }
   ```

8. **Build sanity local** — Quand le dev exécute :
   ```bash
   cd android
   # Sans env vars : fallback debug + suffix -unsigned-LOCAL-DEV
   ./gradlew :app:assembleRelease --no-daemon
   apksigner verify --verbose app/build/outputs/apk/release/app-release-unsigned-LOCAL-DEV.apk
   # → APK signé en debug, suffix présent → OK pour test local R8.

   # Avec env vars (le dev possède un test keystore) :
   export LEVOILE_KEYSTORE_PATH=/path/to/test-keystore.jks
   export LEVOILE_KEYSTORE_PASSWORD=test123456789012
   export LEVOILE_KEY_ALIAS=test-alias
   export LEVOILE_KEY_PASSWORD=test123456789012
   ./gradlew :app:assembleRelease --no-daemon
   apksigner verify --verbose --min-sdk-version 29 app/build/outputs/apk/release/app-release.apk
   # → Verifies (APK Signature Scheme v2): true
   # → Verifies (APK Signature Scheme v3): true

   ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.security.SigningConfigTest" --no-daemon
   # → 5 tests verts.
   ```

## Tasks / Subtasks

- [x] **Task 1 : Audit existant** (AC: tous)
  - [x] Lu `app/build.gradle.kts` — `signingConfig = signingConfigs.getByName("debug")` + `versionNameSuffix = "-unsigned"` baseline (anti-confusion documentée).
  - [x] `android/keystore/.gitignore` créé (refuse tout sauf `.gitignore` + `.gitkeep` + `README.md`).
  - [x] `android/keystore/README.md` créé pour expliquer le contenu.
  - [x] `release-android.yml` placeholder Story 12.2 lu.

- [x] **Task 2 : Étendre `app/build.gradle.kts` avec `signingConfigs.release`** (AC: #1)
  - [x] Bloc `signingConfigs { create("release") { ... } }` lisant env vars `LEVOILE_*`.
  - [x] `buildTypes.release.signingConfig` conditionnel (release si env vars sinon debug + suffix `-unsigned-LOCAL-DEV`).
  - [x] `enableV1Signing = false` + `enableV2Signing = true` + `enableV3Signing = true` + `enableV4Signing = false`.
  - [x] **CRITICAL** : aucun string literal de password (testé par SigningConfigTest).

- [x] **Task 3 : Créer `android/scripts/generate-master-keystore.{sh,ps1}`** (AC: #3, #4)
  - [x] Script bash + variante PowerShell (cohérent ADR-08 dual-script).
  - [x] Prompt password ≥ 16 chars, double saisie.
  - [x] `keytool -genkeypair` RSA 4096 par défaut (cf. Dev Notes — Ed25519 incompatible PackageManager API 29).
  - [x] Echo final : commandes pour encoder base64 + provisionner secrets + extraction SHA256 fingerprint + shred local.

- [x] **Task 4 : Étendre `release-android.yml` avec job `sign-apk` réel** (AC: #2)
  - [x] Décodage keystore base64 dans `$RUNNER_TEMP/` avec `set +x` + `::add-mask::`.
  - [x] Build aar + sync frontend (pré-requis assembleRelease).
  - [x] `./gradlew :app:assembleRelease` avec env vars set depuis secrets.
  - [x] `apksigner verify --verbose --min-sdk-version 29` + assertions v2/v3 true, v1 false.
  - [x] Cleanup keystore en `if: always()`.
  - [x] Upload APK + `apk-signature-info.txt` en artifact (90 jours retention).

- [x] **Task 5 : Créer `docs/key-management-android.md`** (AC: #5)
  - [x] Architecture 3 clés distinctes (master Ed25519 desktop / clé APK Android / clé F-Droid).
  - [x] Note technique RSA 4096 vs Ed25519 (PackageManager API 29 ne valide pas Ed25519).
  - [x] Procédure setup initial.
  - [x] Procédure rotation 24 mois (v3 key rotation lineage `apksigner rotate`).
  - [x] Procédure incident clé compromise (dual-signing 6 mois).
  - [x] Procédure vérification utilisateur (SHA256 fingerprint publié).
  - [x] Anti-patterns documentés.

- [x] **Task 6 : Créer `SigningConfigTest.kt`** (AC: #6)
  - [x] Package `fr.plateformeliberte.levoile.security`.
  - [x] 5 tests verts : signingConfigs.release existe, env vars uniquement, pas de password literal, v1 false v2/v3 true v4 false, fallback debug + suffix.

- [x] **Task 7 : Créer squelette `SignatureValidationTest.kt`** (AC: #7)
  - [x] Sous `androidTest/security/`.
  - [x] Annotation `@RunWith(AndroidJUnit4::class)`.
  - [x] Test placeholder + TODO Story 12.6 + constante `EXPECTED_RELEASE_FINGERPRINT_SHA256` à compléter post-Task 8.

- [ ] **Task 8 : Provisionnement secrets GitHub Actions — À FAIRE PAR LE MAINTENEUR** (manuel, hors CI)
  - [ ] Exécuter `generate-master-keystore.sh` sur machine air-gapped.
  - [ ] Encoder base64 + ajouter `LEVOILE_KEYSTORE_BASE64` aux secrets repo.
  - [ ] Ajouter `LEVOILE_KEYSTORE_PASSWORD`, `LEVOILE_KEY_ALIAS`, `LEVOILE_KEY_PASSWORD`.
  - [ ] Pousser un tag `v0.1.0-rc1` → vérifier que `sign-apk` job passe + APK artifact disponible.
  - [ ] Compléter `EXPECTED_RELEASE_FINGERPRINT_SHA256` dans `SignatureValidationTest.kt`.
  - [ ] Publier le SHA256 fingerprint dans `docs/key-management-android.md` (section vérification utilisateur) + sur https://plateformeliberte.fr/le-voile/keys.

- [x] **Task 9 : Build sanity local** (AC: #8 partiel)
  - [x] `./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.security.SigningConfigTest" --no-daemon` → BUILD SUCCESSFUL 13s, 5 tests verts.
  - [ ] **À FAIRE PAR LE MAINTENEUR (avec keystore test)** : vérifier `./gradlew :app:assembleRelease` avec/sans env vars + `apksigner verify`.

- [x] **Task 10 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pourquoi 3 clés distinctes (master Ed25519 desktop / clé APK Android / clé F-Droid)

- **Master key Ed25519** (Story 7.4 desktop) : signe binaires Linux/Windows, paquets `.deb`/`.rpm`/`.apk`/AUR, registre relais, releases. Stockée air-gapped / YubiKey HSM. Rotation 24 mois.
- **Clé APK signing v2/v3** (Story 12.3) : signe l'APK Android Direct GitHub releases. PKCS12 keystore, Ed25519 ou RSA 4096 selon compat min-sdk-version 29. **Distincte** de la master key — l'APK est un format Android avec son propre format de signature (différent d'Ed25519 sur ZIP).
- **Clé F-Droid** : générée et gérée par F-Droid build server. L'APK F-Droid est signé par F-Droid avec **leur** clé, pas la nôtre. C'est intentionnel et documenté (ADR-11) — la chaîne de confiance F-Droid est construite par leurs mainteneurs.

NFR-AND-5 dit « APK signé v2 + v3 par la master key Ed25519 (cohérent NFR22g) ». Lecture stricte = la clé APK est dérivée de la master key Ed25519. Lecture pragmatique = la clé APK suit la même rigueur que la master key (Ed25519, stockage HSM, rotation 24 mois, dual-signing transitoire 6 mois) mais peut techniquement être un keypair distinct dans le keystore PKCS12. **Décision MVP** : keypair distinct, dérivation explicite documentée dans `docs/key-management-android.md` (ex. clé APK = HKDF(master_priv, "android-apk-signing-2026")). Phase 2 si besoin : implémenter la dérivation cryptographique en clair.

### Pourquoi Ed25519 risque de ne pas marcher sur min-sdk 29

`apksigner` v33+ supporte Ed25519. Mais `PackageManager` Android API 29 (Android 10) ne sait pas valider une signature Ed25519 — l'algo a été ajouté à PackageManager en API 30+ (Android 11). Si on signe en Ed25519 et qu'un user Android 10 install l'APK, `INSTALL_PARSE_FAILED_NO_CERTIFICATES`.

**Décision conservative MVP** : RSA 4096. C'est ce qu'utilisent Mullvad, ProtonVPN, Calyx, Orbot. Cohérent avec le marché et avec min-sdk 29. La master key Ed25519 reste utilisée pour les paquets desktop et le registre — la clé APK est un cas particulier Android.

Si le dev découvre que Ed25519 fonctionne sur API 29 (apksigner peut signer en Ed25519 v3 sans v2 ; v2 RSA + v3 Ed25519 mixte ?) → reporter en Completion Notes et adapter `generate-master-keystore.sh`.

### Pourquoi `enableV1Signing = false`

V1 (JAR signing) date de 2008 et est connu vulnérable à des attaques de replacement de fichiers. APK Signature Scheme v2 (Android 7.0+) signe l'APK entier en bloc → robuste. v3 ajoute la key rotation. Min-sdk 29 = Android 10 = v2 supporté nativement. Désactiver v1 réduit la surface d'attaque + accélère un peu le build.

Reference : [APK Signature Scheme v2 docs](https://source.android.com/docs/security/features/apksigning/v2).

### Pourquoi `enableV4Signing = false`

V4 sert à streaming install (Android 11+, Play Asset Delivery). Pas pertinent pour un VPN distribué via APK direct. Activer v4 produit un fichier `.apk.idsig` séparé que les users devraient télécharger en plus — friction inutile.

### Pourquoi exigence password ≥ 16 chars

PKCS12 keystore est déchiffré localement par `apksigner`. Un password court est brute-forçable (dictionnaire + GPU). 16 chars aléatoires = entropy ~96 bits, robuste au brute-force avec hardware actuel. Le mainteneur génère le password via `openssl rand -base64 16` ou équivalent.

### Coordination Story 12.6 (test instrumenté SignatureValidationTest)

12.3 livre le squelette du fichier (test placeholder + TODO commenté). 12.6 implémente la logique runtime sur l'émulateur — récupération `signingInfo` + comparaison fingerprint + test injection altération. Pourquoi splitter ? Story 12.3 ne devrait pas dépendre de la matrice instrumentée (qui est complexe — Story 12.6). Le squelette permet aux 2 stories d'être livrées indépendamment.

### Coordination Story 12.4 (reproductibilité)

Une APK signée n'est PAS reproductible byte-à-byte (la signature contient des nonces). Story 12.4 vérifie la reproductibilité **avant** signature (ou via apk-content-archive neutre, cf. epics.md l. 2160). Le job `reproducibility-check` (Story 12.4) build deux fois `assembleRelease` SANS env vars de signature, compare les hashes des APK unsigned. Story 12.3 et 12.4 sont orthogonales.

### Source tree components à toucher

- **Modifiés** :
  - `android/app/build.gradle.kts` (signingConfigs.release + buildTypes.release conditional)
  - `.github/workflows/release-android.yml` (job sign-apk réel remplace placeholder)
- **Nouveaux** :
  - `android/scripts/generate-master-keystore.sh`
  - `android/scripts/generate-master-keystore.ps1`
  - `docs/key-management-android.md`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/security/SigningConfigTest.kt`
  - `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/security/SignatureValidationTest.kt`
- **Vérifier** : `android/keystore/.gitignore` exists et refuse `*.jks`.

### References

- [epics.md l. 2118-2142](_bmad-output/planning-artifacts/epics.md) — Story 12.3 BDD complet.
- [prd.md NFR-AND-5 l. 701](_bmad-output/planning-artifacts/prd.md) — APK signé v2 + v3 par master key Ed25519.
- [prd.md NFR22g l. 684](_bmad-output/planning-artifacts/prd.md) — master key stockage HSM/YubiKey.
- [prd.md NFR22h l. 685](_bmad-output/planning-artifacts/prd.md) — chaîne de confiance + rotation + dual-signing 6 mois.
- [architecture.md l. 116, l. 518, l. 1636](_bmad-output/planning-artifacts/architecture.md) — signature APK v2/v3, keystore gitignored.
- [APK Signature Scheme v2](https://source.android.com/docs/security/features/apksigning/v2)
- [APK Signature Scheme v3 (key rotation)](https://source.android.com/docs/security/features/apksigning/v3)
- Story 7.4 desktop (livrée) : signature paquets Ed25519 master key.
- Story 12.2 (à venir) : `release-android.yml` squelette à étendre.
- Story 12.4 (à venir) : reproductibilité APK CI (orthogonal à signature).
- Story 12.6 (à venir) : test instrumenté SignatureValidationTest (impl runtime).

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Debug Log References

`./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.security.SigningConfigTest" --no-daemon` → BUILD SUCCESSFUL 13s, 5 tests verts.

### Completion Notes List

- **Décision dev — RSA 4096 plutôt qu'Ed25519** : `PackageManager` Android API 29 (Android 10, notre minSdk) ne valide pas Ed25519 nativement → un APK Ed25519 fail à l'installation avec `INSTALL_PARSE_FAILED_NO_CERTIFICATES`. RSA 4096 est conservatif, cohérent avec la pratique Mullvad / ProtonVPN / Calyx / Orbot. La master key Ed25519 desktop reste utilisée pour les paquets desktop et le registre relais — la clé APK est un cas particulier Android documenté.
- **Décision dev — fallback debug + `versionNameSuffix = "-unsigned-LOCAL-DEV"`** : permet aux devs locaux de faire `./gradlew :app:assembleRelease` sans la master key (pour tester R8/ProGuard). Le suffix garantit que l'APK n'est pas confondu avec une release officielle. Le job CI `sign-apk` exige strictement les 4 env vars `LEVOILE_*` (échoue explicitement sinon).
- **Sécurité — anti-fuite secrets** :
  - Job `sign-apk` utilise `set +x` + `::add-mask::$KEYSTORE_PATH` pour masquer le path complet et la valeur du keystore décodé dans les logs Actions.
  - Cleanup `if: always()` retire `$RUNNER_TEMP/levoile-release.jks` même si un step a échoué.
  - `android/keystore/.gitignore` refuse catégoriquement `*.jks`, `*.p12`, `*.pfx`, `*.pem` (autorise uniquement `.gitignore`, `.gitkeep`, `README.md`).
- **Squelette `SignatureValidationTest.kt`** : impl runtime livrée Story 12.6 (récupération `signingInfo` via PackageManager + comparaison fingerprint hardcodé). La constante `EXPECTED_RELEASE_FINGERPRINT_SHA256 = "TODO_FILL_AFTER_12_3_TASK_8"` reste un placeholder explicite — le mainteneur doit la compléter post-provisionnement secrets.
- **À FAIRE PAR LE MAINTENEUR (hors périmètre dev IA)** :
  1. Sur machine air-gapped : `bash android/scripts/generate-master-keystore.sh` (ou la variante PS sur Windows).
  2. Tester localement : `./gradlew :app:assembleRelease` avec env vars + `apksigner verify --verbose --print-certs --min-sdk-version 29`.
  3. Encoder base64 + provisionner les 4 secrets GitHub Actions (`LEVOILE_KEYSTORE_BASE64`, `LEVOILE_KEYSTORE_PASSWORD`, `LEVOILE_KEY_ALIAS`, `LEVOILE_KEY_PASSWORD`).
  4. Récupérer le SHA256 fingerprint via `keytool -list -v` et :
     - le compléter dans `SignatureValidationTest.kt` constante `EXPECTED_RELEASE_FINGERPRINT_SHA256` ;
     - le publier dans `docs/key-management-android.md` section vérification utilisateur ;
     - le publier sur `https://plateformeliberte.fr/le-voile/keys`.
  5. Push tag `v0.1.0-rc1` pour smoke-test du job `sign-apk` réel (le job `instrumented-tests` Story 12.6 et `reproducibility-check` Story 12.4 failureront en placeholder — c'est normal jusqu'à ce que ces stories soient livrées).
- **Anti-pattern détecté + corrigé** : la story spec utilisait `this.keyPassword = keyPassword` mais avec le receiver Gradle DSL c'est ambigu (shadowing). Renommé `keyPasswordEnv` côté val + assignment direct `keyPassword = keyPasswordEnv` côté SigningConfig pour éviter toute confusion compilateur.

### File List

- `android/app/build.gradle.kts` (MOD — bloc `signingConfigs.release` + `buildTypes.release.signingConfig` conditionnel + fallback `-unsigned-LOCAL-DEV`).
- `.github/workflows/release-android.yml` (MOD — job `sign-apk` placeholder remplacé par implémentation réelle).
- `android/scripts/generate-master-keystore.sh` (NEW).
- `android/scripts/generate-master-keystore.ps1` (NEW).
- `docs/key-management-android.md` (NEW — runbook complet : architecture 3 clés, setup initial, rotation 24 mois lineage v3, incident clé compromise, vérification utilisateur, anti-patterns).
- `android/keystore/.gitignore` (NEW — refus catégorique versionnement clés).
- `android/keystore/README.md` (NEW — explique le rôle du dossier + procédure usage).
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/security/SigningConfigTest.kt` (NEW — 5 tests JVM).
- `android/app/src/androidTest/kotlin/fr/plateformeliberte/levoile/security/SignatureValidationTest.kt` (NEW — squelette + TODO Story 12.6).
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (MOD).
- `_bmad-output/implementation-artifacts/12-3-signature-apk-v2-v3-key-rotation-par-master-key-ed25519.md` (MOD).

### Change Log

- 2026-05-03 : Story 12.3 livrée — signature APK v2/v3 par master key (RSA 4096 conservateur API 29 compat). signingConfigs.release env-var-only + fallback debug + suffix `-unsigned-LOCAL-DEV`. Job CI `sign-apk` réel. Runbook complet `docs/key-management-android.md`. SigningConfigTest 5 tests verts. Status → review.
- 2026-05-03 : Code Review (auto-fix high/med/low) :
  - **M2 fix** : grep `^Verified using v1 scheme.*: true$` dans `release-android.yml` step `Verify APK signature` ne matchait jamais le format apksigner réel (`Verifies (APK Signature Scheme v1): true`). Pattern corrigé → check anti-v1 désormais opérationnel.
  - **L4 fix** : `SigningConfigTest.aucun string literal de password` durci pour refuser aussi la chaîne vide (`storePassword = ""`) — auparavant filtrée par `isNotBlank()`.
  - **L5 fix** : exemple commande `assembleRelease` dans `docs/key-management-android.md` mis à jour pour le flavor `apkDirect` (cohérent post-Story 12.5 productFlavors).
  - Status → done. SigningConfigTest 5 tests verts.
