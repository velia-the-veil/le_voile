# Story 10.5: Filtrage logs `android.util.Log` par buildType

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Aucune exception « code partagé » n'est nécessaire pour cette story.** Story 10.5 livre :
> 1. Un wrapper Kotlin `LeVoileLog.kt` qui filtre les niveaux de log à la compilation+runtime selon le buildType (`release` = WARN+ uniquement ; `debug` = INFO+).
> 2. Une mise à jour de `proguard-rules.pro` pour stripper `Log.d`, `Log.v` ET `Log.i` du bytecode release (la story 9.1 a déjà strippé `Log.d` + `Log.v` ; cette story étend à `Log.i`).
> 3. Un test unitaire JVM `LogFilteringTest.kt` qui scanne récursivement tous les `.kt` du module `:app` (et `:levoile-core`) et fail si un pattern de log interdit y figure (URL, domaine, IP destination, contenu utilisateur).
>
> Aucun fichier Go n'est lu, créé ou modifié. Aucun import gomobile. Aucune entrée dans `android/shims/*.go`. Aucune ligne dans `go.mod`/`go.sum` racine n'est touchée.
>
> **Rappel ADR-08 (architecture.md l. 2397-2400) — isolation OS maximale.** Le filtrage de logs Android est par construction Android-spécifique (`android.util.Log` est une API Android, pas Java standard ni Linux/Windows). Sur desktop, le filtrage des logs vit dans le code Go via `log/slog` configuré au démarrage (Phase 1). Toute tentative de "factoriser" un wrapper logger cross-OS est une violation directe d'ADR-08 — refusée en code review.
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.x/10.1-10.4, intactes pour 10.5) :
>
> | Zone | Livrée par | État pour 10.5 |
> |---|---|---|
> | `go.mod` / `go.sum` racine | Story 9.2 | INTACT |
> | `android/shims/*.go` | Story 9.2 | INTACT — code Go, hors scope filtrage logs Android |
> | `android/scripts/*` | Story 9.2 | INTACT |
> | `android/levoile-core/build.gradle.kts` | Story 9.1+9.2 | INTACT — Story 10.5 audite ce module mais ne le modifie pas |
> | `android/app/build.gradle.kts` | Stories 9.1+10.1+10.4 | INTACT — aucune nouvelle dépendance Gradle requise pour cette story |
> | `android/app/src/main/AndroidManifest.xml` | Story 9.1 | INTACT |
> | `android/app/src/main/{assets/,res/}` | Stories 9.x/10.2 | INTACT |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/{kill/,conflict/,bridge/,vpn/,MainActivity.kt}` | Stories 9.x/10.1-10.3 | **MODIFIÉ uniquement à la marge** : pas de migration globale `Log.i → LeVoileLog.i` dans cette story (= refactor lourd hors-scope). Voir AC #5 — la migration progressive est traitée par chaque story future qui ajoute du log. Story 10.5 livre **uniquement** le wrapper + le test scan + l'extension proguard |
> | `.github/workflows/android-audit.yml` | Story 10.4 | **MODIFIÉ uniquement à la marge** — ajout d'un step qui invoque `:app:testDebugUnitTest --tests "*LogFilteringTest"` pour que le scan soit vérifié en CI bloquant (cohérent epics.md l. 1801-1804) |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,vpndetect,...}` racine + `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT — hors arbre `android/` |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/log/LeVoileLog.kt` (NOUVEAU — wrapper logger),
>   (b) `android/app/proguard-rules.pro` (MODIFIÉ — extension de `-assumenosideeffects` pour `Log.i` en release),
>   (c) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/log/LogFilteringTest.kt` (NOUVEAU — test unitaire JVM scan source),
>   (d) `.github/workflows/android-audit.yml` (MODIFIÉ — ajout step exécution `LogFilteringTest`),
>   (e) `android/README-android.md` (MODIFIÉ — section « Filtrage logs zéro-data-utilisateur »),
>   (f) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog`/`ready-for-dev` → `review`),
>   (g) `_bmad-output/implementation-artifacts/10-5-filtrage-logs-android-util-log-par-buildtype.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/{assets/,res/,AndroidManifest.xml}`, ni dans les fichiers Kotlin runtime livrés Stories 9.x-10.4** (pas de migration globale des `Log.*` existants vers `LeVoileLog.*` dans cette story — voir AC #5 raison). Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance — refactor global de tous les `android.util.Log.*` du repo Stories 9.x-10.4 vers `LeVoileLog.*` (= scope explosion, hors-scope cette story ; chaque story future ajoutant un log doit utiliser `LeVoileLog` directement, mais le refactor du legacy est un débat ouvert reporté à Phase 2 si intérêt confirmé), ajout d'un crash reporter local (Sentry self-hosted, Crashlib custom) — interdit par ADR-15, audit Story 10.4 le bloquerait, intégration Slack/webhook pour les WARN+ (out-of-scope MVP, NFR-AND-8 zéro réseau sortant non-tunnel), persistence des logs WARN+ dans un fichier local pour bug reports utilisateur (envisagé Phase 2 — `gap mineur architecture.md l. 2192` mentionne « bug reports utilisateur via export texte manuel »), classes ProGuard rules custom pour des chaînes spécifiques. Cette story livre **uniquement** un wrapper minimaliste + un test scan + une extension proguard. Si tu te retrouves à modifier 30+ fichiers Kotlin runtime existants ou à ajouter un upload réseau, tu es hors-scope.

## Story

En tant qu'auditeur de la posture confidentialité Le Voile,
Je veux que les logs Android (via `android.util.Log` ou tout futur wrapper) ne contiennent JAMAIS d'URL visitée, de nom de domaine résolu, de destination IP, de contenu utilisateur, ni aucune autre donnée révélatrice du trafic réseau utilisateur,
Afin que la posture zéro-log soit identique sur Android et desktop (cohérent NFR-AND-9 prd.md l. 705 + NFR22a prd.md l. 672 + ADR-15 architecture.md l. 2439-2442) — vérifiable mécaniquement via un test unitaire `LogFilteringTest` qui scanne le code source à chaque PR et échoue si un template de log contient une variable interdite.

## Acceptance Criteria

1. **`LeVoileLog.kt` est créé sous `app/src/main/kotlin/fr/plateformeliberte/levoile/log/`** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/log/LeVoileLog.kt` est lu après cette story, il déclare un objet singleton (ou classe statique-like Kotlin) :
   ```kotlin
   /**
    * Story 10.5 — Wrapper logger respect zéro-data-utilisateur (NFR-AND-9 / NFR22a).
    *
    * Filtrage par buildType :
    *   - debug : INFO+ visible dans Logcat (i / w / e — d et v strippés par ProGuard rules
    *     -assumenosideeffects depuis Story 9.1).
    *   - release : WARN+ uniquement (i strippé en + de d/v par cette story —
    *     extension de proguard-rules.pro).
    *
    * Pourquoi ce wrapper plutôt que `android.util.Log` direct :
    *   1. Une seule indirection contrôlable au compile-time si nous voulons remplacer le sink
    *      (ex. envoyer à Logcat ET un fichier local pour bug reports utilisateur — hors scope MVP).
    *   2. Discipline d'équipe : `LeVoileLog.i(TAG, message)` est plus court que
    *      `android.util.Log.i(TAG, message)` et signale visuellement le respect de la posture
    *      zéro-data-utilisateur.
    *   3. Le test `LogFilteringTest.kt` scanne TOUS les sites d'appel `Log.*(` et
    *      `LeVoileLog.*(` — donc le wrapper N'ÉCHAPPE PAS au scan (un dev qui appelerait
    *      `LeVoileLog.i(TAG, "user clicked $url")` serait détecté).
    *
    * Cohérent ADR-15 (architecture.md l. 2439-2442) et architecture.md l. 705
    * (release : WARN+ uniquement).
    */
   internal object LeVoileLog {

       /**
        * INFO — strippé en release par ProGuard rules (cf. proguard-rules.pro).
        * En debug, route vers `android.util.Log.i(...)`.
        */
       fun i(tag: String, message: String) {
           if (BuildConfig.DEBUG) {
               android.util.Log.i(tag, message)
           }
           // En release : ProGuard strippe l'appel via -assumenosideeffects
       }

       /**
        * WARN — visible en debug ET release.
        */
       fun w(tag: String, message: String) {
           android.util.Log.w(tag, message)
       }

       fun w(tag: String, message: String, throwable: Throwable) {
           android.util.Log.w(tag, message, throwable)
       }

       /**
        * ERROR — visible en debug ET release.
        */
       fun e(tag: String, message: String) {
           android.util.Log.e(tag, message)
       }

       fun e(tag: String, message: String, throwable: Throwable) {
           android.util.Log.e(tag, message, throwable)
       }

       /**
        * DEBUG / VERBOSE — explicitement non exposés.
        * Le dev qui veut un trace ultra-verbeux doit utiliser directement android.util.Log.d / .v
        * et ProGuard les strippera en release. Pas de wrapper pour décourager leur usage.
        */
   }
   ```
   **Important** :
   - Visibilité `internal` — le wrapper est accessible depuis tout le module `:app` mais pas exposé publiquement.
   - **Pas de méthode `d()` ni `v()`** dans le wrapper — c'est volontaire. Les niveaux DEBUG / VERBOSE sont déjà strippés en release par les rules `-assumenosideeffects` Story 9.1, et le wrapper n'apporte rien à leur ergonomie. Si un dev veut DEBUG, il fait `android.util.Log.d(TAG, ...)` directement — strippé en release.
   - **`if (BuildConfig.DEBUG)`** sur la méthode `i()` : double protection. ProGuard strippera l'appel `LeVoileLog.i(...)` en release via une rule dédiée (AC #2 ci-dessous), mais le `if (BuildConfig.DEBUG)` interne est une 2ème ceinture qui :
     1. Marche même en debug avec ProGuard désactivé (cas standard `debug` buildType).
     2. Évite les calls inutiles à `Log.i` même si la rule ProGuard ne match pas (cas hypothétique d'un futur Android Gradle Plugin qui change la sémantique de `-assumenosideeffects`).
   - **Pas de format-string vararg** : on accepte uniquement `String message` (déjà formaté côté caller). C'est volontaire — les format strings type `LeVoileLog.i(TAG, "User %s connected to %s", userId, server)` cachent les variables au scan. Forcer le caller à pré-formater (`"User connected"` ou template clean sans `$`) rend le scan trivial. Cohérent NFR22a.

2. **`proguard-rules.pro` étendu pour stripper `Log.i` ET `LeVoileLog.i` en release** — Quand `android/app/proguard-rules.pro` est lu après cette story, il contient (en plus du contenu Story 9.1+9.7 existant) :
   ```proguard
   # Story 10.5 (extension Story 9.1) :
   # Strip Log.d / Log.v / Log.i en release pour respecter NFR-AND-9 (release : WARN+ uniquement).
   # La règle Story 9.1 ne couvrait que .d et .v ; cette story ajoute .i.
   -assumenosideeffects class android.util.Log {
       public static int d(...);
       public static int v(...);
       public static int i(...);
   }

   # Story 10.5 :
   # Strip LeVoileLog.i (notre wrapper) en release.
   # Le BuildConfig.DEBUG check interne du wrapper le rend déjà no-op en release,
   # mais la rule -assumenosideeffects permet à ProGuard de l'éliminer entièrement
   # du bytecode (économie taille APK + élimination de la chaîne de message).
   -assumenosideeffects class fr.plateformeliberte.levoile.log.LeVoileLog {
       public void i(...);
   }
   ```
   **Important** :
   - Modifier la rule `-assumenosideeffects class android.util.Log` existante (Story 9.1 livre la version `d` + `v`) en ajoutant `i`. Conserver les règles existantes pour `d` et `v`.
   - Ajouter une rule séparée pour `LeVoileLog.i` (le wrapper). La rule cible `public void i(...)` car les méthodes Kotlin object sont compilées en `INVOKEVIRTUAL` sur l'instance singleton — selon le bytecode généré par kotlinc, ça peut être `public final void i(...)`. **Vérifier au runtime** via inspection APK release : `apkanalyzer dex packages app/build/outputs/apk/release/app-release.apk | grep LeVoileLog` — si la classe `LeVoileLog` est encore présente après ProGuard, ajuster la rule (peut-être `-assumenosideeffects class fr.plateformeliberte.levoile.log.LeVoileLog$Companion { ... }` selon variante singleton). **Reporter dans Debug Log** : strippage observé après build release.
   - **Ne PAS** stripper `LeVoileLog.w` ni `.e` — les WARN et ERROR doivent rester visibles en release (NFR-AND-9 : release = WARN+ visible).
   - **Ne PAS** stripper `android.util.Log.w` ni `.e` — idem.
   - **TODO Story 9.7 préservé** : la rule existante `-keep class fr.plateformeliberte.levoile.core.**` (Story 9.1 ligne 4) reste intacte. Story 10.5 n'altère pas le keep des classes core gomobile.

3. **`LogFilteringTest.kt` scanne récursivement tous les `.kt` du module et fail si pattern interdit** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté après cette story, un fichier de test `app/src/test/kotlin/fr/plateformeliberte/levoile/log/LogFilteringTest.kt` exécute :
   ```kotlin
   class LogFilteringTest {

       /**
        * Liste des variables interdites dans les templates de log.
        * Cohérent NFR-AND-9 (prd.md l. 705) et NFR22a (prd.md l. 672) :
        * « Aucune URL, aucun nom de domaine, aucune destination IP, aucun contenu utilisateur loggué ».
        *
        * La liste cible les noms de variables conventionnels Kotlin :
        *   - $url, $domain, $destIp, $userContent (mentionnés epics.md l. 1799)
        *   - $requestBody, $responseBody (mentionnés epics.md l. 1802)
        *   - $packageName (révèle quel autre VPN concurrent — sensible cohérent NFR-AND-9 + Story 10.3 AC #5)
        *   - $foreignAppId (idem — Story 10.3)
        *   - $pinnedApp (idem — Story 10.1 AC #7 documente déjà l'interdiction)
        *
        * La liste peut être étendue par des stories futures qui introduiraient
        * de nouvelles variables sensibles. Toute extension doit aussi mettre à jour
        * AuditCITest.kt (cohérent Story 10.4) ou un nouvel anti-regression similaire.
        */
       private val forbiddenInterpolations = listOf(
           "url",
           "domain",
           "destIp",
           "userContent",
           "requestBody",
           "responseBody",
           "packageName",
           "foreignAppId",
           "pinnedApp",
       )

       /**
        * Pattern : `Log.[diwev]\(...,\s*"[^"]*\$\{NAME\}` (multi-line=false suffit
        * car les calls Log.* tiennent sur une ligne en pratique).
        * On capture aussi `LeVoileLog.[iwe](...)` pour ne pas laisser le wrapper échapper.
        */
       @Test
       fun `aucun log ne contient de variable interdite`() {
           val srcDirs = listOf(
               java.io.File("src/main/kotlin"),
               // Pas de scan src/test/ — les tests peuvent légitimement contenir des chaînes
               // de fixtures qui matcheraient les patterns. Test = code non production.
           )
           val violations = mutableListOf<String>()
           srcDirs.forEach { rootDir ->
               if (!rootDir.exists()) return@forEach
               rootDir.walkTopDown()
                   .filter { it.isFile && it.extension == "kt" }
                   .forEach { ktFile ->
                       val content = ktFile.readText()
                       content.lineSequence().forEachIndexed { idx, line ->
                           // Match les calls Log.* et LeVoileLog.*
                           val isLogCall = line.contains("Log.") && (
                               line.contains("Log.d(") ||
                               line.contains("Log.v(") ||
                               line.contains("Log.i(") ||
                               line.contains("Log.w(") ||
                               line.contains("Log.e(") ||
                               line.contains("LeVoileLog.")
                           )
                           if (!isLogCall) return@forEachIndexed
                           // Pour chaque variable interdite, vérifier l'interpolation $name OU ${name}
                           forbiddenInterpolations.forEach { variable ->
                               val patternBraced = "\${$variable}"  // ${url}
                               val patternBare = "\$$variable"      // $url
                               if (line.contains(patternBraced) || line.contains(patternBare)) {
                                   violations += "${ktFile.relativeTo(java.io.File(".")).path}:${idx + 1} → variable interdite '\$$variable' (NFR-AND-9)"
                               }
                           }
                       }
                   }
           }
           if (violations.isNotEmpty()) {
               val report = violations.joinToString("\n  ", prefix = "  ")
               throw AssertionError(
                   """
                   |❌ Pattern log interdit détecté — cohérent NFR-AND-9 (prd.md l. 705) :
                   |$report
                   |
                   |Le client Android Le Voile ne doit JAMAIS logger : URL, domaine, IP destination,
                   |contenu utilisateur, packageName d'app concurrente, ou tout autre identifiant
                   |révélateur du trafic ou de l'environnement utilisateur.
                   |Reformulez le log en supprimant la variable, ou justifiez via ADR si la variable
                   |est en réalité non-sensible (ex. constante de build).
                   """.trimMargin()
               )
           }
       }

       /**
        * Anti-regression : la liste forbiddenInterpolations doit comprendre AU MINIMUM
        * les 6 variables nommées par epics.md l. 1799 + l. 1802. Ce test fail si quelqu'un
        * retire une variable de la liste sans avoir mis à jour les sources.
        */
       @Test
       fun `liste forbiddenInterpolations contient les 6 variables canoniques`() {
           val canonical = listOf("url", "domain", "destIp", "userContent", "requestBody", "responseBody")
           canonical.forEach { v ->
               assertTrue(
                   "La liste forbiddenInterpolations doit contenir '$v' (cohérent epics.md l. 1799-1802)",
                   forbiddenInterpolations.contains(v)
               )
           }
       }
   }
   ```
   **Important** :
   - **Test JVM-only**, pas de Robolectric, pas de framework regex avancé. Lecture de fichiers + `String.contains`.
   - **Scan de `src/main/kotlin/` uniquement** : les fichiers de test (`src/test/kotlin/`, `src/androidTest/kotlin/`) peuvent légitimement contenir des chaînes fixtures qui matcheraient (ex. un test `assertEquals("user data with url=$url", actual)` qui n'est pas un vrai log mais un assert). **Justifier ce choix dans le Kdoc** du test.
   - **Working directory au test** : Gradle `cwd` pour `:app:testDebugUnitTest` est `android/app/`. Les chemins `src/main/kotlin` sont relatifs à `android/app/`. Vérifier au runtime — si désync, ajuster (cohérent Story 10.4 Task 6).
   - **`lineSequence()` séquentiel** : pas de parsing AST Kotlin. Approche simple = robuste. Faux-positif possible : un commentaire qui contient « `// TODO: log $url here` » serait détecté. **C'est intentionnel** — un commentaire mentionnant `$url` est probablement un site futur de violation, autant l'attraper. **Cas particulier** : si un dev a un commentaire légitime comme `// Le pattern Log.i(TAG, "${url}") serait interdit ici`, il doit reformuler son commentaire (ex. `// Le pattern « Log.i avec $${dollar}{url} » serait interdit ici`) — coût marginal, gain robustesse.
   - **Pas de regex multiline** : `lineSequence()` traite ligne par ligne. Un log multi-ligne avec `String.format` ou raw string `"""..."""` pourrait échapper. **Acceptable trade-off** : Kotlin Compose et la plupart des codes Android utilisent des logs sur 1 ligne. Un dev qui voudrait tricher avec un multi-ligne reste auditable manuellement à la code review. Pour scanner les multi-lignes proprement, il faudrait un parser AST Kotlin (KSP, Kotlin Compiler API) — overkill MVP, reporter Phase 2 si besoin.
   - **Pas de logging dans le test lui-même** (`println` autorisé en test mais à minimiser pour éviter pollution Logcat de CI).

4. **Le workflow `android-audit.yml` (Story 10.4) invoque `LogFilteringTest`** — Quand `.github/workflows/android-audit.yml` est lu après cette story, il contient en plus du contenu livré Story 10.4 un step additionnel :
   ```yaml
   - name: Run LogFilteringTest unit test
     working-directory: android
     run: ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.log.LogFilteringTest" --no-daemon
   ```
   inséré APRÈS le step `Run AuditCITest unit test` (livré Story 10.4) — cela permet d'exécuter les 2 tests d'audit (télémétrie + filtrage logs) sur le même runner CI sans relancer Gradle.

   **Alternative consolidation** : modifier le step existant `Run AuditCITest unit test` pour exécuter les **2 tests** en une seule commande :
   ```yaml
   - name: Run audit unit tests (telemetry + log filtering)
     working-directory: android
     run: ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.audit.*" --tests "fr.plateformeliberte.levoile.log.LogFilteringTest" --no-daemon
   ```
   **Décision dev à reporter dans Completion Notes** : 2 steps séparés (lisibilité + parallèle output CI) vs 1 step consolidé (économie redémarrage Gradle). **Recommandation** : 2 steps séparés (lisibilité prime).

5. **Story 10.5 ne migre PAS les `Log.*` existants vers `LeVoileLog.*`** — Quand `git diff android/app/src/main/kotlin/` est inspecté après cette story, **aucun fichier Kotlin runtime existant** (Stories 9.x-10.4) n'a vu ses appels `android.util.Log.*` remplacés par `LeVoileLog.*`. La justification :

   - **Volume** : à la livraison de Story 10.5, le module `:app` contient ~10-20 sites d'appel `Log.*` (Stories 9.3 MainActivity, 9.4 LeVoileVpnService, 10.1 KillSwitchDetector — qui sera 0 si Story 10.1 n'a pas eu de besoin Unverifiable…). Migrer ces 10-20 sites est ~30 min de travail mais introduit 10-20 lignes de diff dans des fichiers livrés par d'autres stories — **bruit à la code review** non lié à l'objet de Story 10.5 (filtrage par buildType).

   - **Bénéfice marginal** : `android.util.Log.*` est strippé exactement de la même façon que `LeVoileLog.*` par les rules ProGuard (AC #2). **Aucun gain de sécurité** à la migration. Le bénéfice est uniquement ergonomique (raccourcir `android.util.Log.i(TAG, ...)` en `LeVoileLog.i(TAG, ...)`).

   - **Convention pour les stories futures** : tout NOUVEAU log ajouté par une story future (Stories 9.5+, 11.x, 12.x) DOIT utiliser `LeVoileLog.*` plutôt que `android.util.Log.*` (sauf cas où les niveaux DEBUG/VERBOSE sont nécessaires — `LeVoileLog` ne les expose pas, le dev devrait alors `android.util.Log.d` directement). Cette convention est documentée dans le README AC #6 et **vérifiée à la code review**, pas par un test automatisé.

   **Décision projet à reporter dans Completion Notes** : un éventuel refactor « migration globale `Log.* → LeVoileLog.*` » est reporté à Phase 2 si demandé. Pour l'instant, coexistence acceptée.

6. **Section README-android.md « Filtrage logs zéro-data-utilisateur »** — Quand `android/README-android.md` est lu après cette story, il contient une section additionnelle (insérée APRÈS la section Story 10.4 « Audit télémétrie zéro-tracking ») :

   ```markdown
   ## Filtrage logs zéro-data-utilisateur (Story 10.5 livrée)

   Cohérent NFR-AND-9 (prd.md l. 705) + NFR22a (prd.md l. 672) + ADR-15 :
   les logs Le Voile ne contiennent JAMAIS d'URL, de domaine, d'IP destination,
   de contenu utilisateur, ni d'identifiant d'app concurrente.

   ### Wrapper `LeVoileLog`

   `LeVoileLog` (`app/src/main/kotlin/.../log/LeVoileLog.kt`) expose `i / w / e`
   et applique le filtrage par buildType :
   - **debug** : INFO+ visible Logcat.
   - **release** : WARN+ uniquement (INFO strippé par ProGuard).

   Convention pour les stories futures : utiliser `LeVoileLog.*` plutôt que
   `android.util.Log.*` pour les nouveaux logs. Les sites d'appel existants
   pré-Story 10.5 utilisent encore `android.util.Log.*` — la migration globale
   n'a pas de bénéfice de sécurité (ProGuard strippe les 2 wrappers de la même
   manière) et est reportée si demandée.

   ### Test scan `LogFilteringTest`

   Le test `LogFilteringTest.kt` scanne tous les `.kt` de `src/main/kotlin/`
   et échoue si un site `Log.*` ou `LeVoileLog.*` contient l'une des variables
   interdites : `$url`, `$domain`, `$destIp`, `$userContent`, `$requestBody`,
   `$responseBody`, `$packageName`, `$foreignAppId`, `$pinnedApp`.

   **Test local** :
   ```
   cd android
   ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.log.LogFilteringTest"
   ```

   En cas d'échec, l'output liste exactement le fichier:ligne et la variable
   offensive. **Reformuler le log** en supprimant l'interpolation, ou — si la
   variable est en réalité non-sensible (ex. constante de build) — justifier
   via ADR avant ajustement de la liste.
   ```

   Aucune autre section du README n'est touchée par cette story.

7. **Build sanity** — Quand `cd android && ./gradlew clean assembleDebug assembleRelease :app:testDebugUnitTest :app:lint` est exécuté après cette story, **toutes** les tâches passent (exit 0). En particulier :
   - `assembleDebug` produit un APK debug fonctionnel (les `Log.i` debug compilent).
   - `assembleRelease` produit un APK release fonctionnel — vérifier que ProGuard ne fail pas sur la nouvelle rule (peut arriver si la classe `LeVoileLog` n'est pas trouvée — vérifier le chemin de classe résolu).
   - `apkanalyzer apk file-size app/build/outputs/apk/release/app-release.apk` — taille reste < 25 MB (NFR-AND-3). Le strippage des `Log.i` peut **réduire légèrement** la taille (économie de chaînes constantes).
   - `:app:lint` ne signale pas de violation (la rule `-assumenosideeffects` est syntaxiquement correcte).
   - `:app:testDebugUnitTest` passe vert pour `LogFilteringTest` (le code Kotlin actuel ne contient aucune des 9 variables interdites — vérifié manuellement Stories 9.x-10.4).

## Tasks / Subtasks

- [ ] **Task 1 : Vérifier l'état Stories 9.x + 10.1-10.4 livrées** (AC: tous)
  - [ ] Lire `android/app/proguard-rules.pro` (livré Story 9.1) — confirmer présence de la rule `-assumenosideeffects` pour `Log.d` + `Log.v`. Identifier où insérer l'extension pour `Log.i`.
  - [ ] Lire `android/app/build.gradle.kts` — confirmer `buildConfig = true` (Story 9.1) — `BuildConfig.DEBUG` est requis par AC #1.
  - [ ] Lire `.github/workflows/android-audit.yml` (livré Story 10.4) — identifier où insérer le step `Run LogFilteringTest`.
  - [ ] Scan rapide via `grep -r "android.util.Log\." android/app/src/main/kotlin/ | wc -l` pour compter les sites d'appel actuels (= taille hypothétique d'une migration future). Reporter dans Completion Notes : « N sites d'appel Log.* existants, migration future = ~N min ».
  - [ ] **Reporter dans Debug Log** : état exact des fichiers lus.

- [ ] **Task 2 : Créer `LeVoileLog.kt`** (AC: #1)
  - [ ] Créer `android/app/src/main/kotlin/fr/plateformeliberte/levoile/log/LeVoileLog.kt`.
  - [ ] Implémenter selon AC #1 (object singleton, `i/w/e`, pas de `d/v`, `if (BuildConfig.DEBUG)` interne sur `i`).
  - [ ] Kdoc complet (5-10 lignes) — référence ADR-15, NFR-AND-9, justification du wrapper.

- [ ] **Task 3 : Modifier `proguard-rules.pro`** (AC: #2)
  - [ ] Étendre la rule `-assumenosideeffects class android.util.Log` existante en ajoutant `public static int i(...);`.
  - [ ] Ajouter une nouvelle rule séparée `-assumenosideeffects class fr.plateformeliberte.levoile.log.LeVoileLog { public void i(...); }`.
  - [ ] **Vérifier après build release** que la classe `LeVoileLog` est correctement strippée — voir Task 8.
  - [ ] Conserver toutes les autres rules existantes (Story 9.1 keep gomobile, etc.).

- [ ] **Task 4 : Créer `LogFilteringTest.kt`** (AC: #3)
  - [ ] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/log/LogFilteringTest.kt`.
  - [ ] Implémenter selon AC #3 (2 tests : scan + anti-regression liste canonique).
  - [ ] **Vérifier** que les sources actuelles ne contiennent aucune des 9 variables interdites — sinon le test fail dès création (cas peu probable mais défensif). Si fail → soit reformuler le log offensif, soit retirer la variable de la liste avec ADR justificatif.

- [ ] **Task 5 : Modifier `.github/workflows/android-audit.yml`** (AC: #4)
  - [ ] Insérer le step `Run LogFilteringTest unit test` après le step Story 10.4 `Run AuditCITest unit test`.
  - [ ] Vérifier syntaxe YAML.
  - [ ] **Décision Option A (2 steps séparés) vs Option B (consolidation 1 step)** — recommandation A. Reporter dans Completion Notes.
  - [ ] **Aucune autre modification** du workflow.

- [ ] **Task 6 : Patcher `README-android.md`** (AC: #6)
  - [ ] Insérer la section « Filtrage logs zéro-data-utilisateur (Story 10.5 livrée) » au bon endroit (après section Story 10.4).

- [ ] **Task 7 : Test scan local — vérifier qu'aucune variable interdite n'existe déjà** (AC: #3)
  - [ ] `cd android && ./gradlew :app:testDebugUnitTest --tests "fr.plateformeliberte.levoile.log.LogFilteringTest"` — doit passer vert.
  - [ ] Test négatif local (régression positive) : ajouter temporairement dans un `.kt` runtime un site `LeVoileLog.i(TAG, "User connected to ${url}")`. Re-runner le test — doit fail avec message explicite. **Retirer le test négatif AVANT commit**. Vérifier `git diff` propre.
  - [ ] **Reporter dans Debug Log** : capture textuelle de l'output observé (négatif et positif).

- [ ] **Task 8 : Vérification stripping ProGuard sur APK release** (AC: #2, #7)
  - [ ] `cd android && ./gradlew clean assembleRelease`.
  - [ ] `apkanalyzer dex packages app/build/outputs/apk/release/app-release.apk | grep LeVoileLog` — la classe ne doit **plus apparaître** (strippée par ProGuard) OU apparaître uniquement avec les méthodes `w` et `e` (pas `i`).
  - [ ] Si la classe est encore là avec `i` non-strippé : ajuster la rule (peut-être Kotlin object compile en `LeVoileLog$Companion` ou `LeVoileLog$INSTANCE` selon version kotlinc). Tester variantes.
  - [ ] **Reporter dans Debug Log** : output exact de `apkanalyzer dex packages`.
  - [ ] `apkanalyzer apk file-size app/build/outputs/apk/release/app-release.apk` — la taille devrait être stable ou très légèrement réduite vs Story 10.4.

- [ ] **Task 9 : Build sanity check global** (AC: #7)
  - [ ] `cd android && ./gradlew clean assembleDebug assembleRelease check :app:testDebugUnitTest :app:lint` — toutes tâches vert.
  - [ ] Vérifier que la nouvelle task d'audit télémétrie (Story 10.4) est toujours invoquée par `check` et passe.
  - [ ] Vérifier que `LogFilteringTest` est inclus dans `:app:testDebugUnitTest` (par défaut Gradle Android — pas de configuration explicite nécessaire).

- [ ] **Task 10 : Test du workflow GitHub Actions sur une branche test** (Optionnel mais recommandé)
  - [ ] Pousser une branche test `test/10-5-log-filter` et créer une PR draft.
  - [ ] Vérifier que le workflow `Android · Audit télémétrie` se déclenche et exécute les 2 tests d'audit (télémétrie + filtrage logs).
  - [ ] **Reporter dans Debug Log** : URL de la run GitHub Actions.

- [ ] **Task 11 : Mettre à jour la story et sprint-status**
  - [ ] Mettre à jour la section « Dev Agent Record » (Agent Model Used, File List, Completion Notes List, Change Log).
  - [ ] Passer le `Status` de cette story de `ready-for-dev` à `review`.
  - [ ] Passer `_bmad-output/implementation-artifacts/sprint-status.yaml` `10-5-filtrage-logs-...: backlog` → `review`.

## Dev Notes

### Pattern principal — Wrapper logger + ProGuard stripping + scan source

L'approche est triple-layer :
1. **Compile-time runtime** : `LeVoileLog.i` a un `if (BuildConfig.DEBUG)` qui rend l'appel no-op en release même sans ProGuard.
2. **ProGuard strip** : `-assumenosideeffects` retire les appels du bytecode release entièrement (économie taille APK + élimination des chaînes de message du `.dex`).
3. **Scan statique source** : `LogFilteringTest` scan les `.kt` à chaque PR et fail si une variable interdite apparaît dans un template de log.

Pourquoi **les 3** alors que ProGuard seul suffirait ? Parce que :
- ProGuard ne strippe que les **appels**, pas les **chaînes constantes** isolées si elles existent (rare mais possible).
- Le scan statique attrape AUSSI les `Log.w` / `Log.e` qui ne sont **pas** strippés (release WARN+ visible) — un dev pourrait introduire une fuite via `Log.w(TAG, "User clicked $url")` qui passe ProGuard et reste visible Logcat.

Le scan statique est donc le **filet de sécurité ultime** — il vérifie l'**intention** du code, pas seulement le **bytecode** produit.

### Pourquoi `internal object LeVoileLog` et pas `class LeVoileLog`

- `object` Kotlin = singleton compile-time, zero allocation à chaque call.
- `internal` = visibilité module `:app` uniquement. Les autres modules (`:levoile-core`) n'ont pas accès — c'est OK car `:levoile-core` est juste un container pour le `.aar` gomobile (peu de Kotlin).

### Coordination Story 12.6 (tests instrumentés Espresso)

Les tests instrumentés Espresso (Story 12.6) **n'ont pas vocation à logger** vers `Log.*` — ils utilisent `assertEquals`, `assertTrue`, etc. Donc le scan `LogFilteringTest` (qui exclut `src/test/` et `src/androidTest/`) ne les affecte pas. Cohérent.

### Coordination Story 11.2 (JS Bridge complet)

Story 11.2 ajoutera des méthodes au bridge JS — chacune devra logger des erreurs (input invalide, tunnel fail, etc.). **Story 11.2 doit utiliser `LeVoileLog.*`** plutôt que `android.util.Log.*` (convention documentée dans README AC #6) — et ne **jamais** logger les inputs JS reçus (qui pourraient contenir des `$url` etc. — cohérent NFR-AND-9 et AC #3 de Story 11.2 pour la validation des inputs).

Pas de hard-couplage Story 10.5 ↔ Story 11.2 — les conventions sont documentées et vérifiées par scan.

### Source tree components à toucher

- **Nouveaux** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/log/LeVoileLog.kt`
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/log/LogFilteringTest.kt`
- **Modifiés** :
  - `android/app/proguard-rules.pro` (extension `Log.i` + nouvelle rule `LeVoileLog.i`)
  - `.github/workflows/android-audit.yml` (ajout step LogFilteringTest)
  - `android/README-android.md` (section nouvelle)

### Standards de testing

- Test JVM-only `LogFilteringTest.kt` : 2 tests, < 500ms (lecture de tous les `.kt` du module).
- Pas de Robolectric.
- Pas de framework AST Kotlin (KSP, KCP) — overkill MVP.
- Validation manuelle de l'injection volontaire (Task 7) — preuve que le scan **fail** correctement.

### Project Structure Notes

Le package `fr.plateformeliberte.levoile.log` est nouveau (côté `src/main/`). Cohérent avec le découpage existant (`kill/`, `conflict/`, `bridge/`, `vpn/`).

### References

- [architecture.md l. 705](_bmad-output/planning-artifacts/architecture.md) — NFR-AND-9 release WARN+ / debug INFO+.
- [architecture.md l. 1502](_bmad-output/planning-artifacts/architecture.md) — patterns Activity + lifecycle.
- [architecture.md l. 2397-2400](_bmad-output/planning-artifacts/architecture.md) — ADR-08 isolation OS (logger Android distinct logger desktop).
- [architecture.md l. 2439-2442](_bmad-output/planning-artifacts/architecture.md) — ADR-15 zéro-télémétrie / crash reporter Android.
- [epics.md l. 1783-1804](_bmad-output/planning-artifacts/epics.md) — Story 10.5 BDD complet (3 scénarios Given/When/Then) + liste regex variables interdites.
- [prd.md l. 672](_bmad-output/planning-artifacts/prd.md) — NFR22a aucune URL / domaine / destination IP / contenu utilisateur loggué.
- [prd.md l. 705](_bmad-output/planning-artifacts/prd.md) — NFR-AND-9 spécifique Android.
- Story 9.1 (livrée) : `proguard-rules.pro` baseline avec strip `Log.d` + `Log.v` (étendu ici à `Log.i`).
- Story 9.4 (à venir, file existante non implémentée) : `LeVoileVpnService.kt` — utilisera `Log.w` (visible release) pour les erreurs critiques tunnel.
- Story 10.1 (livrée) : `KillSwitchDetector` AC #7 documente déjà le format de log « heuristique indisponible » sans data utilisateur — exemple canonique de bonne pratique.
- Story 10.3 (livrée) : `VpnConflictDetector` AC #5 documente la convention « pas de log avec `pinnedApp` ou `foreignAppId` ».
- Story 10.4 (livrée) : workflow `android-audit.yml` étendu ici avec le step LogFilteringTest.
- Story 11.2 (à venir) : convention `LeVoileLog.*` à adopter dans tout nouveau code (documentée README).
- Story 12.6 (à venir) : tests instrumentés Espresso n'utilisent pas `Log.*` → pas d'impact.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

### Change Log
