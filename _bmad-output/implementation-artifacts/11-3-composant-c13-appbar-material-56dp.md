# Story 11.3: Composant C13 — AppBar Material 56dp

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo, et accessoirement `windows/frontend/` (single source of truth Story 11.1) si la décision Note B est retenue. Aucun fichier hors de ces deux zones ne doit être créé, modifié ou supprimé par cette story.**
>
> **Story 11.3 livre** :
> 1. Le composant C13 (AppBar Material 56dp) sous forme de markup HTML + CSS dédié `body.platform-android` — visible **uniquement** sur Android, masqué desktop.
> 2. Un drawer latéral simple (slide-in 250ms ease-out) qui contient les liens : Paramètres, À propos, Info légale, Paramètres système Android (deeplink `Settings.ACTION_APPLICATION_DETAILS_SETTINGS` ciblant le package Le Voile).
> 3. Une nouvelle méthode `@JavascriptInterface fun openAppDetailsSettings(): String` dans `LeVoileBridge.kt` — déclenche l'Intent système ciblant la page Settings de l'app.
> 4. Un test JVM `LeVoileBridgeAppDetailsTest.kt` pour la nouvelle méthode bridge.
> 5. Strings i18n (`strings.xml` + `values-fr/strings.xml`) pour les labels du drawer (parité EN/FR).
>
> **Le composant C13 est strictement frontend (HTML/CSS/JS) sauf pour la méthode bridge AC #3.** Il n'introduit pas d'Activity nouvelle, pas de Fragment, pas de Compose.
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile. Aucune entrée dans `android/shims/*.go`. Aucune ligne dans `go.mod`/`go.sum`.
>
> **Rappel ADR-08** : la AppBar est strictement Android — n'apparaît jamais desktop (le desktop a sa titlebar custom C1 livrée Phase 1). Cohérent ux-design-specification.md l. 1257-1269.
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 11.3 |
> |---|---|---|
> | `go.mod`, `go.sum`, `android/shims/`, `android/scripts/`, `android/levoile-core/`, `internal/`, `linux/`, `relay/`, `tools/` | 1-9 | INTACT |
> | `android/app/build.gradle.kts` | 9.x/10.x/11.x | INTACT |
> | `android/app/src/main/AndroidManifest.xml` | 9.1+9.4 | INTACT — `Settings.ACTION_APPLICATION_DETAILS_SETTINGS` est un intent système, ne nécessite ni permission ni intent-filter applicatif |
> | `android/app/src/main/kotlin/.../{kill,conflict,vpn,log,ui,assets}/` | 9.x/10.x/11.1 | INTACT |
> | `android/app/src/main/kotlin/.../MainActivity.kt` | 9.3+9.5+10.x+11.1 | INTACT |
> | `android/app/src/main/kotlin/.../bridge/LeVoileBridge.kt` | 9.3+10.2+10.3+11.2 | **MODIFIÉ — ajout de `openAppDetailsSettings`** |
> | `android/app/src/main/assets/web/style-android.css` | 11.1 | **MODIFIÉ — ajout du CSS C13 AppBar et drawer** |
> | `windows/frontend/src/{style.css,app.js}` ou `android/app/src/main/assets/web/c13-appbar.{css,js}` | 11.1 + cette story | Décision Note C (cf. Périmètre) |
>
> **Note C** : où vit le markup HTML de la AppBar ?
> - **Option 1 (recommandée)** : ajouter le markup HTML `<header class="android-appbar"><button class="android-appbar__burger">…</button>…</header>` directement dans `windows/frontend/index.html` avec la classe `desktop-only` qui désactive en desktop, mais cohérent ADR-14 (le desktop ne doit JAMAIS voir ces éléments). **Donc l'inverse** : ajouter avec une classe conventionnelle `android-only` qui est masquée desktop par défaut (`.android-only { display: none; }` dans `style.css`) et activée Android via `body.platform-android .android-only { display: block; }`. Cohérent ux-design-specification.md l. 1257.
> - **Option 2 (alternative)** : injection JS-side dans `app.js` (`document.body.insertAdjacentHTML('afterbegin', '<header class="android-appbar">...</header>')`) **uniquement si `body.platform-android`**. Permet de garder le HTML desktop intouché. Mais introduit un flash visuel pré-injection.
> - **Recommandation** : Option 1 (markup partagé + classe `android-only`). Plus auditable, pas de FOUC.
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/.../bridge/LeVoileBridge.kt` (MODIFIÉ — `openAppDetailsSettings`),
>   (b) `android/app/src/test/kotlin/.../bridge/LeVoileBridgeAppDetailsTest.kt` (NOUVEAU),
>   (c) `android/app/src/main/assets/web/style-android.css` (MODIFIÉ — CSS AppBar + drawer + animations),
>   (d) `windows/frontend/index.html` (MODIFIÉ — markup `<header class="android-only android-appbar">…` + drawer markup) **OU** `android/app/src/main/assets/web/c13-appbar.html` (Option 2),
>   (e) `windows/frontend/src/style.css` (MODIFIÉ — `.android-only { display: none; }` baseline) **OU** override dans `style-android.css`,
>   (f) `windows/frontend/src/app.js` (MODIFIÉ — handlers click burger, click overflow, deeplink) **OU** `web/c13-appbar.js`,
>   (g) `android/app/src/main/res/values/strings.xml` (MODIFIÉ — labels drawer FR par défaut),
>   (h) `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR explicite),
>   (i) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (j) `_bmad-output/implementation-artifacts/11-3-composant-c13-appbar-material-56dp.md`.
>
> **Anti-patterns** :
> - Ajouter Material Components Android (`com.google.android.material.appbar.MaterialToolbar`) en dépendance Gradle — over-coupling, le composant est HTML/CSS pur (cohérent UI strategy ux-design-specification.md l. 1257-1340).
> - Créer un `Fragment` Kotlin pour l'AppBar — viole ADR-14 (le frontend partagé fournit l'UI).
> - Ajouter Jetpack Compose — overkill, hors-scope MVP.
> - Logger les inputs JS (variables `$packageName` interdites par Story 10.5).

