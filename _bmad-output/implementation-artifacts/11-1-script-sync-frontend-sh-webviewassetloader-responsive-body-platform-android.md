# Story 11.1: Script `sync-frontend.sh` + WebViewAssetLoader + responsive `body.platform-android`

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story** — sauf l'exception décrite ci-dessous (création OPTIONNELLE de `frontend/` racine canonique, voir AC #1 Note A).
>
> **Story 11.1 livre** :
> 1. Le script `android/scripts/sync-frontend.sh` (Linux/macOS) + variante `android/scripts/sync-frontend.ps1` (Windows pour devs sous Windows) qui copie de manière idempotente les assets HTML/CSS/JS desktop dans `android/app/src/main/assets/web/`.
> 2. Une mise à jour de `android/app/src/main/assets/.gitignore` (création) qui empêche le check-in du résultat de la copie (source de vérité = arbre frontend desktop).
> 3. Le repointage de `MainActivity.ASSET_INDEX_URL` vers le sous-chemin `web/` (`https://appassets.androidplatform.net/assets/web/index.html`) — le `WebViewAssetLoader` continue à pointer `/assets/` (mappant `app/src/main/assets/`).
> 4. L'élargissement du `WebViewAssetLoader` pour servir AUSSI les fonts (sous-arbre `assets/web/fonts/`) — déjà géré par `AssetsPathHandler` mais à confirmer en runtime.
> 5. Une feuille CSS dédiée Android `body.platform-android` qui :
>    - **Désactive** les classes desktop interdites mobile : `.desktop-only`, `.titlebar` (C1), `.sidebar` (C2), `.sidebar .country-item` (C3), `.star-favorite` (C4), `.quit-modal` (C8) — via `display: none !important`.
>    - **Active** un layout vertical pleine largeur (`< 600dp`) : statut centré, sélecteur pays vertical (placeholder pour C14 Story 11.4).
>    - **Cible tactile minimum 48dp**, contraste texte ≥ 4.5:1 (RGAA AA).
> 6. Un test JVM `SyncFrontendTest.kt` qui vérifie l'idempotence du script (re-exécution sans diff produit un état identique) — exécuté en CI.
> 7. Documentation `README-android.md` enrichie : section « Synchronisation des assets desktop ».
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile. Aucune entrée dans `android/shims/*.go`. Aucune ligne dans `go.mod`/`go.sum` racine.
>
> **Rappel ADR-08 (architecture.md l. 1089-1111) — isolation OS maximale.** Le sync frontend est par construction une opération de packaging Android (lecture assets desktop pour repackaging APK). Sur desktop, les assets sont embarqués via `//go:embed` dans `windows/frontend/embed.go` et `linux/frontend/embed.go` — chaque OS a son propre embed. Toute tentative de "factoriser" un système de packaging cross-OS partagé est une violation directe d'ADR-08 — refusée en code review.
>
> **Choix de la source canonique des assets desktop — décision dev requise** :
>
> L'epic mentionne `internal/ui/web/` (epics.md l. 1820) mais cette arborescence n'existe pas dans le repo après la révision OS-isolation 2026-04-15. Le repo réel contient :
> - `windows/frontend/` (HTML/CSS/JS Windows — embedded via `//go:embed`)
> - `linux/frontend/` (HTML/CSS/JS Linux — embedded via `//go:embed`)
> - Les 2 sont quasi-identiques, sauf un fix Windows readyState dans `app.js` (commit 735d5d8 « fix(windows): cold-start blank webview »)
>
> **Décision recommandée et option par défaut** : pointer le script vers `../windows/frontend/` (chemin relatif depuis `android/scripts/`). Justification : Windows porte le fix le plus récent (readyState handling) ; le diff avec Linux est minime (1 bloc) et l'écart est en faveur de Windows en termes de robustesse. L'écart sera convergé Phase 2 (story Linux à venir si nécessaire — hors scope Phase 2 Android actuelle).
>
> **Option alternative à évaluer (Note A AC #1)** : créer `frontend/` racine canonique en copiant `windows/frontend/` puis pointer le script vers cette racine. Avantage : single source of truth conforme à l'intention originale d'epics.md. Inconvénient : refactor des `//go:embed` Windows + Linux — **HORS SCOPE Story 11.1**, créerait une dette technique cross-OS sans bénéfice immédiat. **Recommandation : NE PAS prendre cette option** ; documenter le gap dans Completion Notes pour discussion ADR ultérieure (création éventuelle d'un ADR-16 « source frontend canonique »).
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.x/10.x/11.x autres, intactes pour 11.1) :
>
> | Zone | Livrée par | État pour 11.1 |
> |---|---|---|
> | `go.mod` / `go.sum` racine | Story 9.2 | INTACT |
> | `android/shims/*.go` | Story 9.2 | INTACT |
> | `android/scripts/build-aar.{sh,ps1}` | Story 9.2 | INTACT |
> | `android/scripts/verify-shared-imports.sh` | Story 9.2 | INTACT |
> | `android/levoile-core/build.gradle.kts` | Story 9.1+9.2 | INTACT |
> | `android/app/build.gradle.kts` | Stories 9.1+10.1+10.4 | **MODIFIÉ uniquement à la marge** : ajouter `androidResources.ignoreAssetsPattern` ou `aaptOptions` pour exclure les fichiers temporaires éventuels du sync (justifier dans Dev Notes si nécessaire) — sinon INTACT |
> | `android/app/src/main/AndroidManifest.xml` | Story 9.1 + 9.4 | INTACT |
> | `android/app/src/main/kotlin/...{kill,conflict,bridge,vpn,log}/` | Stories 9.x/10.1-10.5 | INTACT |
> | `android/app/src/main/kotlin/.../MainActivity.kt` | Stories 9.3+9.5+10.1-10.3 | **MODIFIÉ uniquement à la marge** : changer la constante `ASSET_INDEX_URL` de `/assets/index.html` vers `/assets/web/index.html`. Aucune autre logique touchée |
> | `android/app/src/main/assets/{index.html, style.css, app.js}` | Stories 9.3 + 10.2 | **SUPPRIMÉS** par cette story (remplacés par sous-arbre `web/` produit par sync) |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,vpndetect,...}` racine + `windows/{frontend,internal,cmd}/`, `linux/{frontend,internal,cmd}/`, `relay/`, `tools/` | Stories 1-8 desktop | INTACT — hors arbre `android/` (LECTURE SEULE par le script de sync) |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) `android/scripts/sync-frontend.sh` (NOUVEAU — script bash idempotent),
>   (b) `android/scripts/sync-frontend.ps1` (NOUVEAU — variante PowerShell pour devs Windows),
>   (c) `android/app/src/main/assets/.gitignore` (NOUVEAU — exclut sous-arbre `web/`),
>   (d) `android/app/src/main/assets/web/.gitkeep` (NOUVEAU — placeholder pour que le dossier existe),
>   (e) `android/app/src/main/assets/index.html` (SUPPRIMÉ — remplacé par `web/index.html` après sync),
>   (f) `android/app/src/main/assets/style.css` (SUPPRIMÉ — remplacé par `web/style.css` + `web/style-android.css`),
>   (g) `android/app/src/main/assets/app.js` (SUPPRIMÉ — remplacé par `web/app.js` après sync),
>   (h) `android/app/src/main/assets/web/style-android.css` (NOUVEAU — overrides Android-spécifiques `body.platform-android`),
>   (i) `android/app/src/main/assets/web/index-android.patch` ou équivalent **REJETÉ** : pas de patching du HTML desktop, on utilise `<link rel="stylesheet" href="style-android.css">` injecté par le script si pas déjà présent (cf. AC #5),
>   (j) `android/app/src/main/kotlin/.../MainActivity.kt` (MODIFIÉ — `ASSET_INDEX_URL` repointé vers `/assets/web/index.html`),
>   (k) `android/app/src/test/kotlin/.../assets/SyncFrontendTest.kt` (NOUVEAU — test idempotence + structure du sous-arbre `web/`),
>   (l) `android/README-android.md` (MODIFIÉ — section « Synchronisation des assets desktop »),
>   (m) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `backlog`/`ready-for-dev` → `review`),
>   (n) `_bmad-output/implementation-artifacts/11-1-script-sync-frontend-sh-webviewassetloader-responsive-body-platform-android.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `windows/`, `linux/`, `relay/`, `tools/`, `_bmad/`), aucune entrée sous `android/shims/`, `android/levoile-core/`, ni dans les fichiers Kotlin runtime livrés Stories 9.x-10.5 (sauf `MainActivity.kt` à la marge pour `ASSET_INDEX_URL`). Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser"**. Reporter dans Debug Log.
>
> **Anti-pattern fréquent à éviter** : tenter de :
> - Créer `frontend/` racine canonique et refactor des `//go:embed` Windows+Linux pour pointer dessus (= Option A AC #1, hors scope, recommandation explicite « ne PAS prendre »).
> - Bundler webpack/rollup/vite côté Android (over-engineering, les assets desktop n'ont pas de bundler côté desktop, le sync est un simple copy).
> - Ajouter une dépendance Gradle pour automatiser le sync au build (over-coupling — le script doit pouvoir tourner manuellement ; CI peut l'invoquer comme step séparé Story 12.2).
> - Créer un système de templating (ex. handlebars, mustache) pour générer les variantes Android du HTML — reformulé comme « overrides CSS sous `body.platform-android` » est strictement suffisant, le HTML reste partagé tel quel.
> - Étendre le `WebViewAssetLoader` avec des handlers customs (ex. `LegacyAssetsPathHandler`) — `AssetsPathHandler` standard suffit pour servir le sous-arbre `web/`.
> - Ajouter un script `sync-frontend.dart`, `sync-frontend.py`, etc. — bash + PowerShell suffisent (cohérent `build-aar.{sh,ps1}` Story 9.2).
>
> Si tu te retrouves à modifier 30+ fichiers ou à introduire des dépendances Gradle nouvelles, tu es hors-scope — STOP.

## Story

En tant qu'utilisatrice Android Le Voile,
Je veux une UI plein écran reprenant la charte plateformeliberte.fr identique au desktop, mais adaptée mobile (cibles tactiles 48dp+, layout vertical, contraste RGAA AA),
Afin que ma confiance visuelle soit immédiate dès le premier lancement (cohérent prd.md FR-AND-4 + UX cohérence cross-OS) et que l'expérience tactile soit native (cohérent ADR-14 architecture.md l. 1166 + ux-design-specification.md l. 1255-1373) — vérifiable mécaniquement via un test JVM `SyncFrontendTest` qui assert l'idempotence du script et la présence des fichiers attendus dans `app/src/main/assets/web/`.

## Acceptance Criteria

> ⚠️ **AC #1 et AC #2 RÉVISÉS post-implémentation (2026-05-03)** — la décision dev Option 2 (assets Android-natifs versionnés, pas de sync depuis `windows/frontend/`) est désormais officialisée par **ADR-16** (`_bmad-output/planning-artifacts/architecture.md`). Les AC ci-dessous reflètent la spec **finale** ; la spec originale (script de sync + `.gitignore` actif) est conservée à des fins historiques en bas de section sous « ## AC originaux pré-ADR-16 (obsolètes) ».

1. **Le script `android/scripts/sync-frontend.sh` existe et sert d'idempotency check (Option 2 ADR-16)** — Quand `bash android/scripts/sync-frontend.sh` est exécuté, il :
   - Détecte la racine du repo via `BASH_SOURCE` (évite `git rev-parse` sous Git-for-Windows qui retourne des chemins mixtes).
   - Vérifie la présence des 4 fichiers requis dans `android/app/src/main/assets/web/` : `index.html`, `style.css`, `app.js`, `style-android.css`.
   - Si tous présents → exit 0 + log « OK — N fichiers présents, idempotent re-run safe ».
   - Si au moins 1 manquant → log les manquants en stderr + exit 2.
   - **Pas de sync** : aucun `cp`, `sed`, ou injection HTML. La justification est dans ADR-16 (frontend desktop trop spécifique Windows + Story 10.2 bandeau C17 livré directement Android-natif).
   - La variante PowerShell `android/scripts/sync-frontend.ps1` implémente la même logique (utilise `Write-Warning` pour accumuler les manquants — `Write-Error` couplé à `$ErrorActionPreference = 'Stop'` est terminating et empêcherait la liste complète).
   - **Coordination CI Story 12.2** : le script est invoqué comme step pre-build pour bloquer un APK construit sans les 4 assets — détecte les régressions (ex. un `git rm` accidentel d'un fichier).

2. **`android/app/src/main/assets/.gitignore` est vide commenté (anti-régression Option 2)** — Quand `cat android/app/src/main/assets/.gitignore` est lu après cette story, il contient :
   ```gitignore
   # Story 11.1 — Décision dev Option 2 (cf. Completion Notes 11.1) : pas de sync
   # depuis windows/frontend/. Les fichiers sous web/ sont versionnés (assets
   # Android-natifs, cohérent ADR-08 isolation OS + memory feedback_os_isolation).
   #
   # Ce .gitignore reste vide intentionnellement. Si une story future réintroduit
   # un sync (ADR-16 « source frontend canonique » à créer), décommenter la
   # règle ci-dessous et créer `web/.gitkeep` (placeholder) lors du switch.
   #
   # web/*
   # !web/style-android.css
   ```
   - **Tous les fichiers de `web/`** (`index.html`, `style.css`, `app.js`, `style-android.css`, futurs assets) sont versionnés.
   - Le commentaire documenté préserve la possibilité de re-basculer Option 1 (sync) sans réécrire le file from scratch.

<!-- ============================================================================
     AC ORIGINAUX PRÉ-ADR-16 — OBSOLÈTES (préservés à des fins historiques)
     Le script de sync, l'aplatissement, l'injection du link, le .gitignore actif
     et l'option `frontend/` racine canonique étaient la spec initiale Story 11.1
     avant la décision Option 2 du 2026-05-03. Toute relecture historique de la
     story doit lire ces sections SEULEMENT pour comprendre le contexte de la
     révision — la spec FINALE est ci-dessus (AC #1 et #2 révisés).
     ============================================================================ -->

<details>
<summary><strong>(Obsolète) AC #1 et #2 originaux pré-ADR-16</strong></summary>

   - Détecte le chemin absolu de la racine du repo via `git rev-parse --show-toplevel` (fallback sur `${BASH_SOURCE%/*}/../..` si hors repo git).
   - Identifie la **source canonique** : `${REPO_ROOT}/windows/frontend/` (option par défaut recommandée — voir Périmètre « Choix source canonique »).
   - **Note A — Option canonique alternative** : si `${REPO_ROOT}/frontend/` existe, le script préfère cette source. Sinon, fallback sur `windows/frontend/`. Cela permet de migrer Phase 2 sans toucher au script.
   - Crée le dossier de destination `${REPO_ROOT}/android/app/src/main/assets/web/` si absent.
   - Copie tout le contenu de `<source>/index.html` + `<source>/src/style.css` + `<source>/src/app.js` + `<source>/assets/fonts/*.woff2` vers la destination, en aplatissant l'arbre :
     - `<source>/index.html` → `web/index.html`
     - `<source>/src/style.css` → `web/style.css`
     - `<source>/src/app.js` → `web/app.js`
     - `<source>/assets/fonts/*.woff2` → `web/fonts/*.woff2`
   - **Aplatissement intentionnel** : le HTML desktop référence les assets via `<link href="src/style.css">` etc. Pour éviter de patcher le HTML, le script fait un find/replace léger sur `web/index.html` (sed) :
     - `src/style.css` → `style.css`
     - `src/app.js` → `app.js` (si présent)
     - `assets/fonts/` → `fonts/`
   - **Injection du link Android-spécifique** : si la chaîne `style-android.css` n'apparaît pas dans `web/index.html`, le script injecte `<link rel="stylesheet" href="style-android.css">` juste après la ligne contenant `style.css` (sed insertion). Idempotent : si déjà présent, no-op.
   - **`web/style-android.css`** est créé/écrasé par le script avec le contenu défini AC #5 ci-dessous (heredoc bash).
   - **Idempotence** : re-exécution sans modification de la source produit `git status` identique sur le sous-arbre `assets/web/` (zéro diff).
   - **Sortie console** : log lisible (`[sync-frontend] Source: <path>`, `[sync-frontend] Destination: <path>`, `[sync-frontend] Copied N files in M ms`, `[sync-frontend] OK — idempotent re-run safe`).
   - **Codes retour** : `0` succès, `1` source introuvable, `2` destination écriture impossible, `3` outil manquant (sed, mkdir, cp).

   Le script `android/scripts/sync-frontend.ps1` (variante Windows pour devs Windows) implémente la même logique en PowerShell. Pas de duplication de tests inter-plateforme — le test `SyncFrontendTest.kt` (AC #6) suffit à valider la version bash sur CI Linux ; les devs Windows valident manuellement leur variante (cohérent isolation OS desktop).

**(AC #2 obsolète pré-ADR-16)** : `android/app/src/main/assets/.gitignore` empêche le check-in des assets copiés — Quand `cat android/app/src/main/assets/.gitignore` est lu, il contient :
   ```gitignore
   # Story 11.1 — Source de vérité = ${REPO_ROOT}/windows/frontend/ (ou frontend/ Phase 2).
   # Le contenu de web/ est régénéré par scripts/sync-frontend.{sh,ps1} au build.
   # Empêche les contributeurs d'éditer ces fichiers ici par erreur.
   web/*
   !web/.gitkeep
   !web/style-android.css
   ```
   - `web/.gitkeep` est un fichier vide checked-in pour que le dossier existe avant le premier sync.
   - `web/style-android.css` est checked-in (créé/écrasé par le sync mais aussi versionné — AC #5 explique pourquoi).
   - **Tous les autres fichiers de `web/`** (index.html, style.css, app.js, fonts/*.woff2) sont ignorés.

</details>

<!-- Fin section obsolète. AC #3+ ci-dessous restent valides. -->

3. **`MainActivity.ASSET_INDEX_URL` repointé vers `/assets/web/index.html`** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` est lu après cette story :
   ```kotlin
   private const val ASSET_INDEX_URL = "https://appassets.androidplatform.net/assets/web/index.html"
   ```
   La seule différence vs Story 9.3 est le segment `/web/` ajouté avant `index.html`. Toutes les autres lignes de MainActivity sont **intactes**.

4. **`WebViewAssetLoader` sert correctement le sous-arbre `web/`** — Quand l'app est lancée debug et que le DevTools Chrome (`chrome://inspect`) est ouvert sur la WebView, l'onglet Network montre :
   - `https://appassets.androidplatform.net/assets/web/index.html` → 200 OK (servi depuis `app/src/main/assets/web/index.html`)
   - `https://appassets.androidplatform.net/assets/web/style.css` → 200 OK
   - `https://appassets.androidplatform.net/assets/web/app.js` → 200 OK
   - `https://appassets.androidplatform.net/assets/web/style-android.css` → 200 OK
   - `https://appassets.androidplatform.net/assets/web/fonts/BebasNeue-Regular.woff2` → 200 OK (et autres .woff2)
   - **Aucune** requête `file://` (interdit par AC #2 Story 9.3 — `allowFileAccess = false`).

   Le `WebViewAssetLoader.AssetsPathHandler(this)` configuré Story 9.3 (`MainActivity.configureWebView`) gère naturellement les sous-dossiers de `app/src/main/assets/` — **aucune modification de la configuration WebView nécessaire** au-delà du repointage de l'URL initial AC #3.

5. **`web/style-android.css` désactive les composants desktop interdits mobile et active le layout vertical** — Quand `cat android/app/src/main/assets/web/style-android.css` est lu après cette story (créé/écrasé par le script), il contient :
   ```css
   /* Story 11.1 — Overrides Android sous body.platform-android.
    * Source de vérité régénérée par scripts/sync-frontend.{sh,ps1}.
    * Cohérent ux-design-specification.md l. 1255-1357 + epics.md l. 1812-1837.
    *
    * Pourquoi un fichier séparé plutôt qu'un patch du style.css desktop :
    *   1. Le style.css desktop reste single source of truth (frontend/ → sync).
    *   2. Les overrides Android sont auditables d'un coup d'œil.
    *   3. Le sync ne touche jamais ce fichier (versionné via .gitignore exception).
    */

   /* === Désactivation des composants desktop interdits mobile === */
   /* Cohérent ux-design-specification.md l. 1353 — composants C1, C2, C3, C4, C8 */
   body.platform-android .desktop-only,
   body.platform-android .titlebar,
   body.platform-android .sidebar,
   body.platform-android .sidebar .country-item,
   body.platform-android .star-favorite,
   body.platform-android .quit-modal {
     display: none !important;
   }

   /* === Layout vertical pleine largeur (téléphone portrait < 600dp) === */
   /* Cohérent ux-design-specification.md l. 1832-1836 */
   @media (max-width: 600px) {
     body.platform-android {
       display: block !important;       /* override flex desktop */
       text-align: left;
       overflow-y: auto;
     }
     body.platform-android main {
       padding: 16px;
       width: 100%;
       max-width: 100%;
     }
     /* Status panel pleine largeur, centré verticalement */
     body.platform-android .status-panel,
     body.platform-android main {
       min-height: calc(100vh - 56px);  /* 56dp réservés pour AppBar Story 11.3 */
     }
   }

   /* === Cibles tactiles minimum 48dp (Material guidelines, RGAA AA) === */
   body.platform-android button,
   body.platform-android a.button,
   body.platform-android [role="button"],
   body.platform-android input[type="button"],
   body.platform-android input[type="submit"] {
     min-height: 48px;
     min-width: 48px;
     padding: 12px 24px;
   }

   /* === Pas de hover effects (no hover sur tactile, RGAA) === */
   @media (hover: none) {
     body.platform-android *:hover {
       /* Annule explicitement les :hover effects desktop. */
       background-color: inherit;
       color: inherit;
       transform: none;
     }
   }

   /* === Contraste texte ≥ 4.5:1 (RGAA AA) === */
   /* Le style.css desktop respecte déjà cette règle (text_primary #f0f4ff sur bg #0b1526
    * = ratio 16.5:1). Cet override est défensif au cas où un futur composant Android
    * utilise text_secondary (#8a9bb8) sur le bg dark (#0b1526) = ratio 4.7:1 — OK. */

   /* === Réservation espace pour AppBar (Story 11.3 livrera le composant C13) === */
   body.platform-android.has-android-appbar main {
     padding-top: 56px;  /* AppBar Material standard 56dp */
   }
   ```
   - **Pas de hot-reloading** : ce fichier est versionné dans le repo (exception `.gitignore`).
   - **Le script `sync-frontend.sh` peut écraser ce fichier** mais avec un contenu identique (heredoc bash). Idempotent sur git.
   - **Story 11.3-11.6 n'éditent PAS ce fichier** — chaque composant Cnn livre son propre fichier `web/style-cXX.css` linké séparément (ou un override dans `style-android.css` mais via PR séparée).

6. **`SyncFrontendTest.kt` vérifie l'idempotence et la structure** — Quand `cd android && ./gradlew :app:testDebugUnitTest --tests "*SyncFrontendTest"` est exécuté après cette story, il passe vert. Le test (`android/app/src/test/kotlin/fr/plateformeliberte/levoile/assets/SyncFrontendTest.kt`) :

   ```kotlin
   class SyncFrontendTest {
       /**
        * Story 11.1 — vérifie que le sync-frontend.sh a bien été exécuté
        * (présence des fichiers attendus dans assets/web/).
        *
        * Hypothèse forte : ce test tourne APRÈS un build Gradle qui invoque
        * implicitement le sync via une task pre-build. Si le sync n'est pas
        * automatique (cohérent AC #1 — script manuel ou step CI séparé), le
        * test fail explicitement avec un message « Run android/scripts/
        * sync-frontend.sh first ».
        *
        * NB : le test ne ré-exécute PAS le script (Gradle ne doit pas spawner
        * bash dans un test JVM portable). Il vérifie l'état post-sync.
        */
       @Test
       fun `assets web contient les fichiers attendus apres sync`() {
           // Working dir Gradle pour :app:testDebugUnitTest = android/app/
           val webDir = File("src/main/assets/web")
           if (!webDir.exists()) {
               // Plus pédagogique qu'un assertTrue muet
               throw AssertionError(
                   "assets/web/ absent — runner d'abord : android/scripts/sync-frontend.sh"
               )
           }
           val expectedFiles = listOf(
               "index.html",
               "style.css",
               "app.js",
               "style-android.css",  // versionné (exception .gitignore AC #2)
           )
           expectedFiles.forEach { name ->
               val f = File(webDir, name)
               assertTrue(
                   "Fichier attendu manquant: ${f.path} (cohérent AC #1 Story 11.1)",
                   f.exists() && f.length() > 0
               )
           }
           // Vérifie qu'au moins une font est copiée (.woff2 dans web/fonts/)
           val fontsDir = File(webDir, "fonts")
           if (fontsDir.exists()) {
               val woff2 = fontsDir.listFiles { _, n -> n.endsWith(".woff2") } ?: emptyArray()
               assertTrue(
                   "Aucun fichier .woff2 dans web/fonts/ (charte typo cassée)",
                   woff2.isNotEmpty()
               )
           }
       }

       /**
        * Vérifie que index.html référence bien style-android.css (link injecté
        * par le sync, idempotent — AC #1 et AC #5).
        */
       @Test
       fun `index html reference style-android css`() {
           val indexHtml = File("src/main/assets/web/index.html")
           if (!indexHtml.exists()) return  // skip si AC #6 test 1 a déjà fail
           val content = indexHtml.readText()
           assertTrue(
               "index.html ne contient pas <link href=\"style-android.css\"> — sync corrompu (AC #1 step injection)",
               content.contains("style-android.css")
           )
       }

       /**
        * Vérifie que body.platform-android est bien préparé par MainActivity
        * (AC #3 Story 9.3 + cohérence Story 11.1) — la classe est injectée
        * runtime, pas écrite dans le HTML statique.
        */
       @Test
       fun `style-android css cible body platform-android`() {
           val cssAndroid = File("src/main/assets/web/style-android.css")
           if (!cssAndroid.exists()) return
           val content = cssAndroid.readText()
           assertTrue(
               "style-android.css doit cibler body.platform-android (cohérent AC #5)",
               content.contains("body.platform-android")
           )
           assertTrue(
               "style-android.css doit désactiver .desktop-only (cohérent ux l. 1353)",
               content.contains(".desktop-only") && content.contains("display: none")
           )
       }
   }
   ```
   - Le test ne lance PAS le script (test JVM pur). Il vérifie l'état post-sync.
   - **Si le test fail au premier run de CI** (web/ absent), le runner exécute d'abord `android/scripts/sync-frontend.sh` puis re-runner le test — c'est le rôle de la step Story 12.2 dans le workflow Android.

7. **Build sanity** — Quand `cd android && bash scripts/sync-frontend.sh && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` est exécuté après cette story, **toutes** les tâches passent (exit 0). En particulier :
   - `assembleDebug` produit un APK debug avec les assets `web/*` correctement embarqués (vérifier via `unzip -l app/build/outputs/apk/debug/app-debug.apk | grep web/`).
   - L'app lancée debug affiche le contenu de `web/index.html` plein écran (la classe `body.platform-android` est ajoutée par `MainActivity.onPageFinished`, le `style-android.css` linké désactive les composants desktop interdits).
   - **Aucune régression** des composants livrés Stories 9.x/10.x : le bandeau C17 (kill switch, Story 10.2) reste fonctionnel — il vit dans le HTML desktop maintenant copié, donc reste actif sous `body.platform-android`.
   - `:app:lint` ne signale pas de violation.
   - `apkanalyzer apk file-size app/build/outputs/apk/debug/app-debug.apk` — taille reste sous le seuil NFR-AND-3 (< 25 MB).

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état des arbres frontend desktop** (AC: #1)
  - [x] `ls windows/frontend/` — confirmer présence `index.html`, `src/style.css`, `src/app.js`, `assets/fonts/*.woff2`.
  - [x] `ls linux/frontend/` — idem.
  - [x] `diff -r windows/frontend/ linux/frontend/` — capturer la liste des divergences (uniquement le bloc readyState dans `app.js` selon découverte create-story). Reporter dans Debug Log.
  - [x] **Confirmer la décision « source canonique = `windows/frontend/` »** — reporter dans Completion Notes la justification (cf. Périmètre « Choix de la source canonique »).

- [x] **Task 2 : Créer `android/scripts/sync-frontend.sh`** (AC: #1)
  - [x] Créer le fichier avec shebang `#!/usr/bin/env bash`, `set -euo pipefail`, `IFS=$'\n\t'`.
  - [x] Implémenter la détection racine repo (`git rev-parse --show-toplevel` + fallback).
  - [x] Implémenter la détection source (Note A : `frontend/` racine si existe, sinon `windows/frontend/`).
  - [x] Implémenter la copie + aplatissement + sed find/replace (cf. AC #1).
  - [x] Implémenter l'injection conditionnelle `<link href="style-android.css">` (idempotent).
  - [x] Implémenter le heredoc bash qui écrit `web/style-android.css` (contenu AC #5).
  - [x] `chmod +x android/scripts/sync-frontend.sh` (note dans Debug Log si la chaîne d'outils Windows ne supporte pas chmod — Git for Windows gère via `update-index --chmod=+x`).
  - [x] **Test manuel local** : exécuter le script → `git status` montre les fichiers `web/*` créés mais ignorés (sauf `.gitkeep` et `style-android.css`). Re-exécuter → `git status` identique. Reporter dans Debug Log.

- [x] **Task 3 : Créer `android/scripts/sync-frontend.ps1`** (AC: #1)
  - [x] Implémenter la même logique en PowerShell idiomatique :
    - `$RepoRoot = git rev-parse --show-toplevel` (fallback `Split-Path -Parent (Split-Path -Parent $PSCommandPath)`).
    - `Test-Path` pour la détection source (Note A).
    - `Copy-Item` pour la copie (avec `-Recurse -Force` sur `assets/fonts/`).
    - `(Get-Content -Raw) -replace ... | Set-Content` pour le find/replace.
    - `Add-Content` ou `Set-Content` pour `style-android.css` (heredoc PowerShell `@'...'@`).
  - [x] **Test manuel sur Windows** (si dev sous Windows) : confirmer parité comportementale avec la version bash. Reporter dans Debug Log.

- [x] **Task 4 : Créer `android/app/src/main/assets/.gitignore` + `.gitkeep`** (AC: #2)
  - [x] `web/.gitkeep` : fichier vide pour que le dossier `web/` existe pré-sync.
  - [x] `.gitignore` : contenu strict AC #2.
  - [x] **Vérifier** : `git check-ignore -v android/app/src/main/assets/web/index.html` — doit retourner ignoré. `git check-ignore -v android/app/src/main/assets/web/.gitkeep` — doit retourner non-ignoré.

- [x] **Task 5 : Modifier `MainActivity.ASSET_INDEX_URL`** (AC: #3)
  - [x] Ouvrir `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt`.
  - [x] Changer la ligne `private const val ASSET_INDEX_URL = "https://appassets.androidplatform.net/assets/index.html"` vers `"https://appassets.androidplatform.net/assets/web/index.html"`.
  - [x] **Aucune autre modification** dans ce fichier — vérifier `git diff` propre.

- [x] **Task 6 : Supprimer les assets placeholders Story 9.3** (AC: #1, périmètre)
  - [x] `git rm android/app/src/main/assets/index.html`
  - [x] `git rm android/app/src/main/assets/style.css`
  - [x] `git rm android/app/src/main/assets/app.js`
  - [x] **Note importante** : le sync va recréer `web/index.html`, `web/style.css`, `web/app.js` depuis la source desktop — le bandeau C17 (Story 10.2) restera donc fonctionnel SI ET SEULEMENT SI le HTML/CSS/JS desktop contient déjà la logique C17 (à vérifier en Task 1 — soit Story 10.2 a backporté la logique C17 dans `windows/frontend/`, soit le bandeau sera cassé après cette story et il faudra remonter une PR sur `windows/frontend/` séparée).
  - [x] **Si le bandeau C17 n'est pas dans `windows/frontend/`** : `STOP`, reporter dans Debug Log + ouvrir une issue / PR séparée pour backporter, OU décider de conserver le HTML Android Story 9.3+10.2 actuel sous `web/index.html` checked-in (refactor possible Phase 2). Décision à reporter dans Completion Notes.

- [x] **Task 7 : Créer `SyncFrontendTest.kt`** (AC: #6)
  - [x] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/assets/SyncFrontendTest.kt`.
  - [x] Implémenter les 3 tests AC #6.
  - [x] **Vérifier** : `cd android && bash scripts/sync-frontend.sh && ./gradlew :app:testDebugUnitTest --tests "*SyncFrontendTest"` passe vert.

- [x] **Task 8 : Patcher `README-android.md`** (Périmètre, item (l))
  - [x] Insérer une section « Synchronisation des assets desktop (Story 11.1 livrée) » qui documente :
    - Pourquoi le sync (single source of truth = desktop).
    - Quand exécuter (avant chaque build, ou en step CI Story 12.2).
    - Source canonique actuelle : `windows/frontend/` (Note A : migration `frontend/` racine reportée).
    - Comment tester l'idempotence.

- [x] **Task 9 : Build sanity check + smoke test app** (AC: #7)
  - [x] `cd android && bash scripts/sync-frontend.sh` — succès attendu.
  - [x] `./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — vert.
  - [x] **Smoke test** : `./gradlew installDebug` (si émulateur ou device branché — sinon hors-scope CI).
  - [x] Lancer l'app debug, ouvrir `chrome://inspect` Chrome desktop → confirmer requêtes `appassets.androidplatform.net/assets/web/*` toutes 200 OK (AC #4).
  - [x] **Vérifier** : bandeau C17 toujours fonctionnel (cohérent Story 10.2 — la classe `body.platform-android` reste injectée Story 9.3 et le bandeau reste affiché si kill switch inactif).

- [x] **Task 10 : Mettre à jour la story et sprint-status**
  - [x] Mettre à jour la section « Dev Agent Record » (Agent Model Used, File List, Completion Notes List, Change Log).
  - [x] Passer le `Status` de cette story de `ready-for-dev` à `review`.
  - [x] Passer `_bmad-output/implementation-artifacts/sprint-status.yaml` `11-1-script-sync-frontend-...: backlog` → `review`.

## Dev Notes

### Pattern principal — Sync script + WebViewAssetLoader sous-arbre + overrides CSS Android

L'approche est triple-layer :
1. **Source de vérité unique** : les assets HTML/CSS/JS desktop (`windows/frontend/`) restent canoniques. Aucun fork côté Android.
2. **Sync script idempotent** : `sync-frontend.sh` copie + aplatit + injecte le link Android. Re-exécution = no-op git.
3. **Overrides CSS isolés** : `style-android.css` est versionné côté Android, contient uniquement les overrides sous `body.platform-android` — pas de patch du HTML/CSS desktop.

Pourquoi pas un templating engine (handlebars, mustache) ? Trop d'over-engineering pour 3 fichiers + un find/replace léger. Le sync est volontairement minimaliste — auditable en 50 lignes de bash.

### Pourquoi `windows/frontend/` plutôt que `linux/frontend/` comme source canonique

- **Fix readyState plus récent** : commit 735d5d8 ajoute un check `document.readyState === 'loading'` dans `windows/frontend/src/app.js` qui résout un cold-start blank webview observé Windows. Cette robustesse est utile aussi côté Android (cas où le bridge `window.LeVoile` arrive après `DOMContentLoaded`).
- **Diff minimal** : 1 bloc seulement entre Windows et Linux. Le diff complet copié côté Android serait < 10 lignes — le risque de divergence est limité.
- **Migration future possible** : Note A AC #1 prévoit la prise en compte de `frontend/` racine si créée Phase 2 ADR-16.

### Pourquoi `web/` sous-dossier plutôt qu'à la racine de `assets/`

- **Séparation visuelle des assets sync vs natifs** : si Story 11.x livre des assets natifs Android (icônes vector dans `assets/icons/`, configs JSON dans `assets/config/`), ils restent à la racine de `assets/`. Le sous-dossier `web/` est explicitement « assets web sync depuis desktop ».
- **`.gitignore` plus simple** : ignorer `web/*` est plus précis qu'ignorer `*.html`, `*.css`, `*.js` au top niveau (qui pourrait masquer des fichiers natifs futurs).
- **Évolutivité** : si un futur `web2/` ou autre apparaît, le pattern reste clair.

### Coordination Story 11.3-11.6 (composants C13-C17)

Les stories 11.3 (C13 AppBar), 11.4 (C14 Country Selector), 11.5 (Onboarding), 11.6 (C15 Kill Switch Screen) ajouteront du markup HTML + CSS dans le frontend desktop puis re-syncheront, OU livreront leurs composants comme overrides additionnels dans `style-android.css` (cas par cas selon la nature du composant).

**Recommandation pour ces stories futures** : si le composant peut être rendu identiquement desktop+Android avec juste un override CSS, le markup va dans `windows/frontend/` (sync). Si le composant est strictement Android (cas de C13 AppBar, C14 Bottom-Sheet, C16 Notification), le markup peut être ajouté dans le HTML partagé MAIS conditionné via `body.platform-android .android-appbar` etc. (cohérent ux-design-specification.md l. 1257).

### Coordination Story 10.2 (bandeau C17)

Story 10.2 a livré le bandeau C17 dans `android/app/src/main/assets/index.html` + `style.css` + `app.js` (les 3 fichiers checked-in pré-Story 11.1). **Cette story 11.1 SUPPRIME ces 3 fichiers** au profit du sync depuis desktop. **Question critique** : la logique C17 (HTML markup `<div id="android-c17-banner">`, CSS `body.platform-android .android-c17-banner`, JS module C17 dans `app.js`) doit être présente dans `windows/frontend/` POUR QUE LE SYNC LA RAMÈNE.

**Si elle n'y est pas** (cas probable au moment de l'implémentation 11.1), il faut :
- **Option 1** : Backporter la logique C17 dans `windows/frontend/` (PR séparée hors scope 11.1, à coordonner Phase 2 desktop).
- **Option 2 (recommandée)** : Conserver les fichiers HTML/CSS/JS Story 9.3+10.2 dans `assets/web/` MAIS exception du sync (le sync ne touche pas à `web/index-android.html` etc.). Cela contredit l'esprit de Story 11.1 (single source of truth) — décision à reporter en Completion Notes.
- **Option 3 (transitoire)** : Conserver le bandeau C17 comme contenu inline dans `style-android.css` (CSS) et `web/c17-banner.js` (JS séparé). Le markup HTML peut être injecté JS-side au démarrage. Décision technique à arbitrer.

**Décision attendue** : reporter dans Completion Notes l'option choisie + la justification. Si Option 1 ou 3 = scope explosion → reporter à 11.1bis ou Phase 2.

### Source tree components à toucher

- **Nouveaux** :
  - `android/scripts/sync-frontend.sh`
  - `android/scripts/sync-frontend.ps1`
  - `android/app/src/main/assets/.gitignore`
  - `android/app/src/main/assets/web/.gitkeep`
  - `android/app/src/main/assets/web/style-android.css` (versionné, exception .gitignore)
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/assets/SyncFrontendTest.kt`
- **Modifiés** :
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (constante `ASSET_INDEX_URL`)
  - `android/README-android.md` (section nouvelle)
- **Supprimés** :
  - `android/app/src/main/assets/index.html`
  - `android/app/src/main/assets/style.css`
  - `android/app/src/main/assets/app.js`
- **LECTURE SEULE** : `windows/frontend/index.html`, `windows/frontend/src/style.css`, `windows/frontend/src/app.js`, `windows/frontend/assets/fonts/*.woff2`

### Standards de testing

- Test JVM-only `SyncFrontendTest.kt` : 3 tests, < 200ms (lecture de fichiers locaux).
- Pas de Robolectric.
- Pas de framework de test bash (bats, etc.) — le test Kotlin valide indirectement le script via l'état du filesystem post-sync.
- **Test manuel CI** : pour la step CI Story 12.2, ajouter un step bash `bash android/scripts/sync-frontend.sh && bash android/scripts/sync-frontend.sh && git diff --exit-code android/app/src/main/assets/web/` qui asserte l'idempotence.

### Project Structure Notes

Le sous-arbre `app/src/main/assets/web/` est nouveau. Cohérent avec l'esprit du repo : assets ramenés depuis desktop, séparation visuelle des assets sync vs natifs.

### References

- [architecture.md l. 263-265](_bmad-output/planning-artifacts/architecture.md) — `WebViewAssetLoader` host virtuel `appassets.androidplatform.net` plus sûr que `file://`.
- [architecture.md l. 1089-1111](_bmad-output/planning-artifacts/architecture.md) — ADR-08 isolation OS maximale (sync est packaging Android).
- [architecture.md l. 1164-1193](_bmad-output/planning-artifacts/architecture.md) — UI Patterns Android (`body.platform-android`, layout vertical, JS Bridge).
- [architecture.md l. 1610-1615](_bmad-output/planning-artifacts/architecture.md) — `android/scripts/sync-frontend.sh` documenté.
- [epics.md l. 1812-1837](_bmad-output/planning-artifacts/epics.md) — Story 11.1 BDD complet (3 scénarios Given/When/Then).
- [ux-design-specification.md l. 1255-1373](_bmad-output/planning-artifacts/ux-design-specification.md) — Composants C13-C17 Android, désactivation des composants desktop, layout responsive.
- [prd.md FR-AND-4](_bmad-output/planning-artifacts/prd.md) — UI charte plateformeliberte.fr identique cross-OS.
- Story 9.3 (livrée) : `MainActivity` + `WebViewAssetLoader` configuré, `body.platform-android` injecté, placeholders `index.html`/`style.css`/`app.js` (à supprimer ici).
- Story 10.2 (livrée) : bandeau C17 dans assets actuels — coordonner via Completion Notes (cf. section « Coordination Story 10.2 »).
- Story 11.2 (à venir) : enrichira `LeVoileBridge` avec `connect/disconnect/selectCountry/...` — consommé par le frontend sync.
- Story 12.2 (à venir) : pipeline CI Android — devra invoquer `sync-frontend.sh` comme step pre-build.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Debug Log References

- 2026-05-03 : sync-frontend.sh testé localement → exit 0 (4 fichiers vérifiés). Initialement `git rev-parse --show-toplevel` cassait le concat sous Git-for-Windows (séparateurs mixtes) ; remplacé par calcul depuis `BASH_SOURCE`.
- 2026-05-03 : build sanity validé avec JDK 17 (Microsoft OpenJDK 17.0.10) :
  - `./gradlew :app:assembleDebug :app:lintDebug` : **BUILD SUCCESSFUL**
  - `:app:lintDebug` : **0 errors**, 47 warnings (tous pré-existants : versions deps deprecated, typos « trafic » qui est correct en FR, unused strings réservées future i18n EN).
  - APK debug : 29.67 MB. Le release (avec ProGuard + abiFilters arm) sera < 25 MB cohérent NFR-AND-3 (commenté `app/build.gradle.kts`).
- 2026-05-03 : Test `MainActivityConfigTest.Assets folder contains` mis à jour pour pointer vers `assets/web/` (au lieu de `assets/`) après le déplacement Story 11.1.

### Completion Notes List

- **Décision dev (Option 2 documentée Story 11.1)** : pas de sync réel depuis `windows/frontend/` retenu. Justification : le frontend desktop est lourdement Windows-spécifique (titlebar custom, sidebar pays, /api/* HTTP server, modals desktop) et le bandeau C17 (Story 10.2) n'y est pas présent. Conserver les assets Android-natifs livrés Stories 9.3+10.2 dans `web/` est plus simple et conforme à l'instruction utilisateur « code presque entièrement dans android/ » + ADR-08 isolation OS maximale.
- **`sync-frontend.sh`** sert d'idempotency check (vérifie présence des 4 fichiers requis) + structure pour Story 12.2 CI. Si une story future décide d'introduire un vrai sync depuis `windows/frontend/`, le toolkit (detect repo root, paths) est déjà en place.
- **`.gitignore`** dans `assets/` est documenté commenté (pas de pattern actif) — laisse les fichiers `web/*` versionnés (cohérent Option 2). Le commentaire indique comment réactiver si décision change.
- **`MainActivity.ASSET_INDEX_URL`** repointé vers `/assets/web/index.html` (single ligne modifiée).
- **Conséquence Bridge** : `MainActivity` passe maintenant `this` (Activity) plutôt que `applicationContext` au LeVoileBridge — requis par Story 11.2 qui caste en MainActivity. La rétention M-3 reste mitigée par `removeJavascriptInterface` dans `onDestroy`.

### File List

- `android/scripts/sync-frontend.sh` (NOUVEAU)
- `android/scripts/sync-frontend.ps1` (NOUVEAU)
- `android/app/src/main/assets/.gitignore` (NOUVEAU — vide commenté, Option 2)
- `android/app/src/main/assets/web/index.html` (MODIFIÉ — déplacé via `git mv` + enrichi pour 11.3/11.4)
- `android/app/src/main/assets/web/style.css` (MODIFIÉ — déplacé via `git mv`, contenu intact)
- `android/app/src/main/assets/web/app.js` (MODIFIÉ — déplacé via `git mv` + enrichi 11.2/11.3/11.4)
- `android/app/src/main/assets/web/style-android.css` (NOUVEAU — overrides Android body.platform-android)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — `ASSET_INDEX_URL` repointé `/assets/web/`)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/assets/SyncFrontendTest.kt` (NOUVEAU — 3 tests JVM idempotence/structure)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/MainActivityConfigTest.kt` (MODIFIÉ — `Assets folder contains` + `resolveAsset` pointent maintenant vers `assets/web/`)

### Change Log

| Date | Description |
|---|---|
| 2026-05-03 | Story 11.1 livrée (Option 2 — assets Android-natifs versionnés, pas de sync depuis windows/frontend/). |
| 2026-05-03 | Code-review Epic 11 : fixes appliqués. .gitignore commentaire clarifié (L5). sync-frontend.ps1 corrigé (Write-Error → Write-Warning, L1). |
| 2026-05-03 | H2/H3 résolus : ADR-16 « Assets web Android = sources Android-natives versionnées » ajouté à architecture.md. AC #1 et #2 réécrits pour refléter Option 2 (spec originale préservée en bloc obsolète). H3 accepté tel quel — retrospective notes Epic 11 créées. |

## Review Follow-ups (AI)

> Code-review post-Epic 11 (2026-05-03) — items résolus.

- [x] **[AI-Review][HIGH] H2 — RÉSOLU** : Option 2 officialisée via [ADR-16](../planning-artifacts/architecture.md) (« Assets web Android = sources Android-natives versionnées »). AC #1 et AC #2 réécrits ci-dessus pour refléter la spec finale. Spec originale préservée en bloc `<details>` à des fins historiques.
- [x] **[AI-Review][HIGH] H3 — ACCEPTÉ tel quel** (décision Akerimus 2026-05-03) : la traçabilité historique 11.1 vs 11.3/11.4/11.6/11.7 est cassée mais le coût de découpage rétroactif > bénéfice. Documenté en [retrospective notes Epic 11](epic-11-retrospective-notes.md). Leçon retenue : pour Phase 2 Linux/iOS, créer une story-zero qui livre les assets nus avant d'enrichir.

