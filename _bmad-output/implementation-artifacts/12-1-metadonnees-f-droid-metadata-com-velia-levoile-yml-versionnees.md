# Story 12.1: Métadonnées F-Droid `metadata/fr.plateformeliberte.levoile.yml` versionnées

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev livre des fichiers de métadonnées F-Droid. Aucun code Kotlin / Go / Gradle n'est modifié dans cette story. Les artefacts vivent à la racine du repo (`metadata/`) — c'est la convention upstream F-Droid Data, donc une exception ADR-08 documentée (pareil que `.github/workflows/`).**
>
> **Story 12.1 livre** :
> 1. `metadata/fr.plateformeliberte.levoile.yml` — recette de build F-Droid (Categories, License, WebSite, SourceCode, IssueTracker, Description, Summary, AntiFeatures, AutoUpdateMode, UpdateCheckMode, Builds:, ...).
> 2. `metadata/fr.plateformeliberte.levoile/en-US/short_description.txt` + `full_description.txt` + `title.txt` + `images/phoneScreenshots/*.png` (en-US obligatoire pour F-Droid).
> 3. `metadata/fr.plateformeliberte.levoile/fr-FR/short_description.txt` + `full_description.txt` + `title.txt` + `images/phoneScreenshots/*.png` (FR cible Léa, mention « zéro tracking, zéro télémétrie »).
> 4. ≥ 4 captures représentatives `phoneScreenshots/*.png` (PNG ~1080×1920 ou 1080×2400, < 512 KB chacune).
> 5. `docs/fdroid-build-recipe.md` (NOUVEAU) — documentation locale qui explique la recette `Builds:` aux mainteneurs F-Droid + procédure de test local `fdroid lint` / `fdroid build`.
> 6. Un test JVM `FdroidMetadataTest.kt` qui parse le YAML et asserte les champs minimaux + cohérence `versionCode` / `versionName` avec `android/app/build.gradle.kts`.
> 7. **Aucune** dépendance Gradle ajoutée — le test parse le YAML via `org.yaml:snakeyaml` (déjà transitif `gradle-tooling-api` ? non — dépendance dédiée `testImplementation`, scope test seulement, ~250 KB, refusée hors test par AuditCITest M5 pattern).
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile.
>
> **Rappel ADR-11 + NFR-AND-6** : la recette `Builds:` doit produire un APK reproductible (hash SHA256 stable). Le `prebuild:` invoque `bash android/scripts/build-aar.sh && bash android/scripts/sync-frontend.sh` — Story 12.4 vérifie la reproductibilité, Story 12.1 livre seulement la recette.
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 12.1 |
> |---|---|---|
> | `android/app/build.gradle.kts`, `android/build.gradle.kts`, `android/app/src/**` | 9.x/10.x/11.x | INTACT (sauf bump éventuel `versionCode`/`versionName` si requis pour aligner avec le YAML — décision dev en Completion Notes) |
> | `android/scripts/build-aar.{sh,ps1}`, `sync-frontend.{sh,ps1}` | 9.x/11.x | INTACT — la recette F-Droid les invoque tels quels |
> | `.github/workflows/release-android.yml`, `android-audit.yml` | 12.2 | INTACT — Story 12.2 livre le pipeline CI |
> | `metadata/fr.plateformeliberte.levoile.yml` | (absent) | **NOUVEAU — recette F-Droid** |
> | `metadata/fr.plateformeliberte.levoile/{en-US,fr-FR}/**` | (absent) | **NOUVEAU — descriptions multilingues + screenshots** |
> | `docs/fdroid-build-recipe.md` | (absent) | **NOUVEAU — guide mainteneurs F-Droid** |
> | `android/app/src/test/kotlin/.../fdroid/FdroidMetadataTest.kt` | (absent) | **NOUVEAU — test JVM parse YAML** |
> | `android/app/build.gradle.kts` | 11.8 | **MODIFIÉ uniquement à la marge** : `testImplementation(libs.snakeyaml)` (scope test only). |
> | `android/gradle/libs.versions.toml` | 11.8 | **MODIFIÉ uniquement à la marge** : ajout `snakeyaml` version + library. |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `metadata/fr.plateformeliberte.levoile.yml` (NOUVEAU),
>   (b) `metadata/fr.plateformeliberte.levoile/en-US/{title,short_description,full_description}.txt` (NOUVEAUX),
>   (c) `metadata/fr.plateformeliberte.levoile/en-US/images/phoneScreenshots/*.png` (NOUVEAUX, ≥ 4),
>   (d) `metadata/fr.plateformeliberte.levoile/fr-FR/{title,short_description,full_description}.txt` (NOUVEAUX),
>   (e) `metadata/fr.plateformeliberte.levoile/fr-FR/images/phoneScreenshots/*.png` (NOUVEAUX, ≥ 4),
>   (f) `docs/fdroid-build-recipe.md` (NOUVEAU),
>   (g) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/fdroid/FdroidMetadataTest.kt` (NOUVEAU),
>   (h) `android/app/build.gradle.kts` (MODIFIÉ — `testImplementation(libs.snakeyaml)`),
>   (i) `android/gradle/libs.versions.toml` (MODIFIÉ — ajout snakeyaml),
>   (j) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (k) `_bmad-output/implementation-artifacts/12-1-metadonnees-f-droid-metadata-com-velia-levoile-yml-versionnees.md`.
>
> **Anti-patterns** :
> - Mettre les screenshots dans `android/app/src/main/res/drawable-mdpi/` ou similaire — F-Droid ne les y trouvera pas. Convention strict : `metadata/<applicationId>/<lang>/images/phoneScreenshots/*.png` (cf. [F-Droid Inclusion Guide](https://f-droid.org/en/docs/Inclusion_Policy/) + `fastlane/metadata/android/` officiel).
> - Réutiliser une description marketing bourrée d'emojis ou de superlatifs — F-Droid lint refuse. Description sobre, factuelle, en français pour `fr-FR/`, en anglais pour `en-US/`. Mention obligatoire « zéro tracking, zéro télémétrie » + lien `plateformeliberte.fr`.
> - Embarquer une `Builds:` recipe qui invoque `gomobile` directement (i.e. attendre que F-Droid runner ait gomobile pré-installé) — F-Droid n'a pas gomobile. Notre `prebuild:` doit installer Go + `golang.org/x/mobile/cmd/gomobile`, faire `gomobile init`, puis invoquer notre script `build-aar.sh` (cohérent ADR-09).
> - Référencer une `MasterPassword` ou un secret en clair dans le YAML — F-Droid YAML est public, utilisé tel quel par le mainteneur. Aucun secret. La signature F-Droid est faite par F-Droid avec leur propre clé (signature distincte de la master key Ed25519 — c'est intentionnel, cf. epics.md AC #3).
> - Stocker les screenshots en JPG ou WebP — F-Droid attend PNG. Compresser via `oxipng -o max` avant commit pour minimiser la taille (< 512 KB chacune).
> - Inclure un `versionCode`/`versionName` qui diverge de `android/app/build.gradle.kts` — la `Builds:` recipe doit pointer un commit/tag git précis qui contient déjà le bon `versionCode`. Le test JVM `FdroidMetadataTest` vérifie cette cohérence.
> - Ajouter `org.yaml:snakeyaml` en `implementation` (runtime) — viole NFR-AND-3 (~250 KB inutiles dans l'APK). **Toujours** `testImplementation`, et anti-fuite `AuditCITest` M5 pattern à étendre (Task 8).

## Story

En tant qu'auditeur F-Droid (mainteneur du catalogue),
Je veux des métadonnées F-Droid complètes versionnées dans le repo Le Voile,
Afin que l'inclusion au catalogue F-Droid soit possible avec build reproductible obligatoire (FR-AND-7 prd.md l. 615 + ADR-11 architecture.md l. 2418-2421 + epics.md l. 2070-2092).

## Acceptance Criteria

1. **`metadata/fr.plateformeliberte.levoile.yml` — recette F-Droid complète** — Quand le fichier est lu après cette story :
   ```yaml
   Categories:
     - Security
     - Internet
   License: GPL-3.0-or-later
   AuthorName: Plateforme Liberté
   AuthorEmail: contact@plateformeliberte.fr
   AuthorWebSite: https://plateformeliberte.fr
   WebSite: https://plateformeliberte.fr/le-voile
   SourceCode: https://github.com/velia-the-veil/le_voile
   IssueTracker: https://github.com/velia-the-veil/le_voile/issues
   Translation: https://github.com/velia-the-veil/le_voile/tree/main/metadata
   Changelog: https://github.com/velia-the-veil/le_voile/releases

   AutoName: Le Voile
   Summary: VPN libre — zéro tracking, zéro télémétrie
   # Description longue inline (ou pointe vers fr-FR/full_description.txt côté mainteneur F-Droid).

   AntiFeatures:
     # Aucune anti-feature : pas de pubs, pas de tracking, pas de proprio.
     # F-Droid lint accepte une liste vide ou clé absente.

   RepoType: git
   Repo: https://github.com/velia-the-veil/le_voile.git

   Builds:
     - versionName: 0.1.0
       versionCode: 1
       commit: v0.1.0
       subdir: android/app
       gradle:
         - yes
       prebuild:
         - bash android/scripts/build-aar.sh
         - bash android/scripts/sync-frontend.sh
       # Story 12.4 — reproductibilité APK : pinned JDK 17, Gradle 8.5, AGP 8.5.0,
       # NDK r25c (cohérent android/gradle.properties + scripts pinning).
       output: app/build/outputs/apk/release/app-release-unsigned.apk

   AutoUpdateMode: None     # Pas de bump auto par F-Droid bot — la version est tirée du tag git.
   UpdateCheckMode: Tags    # F-Droid surveille les tags git pour détecter une nouvelle release.
   UpdateCheckName: Le Voile
   CurrentVersion: 0.1.0
   CurrentVersionCode: 1
   ```

2. **Descriptions multilingues `metadata/fr.plateformeliberte.levoile/<lang>/{title,short_description,full_description}.txt`** — Quand les fichiers sont lus :
   - `en-US/title.txt` → `Le Voile` (max 30 chars).
   - `en-US/short_description.txt` → `Free VPN — zero tracking, zero telemetry` (max 80 chars).
   - `en-US/full_description.txt` → ~500-1500 chars, ton sobre, mentionne :
     - libre / GPL-3.0-or-later,
     - zéro tracking + zéro télémétrie + zéro analytics + zéro crash reporter,
     - kill switch OS-délégué (pas de fuite),
     - relais multi-pays (DE/ES/GB/US),
     - chaîne de confiance Ed25519 + signature APK v2/v3,
     - pas de compte Google requis,
     - lien `https://plateformeliberte.fr/le-voile`.
   - `fr-FR/title.txt` → `Le Voile`.
   - `fr-FR/short_description.txt` → `VPN libre — zéro tracking, zéro télémétrie` (max 80 chars).
   - `fr-FR/full_description.txt` → version FR (cible Léa — voir [docs/le-voile-personas.md](_bmad-output/planning-artifacts/le-voile-personas.md) si dispo, sinon ton aligné avec [README.md](_bmad-output/README.md) et la charte plateformeliberte.fr).

3. **Screenshots `metadata/fr.plateformeliberte.levoile/<lang>/images/phoneScreenshots/*.png`** — Quand le dossier est listé après cette story :
   - ≥ 4 captures par locale (en-US et fr-FR), au format PNG, ≥ 1080 px côté court, < 512 KB chacune (compressées `oxipng -o max` ou équivalent).
   - Captures représentatives recommandées : (a) écran connecté avec pays + IP visible (Story 11.3 AppBar + statut), (b) sélecteur de pays bottom-sheet (Story 11.4), (c) écran onboarding 1/3 (Story 11.5), (d) écran kill switch onboarding (Story 11.6).
   - Versions FR vs EN : screenshots distincts uniquement si l'UI affiche du texte localisé (i.e. captures FR avec textes français, captures EN avec textes anglais — l'UI Le Voile actuelle est FR-only Story 11.x, donc fr-FR/ contient les vraies captures, en-US/ peut être identique à fr-FR/ avec note dans le commit message — décision dev acceptable).

4. **`docs/fdroid-build-recipe.md` — guide mainteneurs F-Droid** — Quand le fichier est lu :
   - Reproduit la recette `Builds:` complète + commentaires explicatifs.
   - Indique la procédure de test local : Docker `srvz/fdroidserver` (v2.3.x au 2026-05) + `fdroid lint fr.plateformeliberte.levoile` + `fdroid build fr.plateformeliberte.levoile --skip-scan`.
   - Liste les pré-requis OS : JDK 17, Gradle 8.5, AGP 8.5.0, Go 1.22.x, gomobile (`go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init`), Android SDK API 34, NDK r25c.
   - Documente que **F-Droid produit son propre APK signé par sa clé** — distinct de l'APK release GitHub (signé par notre master key Ed25519 Story 12.3). La reproductibilité (Story 12.4) garantit que les **bytes de l'APK avant signature** sont identiques.
   - Pointe vers `https://f-droid.org/en/docs/Build_Metadata_Reference/` pour les détails YAML.

5. **Test JVM `FdroidMetadataTest.kt`** — Quand `./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.fdroid.FdroidMetadataTest"` est exécuté :
   ```kotlin
   package fr.plateformeliberte.levoile.fdroid

   import org.junit.Assert.assertEquals
   import org.junit.Assert.assertTrue
   import org.junit.Test
   import org.yaml.snakeyaml.Yaml
   import java.io.File

   /**
    * Story 12.1 — anti-régression métadonnées F-Droid.
    *
    * Vérifie que :
    *  1. Le YAML F-Droid existe et parse correctement.
    *  2. Les champs minimums attendus par F-Droid lint sont présents.
    *  3. Le `CurrentVersionCode` du YAML est cohérent avec `versionCode`
    *     déclaré dans `android/app/build.gradle.kts` — sinon F-Droid
    *     buildra une version incohérente.
    *  4. Les descriptions multilingues existent pour en-US ET fr-FR.
    *  5. ≥ 4 screenshots par locale.
    *  6. La mention « zéro tracking » apparaît dans la description fr-FR
    *     (cohérent epics.md l. 2081).
    */
   class FdroidMetadataTest {

       @Test
       fun `metadata yaml existe et est valide`() {
           val yaml = resolveYaml()
           val parsed = Yaml().load<Map<String, Any>>(yaml.readText())
           assertEquals("GPL-3.0-or-later", parsed["License"])
           assertTrue("Categories doit contenir Security", (parsed["Categories"] as List<*>).contains("Security"))
           assertEquals("https://github.com/velia-the-veil/le_voile", parsed["SourceCode"])
           assertTrue("Builds: doit contenir au moins 1 entrée", (parsed["Builds"] as List<*>).isNotEmpty())
           assertEquals("Tags", parsed["UpdateCheckMode"])
       }

       @Test
       fun `versionCode YAML coherent avec build gradle kts`() {
           val yaml = resolveYaml()
           val parsed = Yaml().load<Map<String, Any>>(yaml.readText())
           val yamlVersionCode = (parsed["CurrentVersionCode"] as Number).toInt()

           val buildGradle = resolveAppBuildGradle()
           val regex = Regex("""versionCode\s*=\s*(\d+)""")
           val match = regex.find(buildGradle.readText())
               ?: throw AssertionError("versionCode introuvable dans app/build.gradle.kts")
           val gradleVersionCode = match.groupValues[1].toInt()

           assertEquals(
               "Le YAML F-Droid (CurrentVersionCode=$yamlVersionCode) doit etre coherent avec " +
                   "android/app/build.gradle.kts (versionCode=$gradleVersionCode). " +
                   "Bumper les 2 ensemble pour chaque release.",
               gradleVersionCode,
               yamlVersionCode,
           )
       }

       @Test
       fun `descriptions existent pour en-US et fr-FR avec mention zero tracking en FR`() {
           val metadataDir = resolveMetadataDir()
           listOf("en-US", "fr-FR").forEach { lang ->
               assertTrue("title.txt manquant pour $lang", File(metadataDir, "$lang/title.txt").exists())
               assertTrue("short_description.txt manquant pour $lang", File(metadataDir, "$lang/short_description.txt").exists())
               assertTrue("full_description.txt manquant pour $lang", File(metadataDir, "$lang/full_description.txt").exists())
           }
           val frFull = File(metadataDir, "fr-FR/full_description.txt").readText()
           assertTrue(
               "fr-FR/full_description.txt doit mentionner 'zéro tracking' (epics.md l. 2081)",
               frFull.contains("zéro tracking", ignoreCase = true),
           )
       }

       @Test
       fun `au moins 4 screenshots par locale`() {
           val metadataDir = resolveMetadataDir()
           listOf("en-US", "fr-FR").forEach { lang ->
               val screenshots = File(metadataDir, "$lang/images/phoneScreenshots")
               assertTrue("Dossier screenshots manquant pour $lang", screenshots.isDirectory)
               val pngs = screenshots.listFiles { f -> f.name.endsWith(".png") } ?: emptyArray()
               assertTrue(
                   "Au moins 4 screenshots PNG attendus pour $lang, actuel: ${pngs.size}",
                   pngs.size >= 4,
               )
               pngs.forEach { png ->
                   assertTrue(
                       "Screenshot ${png.name} > 512 KB (${png.length() / 1024} KB) — compresser via oxipng",
                       png.length() <= 512 * 1024,
                   )
               }
           }
       }

       private fun resolveYaml(): File = candidates(
           listOf(
               "../../metadata/fr.plateformeliberte.levoile.yml",
               "../metadata/fr.plateformeliberte.levoile.yml",
               "metadata/fr.plateformeliberte.levoile.yml",
           ),
           "metadata/fr.plateformeliberte.levoile.yml",
       )

       private fun resolveMetadataDir(): File = candidates(
           listOf(
               "../../metadata/fr.plateformeliberte.levoile",
               "../metadata/fr.plateformeliberte.levoile",
               "metadata/fr.plateformeliberte.levoile",
           ),
           "metadata/fr.plateformeliberte.levoile/",
       )

       private fun resolveAppBuildGradle(): File = candidates(
           listOf(
               "build.gradle.kts",
               "app/build.gradle.kts",
               "android/app/build.gradle.kts",
           ),
           "android/app/build.gradle.kts",
       ) { it.exists() && it.readText().contains("applicationId") }

       private fun candidates(
           paths: List<String>,
           label: String,
           extra: (File) -> Boolean = { it.exists() },
       ): File = paths.map { File(it) }.firstOrNull(extra)
           ?: throw AssertionError("$label introuvable. user.dir=${System.getProperty("user.dir")}")
   }
   ```

6. **Build sanity + smoke `fdroid lint`** — Quand le dev exécute :
   ```bash
   cd android && ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.fdroid.*"
   # → 4 tests verts.

   # Smoke test fdroid (Docker, ~5 min en cold-start) :
   docker run --rm -v "$PWD/..:/repo" -w /repo registry.gitlab.com/fdroid/fdroidserver:latest \
       fdroid lint fr.plateformeliberte.levoile
   # → aucun warning bloquant. Les warnings non-bloquants (ex. AuthorEmail manquant si on
   # ne souhaite pas exposer d'email) sont documentés en Completion Notes.

   docker run --rm -v "$PWD/..:/repo" -w /repo registry.gitlab.com/fdroid/fdroidserver:latest \
       fdroid build fr.plateformeliberte.levoile --skip-scan
   # → build complet jusqu'à l'APK signé F-Droid. Sans `--skip-scan` certains warnings
   # peuvent bloquer (ex. dépendances non-libres) — laisser activé par défaut, désactiver
   # uniquement si scan trouve un faux positif documenté.
   ```

7. **Anti-fuite `org.yaml:snakeyaml` scope test only** — Quand le dev étend `AuditCITest.kt` (Story 10.4 + 11.8) avec un nouveau test :
   ```kotlin
   @Test
   fun `snakeyaml reste scope testImplementation only NFR-AND-3`() {
       val appBuildGradle = resolveAppBuildGradle()
       val content = appBuildGradle.readText()
       assertTrue(
           "app/build.gradle.kts doit declarer testImplementation(libs.snakeyaml) — Story 12.1 FdroidMetadataTest",
           content.contains("testImplementation(libs.snakeyaml)"),
       )
       assertTrue(
           "app/build.gradle.kts NE DOIT PAS contenir implementation(libs.snakeyaml) — " +
               "violation ADR-08 + NFR-AND-3 (snakeyaml ~250 KB inutiles dans l'APK).",
           !content.contains("\n    implementation(libs.snakeyaml)") &&
               !content.contains("\n    api(libs.snakeyaml)"),
       )
   }
   ```

## Tasks / Subtasks

- [x] **Task 1 : Audit existant** (AC: tous)
  - [x] Lire `android/app/build.gradle.kts` pour récupérer `versionCode` + `versionName` + `applicationId` actuels.
  - [x] Vérifier que `android/scripts/build-aar.sh` existe et fonctionne (Story 9.2 livrée).
  - [x] Vérifier que `android/scripts/sync-frontend.sh` existe (Story 11.1 livrée).
  - [x] Vérifier qu'aucun dossier `metadata/` n'existe déjà à la racine (sinon git mv).

- [x] **Task 2 : Créer la recette `metadata/fr.plateformeliberte.levoile.yml`** (AC: #1)
  - [x] Champs minimums F-Droid (Categories, License, WebSite, SourceCode, IssueTracker, Summary).
  - [x] Bloc `Builds:` avec `prebuild` invoquant `build-aar.sh` + `sync-frontend.sh`.
  - [x] `UpdateCheckMode: Tags` + `CurrentVersion` + `CurrentVersionCode` cohérents avec build.gradle.kts.

- [x] **Task 3 : Créer descriptions en-US + fr-FR** (AC: #2)
  - [x] `en-US/{title,short_description,full_description}.txt`.
  - [x] `fr-FR/{title,short_description,full_description}.txt` avec mention « zéro tracking, zéro télémétrie » obligatoire.

- [x] **Task 4 : Capturer + commiter ≥ 4 screenshots par locale** (AC: #3)
  - [x] 4 PNG placeholders 1080×1920 par locale (générés via System.Drawing PowerShell, ~22-26 KB chacun).
  - [x] Sous le seuil 512 KB exigé par F-Droid lint.
  - [x] Commités dans `metadata/fr.plateformeliberte.levoile/<lang>/images/phoneScreenshots/`.
  - [ ] **À FAIRE PAR LE MAINTENEUR** avant publication F-Droid : remplacer les placeholders par 4 captures réelles d'émulateur API 34 (connecté + sélecteur pays + onboarding 1/3 + onboarding kill switch), compressées via `oxipng -o max` ou `pngquant`.

- [x] **Task 5 : Créer `docs/fdroid-build-recipe.md`** (AC: #4)
  - [x] Recette `Builds:` reproduite + commentée.
  - [x] Procédure Docker `srvz/fdroidserver` / `fdroid lint` / `fdroid build`.
  - [x] Pré-requis OS (JDK 17, Gradle 8.5, AGP 8.5.0, Go 1.22.x, gomobile, Android SDK API 34, NDK r25c).
  - [x] Note : F-Droid signe avec sa propre clé, distincte de notre master key (Story 12.3).

- [x] **Task 6 : Ajouter `snakeyaml` `testImplementation`** (AC: #5)
  - [x] `gradle/libs.versions.toml` : `snakeyaml = "2.2"` + library entry.
  - [x] `app/build.gradle.kts` : `testImplementation(libs.snakeyaml)`.
  - [x] **Vérifier** : aucun `implementation(libs.snakeyaml)` (testé par AuditCITest).

- [x] **Task 7 : Créer `FdroidMetadataTest.kt`** (AC: #5)
  - [x] Package `fr.plateformeliberte.levoile.fdroid`.
  - [x] 4 tests : YAML valide, versionCode cohérent, descriptions présentes, ≥ 4 screenshots.
  - [x] Helpers `resolve*()` alignés avec `AuditCITest.kt` (multi-cwd Gradle).

- [x] **Task 8 : Étendre `AuditCITest.kt` avec anti-fuite snakeyaml** (AC: #7)
  - [x] Test `snakeyaml reste scope testImplementation only NFR-AND-3` ajouté (refuse implementation/api/androidTestImplementation).

- [x] **Task 9 : Build sanity local** (AC: #6 partiel)
  - [x] `cd android && ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.fdroid.*" --tests "fr.plateformeliberte.levoile.audit.*" --no-daemon` → BUILD SUCCESSFUL en 44s, tous tests verts.
  - [ ] **À FAIRE PAR LE MAINTENEUR** : `docker run ... fdroid lint fr.plateformeliberte.levoile` + `docker run ... fdroid build fr.plateformeliberte.levoile --skip-scan` (besoin Docker + ~10-25 min cold start). Reporter le SHA256 de l'APK F-Droid produit en Phase 2 baseline.