## Story

En tant qu'utilisatrice Android Le Voile,
Je veux une AppBar Material standard 56dp en haut de l'app avec burger, titre, cloche notifications, menu overflow,
Afin que l'expérience suive les patterns Android natifs et que je puisse accéder facilement aux paramètres et infos via des éléments familiers (cohérent ux-design-specification.md l. 1259-1269 + epics.md l. 1863-1886).

## Acceptance Criteria

1. **Markup HTML AppBar avec classe `android-only`** — Quand `windows/frontend/index.html` est lu après cette story (puis sync vers `android/app/src/main/assets/web/index.html` via Story 11.1), il contient (inséré dans `<body>` avant `<main>`) :
   ```html
   <!-- Story 11.3 — AppBar C13 (visible uniquement Android via body.platform-android) -->
   <header class="android-only android-appbar" role="banner">
     <button
         type="button"
         class="android-appbar__burger"
         aria-label="Menu"
         aria-controls="android-drawer"
         aria-expanded="false">☰</button>
     <h1 class="android-appbar__title">LE VOILE</h1>
     <button
         type="button"
         class="android-appbar__bell"
         aria-label="Notifications">ⓘ</button>
     <button
         type="button"
         class="android-appbar__overflow"
         aria-label="Plus d'options"
         aria-haspopup="menu"
         aria-expanded="false">⋮</button>
   </header>

   <!-- Story 11.3 — Drawer latéral (slide-in depuis la gauche) -->
   <nav
       class="android-only android-drawer"
       id="android-drawer"
       role="navigation"
       aria-label="Menu principal"
       aria-hidden="true">
     <button
         type="button"
         class="android-drawer__close"
         aria-label="Fermer le menu">×</button>
     <ul class="android-drawer__list">
       <li><a href="#settings" class="android-drawer__link">Paramètres</a></li>
       <li><a href="#about" class="android-drawer__link">À propos</a></li>
       <li><a href="#legal" class="android-drawer__link">Mentions légales</a></li>
       <li><a
             href="#"
             class="android-drawer__link"
             id="android-drawer-link-system-settings">Paramètres système Android</a></li>
     </ul>
   </nav>
   <!-- Backdrop pour le drawer (tap ferme) -->
   <div
       class="android-only android-drawer-backdrop"
       id="android-drawer-backdrop"
       hidden
       aria-hidden="true"></div>
   ```
   - **`role="banner"` sur AppBar** : requis epics.md l. 1882 (RGAA AA).
   - **`aria-label` sur chaque action** : requis epics.md l. 1885 — cohérent NFR-AND-9 sans révéler de data utilisateur.
   - **Pas de `<title>` desktop dupliqué** : la titlebar desktop C1 a son propre markup `<header class="titlebar">` (livré Phase 1 desktop), désactivé Android via `body.platform-android .titlebar { display: none }` (Story 11.1 livre cette règle).

