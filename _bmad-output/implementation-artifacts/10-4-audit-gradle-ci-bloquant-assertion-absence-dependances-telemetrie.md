# Story 10.4: Audit Gradle CI bloquant — assertion absence dépendances télémétrie

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler MAJORITAIREMENT depuis le sous-dossier `android/` du repo. UNE EXCEPTION DOCUMENTÉE est autorisée pour le workflow GitHub Actions à la racine — voir ci-dessous.**
>
> ### EXCEPTION ADR-08 EXPLICITE — Workflow GitHub Actions
>
> Cette story doit créer un **nouveau workflow GitHub Actions** sous `.github/workflows/` (à la racine du repo). C'est par construction l'unique emplacement accepté par GitHub Actions (cf. spec officielle GitHub : tous les workflows DOIVENT vivre sous `.github/workflows/<name>.yml` dans le dépôt racine — aucune exception, aucune sub-config possible).
>
> **C'est cohérent avec le précédent existant** : `.github/workflows/release.yml` (Story 7.x desktop), `.github/workflows/aur-publish.yml` (Story 7.3 AUR Linux). Le repo a déjà 2 workflows à la racine — cette story en ajoute un 3ème dédié Android. Cohérent architecture.md l. 397-399 (« release-android.yml » prévu) — bien que cette story ne livre PAS le workflow `release-android.yml` complet (qui appartient à Story 12.2 Pipeline GitHub Actions Android), elle livre un workflow d'audit séparé `android-audit.yml` qui peut tourner sur chaque PR (= pas couplé au flow release).
>
> **Justification ADR-08 conservée** : la règle « OS-isolation maximale, pas de mutualisation cross-OS » s'applique au CODE (Kotlin Android vs Go desktop). Les pipelines CI sont **par-OS aussi** — ce nouveau workflow `android-audit.yml` est purement Android (jobs Kotlin/Gradle), il ne mutualise rien avec `release.yml` qui est purement desktop (jobs Go/GoReleaser). C'est de la duplication assumée cohérente ADR-08 (architecture.md l. 2397-2400).
>
> **Borne stricte de l'exception** : seul `.github/workflows/android-audit.yml` est créé. **Aucune** modification de `.github/workflows/release.yml` ni `aur-publish.yml`. **Aucune** création d'autre fichier hors `.github/workflows/` et `android/`. **Aucune** modification de `Makefile`, scripts racine, ou config racine non-Android.
>
> ### Reste : `android/` only
>
> **Aucune autre exception « code partagé » n'est nécessaire pour cette story.** Story 10.4 livre :
> 1. Le workflow `.github/workflows/android-audit.yml` (exception documentée ci-dessus).
> 2. Une task Gradle Kotlin DSL (`auditTelemetryDependencies`) sous `android/app/build.gradle.kts` qui réifie l'audit pour exécution locale + CI.
> 3. Un test unitaire JVM `AuditCITest.kt` qui valide la liste des modules canoniques.
> 4. Documentation README + commentaires inline.
>
> Aucun fichier Go n'est lu, créé ou modifié. Aucun import gomobile. Aucune entrée dans `android/shims/*.go`. Aucune ligne dans `go.mod`/`go.sum` racine n'est touchée.
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.x/10.1-10.3, intactes pour 10.4) :
>
> | Zone | Livrée par | État pour 10.4 |
> |---|---|---|
> | `go.mod` / `go.sum` racine | Story 9.2 | INTACT — l'audit Gradle ne lit pas le module Go |
> | `android/shims/*.go` | Story 9.2 | INTACT — code Go, hors scope audit télémétrie Gradle |
> | `android/scripts/*` | Story 9.2 | INTACT — non modifié par cette story (l'audit est porté par Gradle + GitHub Actions, pas par un script bash dédié) |
> | `android/levoile-core/build.gradle.kts` | Story 9.1+9.2 | **MODIFIÉ uniquement à la marge** — la task Gradle d'audit doit aussi auditer ce module (il consomme le `.aar` qui pourrait théoriquement contenir du Go vendor télémétrie via gomobile). Voir AC #4 |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/{kill/,conflict/,bridge/,vpn/,MainActivity.kt}` | Stories 9.x/10.1-10.3 | INTACT — Story 10.4 ne touche aucun code Kotlin de runtime |
> | `android/app/src/main/AndroidManifest.xml` | Story 9.1 | INTACT — aucune nouvelle permission, l'audit est build-time |
> | `android/app/src/main/{assets/,res/}` | Stories 9.x/10.2 | INTACT |
> | `android/proguard-rules.pro` | Story 9.1 (modifié Story 10.5) | INTACT — Story 10.5 modifiera, pas 10.4 |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,vpndetect,...}` racine + `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) `.github/workflows/android-audit.yml` (NOUVEAU — workflow GitHub Actions Audit) — **exception ADR-08 documentée ci-dessus**,
>   (b) `android/app/build.gradle.kts` (MODIFIÉ — ajout task `auditTelemetryDependencies` + `tasks.named("check") { dependsOn("auditTelemetryDependencies") }` ou équivalent),
>   (c) `android/levoile-core/build.gradle.kts` (MODIFIÉ — même task `auditTelemetryDependencies` portée par ce module aussi, OU délégation à la task `:app:auditTelemetryDependencies` qui audite la totalité du graphe — voir AC #4),
>   (d) `android/build.gradle.kts` (MODIFIÉ POSSIBLEMENT — task convenience top-level `auditAllTelemetryDependencies` agrégeant `:app` + `:levoile-core`, optionnel — voir Task 5),
>   (e) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/audit/AuditCITest.kt` (NOUVEAU — test unitaire JVM qui parse `.github/workflows/android-audit.yml` + valide la liste canonique),
>   (f) `android/README-android.md` (MODIFIÉ — section « Audit télémétrie zéro-tracking »),
>   (g) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog`/`ready-for-dev` → `review`),
>   (h) `_bmad-output/implementation-artifacts/10-4-audit-gradle-ci-bloquant-assertion-absence-dependances-telemetrie.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune autre entrée à la racine** (pas de `go.mod`, pas de `Makefile`, pas de modification de `release.yml` ni `aur-publish.yml`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/app/src/main/`** (autre que `build.gradle.kts`), **aucune entrée sous `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`**. Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance des fichiers prévus par les stories suivantes — workflow `release-android.yml` complet avec build APK + signature + upload F-Droid (Story 12.2 dédiée), pipeline complet de tests instrumentés Espresso (Story 12.6), action de signature APK v2/v3 par master key Ed25519 (Story 12.3), reproductibilité APK CI (Story 12.4), workflow F-Droid metadata generator (Story 12.1), notification Slack/Discord en cas d'échec d'audit (out-of-scope MVP, pas de webhook tiers cohérent NFR-AND-8). Cette story livre **uniquement** un audit dépendances Gradle bloquant. Si tu te retrouves à orchestrer des artefacts release ou à toucher un secret CI signature, tu es hors-scope.