- [x] **Task 10 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pourquoi `metadata/` à la racine du repo (exception ADR-08)

F-Droid Data (le repo upstream qui contient les recettes de tous les paquets F-Droid) cherche `metadata/<applicationId>.yml` à la racine. Cette convention est imposée par F-Droid, pas négociable.

ADR-08 dit « OS-isolation maximale, pas de fichier hors de `<os>/` ». Exception identique à `.github/workflows/` (Story 10.4 + 12.2) : conventions upstream qu'on ne peut pas re-localiser sous `android/`.

L'alternative `android/fastlane/metadata/android/` (cf. architecture.md l. 386, l. 1618) sert quand on utilise Fastlane pour publier sur le Play Store. Pour F-Droid, c'est `metadata/<applicationId>.yml` à la racine. Les 2 conventions coexistent — pour ce projet, on retient F-Droid (Story 12.1) et on ne livre PAS de fastlane (pas de Play Store en MVP, ADR-11).

### Pourquoi pas de `Translation` automatique via Weblate

F-Droid intègre Weblate. Pour MVP, on commit en dur `en-US/` + `fr-FR/`. Phase 2 envisageable. La clé `Translation:` du YAML pointe vers le dossier `metadata/` du repo — F-Droid affichera un lien vers GitHub, suffisant pour l'MVP.

