# Le Voile · Reproducible build Android

Story 12.4 — runbook auditeur indépendant pour la vérification de reproductibilité de l'APK Le Voile.

## Quick check

```bash
git clone https://github.com/velia-the-veil/le_voile.git
cd le_voile
git checkout v0.1.0       # ou la version a auditer
bash android/scripts/build-apk-release.sh
sha256sum apk-content-archive.zip
```

Comparer le SHA256 produit avec celui publié dans [`docs/release-hashes.md`](release-hashes.md).

* **Identique** → la chaîne de confiance est validée.
* **Différent** → ouvrir une [issue GitHub](https://github.com/velia-the-veil/le_voile/issues) avec le rapport `diffoscope` (cf. section diagnostic ci-dessous).

## Pré-requis OS

L'auditeur peut soit utiliser le runner officiel F-Droid, soit reproduire manuellement le pinning.

### Option recommandée — Docker `srvz/fdroidserver`

```bash
docker run --rm -v "$PWD:/repo" -w /repo \
    registry.gitlab.com/fdroid/fdroidserver:latest \
    bash android/scripts/build-apk-release.sh
```

L'image officielle F-Droid contient JDK 17, Gradle 8.5, Android SDK API 34, NDK r25c, Go 1.22.x, gomobile pré-installés.

### Option manuelle — pinnings exacts

| Outil | Version pinnée | Vérification |
|---|---|---|
| JDK | Temurin 17.0.x | `java -version` doit afficher `version "17.0.x"` |
| Gradle | 8.5 (via `gradlew`) | `cat android/gradle/wrapper/gradle-wrapper.properties` |
| AGP | 8.5.0 | `cat android/gradle/libs.versions.toml` (ligne `agp = "8.5.0"`) |
| Kotlin | 1.9.24 | idem |
| Go | 1.22.x ou plus récent | `go version` |
| gomobile | dernier release | `go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init` |
| Android SDK | API 34 | `sdkmanager --list_installed` |
| Android NDK | r25c (25.2.9519653) | `cat $ANDROID_HOME/ndk-bundle/source.properties` ou via `setup-android` action |

## Diagnostic dérive

Si les hashes diffèrent :

```bash
# 1. Run un Build #1, sauvegarder l'APK et l'archive contenu.
bash android/scripts/build-apk-release.sh
cp android/app/build/outputs/apk/release/*.apk /tmp/build1.apk
cp apk-content-archive.zip /tmp/content1.zip

# 2. Cleanup absolu.
cd android && ./gradlew clean
rm -rf app/build/ levoile-core/build/ ~/.gradle/caches/build-cache-*

# 3. Run un Build #2.
SKIP_GOMOBILE_VERIFY=1 bash scripts/build-apk-release.sh
cp app/build/outputs/apk/release/*.apk /tmp/build2.apk
cp apk-content-archive.zip /tmp/content2.zip

# 4. Diff via diffoscope (apt-get install diffoscope).
diffoscope --html-dir /tmp/diff-report /tmp/content1.zip /tmp/content2.zip
xdg-open /tmp/diff-report/index.html
```

Sources fréquentes de dérive observées sur Android :

* **Timestamps embarqués** : `META-INF/MANIFEST.MF` ou `META-INF/CERT.SF` contiennent un `Created-By:` avec timestamp build (mitigation Story 12.4 : on exclut `META-INF/*.SF`/`*.RSA`/`*.DSA`/`*.EC` du `apk-content-archive.zip` ; `MANIFEST.MF` reste inclus mais reproductible si `tasks.withType<AbstractArchiveTask>` est appliqué — vérifier `app/build.gradle.kts`).
* **Ordre fichiers ZIP** : sans `isReproducibleFileOrder = true`, dépend du filesystem-walk. Vérifier le hook dans `app/build.gradle.kts`.
* **Métadonnées AAPT** : pour AGP 8.5.0, AAPT2 produit des resources binaires déterministes. Pas de problème connu.
* **Certificat debug** : un `app-release-unsigned-LOCAL-DEV.apk` est signé par le keystore debug AGP qui est stable PAR machine mais pas reproductible across machines. **Solution** : on compare le `apk-content-archive.zip` (signature META-INF exclue) plutôt que l'APK directement.
* **gomobile non-déterministe** : `.so` natifs Go peuvent contenir des build-id non-reproductibles. Mitigation Phase 2 : patcher `android/scripts/build-aar.sh` avec `gomobile bind -trimpath -ldflags=-buildid=`. À faire si le content SHA256 diffère malgré le `dependenciesInfo` désactivé + `AbstractArchiveTask` hook.
* **Dépendances non-pinnées** : `libs.versions.toml` et `gradle-wrapper.properties` pinnent les versions critiques. Vérifier que `~/.gradle/caches/` ne contient pas une version cached qui diffère.

## Fingerprints publiés

Voir [`docs/release-hashes.md`](release-hashes.md) pour la liste des SHA256 attendus par release.

Le fingerprint canonique est aussi publié sur `https://plateformeliberte.fr/le-voile/releases` (page chargée HTTPS, durcie HSTS, monitor warrant canary).

## Coordination autres stories Epic 12

* **Story 12.1** — la recette F-Droid `metadata/fr.plateformeliberte.levoile.yml` invoque `bash android/scripts/build-aar.sh && bash android/scripts/sync-frontend.sh` dans son `prebuild`. F-Droid utilise sa propre chaîne `gradle assembleRelease` ensuite — la reproductibilité est garantie si **les deux chaînes** appellent les mêmes scripts pré-Gradle et héritent des options `app/build.gradle.kts` + `gradle.properties` livrées Story 12.4.
* **Story 12.3** — signe l'APK avec la master key Le Voile. La signature **casse** la reproductibilité de l'APK signé (nonces) → on compare le `apk-content-archive.zip` qui exclut `META-INF/*.SF`, `*.RSA`, `*.DSA`, `*.EC`. Les 2 stories sont orthogonales.
* **Story 12.5** — introduit les flavors `apkDirect` / `fdroid`. Le contenu APK varie entre les 2 flavors (BuildConfig.AUTO_UPDATE_ENABLED), donc les hashes content diffèrent par flavor. C'est attendu et documenté.

## Références

* [Story 12.4](../_bmad-output/implementation-artifacts/12-4-verification-reproductibilite-apk-ci.md) — story file complète.
* [Reproducible Builds Project](https://reproducible-builds.org/) — pratique générale.
* [Android Reproducible Builds Wiki](https://gitlab.com/fdroid/wiki/-/wikis/Build-Process).
* [Diffoscope](https://diffoscope.org/) — outil de comparaison binaire.
* [`docs/fdroid-build-recipe.md`](fdroid-build-recipe.md) — Story 12.1 (chaîne build F-Droid).
* [`docs/key-management-android.md`](key-management-android.md) — Story 12.3 (signature, orthogonale).
* [`docs/release-hashes.md`](release-hashes.md) — fingerprints publiés.