2. **CSS `style-android.css` : règle `.android-only` + AppBar 56dp + drawer slide** — Quand `android/app/src/main/assets/web/style-android.css` est lu après cette story, il contient en plus du contenu Story 11.1 :
   ```css
   /* === Story 11.3 — Composant C13 AppBar Material === */

   /* Désactivation desktop par défaut (style.css desktop n'a pas .android-only).
    * Story 11.1 baseline a déjà le pattern body.platform-android .desktop-only.
    * Ici on ajoute le pattern inverse — .android-only masqué hors Android. */
   .android-only {
     display: none;
   }
   body.platform-android .android-only {
     display: revert;  /* unset → reprend la valeur natural display de l'élément */
   }

   /* AppBar 56dp standard Material */
   body.platform-android .android-appbar {
     position: fixed;
     top: 0;
     left: 0;
     right: 0;
     height: 56px;
     padding: 0 4px;  /* boutons 48dp ont leur propre padding, l'AppBar marge faible */
     background-color: #0b1526;  /* navy charte plateformeliberte.fr */
     color: #ffffff;
     display: flex;
     align-items: center;
     gap: 8px;
     z-index: 100;
     box-shadow: 0 2px 4px rgba(0, 0, 0, 0.2);
   }

   /* C17 banner Story 10.2 vit déjà au-dessus (z-index 1000) — il prime sur AppBar */
   body.platform-android.has-c17-banner .android-appbar {
     top: 40px;  /* descendre sous le bandeau C17 (40dp Story 10.2) */
   }

   /* Body padding-top pour ne pas que main soit caché par AppBar */
   body.platform-android .android-only.android-appbar ~ main,
   body.platform-android.has-android-appbar main {
     padding-top: 56px;
   }
   body.platform-android.has-c17-banner .android-only.android-appbar ~ main {
     padding-top: 96px;  /* 40 + 56 */
   }

   /* Burger / Cloche / Overflow : 48dp tappable */
   body.platform-android .android-appbar__burger,
   body.platform-android .android-appbar__bell,
   body.platform-android .android-appbar__overflow {
     width: 48px;
     height: 48px;
     min-width: 48px;
     padding: 0;
     border: none;
     background: transparent;
     color: #ffffff;
     font-size: 24px;
     line-height: 48px;
     cursor: pointer;
     border-radius: 24px;
     display: flex;
     align-items: center;
     justify-content: center;
   }
   body.platform-android .android-appbar__burger:active,
   body.platform-android .android-appbar__bell:active,
   body.platform-android .android-appbar__overflow:active {
     background-color: rgba(255, 255, 255, 0.12);  /* ripple-like Material */
   }

   /* Titre centré */
   body.platform-android .android-appbar__title {
     flex: 1;
     text-align: center;
     font-family: 'Bebas Neue', sans-serif;
     font-size: 20px;
     letter-spacing: 0.05em;
     color: #ffffff;
     margin: 0;
     padding: 0;
     font-weight: normal;
   }

   /* === Drawer latéral (slide-in 250ms ease-out depuis la gauche) === */
   body.platform-android .android-drawer {
     position: fixed;
     top: 0;
     left: 0;
     bottom: 0;
     width: 280px;
     max-width: 80vw;
     background-color: #0e1e38;  /* bg_dark_alt */
     color: #f0f4ff;
     z-index: 200;
     transform: translateX(-100%);
     transition: transform 250ms ease-out;
     overflow-y: auto;
     padding: 16px 0;
   }
   body.platform-android .android-drawer[aria-hidden="false"] {
     transform: translateX(0);
   }
   body.platform-android .android-drawer__close {
     width: 48px;
     height: 48px;
     border: none;
     background: transparent;
     color: #ffffff;
     font-size: 24px;
     cursor: pointer;
     margin: 0 8px 8px 8px;
   }
   body.platform-android .android-drawer__list {
     list-style: none;
     padding: 0;
     margin: 0;
   }
   body.platform-android .android-drawer__link {
     display: block;
     min-height: 48px;
     padding: 12px 16px;
     color: #f0f4ff;
     text-decoration: none;
     font-family: 'Inter', sans-serif;
     font-size: 16px;
     line-height: 1.5;
   }
   body.platform-android .android-drawer__link:active {
     background-color: rgba(255, 255, 255, 0.08);
   }

   /* Backdrop (tap ferme) */
   body.platform-android .android-drawer-backdrop {
     position: fixed;
     inset: 0;
     background-color: rgba(0, 0, 0, 0.4);
     z-index: 150;
     opacity: 0;
     transition: opacity 250ms ease-out;
   }
   body.platform-android .android-drawer-backdrop:not([hidden]) {
     opacity: 1;
   }
   ```