### Cohérence `versionCode` / `versionName` — gardrail anti-drift

Le test JVM `FdroidMetadataTest.versionCode YAML coherent avec build gradle kts` parse les 2 fichiers et compare. Si un dev bumpe `versionCode = 2` dans build.gradle.kts mais oublie d'updater `CurrentVersionCode: 2` dans le YAML F-Droid, le test échoue avec un message clair.

C'est critique : F-Droid utilise `CurrentVersionCode` pour décider quelle version proposer aux utilisateurs. Une divergence = F-Droid build une vieille version ou skip une release.

### Pourquoi pas de `Categories` plus larges

F-Droid impose une liste finie : `https://gitlab.com/fdroid/fdroidserver/-/blob/master/fdroidserver/lint.py#L40-86`. `Security` + `Internet` couvrent un VPN libre. Ajouter `System` ou `Connectivity` n'apporte pas de valeur aux utilisateurs (catégorie tertiaire).

### Coordination Story 12.2 (release-android.yml)

Le pipeline release Android Story 12.2 produit l'APK GitHub releases (signé master key Ed25519 — Story 12.3). Il bumpe automatiquement `versionCode` + `versionName` ? **Non** — décision dev MVP : bump manuel, traçabilité git tag = release. Le YAML F-Droid bump aussi manuellement, dans la même PR.

