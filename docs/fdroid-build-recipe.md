# F-Droid build recipe — Le Voile Android

Ce document est le runbook destiné aux mainteneurs F-Droid (et à toute personne souhaitant rejouer localement le build F-Droid de Le Voile). Il accompagne le fichier [`metadata/fr.plateformeliberte.levoile.yml`](../metadata/fr.plateformeliberte.levoile.yml) versionné dans ce repo (Story 12.1).

## Vue d'ensemble

F-Droid Data lit la recette `metadata/fr.plateformeliberte.levoile.yml` à la racine du repo cible (convention upstream non négociable, exception ADR-08 documentée comme `.github/workflows/`). La recette `Builds:` invoque exactement la chaîne de scripts que nous utilisons en CI et en local :

1. `bash android/scripts/build-aar.sh` — produit `android/app/libs/levoile-core.aar` via `gomobile bind` sur les 5 shims `android/shims/{protocol,auth,crypto,registry,leakcheck}` (cf. ADR-09).
2. `bash android/scripts/sync-frontend.sh` — synchronise les assets web Android-natifs (cf. ADR-16, Story 11.1).
3. `gradle assembleRelease` — produit `app/build/outputs/apk/release/app-release-unsigned.apk` (Story 12.5 ajoute le flavor `apkDirect` / `fdroid` ; à l'issue de Story 12.5 la recette doit invoquer `assembleFdroidRelease`).

**Important** : F-Droid produit son **propre APK signé par sa clé F-Droid**, distinct de l'APK GitHub direct signé par notre master key Le Voile (Story 12.3). Les deux signatures sont **explicitement différentes** et documentées (cf. [`docs/key-management-android.md`](key-management-android.md), livré Story 12.3). La reproductibilité (Story 12.4) garantit que **les bytes du contenu APK avant signature** sont identiques d'un build à l'autre — ce que les deux clés signent au-dessus diffère, mais pas le contenu.

## Pré-requis OS

Pour rejouer le build localement (équivalent au runner F-Droid `srvz/fdroidserver:latest`) :

| Outil | Version | Notes |
|---|---|---|
| JDK | 17.0.x (Temurin recommandé) | requis par AGP 8.5.0 |
| Gradle | 8.5 | fourni par `gradlew`, déjà pinné |
| AGP | 8.5.0 | pinné dans `android/gradle/libs.versions.toml` |
| Kotlin | 1.9.24 | pinné dans `android/gradle/libs.versions.toml` |
| Go | 1.22.x ou plus récent (`go.mod` racine impose `go 1.26`) | requis par `gomobile bind` |
| gomobile | dernier release | `go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init` |
| Android SDK | API 34 (compileSdk) | `android-actions/setup-android@v3` ou `sdkmanager` |
| Android NDK | r25c (25.2.9519653) | requis pour les ABIs ARM `arm64-v8a` + `armeabi-v7a` |

## Procédure de test local — Docker `srvz/fdroidserver`

L'image officielle F-Droid Data inclut tous les pré-requis :

```bash
# Lint la recette : signale les champs manquants ou invalides.
docker run --rm \
    -v "$PWD:/repo" \
    -w /repo \
    registry.gitlab.com/fdroid/fdroidserver:latest \
    fdroid lint fr.plateformeliberte.levoile

# Build complet jusqu'à l'APK F-Droid signé. ~10-25 min cold start (image ~3 GB
# à pull la première fois). `--skip-scan` désactive le scanner de sources non-libres
# qui peut produire des faux positifs sur le code Go partagé via gomobile.
docker run --rm \
    -v "$PWD:/repo" \
    -w /repo \
    registry.gitlab.com/fdroid/fdroidserver:latest \
    fdroid build fr.plateformeliberte.levoile --skip-scan
```

L'APK signé F-Droid sort dans `unsigned/` puis `signed/` selon la phase F-Droid Data. Pour un usage purement local de validation, c'est l'APK `app-release-unsigned.apk` produit par `assembleRelease` qui sert de référence (Story 12.4 calcule son SHA256 et le compare entre 2 builds).

## Procédure de test local — sans Docker

Si Docker n'est pas disponible, rejouer la chaîne directement :

```bash
# Pré-requis : JDK 17, Go 1.22+, gomobile installé, NDK r25c configuré.
cd /chemin/vers/le_voile
bash android/scripts/build-aar.sh
bash android/scripts/sync-frontend.sh
cd android
./gradlew :app:assembleRelease --no-daemon --no-parallel --no-build-cache
# APK : android/app/build/outputs/apk/release/app-release-unsigned.apk
```

## Coordination avec les autres stories Epic 12

* **Story 12.2** — pipeline GitHub Actions `release-android.yml` invoque la même chaîne (`build-aar.sh` puis `sync-frontend.sh` puis Gradle) sans utiliser cette recette F-Droid (le pipeline Le Voile produit l'APK GitHub direct, distinct).
* **Story 12.3** — signe l'APK GitHub direct par la master key Le Voile. **F-Droid signe son propre APK** : aucune valeur des secrets `LEVOILE_KEYSTORE_*` n'est utilisée dans cette recette.
* **Story 12.4** — vérifie la reproductibilité APK byte-à-byte. La recette F-Droid hérite mécaniquement des options `dependenciesInfo { includeInApk = false }` (déjà en place) et du hook `tasks.withType<AbstractArchiveTask>` (livré Story 12.4) car ils vivent dans `app/build.gradle.kts`.
* **Story 12.5** — introduit le flavor `fdroid` qui désactive le worker auto-update (les utilisateurs F-Droid voient les mises à jour côté store). À l'issue de Story 12.5, la recette ci-dessus doit invoquer `assembleFdroidRelease` au lieu de `assembleRelease` :
  ```yaml
  Builds:
    - versionName: 0.X.0
      versionCode: N
      gradle:
        - fdroid    # remplace `yes` (= assembleRelease) par le flavor fdroid
  ```

## Mise à jour `versionCode` / `versionName`

Le test JVM `FdroidMetadataTest.versionCode YAML coherent avec build gradle kts` parse `metadata/fr.plateformeliberte.levoile.yml` et `android/app/build.gradle.kts` et **fail** si les deux divergent. Pour chaque release :

1. Bumper `versionCode` et `versionName` dans `android/app/build.gradle.kts`.
2. Bumper `CurrentVersion`, `CurrentVersionCode`, et la première entrée `Builds:` (versionCode + versionName + commit) dans `metadata/fr.plateformeliberte.levoile.yml`.
3. Tagger git : `git tag -s v0.X.0 -m "Release v0.X.0"`.
4. La CI Story 12.2 publie l'APK GitHub direct ; F-Droid détecte le tag (clé `UpdateCheckMode: Tags`) et produit son propre APK.

## Références

* [F-Droid Inclusion Policy](https://f-droid.org/en/docs/Inclusion_Policy/)
* [F-Droid Build Metadata Reference](https://f-droid.org/en/docs/Build_Metadata_Reference/)
* [F-Droid reproducible build wiki](https://gitlab.com/fdroid/wiki/-/wikis/Build-Process)
* [`metadata/fr.plateformeliberte.levoile.yml`](../metadata/fr.plateformeliberte.levoile.yml)
* [`android/app/build.gradle.kts`](../android/app/build.gradle.kts) — source de vérité `versionCode` / `versionName`.
* [`docs/key-management-android.md`](key-management-android.md) — gestion clés, livrée Story 12.3.
* [`docs/reproducible-build-android.md`](reproducible-build-android.md) — reproductibilité, livrée Story 12.4.