## Story

En tant qu'auditeur de la posture confidentialité Le Voile,
Je veux que le pipeline CI Android bloque automatiquement toute PR (et tout push sur `main`) si une dépendance Gradle de télémétrie / analytics / crash reporter est introduite dans `android/app/` ou `android/levoile-core/`,
Afin que la promesse zéro-tracking (FR-AND-8 + NFR-AND-8 + ADR-15) soit vérifiable mécaniquement et pas uniquement déclarative — un commit qui ajouterait `firebase-analytics`, `crashlytics`, `sentry`, `bugsnag`, `mixpanel`, `adjust.io`, `branch.io` ou `amplitude` doit échouer le job CI avec un message explicite expliquant la violation et l'ADR de référence.

## Acceptance Criteria

1. **Workflow GitHub Actions `.github/workflows/android-audit.yml` créé** — Quand `.github/workflows/android-audit.yml` est lu après cette story, il déclare un workflow GitHub Actions standard :
   ```yaml
   name: Android · Audit télémétrie

   on:
     pull_request:
       paths:
         - 'android/**'
         - '.github/workflows/android-audit.yml'
     push:
       branches: [main]
       paths:
         - 'android/**'
         - '.github/workflows/android-audit.yml'

   permissions:
     contents: read

   jobs:
     audit-dependencies:
       name: Audit dépendances Gradle (zéro-tracking)
       runs-on: ubuntu-22.04
       timeout-minutes: 10
       steps:
         - name: Checkout
           uses: actions/checkout@v4

         - name: Setup JDK 17
           uses: actions/setup-java@v4
           with:
             distribution: temurin
             java-version: '17'

         - name: Setup Gradle
           uses: gradle/actions/setup-gradle@v3

         - name: Run telemetry audit task
           working-directory: android
           run: ./gradlew :app:auditTelemetryDependencies :levoile-core:auditTelemetryDependencies --no-daemon --stacktrace

         - name: Run AuditCITest unit test
           working-directory: android
           run: ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.audit.AuditCITest" --no-daemon
   ```
   **Important** :
   - **Triggers** : PR + push main, **uniquement** quand des fichiers sous `android/**` ou le workflow lui-même changent — économise le runner pour les modifs purement desktop.
   - **`permissions: contents: read`** : least-privilege — aucune écriture, aucun secret. Ce workflow ne modifie rien et ne fetch aucun secret CI (pas de signature, pas d'upload).
   - **`timeout-minutes: 10`** : bornage sécurité (un audit Gradle prend ~3-5 min download + exécution). Si dépassé, le job échoue avec timeout — préférable à un job qui boucle indéfiniment.
   - **Pas de matrice multi-OS** : l'audit Gradle est OS-agnostique (analyse statique du graphe de dépendances, pas de build APK), un seul `ubuntu-22.04` suffit. Le build APK release multi-OS sera Story 12.2.
   - **Pas de cache Gradle agressif** : `gradle/actions/setup-gradle@v3` fait du cache léger (downloads JARs) mais pas de cache du build state — l'audit doit toujours re-résoudre les dépendances pour détecter une PR qui ajoute du télémétrie.
   - **`--no-daemon` + `--stacktrace`** : pas de daemon (CI éphémère), stacktrace pour faciliter le debug en cas d'échec inattendu.
   - **Pas de matrice de variants** : la task auditTelemetryDependencies (AC #2) audite **toutes** les configurations release (`releaseRuntimeClasspath` + `releaseCompileClasspath`) — un seul appel suffit.

2. **Task Gradle Kotlin DSL `auditTelemetryDependencies` dans `android/app/build.gradle.kts`** — Quand `android/app/build.gradle.kts` est lu après cette story, il contient (en plus du contenu Story 9.1+10.1 existant) le bloc :
   ```kotlin
   /**
    * Story 10.4 — Audit dépendances zéro-télémétrie (FR-AND-8 / NFR-AND-8 / ADR-15).
    *
    * Inspecte le graphe complet de dépendances `releaseRuntimeClasspath` et fait échouer le build
    * si l'un des modules canoniquement interdits est présent. La liste vit dans une seule constante
    * (FORBIDDEN_TELEMETRY_GROUPS) qui sert de source de vérité unique pour la task ET pour
    * AuditCITest.kt (cohérent AC #6).
    *
    * Cette task fail-fast :
    *   - Détecte par `group:name` (matche prefix : "com.google.firebase:" matchera firebase-analytics,
    *     firebase-crashlytics, etc. ; matche suffix sur "name" pour les modules dont le group est partagé,
    *     ex. "io.sentry:sentry-android" → préfix "io.sentry:")
    *   - Affiche un message explicite (groupId / nom / chemin de transitive si applicable)
    *   - Exit code non-zero (échec build)
    *
    * Cette task doit aussi auditer toutes les configurations debug (debugRuntimeClasspath,
    * debugCompileClasspath) — il serait absurde qu'un dev installe Crashlytics « juste pour debug »
    * et que ça passe l'audit, car le commit serait quand même mergé sur main.
    */
   tasks.register("auditTelemetryDependencies") {
       group = "verification"
       description = "Bloque le build si une dépendance télémétrie/analytics est présente (cohérent ADR-15)."
       doLast {
           val forbiddenGroups = listOf(
               "com.google.firebase",
               "com.crashlytics.sdk.android",
               "io.sentry",
               "com.bugsnag",
               "com.mixpanel.android",
               "com.adjust.sdk",
               "io.branch.sdk.android",
               "com.amplitude"
           )
           val configurationsToAudit = listOf(
               "releaseRuntimeClasspath",
               "releaseCompileClasspath",
               "debugRuntimeClasspath",
               "debugCompileClasspath"
           )
           val violations = mutableListOf<String>()
           configurationsToAudit.forEach { configName ->
               val config = configurations.findByName(configName) ?: return@forEach
               config.resolvedConfiguration.lenientConfiguration.allModuleDependencies.forEach { dep ->
                   val moduleId = "${dep.moduleGroup}:${dep.moduleName}"
                   forbiddenGroups.forEach { forbiddenPrefix ->
                       if (dep.moduleGroup.startsWith(forbiddenPrefix)) {
                           violations += "[$configName] $moduleId (cohérent ADR-15)"
                       }
                   }
               }
           }
           if (violations.isNotEmpty()) {
               val report = violations.distinct().joinToString("\n  - ", prefix = "  - ")
               throw GradleException(
                   """
                   |❌ Audit télémétrie échoué — dépendances interdites détectées :
                   |$report
                   |
                   |Cohérent ADR-15 (architecture.md) + NFR-AND-8 (prd.md l. 704) + FR-AND-8 (prd.md l. 616).
                   |Le client Android Le Voile n'embarque AUCUNE télémétrie, AUCUN crash reporter, AUCUN analytics.
                   |Retirez la dépendance offensive ou justifiez-la dans un ADR avant ré-introduction.
                   """.trimMargin()
               )
           }
           logger.lifecycle("✓ Audit télémétrie passé — aucun module interdit détecté.")
       }
   }

   tasks.named("check") {
       dependsOn("auditTelemetryDependencies")
   }
   ```
   **Important** :
   - **Liste `FORBIDDEN_TELEMETRY_GROUPS`** : couvre les 8 modules canoniques cités epics.md l. 1769 + prd.md l. 704. Chaque entrée matche un **préfixe groupId** (ex : `"com.google.firebase"` matche `com.google.firebase`, `com.google.firebase.analytics`, etc. — `startsWith` est inclusif sans risque de false-positive car aucun groupId Android légitime ne commence par ces préfixes).
   - **Configurations auditées** : `release{Runtime,Compile}Classpath` ET `debug{Runtime,Compile}Classpath`. Auditer debug aussi est important — un dev tenté d'ajouter Crashlytics « juste en debug » verrait son commit échouer en CI, ce qui force le respect d'ADR-15 même avant le release.
   - **`lenientConfiguration.allModuleDependencies`** : récursif (inclut les transitives). Une dépendance directe `androidx.foo` qui tirerait transitivement `firebase-bar` serait détectée. C'est exactement le comportement souhaité (le dev ne peut pas se cacher derrière une transitive).
   - **`throw GradleException`** : exit code non-zero, le build échoue clairement. Pas de `logger.error` silencieux.
   - **Message d'erreur** : explicite, multi-ligne, référence ADR-15 + NFR-AND-8 + FR-AND-8 (sources de la règle). Donne un guide d'action (retirer ou ADR justificatif).
   - **`tasks.named("check") { dependsOn(...) }`** : l'audit s'exécute automatiquement à `./gradlew check` (qui est invoqué par défaut dans la plupart des pipelines CI). Garantit qu'un dev qui lance `./gradlew check` localement avant push verra l'erreur immédiatement, avant le PR.

3. **Le commentaire automatique sur la PR n'est pas implémenté dans cette story** — Quand une PR introduit une dépendance interdite, **le job CI échoue avec exit code 1 et le message multi-ligne d'AC #2 visible dans l'output** — cette information est suffisante pour qu'un dev comprenne la violation. **Pas d'ajout d'un commentaire automatique sur la PR via `gh` ou GitHub Actions Bot** dans cette story (epics.md l. 1775 mentionne un commentaire — c'est un nice-to-have, mais (a) demande des permissions write `pull-requests: write` qui élargissent la surface de risk CI, (b) nécessite une logique de parsing de l'output Gradle, (c) GitHub Actions affiche déjà un statut « Failed » bien visible sur la PR avec un lien vers les logs). **Décision projet à reporter dans Completion Notes** : commentaire auto reporté à backlog Phase 2+ (= post-MVP F-Droid). L'AC #5 du test smoke (mention de la mention dans le message) reste à valider de manière allégée.

4. **`android/levoile-core/build.gradle.kts` enregistre la même task** — Quand `android/levoile-core/build.gradle.kts` est lu après cette story, il contient également (en plus du contenu Story 9.1+9.2 existant) un bloc équivalent à AC #2 pour ce module. **Pourquoi audit ce module aussi** : il consomme le `.aar` gomobile qui peut théoriquement (faible probabilité, mais possible) embarquer des dépendances Java/Android via gomobile bind si une lib Go partagée importait du JNI Java instrumented. **Approche recommandée** :
   - **Option A** (préférée) : factoriser la task dans le `android/build.gradle.kts` top-level via `subprojects { ... }` :
     ```kotlin
     subprojects {
         tasks.register("auditTelemetryDependencies") {
             // ... même body que AC #2 ...
         }
         afterEvaluate {
             tasks.findByName("check")?.dependsOn("auditTelemetryDependencies")
         }
     }
     ```
     Cela évite la duplication entre `:app/build.gradle.kts` et `:levoile-core/build.gradle.kts`. Cohérent KISS.
   - **Option B** : dupliquer la définition dans chaque module (verbose mais explicite).

   **Décision dev à reporter dans Debug Log** : Option A vs Option B. Recommandation : A.

5. **`AuditCITest.kt` valide la liste canonique présente dans le workflow ET dans le build.gradle.kts** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté après cette story, un fichier de test `app/src/test/kotlin/fr/plateformeliberte/levoile/audit/AuditCITest.kt` exécute :
   ```kotlin
   class AuditCITest {

       @Test
       fun `workflow android-audit yml referrence les 8 modules canoniques`() {
           // Lecture relative au project root (cwd Gradle = android/, donc remontée d'un cran).
           val workflowPath = java.io.File("../.github/workflows/android-audit.yml")
           assertTrue("Workflow android-audit.yml introuvable à $workflowPath", workflowPath.exists())
           val content = workflowPath.readText()

           // Pattern : on cherche dans le workflow l'appel `:auditTelemetryDependencies` qui prouve
           // que le job CI invoque bien la task Gradle d'audit.
           assertTrue(
               "Le workflow doit invoquer :app:auditTelemetryDependencies",
               content.contains(":app:auditTelemetryDependencies")
           )
           assertTrue(
               "Le workflow doit invoquer :levoile-core:auditTelemetryDependencies",
               content.contains(":levoile-core:auditTelemetryDependencies")
           )
       }

       @Test
       fun `liste canonique 8 modules est complete`() {
           // La liste canonique Story 10.4 (epics.md l. 1769 + prd.md l. 704) doit comprendre
           // au minimum ces 8 préfixes — c'est une régression-protection : si quelqu'un retire
           // un préfixe (ex. enlever "io.sentry" parce qu'il pense que Sentry n'est plus une menace),
           // ce test fail explicitement avec un message clair.
           val expectedCanonical = listOf(
               "com.google.firebase",
               "com.crashlytics.sdk.android",
               "io.sentry",
               "com.bugsnag",
               "com.mixpanel.android",
               "com.adjust.sdk",
               "io.branch.sdk.android",
               "com.amplitude",
           )
           // La liste actuelle est lue depuis le code Kotlin sur disque
           // (build.gradle.kts est du Kotlin DSL, parseable en text).
           val buildGradle = java.io.File("app/build.gradle.kts").readText()
           expectedCanonical.forEach { canonicalGroup ->
               assertTrue(
                   "build.gradle.kts doit lister le préfixe canonique '$canonicalGroup' (ADR-15)",
                   buildGradle.contains("\"$canonicalGroup\"")
               )
           }
       }
   }
   ```
   **Important** :
   - Test **JVM-only**, pas de Robolectric, pas de framework YAML lourd (le test fait juste un `contains` sur le contenu textuel du fichier).
   - **Lecture relative `../.github/workflows/`** : le cwd Gradle pour `:app:testDebugUnitTest` est `android/app/`, donc remontée de 2 crans (`../../.github/workflows/`). **À vérifier au runtime** — si le cwd diffère selon Gradle version / wrapper version, fallback : `java.io.File(System.getProperty("user.dir")).resolve("../../../.github/workflows/android-audit.yml")` ou utilisation de `org.gradle.api.Project.rootDir`. **Reporter dans Debug Log** : chemin résolu effectif au runtime du test.
   - **Pas de parser YAML** (snakeyaml ou yamlbeans) — overkill pour 2 `contains`. Cohérent KISS.
   - Le test 2 « liste canonique 8 modules » est l'**anti-regression** : empêche qu'un dev edits `build.gradle.kts` en retirant un module canonique sans être conscient de la perte de couverture audit. Cohérent epics.md l. 1781 « test unitaire `AuditCITest` vérifie que la liste contient au minimum les 8 modules canoniques ».

6. **Test manuel d'introduction d'une dépendance interdite** — Quand le dev fait localement (cf. README AC #7) :
   ```bash
   cd android
   # Ajouter temporairement dans app/build.gradle.kts :
   #   dependencies { implementation("com.google.firebase:firebase-analytics:21.0.0") }
   ./gradlew :app:auditTelemetryDependencies
   ```
   La task échoue avec exit code 1 et le message :
   ```
   ❌ Audit télémétrie échoué — dépendances interdites détectées :
     - [releaseRuntimeClasspath] com.google.firebase:firebase-analytics (cohérent ADR-15)
     - [debugRuntimeClasspath] com.google.firebase:firebase-analytics (cohérent ADR-15)

   Cohérent ADR-15 (architecture.md) + NFR-AND-8 (prd.md l. 704) + FR-AND-8 (prd.md l. 616).
   ...
   ```
   Après retrait de la ligne `implementation(...firebase...)` et re-run, la task passe vert :
   ```
   ✓ Audit télémétrie passé — aucun module interdit détecté.
   ```
   **Reporter dans Debug Log** : capture exacte de l'output observé localement (peut différer légèrement selon Gradle / runner version, mais doit contenir le message « Audit télémétrie échoué » et lister la dépendance offensive).

7. **`README-android.md` patché — section « Audit télémétrie zéro-tracking »** — Quand `android/README-android.md` est lu après cette story, il contient une section additionnelle (insérée APRÈS la section Story 10.3 « Détection conflit VPN ») :
   ```markdown
   ## Audit télémétrie zéro-tracking (Story 10.4 livrée)

   Le pipeline CI Android (workflow GitHub Actions `.github/workflows/android-audit.yml`)
   bloque toute PR ou push sur `main` qui introduirait une dépendance Gradle de
   télémétrie / analytics / crash reporter. Cohérent FR-AND-8, NFR-AND-8, ADR-15.

   La liste canonique (8 préfixes groupId) :
   - `com.google.firebase` (Firebase Analytics, Crashlytics, Performance, etc.)
   - `com.crashlytics.sdk.android` (Crashlytics héritage)
   - `io.sentry` (Sentry SDK Android)
   - `com.bugsnag` (Bugsnag SDK Android)
   - `com.mixpanel.android` (Mixpanel)
   - `com.adjust.sdk` (Adjust attribution)
   - `io.branch.sdk.android` (Branch deeplinks/attribution)
   - `com.amplitude` (Amplitude analytics)

   **Test local** :
   ```
   cd android
   ./gradlew :app:auditTelemetryDependencies :levoile-core:auditTelemetryDependencies
   ```

   **Mise à jour de la liste** : la liste est en double (workflow + `app/build.gradle.kts`),
   le test `AuditCITest.kt` empêche le drift. Toute évolution doit modifier les 2
   sources et passer la suite de tests. Si justification d'ajout d'un module
   précédemment interdit (cas hypothétique), un ADR doit être créé AVANT
   modification de ces fichiers (NFR22i — commit signature GPG mainteneur).
   ```

   Aucune autre section du README n'est touchée par cette story.

8. **Le commit qui modifie cette liste DOIT être signé GPG (NFR22i)** — Quand un dev commits un changement de la liste canonique (par exemple ajouter un nouveau module à la blacklist car une nouvelle menace télémétrie émerge), le commit doit être signé GPG par le mainteneur (cohérent NFR22i — prd.md l. 686). **Cette story ne LIVRE PAS de mécanisme automatique de vérification de signature GPG** (= un GitHub branch protection rule, configuré par l'owner du repo, hors-scope CI workflow). Mais le README documente la règle (cf. AC #7 dernier paragraphe).

   **Vérification manuelle** : `git log --show-signature -- android/app/build.gradle.kts` doit montrer signature GPG valide pour les commits qui touchent la liste.

## Tasks / Subtasks

- [ ] **Task 1 : Vérifier l'état Stories 9.x + 10.1-10.3 livrées + le repo** (AC: tous)
  - [ ] Lire `android/app/build.gradle.kts` (livré 9.1+10.1) — confirmer plugins + namespace + buildTypes. Identifier où insérer le bloc task `auditTelemetryDependencies`.
  - [ ] Lire `android/levoile-core/build.gradle.kts` (livré 9.1+9.2) — idem.
  - [ ] Lire `android/build.gradle.kts` (top-level — livré 9.1) — vérifier s'il a déjà un bloc `subprojects { ... }` (livré 9.1 ?). Décider Option A (factoriser dans top-level) vs Option B (duplication).
  - [ ] Lire `.github/workflows/release.yml` (livré 7.x desktop) — noter le pattern (working-directory, setup-java, gradle/setup-gradle action) pour cohérence stylistique avec le nouveau workflow.
  - [ ] Lire `.github/workflows/aur-publish.yml` (livré 7.3 desktop) — idem.
  - [ ] **Reporter dans Debug Log** : état exact des fichiers lus, choix Option A/B.

- [ ] **Task 2 : Créer `.github/workflows/android-audit.yml`** (AC: #1)
  - [ ] Créer le fichier avec le contenu AC #1.
  - [ ] Vérifier la syntaxe YAML (indentation, no tabs) — outil : `python -c "import yaml; yaml.safe_load(open('.github/workflows/android-audit.yml'))"` ou validateur GitHub Actions.
  - [ ] Permissions least-privilege (`contents: read`).
  - [ ] **Aucune autre modification** dans `.github/workflows/`.

- [ ] **Task 3 : Implémenter la task `auditTelemetryDependencies` dans `android/app/build.gradle.kts`** (AC: #2)
  - [ ] Insérer le bloc `tasks.register(...)` selon AC #2 + le `tasks.named("check") { dependsOn(...) }`.
  - [ ] Si Option A retenue (Task 1) : insérer plutôt dans `android/build.gradle.kts` top-level via `subprojects { ... }`. Dans ce cas, `:app/build.gradle.kts` reste inchangé pour la task (mais le `dependsOn check` peut nécessiter ajustement selon timing afterEvaluate).
  - [ ] Vérifier compilation Kotlin DSL : `cd android && ./gradlew help` (lit toutes les `build.gradle.kts`).
  - [ ] Lancer `./gradlew :app:auditTelemetryDependencies` localement — doit afficher « ✓ Audit télémétrie passé » (état courant du repo : pas de télémétrie).

- [ ] **Task 4 : Étendre la task au module `:levoile-core`** (AC: #4)
  - [ ] Si Option A : la task est déjà étendue via `subprojects` — vérifier `./gradlew :levoile-core:auditTelemetryDependencies` passe.
  - [ ] Si Option B : dupliquer le bloc dans `levoile-core/build.gradle.kts`.
  - [ ] Vérifier la totale invocation `./gradlew :app:auditTelemetryDependencies :levoile-core:auditTelemetryDependencies` passe.

- [ ] **Task 5 : (Optionnel) Task convenience top-level `auditAllTelemetryDependencies`** (Task)
  - [ ] Pour faciliter `./gradlew auditAllTelemetryDependencies` (1 seul appel), enregistrer dans `android/build.gradle.kts` :
    ```kotlin
    tasks.register("auditAllTelemetryDependencies") {
        group = "verification"
        description = "Audit télémétrie agrégé sur tous les modules."
        dependsOn(":app:auditTelemetryDependencies", ":levoile-core:auditTelemetryDependencies")
    }
    ```
  - **Décision dev à reporter dans Completion Notes** : task convenience livrée ou non. Pas bloquant pour AC.

- [ ] **Task 6 : Créer `AuditCITest.kt`** (AC: #5)
  - [ ] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/audit/AuditCITest.kt`.
  - [ ] Implémenter les 2 tests selon AC #5.
  - [ ] **Vérifier le chemin relatif** au runtime : ajouter un troisième test smoke `@Test fun `cwd_documente_pour_debug`()` qui logge `System.getProperty("user.dir")` et `File(".").canonicalPath` — utile en cas de désync de cwd. **Décision dev à reporter dans Debug Log** : test smoke conservé ou retiré avant commit.
  - [ ] Vérifier `cd android && ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.audit.AuditCITest"` passe vert.

- [ ] **Task 7 : Test local d'injection volontaire (régression positive)** (AC: #6)
  - [ ] Localement, ajouter temporairement dans `android/app/build.gradle.kts` `dependencies` : `implementation("com.google.firebase:firebase-analytics:21.0.0")`.
  - [ ] Lancer `cd android && ./gradlew :app:auditTelemetryDependencies` — doit échouer avec message « Audit télémétrie échoué ».
  - [ ] **Reporter dans Debug Log** : capture textuelle de l'output (anonymisée — pas de chemins absolus de la machine dev).
  - [ ] **OBLIGATOIRE** : retirer la ligne firebase avant commit. Vérifier `git diff android/app/build.gradle.kts` est propre (pas de ligne firebase résiduelle).

- [ ] **Task 8 : Patcher `README-android.md`** (AC: #7)
  - [ ] Insérer la section « Audit télémétrie zéro-tracking (Story 10.4 livrée) » au bon endroit (après section Story 10.3).

- [ ] **Task 9 : Build sanity check global**
  - [ ] `cd android && ./gradlew clean assembleDebug check :app:testDebugUnitTest :app:lint` — toutes tâches vert. Vérifier que `check` invoque automatiquement `auditTelemetryDependencies`.
  - [ ] Vérifier que la modification du `build.gradle.kts` n'a pas cassé `assembleDebug` ni `assembleRelease` (Story 9.1 baseline).
  - [ ] `apkanalyzer apk file-size app/build/outputs/apk/debug/app-debug.apk` — taille reste < 25 MB (NFR-AND-3) — l'audit est build-time, n'impacte pas la taille APK.

- [ ] **Task 10 : Test du workflow GitHub Actions sur une branche test** (Optionnel mais recommandé)
  - [ ] Pousser une branche test `test/10-4-audit-ci` et créer une PR draft.
  - [ ] Vérifier que le workflow `Android · Audit télémétrie` se déclenche.
  - [ ] Vérifier que le job `audit-dependencies` réussit (status « ✓ »).
  - [ ] **Reporter dans Debug Log** : URL de la run GitHub Actions (anonymisée si nécessaire).
  - [ ] Optionnel : pousser un commit qui ajoute volontairement firebase pour vérifier que le workflow échoue (puis reverter le commit avant merge).

- [ ] **Task 11 : Mettre à jour la story et sprint-status**
  - [ ] Mettre à jour la section « Dev Agent Record » (Agent Model Used, File List, Completion Notes List, Change Log).
  - [ ] Passer le `Status` de cette story de `ready-for-dev` à `review`.
  - [ ] Passer `_bmad-output/implementation-artifacts/sprint-status.yaml` `10-4-audit-gradle-ci-...: backlog` → `review`.
  - [ ] **Si commit GPG-signed est requis** (NFR22i — pour ajout futur à la liste canonique) : pas applicable à cette story (création initiale, pas modification de la liste canonique pré-existante). Documentation dans le README suffit.

## Dev Notes

### Pattern principal — Audit statique du graphe Gradle

L'approche choisie est l'inspection du graphe résolu par Gradle (`configuration.resolvedConfiguration.lenientConfiguration.allModuleDependencies`). Avantages :
- **Détection des transitives** : un dev qui ajoute `lib-foo` qui transitivement tire `firebase-bar` est détecté. Aucune façon de se cacher.
- **Pas de regex sur les fichiers source** : robuste aux différentes syntaxes Gradle (Groovy DSL, Kotlin DSL, BOMs, version catalogs, etc.).
- **Réutilise la résolution Gradle native** : pas d'overhead supplémentaire (la résolution est faite de toute façon pour le build).

Inconvénients :
- L'audit nécessite la résolution complète des dépendances — donc le runner CI doit télécharger l'index Maven (~30 secondes premier run, cache ensuite).
- Si une dépendance est supprimée du Maven Central, la résolution échoue avec une erreur de réseau, pas avec « pas de télémétrie ». **Mitigation** : le job CI doit faire confiance au timeout (10 min) pour échouer proprement.

### Pourquoi pas de regex sur `app/build.gradle.kts` directement

Approche alternative envisagée : grep `app/build.gradle.kts` pour des chaînes comme `firebase`, `crashlytics`. Rejetée car :
- Ne détecte pas les transitives.
- Faux-positifs sur commentaires (un commentaire « // No firebase needed » serait détecté comme violation).
- Faux-négatifs sur version catalogs : si `libs.versions.toml` définit `firebase = "..."` et que `app/build.gradle.kts` utilise `libs.firebase`, le grep sur `build.gradle.kts` ne voit pas firebase explicitement.

L'audit sur le **graphe résolu** évite ces 3 problèmes.

### Source de vérité unique vs duplication contrôlée

La liste canonique vit en 3 endroits :
1. `android/app/build.gradle.kts` (et/ou `android/build.gradle.kts` si Option A) — **source de vérité runtime** pour la task Gradle.
2. `.github/workflows/android-audit.yml` — référence implicite (la task est invoquée mais le workflow ne re-déclare pas la liste).
3. `AuditCITest.kt` — **anti-regression** qui force la cohérence avec le `build.gradle.kts`.

Le test `AuditCITest.kt` (test 2) lit le contenu de `build.gradle.kts` et vérifie la présence des 8 préfixes canoniques. Si le dev retire un préfixe, le test fail explicitement. C'est l'équivalent fonctionnel d'avoir une « source unique » sans le coût d'introduire une dépendance JSON/YAML pour partager la liste entre Kotlin DSL et Kotlin code de test.

### Coordination Story 12.2

Story 12.2 livrera le pipeline CI Android complet (`release-android.yml`) qui inclura :
- Build APK debug + release
- Tests unitaires + tests instrumentés Espresso
- Signature APK v2/v3 (Story 12.3)
- Upload artefacts F-Droid + GitHub release

L'audit télémétrie **devra** être exécuté dans ce pipeline aussi (cohérence). Story 12.2 ajoutera le step `./gradlew :app:auditTelemetryDependencies :levoile-core:auditTelemetryDependencies` dans `release-android.yml` — **sans dupliquer** le workflow `android-audit.yml`. Les 2 workflows coexistent :
- `android-audit.yml` (Story 10.4 — cette story) : audit minimaliste rapide (~3-5 min), tourne sur **chaque PR + push main**, zero-coût supplémentaire (pas de build).
- `release-android.yml` (Story 12.2) : pipeline release complet, tourne sur **tag git** ou push spécifique, contient l'audit en step.

Rationale : avoir un workflow d'audit **dédié et léger** garantit que même une PR qui ne déclenche pas le pipeline release est auditée pour la télémétrie. Pattern « fail-fast » sécurité.

### Pourquoi pas un Renovate/Dependabot config

Renovate (déjà mentionné dans architecture.md l. 716 pour le desktop) automatise les mises à jour de dépendances. Il **n'a pas de fonction « bloquer ajout de dépendance interdite »** — c'est exactement le contraire de son rôle (il ajoute, n'audite pas).

Un ajout volontaire d'une dépendance par un humain est attrapé par notre task `auditTelemetryDependencies`. Un bump automatique par Renovate (ex. `firebase-analytics: 21.0.0 → 21.1.0`) ne déclenche **pas** le scénario violation — Renovate ne peut pas bumper un module qui n'existe pas dans `app/build.gradle.kts`. **Donc l'audit est indépendant de la stratégie Renovate**.

### Source tree components à toucher

- **Nouveaux** :
  - `.github/workflows/android-audit.yml` (exception ADR-08 documentée)
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/audit/AuditCITest.kt`
- **Modifiés** :
  - `android/app/build.gradle.kts` (ajout task + dependsOn check) — sauf si Option A → `android/build.gradle.kts` à la place
  - `android/levoile-core/build.gradle.kts` (idem)
  - `android/build.gradle.kts` (Option A — task partagée subprojects + task convenience)
  - `android/README-android.md` (section nouvelle)

### Standards de testing

- Test JVM-only `AuditCITest.kt` : 2 tests, < 200ms.
- Pas de Robolectric.
- Pas de framework YAML (snakeyaml, etc.).
- Validation manuelle de l'injection volontaire (Task 7) — preuve que la task **fail** correctement.

### Project Structure Notes

Le package Kotlin `fr.plateformeliberte.levoile.audit` est nouveau (et reste dans `src/test/`, pas dans `src/main/` — c'est uniquement un test). Pas de package `audit` côté production (la task Gradle est en DSL Kotlin, pas en code Kotlin runtime).

### References

- [architecture.md l. 397-399](_bmad-output/planning-artifacts/architecture.md) — workflows GitHub Actions à la racine, pattern existant `release-android.yml` mentionné (livré Story 12.2).
- [architecture.md l. 716](_bmad-output/planning-artifacts/architecture.md) — Renovate bot (référence pour comparaison avec audit).
- [architecture.md l. 2397-2400](_bmad-output/planning-artifacts/architecture.md) — ADR-08 isolation OS (justifie l'exception workflow).
- [architecture.md l. 2439-2442](_bmad-output/planning-artifacts/architecture.md) — ADR-15 zéro-télémétrie Android.
- [epics.md l. 1758-1781](_bmad-output/planning-artifacts/epics.md) — Story 10.4 BDD complet (3 scénarios Given/When/Then) + liste des 8 modules canoniques.
- [prd.md l. 614-617](_bmad-output/planning-artifacts/prd.md) — FR-AND-8 zéro télémétrie / analytics / crash reporter.
- [prd.md l. 686](_bmad-output/planning-artifacts/prd.md) — NFR22i signing commits GPG.
- [prd.md l. 704](_bmad-output/planning-artifacts/prd.md) — NFR-AND-8 audit dépendances Gradle bloqué en CI si modules : Firebase, Sentry, Bugsnag, Crashlytics, Mixpanel, Adjust, Branch, Amplitude.
- `.github/workflows/release.yml` (existant Story 7.x desktop) — pattern référence pour syntaxe workflow.
- `.github/workflows/aur-publish.yml` (existant Story 7.3) — idem.
- Story 9.1 (livrée) : `android/app/build.gradle.kts` baseline + `android/levoile-core/build.gradle.kts` baseline.
- Story 9.2 (livrée) : `android/levoile-core/` consomme `.aar` gomobile — auditer ce module est pertinent (cf. AC #4 justification).
- Story 12.2 (à venir) : pipeline GitHub Actions Android complet — coexiste avec ce workflow d'audit (cf. Coordination Story 12.2 ci-dessus).

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

### Change Log