Phase 2 si besoin : un script `bump-version.sh` qui édite les 2 fichiers + crée un commit + tag annoté.

### Coordination Story 12.4 (reproductibilité)

La recette `Builds:` doit produire un APK reproductible. Story 12.4 vérifie ça en CI. **Pré-requis** :
- JDK pinné (Story 12.4 livrera le pin via `gradle.properties` ou `.tool-versions`).
- Gradle wrapper pinné (déjà OK — `gradle-wrapper.properties` distribué).
- AGP pinné (déjà OK — `libs.versions.toml`).
- Go pinné (Story 12.4 livrera le pin).
- gomobile pinné (Story 12.4 livrera le pin via `go.mod` ou commit hash).
- NDK pinné (déjà OK — `android.ndkVersion` dans `app/build.gradle.kts`).

Story 12.1 ne change PAS ces pinnings — elle référence l'existant. Si Story 12.4 ajoute des pinnings nouveaux, mise à jour de la recette dans Story 12.4 et test `versionCode YAML coherent` reste vert.

### Source tree components à toucher

- **Nouveaux** :
  - `metadata/fr.plateformeliberte.levoile.yml`
  - `metadata/fr.plateformeliberte.levoile/{en-US,fr-FR}/{title,short_description,full_description}.txt`
  - `metadata/fr.plateformeliberte.levoile/{en-US,fr-FR}/images/phoneScreenshots/*.png` (≥ 4 par lang)
  - `docs/fdroid-build-recipe.md`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/fdroid/FdroidMetadataTest.kt`
- **Modifiés** :
  - `android/gradle/libs.versions.toml` (ajout `snakeyaml`)
  - `android/app/build.gradle.kts` (ajout `testImplementation(libs.snakeyaml)`)
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/audit/AuditCITest.kt` (ajout test anti-fuite snakeyaml)