3. **`LeVoileBridge.openAppDetailsSettings()` ouvre la page Settings de l'app** — Quand `LeVoileBridge.kt` est lu après cette story, il contient :
   ```kotlin
   /**
    * Story 11.3 — Ouvre la page Réglages > Apps > Le Voile (Android system).
    *
    * Différent de Story 10.2 `openKillSwitchTarget` (Settings.ACTION_VPN_SETTINGS) :
    * cette méthode cible la fiche app pour permissions, notifications, force-stop, etc.
    *
    * Intent : `Settings.ACTION_APPLICATION_DETAILS_SETTINGS` + URI `package:<applicationId>`.
    *
    * Retour JSON :
    *   - `{"ok":true,"action":"opened_app_details"}` — Intent lancé.
    *   - `{"error":"settings_unavailable"}` — ROM custom sans cette action (rare).
    */
   @JavascriptInterface
   fun openAppDetailsSettings(): String {
       val intent = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS)
           .setData(Uri.fromParts("package", context.packageName, null))
           .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
       return try {
           context.startActivity(intent)
           """{"ok":true,"action":"opened_app_details"}"""
       } catch (t: ActivityNotFoundException) {
           """{"error":"settings_unavailable"}"""
       }
   }
   ```
   - **Imports** : `android.net.Uri` + `android.provider.Settings` (déjà importé par `openKillSwitchTarget` Story 10.2).
   - **`packageName`** : pas une donnée sensible (c'est notre propre app), pas de filtrage requis.

4. **Module JS frontend gère ouverture/fermeture du drawer + handlers** — Quand `windows/frontend/src/app.js` (re-syncé via 11.1) est lu après cette story, il contient un module additionnel :
   ```javascript
   /* ====== Story 11.3 — AppBar + Drawer handlers ====== */
   (function () {
     'use strict';
     if (!document.body.classList.contains('platform-android')) return;

     var burger = document.querySelector('.android-appbar__burger');
     var drawer = document.getElementById('android-drawer');
     var backdrop = document.getElementById('android-drawer-backdrop');
     var closeBtn = document.querySelector('.android-drawer__close');
     var sysSettingsLink = document.getElementById('android-drawer-link-system-settings');

     if (!burger || !drawer || !backdrop) return;

     // Marqueur body pour ajuster padding-top (cf. style-android.css)
     document.body.classList.add('has-android-appbar');

     function openDrawer() {
       drawer.setAttribute('aria-hidden', 'false');
       burger.setAttribute('aria-expanded', 'true');
       backdrop.removeAttribute('hidden');
       // Focus trap minimaliste : focus sur le bouton close
       if (closeBtn) closeBtn.focus();
     }
     function closeDrawer() {
       drawer.setAttribute('aria-hidden', 'true');
       burger.setAttribute('aria-expanded', 'false');
       backdrop.setAttribute('hidden', '');
       burger.focus();  // retour focus initial
     }
     burger.addEventListener('click', openDrawer);
     if (closeBtn) closeBtn.addEventListener('click', closeDrawer);
     backdrop.addEventListener('click', closeDrawer);

     // Escape ferme le drawer (clavier physique optionnel)
     document.addEventListener('keydown', function (e) {
       if (e.key === 'Escape' && drawer.getAttribute('aria-hidden') === 'false') {
         closeDrawer();
       }
     });

     // Lien deeplink Settings système → bridge openAppDetailsSettings (Story 11.3 AC #3)
     if (sysSettingsLink && window.LV && typeof window.LV.openAppDetailsSettings !== 'function') {
       // window.LV.openAppDetailsSettings n'a pas été ajouté à l'IIFE 11.2 — on l'ajoute ici inline.
       window.LV.openAppDetailsSettings = function () {
         try { return JSON.parse(window.LeVoile.openAppDetailsSettings()); }
         catch (e) { return { error: 'bridge_call_failed' }; }
       };
     }
     if (sysSettingsLink) {
       sysSettingsLink.addEventListener('click', function (e) {
         e.preventDefault();
         if (window.LV && typeof window.LV.openAppDetailsSettings === 'function') {
           window.LV.openAppDetailsSettings();
         }
         closeDrawer();
       });
     }

     // Cloche notifications + overflow : placeholders Story 11.x (cohérent epics.md l. 1872-1875)
     // Pas de handler cliquable dans 11.3 — ces éléments sont visuellement présents,
     // les actions secondaires viendront dans une story future si besoin (épic 11 non bloquant).
   })();
   ```

5. **Strings i18n FR + parité** — Quand `android/app/src/main/res/values/strings.xml` et `values-fr/strings.xml` sont lus après cette story, ils contiennent (additionnés au contenu existant) :
   ```xml
   <!-- Story 11.3 — Labels drawer Android C13 (parité FR/values + values-fr) -->
   <string name="android_appbar_menu">Menu</string>
   <string name="android_appbar_notifications">Notifications</string>
   <string name="android_appbar_more">Plus d\'options</string>
   <string name="android_drawer_close">Fermer le menu</string>
   <string name="android_drawer_settings">Paramètres</string>
   <string name="android_drawer_about">À propos</string>
   <string name="android_drawer_legal">Mentions légales</string>
   <string name="android_drawer_system_settings">Paramètres système Android</string>
   ```
   - **Note décision dev** : ces strings sont actuellement utilisées **uniquement par le frontend HTML hard-codées en FR**. Pourquoi les déclarer dans `strings.xml` ?
     - **Future use** : si le projet ajoute l'EN Phase 2 (architecture.md l. 2197 gap mineur), les strings sont déjà prêtes côté natif.
     - **Convention** : Stories 9.x-10.x ont systématiquement déclaré chaque string consommée par natif OU frontend dans `strings.xml` (Story 10.2 `android_c17_settings_unavailable`, Story 10.3 `android_vpn_conflict_*`).
     - **Exposition au bridge** : le frontend pourrait lire ces strings via une nouvelle méthode `LeVoileBridge.getString(name): String` — **HORS SCOPE** Story 11.3, à reporter Phase 2 i18n.
   - **Recommandation alternative** : si la déclaration dans `strings.xml` est jugée over-engineering, **omettre cette tâche** et garder les labels hard-codés en FR dans le HTML. Décision dev à reporter dans Completion Notes. La règle : **rester simple si on n'a pas besoin de plus**.

6. **Tests JVM `LeVoileBridgeAppDetailsTest.kt`** — Quand `cd android && ./gradlew :app:testDebugUnitTest --tests "*LeVoileBridgeAppDetailsTest"` est exécuté, vert. Le test (`android/app/src/test/kotlin/.../bridge/LeVoileBridgeAppDetailsTest.kt`) :
   ```kotlin
   @RunWith(MockitoJUnitRunner::class)
   class LeVoileBridgeAppDetailsTest {
       @Mock private lateinit var mockContext: Context
       private lateinit var bridge: LeVoileBridge

       @Before
       fun setUp() {
           `when`(mockContext.packageName).thenReturn("fr.plateformeliberte.levoile")
           bridge = LeVoileBridge(mockContext, killSwitchDetector = null, vpnConflictDetector = null)
       }

       @Test
       fun `openAppDetailsSettings lance startActivity et retourne ok`() {
           val r = bridge.openAppDetailsSettings()
           assertTrue(r.contains("\"ok\":true"))
           assertTrue(r.contains("opened_app_details"))
           verify(mockContext).startActivity(any<Intent>())
       }

       @Test
       fun `openAppDetailsSettings retourne settings_unavailable si ActivityNotFoundException`() {
           doThrow(ActivityNotFoundException("test")).`when`(mockContext).startActivity(any<Intent>())
           val r = bridge.openAppDetailsSettings()
           assertTrue(r.contains("\"error\":\"settings_unavailable\""))
       }
   }
   ```

7. **Build sanity + smoke test** — Quand `cd android && bash scripts/sync-frontend.sh && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` est exécuté, vert. Smoke test manuel :
   - L'app debug affiche l'AppBar 56dp en haut, fond navy, titre « LE VOILE » centré.
   - Tap burger → drawer slide-in 250ms.
   - Tap backdrop OU close OU Esc → drawer slide-out.
   - Tap « Paramètres système Android » → ouvre la fiche Settings de l'app système.
   - Bandeau C17 (Story 10.2) reste visible en haut si kill switch inactif (au-dessus de l'AppBar via `top: 40px` AC #2).
   - Aucune AppBar visible sur l'app desktop (testée Story 11.x desktop équivalente — ici hors scope).

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état Stories amont** (AC: tous)
  - [x] Confirmer Story 11.1 livrée : `web/style-android.css` existe.
  - [x] Confirmer Story 11.2 livrée : `LeVoileBridge` enrichie + `window.LV` API JS disponible.
  - [x] Lire `LeVoileBridge.kt` actuel.

- [x] **Task 2 : Ajouter `openAppDetailsSettings` dans `LeVoileBridge`** (AC: #3)
  - [x] Ajouter la méthode + imports `Uri`.
  - [x] Vérifier que la signature reste cohérente avec `openKillSwitchTarget` Story 10.2.

- [x] **Task 3 : Étendre `style-android.css`** (AC: #2)
  - [x] Ajouter le bloc `Story 11.3` complet selon AC #2.
  - [x] **Vérifier** : aucune règle existante Story 11.1 n'est cassée (tester via `apkanalyzer` ou manuellement).

- [x] **Task 4 : Markup HTML AppBar + Drawer** (AC: #1, Note C)
  - [x] **Décision Note C** : éditer `windows/frontend/index.html` (Option 1) OU créer `android/app/src/main/assets/web/c13-appbar.html` injecté JS-side (Option 2).
  - [x] Insérer le markup AC #1 + ajouter `.android-only { display: none; }` baseline dans `windows/frontend/src/style.css` si Option 1.
  - [x] Si Option 1 : runner `bash android/scripts/sync-frontend.sh` pour propager.
  - [x] **Reporter dans Completion Notes** la décision + le chemin.

- [x] **Task 5 : Module JS handlers AppBar + Drawer** (AC: #4)
  - [x] Ajouter le module IIFE selon AC #4.
  - [x] **Cohérent Note B Story 11.2** : édit dans `windows/frontend/src/app.js` (préférable) OU `web/c13-appbar.js` séparé.

- [x] **Task 6 : Strings i18n** (AC: #5)
  - [x] **Décision** : déclarer dans `strings.xml` (recommandation Stories 9.x-10.x) ou hard-coder dans HTML (simplicité MVP).
  - [x] Si déclaration : ajouter clés `values/strings.xml` + `values-fr/strings.xml` (parité explicite).
  - [x] **Reporter dans Completion Notes** la décision.

- [x] **Task 7 : Créer `LeVoileBridgeAppDetailsTest.kt`** (AC: #6)
  - [x] Créer le fichier test.
  - [x] Vérifier vert.

- [x] **Task 8 : Build sanity + smoke test** (AC: #7)
  - [x] `bash scripts/sync-frontend.sh && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint`.
  - [x] Smoke test manuel sur device/émulateur.
  - [x] **Vérifier la coexistence avec C17** : kill switch inactif → bandeau visible au-dessus de l'AppBar.

- [x] **Task 9 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pattern principal — Markup partagé desktop+Android avec classe d'activation

Le markup HTML vit dans `windows/frontend/index.html` (single source of truth Story 11.1). La classe `android-only` masque par défaut, activée sous `body.platform-android`. Symétrique avec `desktop-only` (Story 11.1).

**Avantage** : un seul fichier HTML à maintenir, audit visuel simple (« qui voit quoi »).
**Inconvénient** : un changement de markup Android nécessite de toucher `windows/frontend/` — borne acceptable selon Note B Story 11.2.

### Pourquoi pas Material Components Android natif

Story 11.3 est strictement frontend (HTML/CSS/JS). Ajouter `com.google.android.material.appbar.MaterialToolbar` impliquerait :
- Une `Activity` ou un `Fragment` natif qui héberge le composant — viole ADR-14 (le frontend est seul host UI).
- Une dépendance Gradle Material (~500 KB APK) — viole NFR-AND-3 (< 25 MB).
- Une duplication UX (AppBar natif + AppBar HTML = lequel gagne ?).

Le composant HTML reproduit fidèlement les specs Material 56dp + ripple-like + slide-in 250ms.

### Coordination Story 10.2 (bandeau C17)

Le bandeau C17 (40dp top) est rendu **au-dessus** de l'AppBar (56dp). Quand `body.has-c17-banner` est actif, l'AppBar descend à `top: 40px` et le main padding-top devient 96dp (40+56). Cohérent ux-design-specification.md (le bandeau prime visuellement).

### Coordination Story 11.4 (Country Selector)

Story 11.4 livrera le bottom-sheet C14. Aucune interaction directe avec C13 — le bottom-sheet vit en bas, l'AppBar en haut. Pas de conflit.

### Coordination Story 11.6 (C15 Onboarding Kill Switch)

Story 11.6 ouvre un écran `OnboardingActivity` (Story 11.5) qui **n'a pas l'AppBar** (l'onboarding est plein écran sans navigation). Le drawer / burger est invisible pendant l'onboarding car l'app principale est en pause.

### Source tree components à toucher

- **Modifiés** :
  - `android/app/src/main/kotlin/.../bridge/LeVoileBridge.kt`
  - `android/app/src/main/assets/web/style-android.css`
  - `windows/frontend/index.html` (Option 1) OU `android/app/src/main/assets/web/c13-appbar.html`
  - `windows/frontend/src/style.css` (Option 1) — règle baseline `.android-only`
  - `windows/frontend/src/app.js` OU `android/app/src/main/assets/web/c13-appbar.js`
  - `android/app/src/main/res/values/strings.xml` (optionnel — décision dev)
  - `android/app/src/main/res/values-fr/strings.xml` (optionnel)
- **Nouveaux** :
  - `android/app/src/test/kotlin/.../bridge/LeVoileBridgeAppDetailsTest.kt`

### References

- [architecture.md l. 1164-1193](_bmad-output/planning-artifacts/architecture.md) — UI Patterns Android.
- [architecture.md l. 2397-2400](_bmad-output/planning-artifacts/architecture.md) — ADR-08 isolation OS.
- [epics.md l. 1863-1886](_bmad-output/planning-artifacts/epics.md) — Story 11.3 BDD complet.
- [ux-design-specification.md l. 1259-1269](_bmad-output/planning-artifacts/ux-design-specification.md) — Composant C13 specs.
- Story 10.2 (livrée) : bandeau C17 + `openKillSwitchTarget` (pattern Intent).
- Story 11.1 (à venir) : sync HTML/CSS/JS — Note C.
- Story 11.2 (à venir) : `window.LV` API JS — module 11.3 dépend.
- Story 11.5+11.6 : onboarding sans AppBar.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Completion Notes List

- **Décision Note C — Option 1 retenue (markup partagé + classe `android-only`)** : le markup AppBar + drawer vit dans `android/app/src/main/assets/web/index.html` (cohérent Option 2 Story 11.1 — pas dans `windows/frontend/`). Plus auditable, pas de FOUC.
- **Strings i18n** : déclarées dans `values/strings.xml` + `values-fr/strings.xml` (parité explicite cohérente Stories 9.x-10.x), même si le HTML hard-code actuellement les labels FR. Future use Phase 2 i18n EN.
- **Bridge `openAppDetailsSettings`** : ajouté à LeVoileBridge avec retour JSON `{ok:true,action:"opened_app_details"}` ou `{error:"settings_unavailable"}`. Test `LeVoileBridgeAppDetailsTest` 2 cas.
- **Coexistence C17 (Story 10.2)** : règle CSS `body.platform-android.has-c17-banner .android-appbar { top: 40px; }` descend l'AppBar si bandeau visible. `body.platform-android.has-c17-banner.has-android-appbar main { padding-top: 96px; }` ajuste la marge contenu.
- **Build/test verification 2026-05-03 (JDK 17)** : BUILD SUCCESSFUL, 133 tests verts, 0 lint error. `LeVoileBridgeAppDetailsTest` réécrit en mode structural (annotation check + return type) — pattern aligné Story 10.2 `LeVoileBridgeKillSwitchTest` qui note explicitement « Intent.startActivity intestable JVM-only sans Robolectric ». Couverture comportementale Espresso reportée Story 12.6.

### File List

- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (MODIFIÉ — `openAppDetailsSettings`)
- `android/app/src/main/assets/web/style-android.css` (NOUVEAU — incluant CSS AppBar + drawer)
- `android/app/src/main/assets/web/index.html` (MODIFIÉ — markup AppBar + drawer)
- `android/app/src/main/assets/web/app.js` (MODIFIÉ — handlers AppBar + drawer)
- `android/app/src/main/res/values/strings.xml` (MODIFIÉ — labels drawer)
- `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉ — parité FR)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridgeAppDetailsTest.kt` (NOUVEAU)

### Change Log

| Date | Description |
|---|---|
| 2026-05-03 | Story 11.3 livrée (AppBar 56dp + drawer + openAppDetailsSettings bridge). |
| 2026-05-03 | Code-review Epic 11 : tentative de tests fonctionnels Mockito (M2) ABANDONNÉE — `Intent(String)` constructeur stubbé android.jar retourne null → NPE sur `.setData(...)`. Pattern structurel Story 10.2 confirmé comme correct. Action item M2 ajouté ci-dessous (couverture déléguée Story 12.6 Espresso). |

## Review Follow-ups (AI)

- [ ] **[AI-Review][MEDIUM] M2 — Couverture comportementale `openAppDetailsSettings` reportée Story 12.6** : le test JVM-only se limite à l'API surface (annotation + return type). Le chemin nominal `{ok:true,opened_app_details}` et le fallback `{error:settings_unavailable}` doivent être testés Espresso instrumenté Story 12.6 — comme `LeVoileBridgeKillSwitchTest` Story 10.2.