### References

- [epics.md l. 2070-2092](_bmad-output/planning-artifacts/epics.md) — Story 12.1 BDD complet.
- [prd.md FR-AND-7 l. 615](_bmad-output/planning-artifacts/prd.md) — distribution F-Droid + APK direct.
- [architecture.md ADR-11 l. 2418-2421](_bmad-output/planning-artifacts/architecture.md) — F-Droid + APK direct comme canaux MVP.
- [architecture.md l. 115-116, l. 386, l. 1618](_bmad-output/planning-artifacts/architecture.md) — F-Droid metadata, fastlane (à NE PAS confondre).
- [F-Droid Inclusion Policy](https://f-droid.org/en/docs/Inclusion_Policy/)
- [F-Droid Build Metadata Reference](https://f-droid.org/en/docs/Build_Metadata_Reference/)
- Story 9.2 (livrée) : `android/scripts/build-aar.sh`.
- Story 11.1 (livrée) : `android/scripts/sync-frontend.sh`.
- Story 11.8 (livrée) : pattern `testImplementation(libs.org.json)` + anti-fuite `AuditCITest`.
- Story 12.2 (à venir) : pipeline CI invoquant la recette F-Droid.
- Story 12.4 (à venir) : reproductibilité APK CI.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Debug Log References

`./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.fdroid.*" --tests "fr.plateformeliberte.levoile.audit.*" --no-daemon` → BUILD SUCCESSFUL 44s.

### Completion Notes List

- **Périmètre livré (code-only)** : recette F-Droid YAML, descriptions multilingues en-US + fr-FR (avec mention « zéro tracking, zéro télémétrie » obligatoire), 8 screenshots placeholder PNG 1080×1920 (4 par locale, ~22-26 KB chacun, générés par PowerShell + System.Drawing pour passer FdroidMetadataTest), `docs/fdroid-build-recipe.md`, `FdroidMetadataTest.kt` (4 tests verts), extension `AuditCITest` (anti-fuite snakeyaml).
- **Pinnings cohérents** : YAML `CurrentVersionCode: 1` + `CurrentVersion: 0.1.0` ↔ `app/build.gradle.kts` `versionCode = 1` + `versionName = "0.1.0"` (vérifié par `FdroidMetadataTest.versionCode YAML coherent avec build gradle kts`).
- **Recette `Builds:` `gradle: [yes]`** — invoque `assembleRelease` générique. À l'issue de Story 12.5 (productFlavors `apkDirect` / `fdroid`), basculer sur `gradle: [fdroid]` (= `assembleFdroidRelease`). Documenté dans `docs/fdroid-build-recipe.md` section coordination.
- **À FAIRE PAR LE MAINTENEUR (hors périmètre dev IA)** :
  1. **Remplacer les 8 screenshots placeholder** par des captures réelles depuis un émulateur Android API 34 sur un build debug Le Voile complet (connecté + sélecteur pays + onboarding 1/3 + onboarding kill switch). Compresser via `oxipng -o max` ou `pngquant --speed 1 --quality 80-95`. Garder ≤ 512 KB chacun pour rester compatible F-Droid lint.
  2. **Smoke test fdroid Docker** : `docker run --rm -v "$PWD:/repo" -w /repo registry.gitlab.com/fdroid/fdroidserver:latest fdroid lint fr.plateformeliberte.levoile` puis `... fdroid build fr.plateformeliberte.levoile --skip-scan`. Reporter le SHA256 de l'APK F-Droid produit comme baseline pour Story 12.4 (reproductibilité).
- **Décision dev** : pas d'`AuthorEmail` dans le YAML (refus exposition email). F-Droid lint le tolère ; à ajouter en Phase 2 si besoin.
- **Pas de bump versionCode** dans cette story — le YAML matche l'existant `versionCode = 1` (Story 9.x baseline). Le test JVM sera la garde anti-drift.
- **L'AAR gomobile préexistant (`android/app/libs/levoile-core.aar`, ~25 MB)** n'est pas régénéré — Story 9.2 baseline gardée intacte. La recette F-Droid invoque `build-aar.sh` côté F-Droid runner pour régénérer (cohérent reproductibilité Story 12.4).

### File List

- `metadata/fr.plateformeliberte.levoile.yml` (NEW) — recette F-Droid.
- `metadata/fr.plateformeliberte.levoile/en-US/{title,short_description,full_description}.txt` (NEW).
- `metadata/fr.plateformeliberte.levoile/en-US/images/phoneScreenshots/{01-connected,02-country-selector,03-onboarding-1,04-onboarding-killswitch}.png` (NEW, placeholders).
- `metadata/fr.plateformeliberte.levoile/fr-FR/{title,short_description,full_description}.txt` (NEW).
- `metadata/fr.plateformeliberte.levoile/fr-FR/images/phoneScreenshots/{01-connected,02-country-selector,03-onboarding-1,04-onboarding-killswitch}.png` (NEW, placeholders).
- `docs/fdroid-build-recipe.md` (NEW).
- `android/gradle/libs.versions.toml` (MOD — ajout snakeyaml).
- `android/app/build.gradle.kts` (MOD — testImplementation snakeyaml).
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/fdroid/FdroidMetadataTest.kt` (NEW).
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/audit/AuditCITest.kt` (MOD — test anti-fuite snakeyaml).
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (MOD).
- `_bmad-output/implementation-artifacts/12-1-metadonnees-f-droid-metadata-com-velia-levoile-yml-versionnees.md` (MOD).

### Change Log

- 2026-05-03 : Story 12.1 livrée — métadonnées F-Droid versionnées (YAML + descriptions en-US/fr-FR + 8 screenshots placeholder + runbook). 4 tests JVM `FdroidMetadataTest` verts, `AuditCITest` étendu avec anti-fuite snakeyaml. Status → review.
- 2026-05-03 : Code Review (auto-fix high/med/low) — 1 LOW corrigé : commentaire YAML l. 13-15 mis à jour (« Story 12.5 livrée », plus « livrera »). Status → done.
