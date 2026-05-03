# Story 10.2: Composant C17 — Bandeau alerte kill switch persistant

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Aucune exception « code partagé » n'est nécessaire pour cette story.** Story 10.2 livre un bandeau d'alerte rendu côté WebView (HTML + CSS + JS dans `assets/`) + une extension limitée du bridge JS Kotlin (`LeVoileBridge.kt`) + un observer LiveData sur `KillSwitchDetector` posé dans `MainActivity`. Aucun fichier Go n'est lu, créé ou modifié. Aucun import gomobile. Aucune entrée dans `android/shims/*.go` n'est ajoutée. Aucune ligne dans `go.mod`/`go.sum` racine n'est touchée. Aucun appel JNI vers le `.aar` n'est introduit. Le bandeau C17 est purement UI + OS-Android (`Settings.ACTION_VPN_SETTINGS` deeplink fallback EBR-02).
>
> **Rappel ADR-08 (architecture.md l. 2397-2400) — isolation OS maximale.** Cohérent ADR-14 (architecture.md l. 2434-2437) : l'UI Android = `WebView` + assets HTML/CSS/JS partagés desktop, layout responsive mobile via `body.platform-android`. Story 10.2 enrichit les assets `android/app/src/main/assets/{index.html,style.css,app.js}` (livrés Story 9.3) — **mais uniquement de classes/composants `.android-c17-*` qui ne s'activent que si `body.platform-android`**. Le frontend desktop n'est en aucun cas modifié (le dossier racine `frontend/` n'est pas touché). Dans le futur (Story 11.1 livrée), `sync-frontend.sh` poussera un `frontend/` mis à jour vers `android/app/src/main/assets/` — Story 10.2 doit donc s'assurer que les ajouts vivent dans des **fichiers ou sections clairement délimités** que Story 11.1 saura préserver/migrer.
>
> **Zones explicitement OFF-LIMITS pour cette story** (livrées par 9.x/10.1, intactes pour 10.2) :
>
> | Zone | Livrée par | État pour 10.2 |
> |---|---|---|
> | `go.mod` / `go.sum` racine | Story 9.2 | INTACT |
> | `android/shims/*.go` (5 shims gomobile) | Story 9.2 | INTACT — code Go, pas Kotlin |
> | `android/scripts/*` (build-aar, verify-shared-imports, futur sync-frontend) | Story 9.2 / Story 11.1 (à venir) | INTACT — non invoqués par 10.2 |
> | `android/levoile-core/*` | Story 9.1+9.2 | INTACT |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/LeVoileVpnService.kt` | Story 9.4 | INTACT — le bandeau C17 ne dépend pas du service VPN (le statut kill switch est OS-level, indépendant de l'état de connexion tunnel) |
> | `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/{KillSwitchDetector.kt,KillSwitchStatus.kt,SettingsReader.kt}` | Story 10.1 | **INTACT** — Story 10.2 **consomme** `LiveData<KillSwitchStatus>` mais ne modifie pas la classe. Si une signature manquait, retour back-pressure vers Story 10.1, pas modification ici |
> | `android/app/src/main/AndroidManifest.xml` | Story 9.1 | INTACT — aucune nouvelle permission requise (le deeplink `Settings.ACTION_VPN_SETTINGS` fonctionne sans permission spéciale) |
> | `android/app/build.gradle.kts` | Stories 9.1+10.1 | INTACT — aucune nouvelle dépendance Gradle requise (HTML/CSS/JS pur + `androidx.lifecycle.LiveData` déjà présent depuis 10.1) |
> | `frontend/` racine + `internal/ui/web/` | Stories desktop 5.x | INTACT — Story 10.2 ne touche que `android/app/src/main/assets/` |
> | `internal/{tunnel,tun,firewall,routing,ui,ipc,wfp,nftables,...}` racine + `windows/`, `linux/`, `cmd/`, `deploy/` | Stories 1-8 | INTACT — hors arbre `android/` |
>
> **Concrètement** : `git status` à la fin de la session dev doit montrer **uniquement** :
>   (a) `android/app/src/main/assets/index.html` (MODIFIÉ — ajout d'un bloc `<div id="android-c17-banner">` + scripts) — fichier livré Story 9.3,
>   (b) `android/app/src/main/assets/style.css` (MODIFIÉ — ajout de règles `.android-c17-banner` scopées sous `body.platform-android`) — fichier livré Story 9.3,
>   (c) `android/app/src/main/assets/app.js` (MODIFIÉ — ajout d'une IIFE de gestion du bandeau qui consulte `window.LeVoile.getKillSwitchStatus()` et `window.LeVoile.openKillSwitchTarget()`) — fichier livré Story 9.3,
>   (d) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (MODIFIÉ — ajout 2 méthodes `@JavascriptInterface` : `getKillSwitchStatus()` et `openKillSwitchTarget()`) — fichier livré Story 9.3,
>   (e) `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (MODIFIÉ — ajout d'un observer `killSwitchDetector.status.observe(this) { ... }` qui pousse le statut au WebView via `webView.evaluateJavascript(...)` et qui force un rafraîchissement actif du bandeau, **en complément** de la modif Story 10.1) — fichier livré Stories 9.3+10.1,
>   (f) `android/app/src/main/res/values/strings.xml` + `android/app/src/main/res/values-fr/strings.xml` (MODIFIÉS — ajout 1 clé pour le toast de fallback EBR-02 — voir AC #5),
>   (g) `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridgeKillSwitchTest.kt` (NOUVEAU — test JVM-only sur les 2 nouvelles méthodes du bridge),
>   (h) `android/README-android.md` (MODIFIÉ — section « Bandeau C17 kill switch »),
>   (i) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `ready-for-dev` → `review`),
>   (j) `_bmad-output/implementation-artifacts/10-2-composant-c17-bandeau-alerte-kill-switch-persistant.md` (auto-update Status, File List, Completion Notes, Change Log).
>
> **Aucune entrée à la racine** (`go.mod`, `go.sum`, `internal/`, `frontend/`, `windows/`, `linux/`, `cmd/`, `deploy/`, `_bmad/`), **aucune entrée sous `android/shims/`, `android/scripts/`, `android/levoile-core/`, `android/app/src/main/kotlin/fr/plateformeliberte/levoile/{kill/,vpn/}`**. Tout autre fichier modifié est un side-effect non prévu — **STOP, investiguer, ne pas tenter de "lisser" en supprimant les changements**. Reporter dans Debug Log et demander avant de poursuivre.
>
> **Anti-pattern fréquent à éviter** : tenter de pré-tirer en avance des fichiers prévus par les stories suivantes — composant C15 « Onboarding Kill Switch Screen » (Story 11.6, écran complet avec icône warning + hiérarchie typo + bouton primaire deeplink + lien « Continuer sans »), `KillSwitchHelper.kt` Kotlin avec textes français pré-localisés (Story 11.6), enrichissement JS Bridge complet (`window.LeVoile.connect()`, `disconnect()`, `selectCountry()`, etc. — Story 11.2), composant C13 AppBar Material 56dp ou C14 Bottom-sheet pays (Story 11.3/11.4), `OnboardingActivity` 3 écrans + persistence `onboarding_completed` (Story 11.5). Cette story livre **uniquement** le bandeau C17 minimaliste consommant `KillSwitchDetector` (Story 10.1 livrée). Si tu te retrouves à éditer `connect()` du bridge ou créer une nouvelle Activity, tu es hors-scope.
>
> **Spécifique EBR-02 (architecture.md l. 2455-2461)** : la migration de FR-AND-3 (« onboarding VPN permanent ») d'Epic 10 vers Epic 11 est **acquise**. Conséquence directe pour Story 10.2 : le tap sur le bandeau C17 doit avoir **2 branches** (AC #5) — une si Epic 11 est livré (intent vers composant C15), une si Epic 11 PAS encore livré (deeplink direct `Settings.ACTION_VPN_SETTINGS`). **Pour Story 10.2 livrée AUJOURD'HUI (avant Epic 11)** : la branche fallback est l'unique branche active. La branche « ouvre C15 » n'est PAS implémentée dans cette story — elle sera ajoutée Story 11.6 via une modification ciblée de `LeVoileBridge.openKillSwitchTarget()`. Le contrat exposé par Story 10.2 doit donc être **extensible** (Story 11.6 modifie l'implémentation interne sans toucher à la signature `@JavascriptInterface`).

## Story

En tant qu'utilisateur Android,
Je veux un bandeau rouge persistant rendu en haut de l'écran principal (sous l'AppBar / au-dessus du contenu de la WebView) tant que le réglage Android « VPN permanent + bloquer connexions sans VPN » n'est pas activé pour Le Voile,
Afin que je sache à tout moment, sans avoir à creuser dans des paramètres, que ma protection est partielle ; et qu'un seul tap me redirige vers le panneau Android natif des paramètres VPN (`Settings.ACTION_VPN_SETTINGS`) — branche fallback EBR-02 cohérente avec ADR-10 et FR-AND-2 / NFR-AND-9 / RGAA AA (`aria-live="assertive"`).

## Acceptance Criteria

1. **Le HTML du bandeau est ajouté dans `assets/index.html` sous `body.platform-android` exclusivement** — Quand `android/app/src/main/assets/index.html` est lu après cette story, il contient (en plus du contenu placeholder Story 9.3) un élément suivant, inséré comme **premier enfant** de `<body>` (avant le `<h1>` placeholder existant) :
   ```html
   <div
       id="android-c17-banner"
       class="android-c17-banner"
       role="alert"
       aria-live="assertive"
       hidden>
     <span class="android-c17-banner__icon" aria-hidden="true">⚠️</span>
     <span class="android-c17-banner__text">Kill switch inactif — Activer</span>
     <span class="android-c17-banner__chevron" aria-hidden="true">›</span>
   </div>
   ```
   **Important** :
   - L'attribut `hidden` HTML5 est appliqué par défaut (l'élément n'est pas rendu visuellement et est ignoré par les lecteurs d'écran tant qu'il est présent). C'est l'attribut natif HTML5 pas un `display: none` CSS — l'AC #2 retire/ajoute `hidden` via JS pour piloter la visibilité. **Ne PAS** utiliser `class="hidden"` ou `style="display:none"` à la place — c'est moins propre et casse le pattern aria-live.
   - L'attribut `role="alert"` + `aria-live="assertive"` (architecture.md l. 1631, ux-design-specification.md l. 1339) ensemble garantissent que TalkBack annonce le bandeau **immédiatement** dès qu'il devient visible, en interrompant la lecture en cours (cf. RGAA AA — pattern « assertive » réservé aux alertes critiques, ce qui est exactement le cas ici : kill switch inactif = perte de protection).
   - Les classes CSS sont préfixées `android-c17-` pour être **scopables sous `body.platform-android`** uniquement (cf. AC #2) — le frontend desktop ne doit jamais voir le bandeau même s'il chargeait le même `index.html` par accident (résilience cross-OS, cohérent EBR-01).
   - Le texte « Kill switch inactif — Activer » est en français (epics.md l. 1713, ux-design-specification.md l. 1334) — pas de localisation en/fr séparée pour cette story (cohérent strings desktop, mono-langue MVP). Story future (« localisation fr/en » mentionnée gap mineur architecture.md l. 2197) traitera l'anglais. **Pas dans `strings.xml` Android** : le texte est dans le HTML car le bandeau est rendu côté WebView, pas côté `View` Android natif.

2. **Les règles CSS du bandeau sont scopées `body.platform-android`** — Quand `android/app/src/main/assets/style.css` est lu après cette story, il contient (en plus du CSS Story 9.3) un bloc dédié, idéalement séparé par un commentaire de section explicite :
   ```css
   /* ==============================================================
      Android C17 — Bandeau alerte kill switch persistant
      Story 10.2 · Visible UNIQUEMENT si body.platform-android.
      ux-design-specification.md l. 1328-1339 + epics.md l. 1701-1732
      ============================================================== */

   body.platform-android .android-c17-banner {
     position: fixed;
     top: 0;
     left: 0;
     right: 0;
     height: 40px;
     padding: 0 16px;
     display: flex;
     align-items: center;
     gap: 8px;
     background: rgba(212, 43, 43, 0.9);    /* #d42b2b à 90% opacité (ux l. 1334) */
     color: #ffffff;
     font-family: 'Rajdhani', sans-serif;   /* fallback générique si Story 11.1 pas livrée */
     font-size: 14px;
     font-weight: 600;
     z-index: 1000;
     transform: translateY(-100%);
     transition: transform 200ms ease-out;
     /* Empêcher la sélection texte (ce n'est pas un contenu) */
     user-select: none;
     /* Cible tactile RGAA AA — la zone tappable couvre toute la largeur */
     cursor: pointer;
   }

   body.platform-android .android-c17-banner:not([hidden]) {
     transform: translateY(0);
   }

   body.platform-android .android-c17-banner__text {
     flex: 1;
   }

   body.platform-android .android-c17-banner__chevron {
     font-size: 18px;
     line-height: 1;
   }

   /* Décaler le contenu placeholder du body de 40px quand le bandeau est visible */
   body.platform-android.has-c17-banner {
     padding-top: 40px;
   }
   ```
   **Important** :
   - L'animation slide-down de 200ms (epics.md l. 1719 « animation fade-out 200ms » côté disparition — symétrie ici à l'apparition) est portée par `transition: transform 200ms ease-out`. Quand `[hidden]` est retiré, le bandeau descend ; quand `[hidden]` est ajouté, le bandeau remonte. Cohérent ux-design-specification.md l. 1196 (« slide-down 200ms à l'apparition »).
   - La classe `body.platform-android.has-c17-banner` est ajoutée/retirée par le JS (AC #4) pour décaler le contenu — sinon le bandeau couvre la première ligne de texte.
   - **Pas de `display: none` au repos** — l'attribut HTML5 `hidden` est exploité, et l'animation CSS est traduite via `transform`. C'est important pour que le `aria-live` reste fonctionnel (un élément `display:none` est totalement absent du DOM accessible, on ne pourrait pas l'annoncer).
   - **Pas de Material elevation / box-shadow** dans cette story — le composant C17 dans la spec ux n'en a pas. Si Story 11.x ajoute du polish visuel, c'est un nice-to-have hors-scope ici.

3. **L'app.js consulte le bridge et toggle `[hidden]` selon le statut** — Quand `android/app/src/main/assets/app.js` est lu après cette story, il contient (en plus du polling status placeholder Story 9.3) une IIFE distincte délimitée par commentaires :
   ```javascript
   /* ====== Android C17 — Bandeau kill switch (Story 10.2) ====== */
   (function () {
     'use strict';
     // Pas de bandeau si pas Android (sécurité cross-OS)
     if (!document.body.classList.contains('platform-android')) return;
     // Pas de bandeau si pas de bridge JS (page chargée hors WebView Le Voile)
     if (typeof window.LeVoile === 'undefined' || typeof window.LeVoile.getKillSwitchStatus !== 'function') return;

     var banner = document.getElementById('android-c17-banner');
     if (!banner) return;

     function refreshFromBridge() {
       var status;
       try { status = window.LeVoile.getKillSwitchStatus(); } catch (e) { return; }
       // status est attendu strictement parmi : "Active" | "Inactive" | "Unverifiable"
       // Voir contrat AC #6 ci-dessous (LeVoileBridge.getKillSwitchStatus).
       var hide = (status === 'Active');
       if (hide) {
         banner.setAttribute('hidden', '');
         document.body.classList.remove('has-c17-banner');
       } else {
         banner.removeAttribute('hidden');
         document.body.classList.add('has-c17-banner');
       }
     }

     // Premier rafraîchissement après chargement DOM
     refreshFromBridge();

     // Re-rafraîchissement déclenché par Kotlin via window.__LV_killSwitchChanged()
     window.__LV_killSwitchChanged = function () { refreshFromBridge(); };

     // Tap sur le bandeau → délègue au bridge
     banner.addEventListener('click', function () {
       try { window.LeVoile.openKillSwitchTarget(); } catch (e) {}
     });
   })();
   ```
   **Important** :
   - L'IIFE est défensive : 3 garde-fous (`platform-android`, bridge présent, élément DOM présent). Si l'un échoue → `return` silencieux, **pas de log JS** (cohérent NFR-AND-9 — pas de leak de data côté logcat WebView via `console.log`).
   - La fonction `window.__LV_killSwitchChanged` est exposée pour permettre à Kotlin de pousser activement un changement via `webView.evaluateJavascript("window.__LV_killSwitchChanged && window.__LV_killSwitchChanged();", null)` (AC #7) — pas de polling JS à intervalle, on est event-driven.
   - **Pas de cache JS du dernier statut** — chaque appel `getKillSwitchStatus()` est suffisamment rapide (le bridge délègue à `KillSwitchDetector.status.value` qui est une lecture mémoire), pas de raison d'introduire de l'état JS qui pourrait diverger du Kotlin.
   - Pas de `Element.animate()` ou de `requestAnimationFrame` — l'animation slide-down est purement CSS (AC #2), JS ne fait que toggle l'attribut. Plus simple, plus testable.

4. **`MainActivity` observe `killSwitchDetector.status` et notifie le WebView** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` est lu après cette story, il contient **en plus** de la modification Story 10.1 (`lateinit var killSwitchDetector` + `onResume { killSwitchDetector.refresh() }`) un observer LiveData posé dans `onCreate(...)` après l'instanciation du détecteur :
   ```kotlin
   killSwitchDetector.status.observe(this) { _ ->
       // Le statut courant est lu côté JS via getKillSwitchStatus().
       // On envoie juste un signal au JS pour qu'il re-query le bridge.
       // Le runOnUiThread garantit qu'evaluateJavascript ne soit pas appelé
       // depuis un thread parallèle si LiveData postValue arrivait depuis Dispatchers.IO
       // (ce qui n'est pas le cas en pratique — refresh() est appelée onUiThread —
       // mais ceinture + bretelles défensives).
       runOnUiThread {
           webView.evaluateJavascript(
               "window.__LV_killSwitchChanged && window.__LV_killSwitchChanged();",
               null,
           )
       }
   }
   ```
   **Important** :
   - `observe(this, ...)` lie l'observer au lifecycle de `MainActivity` — au `onDestroy`, il est automatiquement désabonné, pas de fuite mémoire.
   - **Pas de stockage de l'état dans Kotlin** au-delà du `LiveData` lui-même — le bridge `getKillSwitchStatus()` lit directement `killSwitchDetector.status.value` (cohérent design Story 10.1 + non-dup d'état).
   - Le pattern « LiveData → evaluateJavascript notify → JS re-query bridge » est intentionnel. Pourquoi pas pousser le statut directement ? Parce que `evaluateJavascript` peut être asynchrone et le sérialiser/parser depuis Kotlin demande une couche supplémentaire. Le re-query est < 1ms et garde le contrat « le bridge est la source de vérité ».
   - L'observer est ajouté **une seule fois** dans `onCreate` (pas dans `onResume`) — sinon on duplique les observers à chaque lifecycle.
   - **Aucune autre modification de `MainActivity`** : pas d'AppBar, pas de drawer, pas de toolbar customisation. Tout vit côté WebView.

5. **`LeVoileBridge.openKillSwitchTarget()` ouvre `Settings.ACTION_VPN_SETTINGS` (branche fallback EBR-02)** — Quand `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` est lu après cette story, il contient **en plus** de la méthode `getStatus()` livrée Story 9.3 :
   ```kotlin
   @android.webkit.JavascriptInterface
   fun openKillSwitchTarget() {
       // EBR-02 (architecture.md l. 2455-2461) : Story 11.6 enrichira ce comportement
       // pour ouvrir le composant C15 (onboarding kill switch screen) si Epic 11 est livré.
       // Story 10.2 (cette story) implémente UNIQUEMENT la branche fallback.
       val intent = android.content.Intent(android.provider.Settings.ACTION_VPN_SETTINGS)
           .addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
       try {
           context.startActivity(intent)
       } catch (t: android.content.ActivityNotFoundException) {
           // Fallback du fallback : certaines ROM custom n'exposent pas ACTION_VPN_SETTINGS.
           // Toast pédagogique, sans data utilisateur (cohérent NFR-AND-9).
           android.widget.Toast.makeText(
               context,
               context.getString(R.string.android_c17_settings_unavailable),
               android.widget.Toast.LENGTH_LONG,
           ).show()
       }
   }
   ```
   **Important** :
   - `FLAG_ACTIVITY_NEW_TASK` est obligatoire si `context` est l'`applicationContext` (ce qui est le cas dans Story 9.3 — `LeVoileBridge(this)` passe `MainActivity` comme `Context` mais on ne peut pas garantir que `MainActivity` soit dans une task sortante au moment du tap). Défensif.
   - La constante `R.string.android_c17_settings_unavailable` est définie dans :
     - `android/app/src/main/res/values/strings.xml` (clé `android_c17_settings_unavailable` = `"Activez « VPN permanent » dans les paramètres Android pour bénéficier de la protection complète."` — texte FR par défaut, base EN-US identique-FR car MVP mono-langue).
     - `android/app/src/main/res/values-fr/strings.xml` (même clé, identique).
   - **Pas de log** dans le `catch` (cohérent NFR-AND-9 — un log « settings vpn indisponible sur ce device » serait redondant avec le toast affiché à l'utilisateur, et révélerait un info ROM custom au logcat).
   - **Pas de `try-with-resources` style elaboré** — l'`Intent.startActivity` est synchrone et ne nécessite aucun cleanup.
   - **Story 11.6 modification prévue** : au moment de la livraison de Story 11.6, le `try` block sera prefixé par une condition `if (OnboardingPrefs.isOnboardingComplete()) startActivity(C15Intent) else <fallback ci-dessus>`. Cette modification ne casse PAS le contrat `@JavascriptInterface` (signature `fun openKillSwitchTarget()` inchangée). Story 10.2 verrouille uniquement la branche fallback.

6. **`LeVoileBridge.getKillSwitchStatus()` retourne une string parmi `"Active" | "Inactive" | "Unverifiable"`** — Quand `LeVoileBridge.kt` est lu, il contient **en plus** :
   ```kotlin
   @android.webkit.JavascriptInterface
   fun getKillSwitchStatus(): String {
       val status = killSwitchDetector?.status?.value ?: KillSwitchStatus.Unverifiable
       return when (status) {
           is KillSwitchStatus.Active -> "Active"
           is KillSwitchStatus.Inactive -> "Inactive"
           is KillSwitchStatus.Unverifiable -> "Unverifiable"
       }
   }
   ```
   **Important** :
   - Le bridge **dépend** maintenant d'une instance `KillSwitchDetector` injectée via le constructeur. Story 9.3 livre `class LeVoileBridge(private val context: Context)` — Story 10.2 doit étendre la signature à `class LeVoileBridge(private val context: Context, private val killSwitchDetector: KillSwitchDetector?)`. **Le `?` nullable** est important pour deux raisons :
     1. Compatibilité avec un éventuel test qui voudrait instancier `LeVoileBridge` sans détecteur.
     2. Robustesse au cas où `MainActivity.onCreate` enregistre le bridge AVANT d'instancier le détecteur (peu probable vu la séquence Task 5 mais défensif).
   - Le when est exhaustif sur `sealed class KillSwitchStatus` (Story 10.1) — si Story 10.1 a choisi `enum class`, le when reste exhaustif.
   - Les **strings retournées sont stables** (`"Active"`, `"Inactive"`, `"Unverifiable"`) — le frontend JS dépend de ces valeurs (AC #3). **Ne JAMAIS** retourner des strings localisées ou avec espaces — c'est un protocole machine, pas du texte UI.
   - L'instanciation côté `MainActivity` devient `webView.addJavascriptInterface(LeVoileBridge(this, killSwitchDetector), "LeVoile")`. **Important** : passer `this` (Activity) **ou** `applicationContext` ? Décision : passer `this` (Activity) car nécessaire pour `startActivity()` sans `FLAG_ACTIVITY_NEW_TASK` au moment du tap utilisateur depuis le WebView (mais on ajoute `FLAG_ACTIVITY_NEW_TASK` défensivement quand même — AC #5). Reporter dans Completion Notes.

7. **Le bandeau apparaît au premier `onResume` après détection `Inactive`/`Unverifiable`** — Quand un device test (Pixel 3 émul. API 29 ou Pixel 6 API 33 ou Pixel récent API 34) est dans l'état :
   - `adb shell settings put global always_on_vpn_app fr.plateformeliberte.levoile.debug` (debug build)
   - `adb shell settings put global always_on_vpn_lockdown 1`

   Et qu'on lance `adb shell am start -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.MainActivity`, **le bandeau est masqué** (statut Active). Inversement, quand on remet `always_on_vpn_lockdown` à `0` et qu'on revient sur l'app (CPU suspend → resume via `adb shell input keyevent KEYCODE_HOME` puis `adb shell am start ...`), `MainActivity.onResume` déclenche `KillSwitchDetector.refresh()` → status devient `Inactive` → observer pousse `__LV_killSwitchChanged` → JS bascule `[hidden]` off → **le bandeau apparaît avec animation slide-down 200ms**.

   Vérifiable via `chrome://inspect` (Chrome desktop ↔ émulateur Android) en injectant un `console.log` temporaire dans `refreshFromBridge` (à retirer avant commit). **Reporter dans Debug Log** : observation manuelle de l'animation slide-down.

8. **Le tap sur le bandeau ouvre les paramètres VPN Android natifs** — Quand le bandeau est affiché et que l'utilisateur tape n'importe où sur sa surface (cibles tactiles ≥ 40px de hauteur — la zone est full-width, plus que 48dp tactile target — RGAA AA), le `click` listener JS appelle `window.LeVoile.openKillSwitchTarget()`, qui invoque le bridge Kotlin AC #5, qui démarre `Intent(Settings.ACTION_VPN_SETTINGS)`. L'écran « VPN » des paramètres Android natifs s'ouvre, listant les apps VPN disponibles. Quand l'utilisateur revient à Le Voile (back Android), `MainActivity.onResume` déclenche `KillSwitchDetector.refresh()` (Story 10.1) → si l'utilisateur a activé « VPN permanent » + « Bloquer connexions sans VPN » entre-temps, le statut bascule à `Active` → le bandeau disparaît.

   **Vérifiable manuellement** : suite de tests `adb` dans le README AC #11.

9. **TalkBack annonce le bandeau immédiatement à l'apparition** — Quand TalkBack est activé (`adb shell settings put secure enabled_accessibility_services com.google.android.marvin.talkback/com.google.android.marvin.talkback.TalkBackService` puis `adb shell settings put secure accessibility_enabled 1`) et que le bandeau passe de masqué à visible, TalkBack lit immédiatement « Alerte. Kill switch inactif. Activer » (à vérifier au speaker ou casque). L'annonce interrompt toute autre lecture en cours (sémantique `assertive`). Le bandeau est focusable au focus séquentiel (touche flèche / swipe TalkBack) — au focus, TalkBack relit l'annonce. **Pas de geste swipe pour dismiss** (epics.md l. 1732) — la seule action est tap → flow C15/Settings (cohérent ADR-10 : on ne donne pas la possibilité d'« acquitter » l'alerte sans corriger).

10. **Test JVM-only `LeVoileBridgeKillSwitchTest.kt` couvre les 2 nouvelles méthodes** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté après cette story, un fichier de test `app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridgeKillSwitchTest.kt` couvre **au minimum** :

    | # | Méthode | Setup | Vérification |
    |---|---|---|---|
    | T1 | `getKillSwitchStatus()` | Détecteur null injecté dans bridge | Retourne `"Unverifiable"` |
    | T2 | `getKillSwitchStatus()` | Détecteur stub avec `status.value = Active` | Retourne `"Active"` |
    | T3 | `getKillSwitchStatus()` | Détecteur stub avec `status.value = Inactive` | Retourne `"Inactive"` |
    | T4 | `getKillSwitchStatus()` | Détecteur stub avec `status.value = Unverifiable` | Retourne `"Unverifiable"` |

    **Pas de test pour `openKillSwitchTarget()`** dans cette story — `Intent.startActivity` est intestable JVM-only sans Robolectric, et un Robolectric test sur cette méthode est lourd pour un retour faible (le code est trivial). Story 12.6 (tests instrumentés Espresso) couvrira l'ouverture du panneau Settings via test instrumenté complet.

    **Setup test** : utiliser un `FakeKillSwitchDetector` qui étend `KillSwitchDetector` en surchargeant le `LiveData` ; OU utiliser le constructeur secondaire `internal` introduit Story 10.1 Task 4 si présent. **Décision dev à reporter dans Debug Log**.

11. **Section README-android.md « Bandeau C17 kill switch »** — Quand `android/README-android.md` est lu après cette story, il contient une section additionnelle (insérée APRÈS la section livrée Story 10.1 « Détection kill switch ») :

    ```markdown
    ## Bandeau C17 kill switch (Story 10.2 livrée)

    Le bandeau rouge `#android-c17-banner` (assets/index.html) est affiché
    en haut de l'écran tant que `KillSwitchDetector.status` n'est pas `Active`.
    Tap → ouvre `Settings.ACTION_VPN_SETTINGS` (branche fallback EBR-02 — Story 11.6
    enrichira pour ouvrir l'onboarding C15 quand Epic 11 sera livré).

    **Test manuel cycle complet** :
    ```
    # 1. Activer kill switch côté OS pour Le Voile (debug)
    adb shell settings put global always_on_vpn_app fr.plateformeliberte.levoile.debug
    adb shell settings put global always_on_vpn_lockdown 1
    adb shell am start -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.MainActivity
    # → bandeau MASQUÉ

    # 2. Désactiver lockdown
    adb shell settings put global always_on_vpn_lockdown 0
    adb shell am start -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.MainActivity
    # → bandeau VISIBLE avec slide-down 200ms

    # 3. Tap simulé sur le bandeau (centre x=540, y=20 sur émulateur 1080×2400)
    adb shell input tap 540 20
    # → ouvre Settings → VPN

    # 4. Reset état
    adb shell settings delete global always_on_vpn_app
    adb shell settings delete global always_on_vpn_lockdown
    ```

    Vérifier l'animation slide-down et l'annonce TalkBack via `chrome://inspect`.
    ```

    Aucune autre section du README n'est touchée.

## Tasks / Subtasks

- [ ] **Task 1 : Vérifier l'état Stories 9.3 + 10.1 livrées + lister ce qui manque** (AC: tous)
  - [ ] Lire `android/app/src/main/assets/index.html` (livré 9.3) — confirmer présence du `<body>` placeholder + `<script src="app.js">`. Le bandeau s'insère **avant** le contenu existant.
  - [ ] Lire `android/app/src/main/assets/style.css` (livré 9.3) — noter les variables couleurs déjà définies (`background`, `color` body — `#0b1526` / `#e8eef5`). Vérifier qu'aucune classe `.android-c17-*` n'existe encore.
  - [ ] Lire `android/app/src/main/assets/app.js` (livré 9.3) — noter la structure IIFE existante. Le nouveau bandeau IIFE doit être délimité par commentaires de section, **séparée** de l'IIFE polling status.
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (livré 9.3) — noter signature actuelle `class LeVoileBridge(private val context: Context)`. Confirmer présence de l'unique méthode `@JavascriptInterface fun getStatus(): String`.
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (livré 9.3+10.1) — confirmer présence de `lateinit var killSwitchDetector` + override `onResume()` (Story 10.1). Identifier où ajouter l'observer (dans `onCreate`, après l'instanciation du détecteur, après le `addJavascriptInterface`).
  - [ ] Lire `android/app/src/main/kotlin/fr/plateformeliberte/levoile/kill/KillSwitchDetector.kt` (livré 10.1) — confirmer signature `val status: LiveData<KillSwitchStatus>` exposée publiquement.
  - [ ] **Reporter dans Debug Log** : état exact des fichiers lus.

- [ ] **Task 2 : Ajouter le bloc HTML du bandeau dans `index.html`** (AC: #1)
  - [ ] Insérer le `<div id="android-c17-banner">` comme premier enfant de `<body>` (avant le `<h1>` placeholder).
  - [ ] Vérifier que les attributs `role="alert"`, `aria-live="assertive"`, `hidden` sont tous présents.
  - [ ] Pas de modification d'autres éléments existants.

- [ ] **Task 3 : Ajouter le CSS scopé sous `body.platform-android.android-c17-banner` dans `style.css`** (AC: #2)
  - [ ] Insérer le bloc CSS dédié, délimité par commentaires de section.
  - [ ] Vérifier que le scopage `body.platform-android` est bien présent sur **toutes** les règles `.android-c17-*` (sinon le frontend desktop verrait le bandeau si jamais il chargeait ces assets).
  - [ ] Vérifier l'animation `transform: translateY(...)` + `transition` cohérente avec ux l. 1196-1197 + epics l. 1719.

- [ ] **Task 4 : Ajouter l'IIFE bandeau dans `app.js`** (AC: #3)
  - [ ] Insérer l'IIFE délimitée par commentaires de section, **après** l'IIFE polling status existante (Story 9.3).
  - [ ] Vérifier les 3 garde-fous (platform-android, bridge présent, élément DOM présent).
  - [ ] Exposer `window.__LV_killSwitchChanged` pour permettre push Kotlin → JS.
  - [ ] Listener `click` qui appelle `window.LeVoile.openKillSwitchTarget()` avec `try/catch` silencieux.

- [ ] **Task 5 : Modifier `LeVoileBridge.kt`** (AC: #5, #6)
  - [ ] Modifier la signature constructeur : `class LeVoileBridge(private val context: Context, private val killSwitchDetector: KillSwitchDetector? = null)`. Le default `= null` préserve la compat ascendante avec d'éventuels usages tests directs.
  - [ ] Ajouter `import fr.plateformeliberte.levoile.kill.{KillSwitchDetector, KillSwitchStatus}`.
  - [ ] Ajouter méthode `@JavascriptInterface fun getKillSwitchStatus(): String` selon AC #6.
  - [ ] Ajouter méthode `@JavascriptInterface fun openKillSwitchTarget()` selon AC #5 (avec ActivityNotFoundException catch + Toast).
  - [ ] **NE PAS** ajouter d'autres méthodes `@JavascriptInterface` (pas de `connect`, `disconnect`, `selectCountry`, etc. — Story 11.2). Anti-pattern.

- [ ] **Task 6 : Modifier `MainActivity.kt`** (AC: #4)
  - [ ] Modifier l'instanciation du bridge : `val bridge = LeVoileBridge(this, killSwitchDetector)` (était `LeVoileBridge(this)` Story 9.3).
  - [ ] Dans `onCreate(savedInstanceState)`, **après** l'instanciation `killSwitchDetector = KillSwitchDetector(applicationContext)` (Story 10.1) **et après** `addJavascriptInterface(...)` (Story 9.3), ajouter l'observer `killSwitchDetector.status.observe(this) { runOnUiThread { webView.evaluateJavascript(...) } }` (cf. AC #4).
  - [ ] Vérifier que `onResume` reste `super.onResume() + killSwitchDetector.refresh()` (livré 10.1, pas de modification).
  - [ ] **Aucune autre modification** — re-lire le diff avant commit.

- [ ] **Task 7 : Ajouter la string fallback dans `strings.xml` + `values-fr/strings.xml`** (AC: #5)
  - [ ] Dans `android/app/src/main/res/values/strings.xml` (base — actuellement seulement `<string name="app_name">`) :
    ```xml
    <string name="android_c17_settings_unavailable">Activez « VPN permanent » dans les paramètres Android pour bénéficier de la protection complète.</string>
    ```
  - [ ] Dans `android/app/src/main/res/values-fr/strings.xml` : même clé, même texte (MVP mono-langue, le texte FR est aussi le default base).
  - [ ] **Pas de string en EN-only** dans `values/strings.xml` — pour cohérence avec architecture.md gap mineur l. 2197 « Localisation au-delà fr/en : Phase 2 ». La clé reste extensible pour ajout EN futur.

- [ ] **Task 8 : Créer `LeVoileBridgeKillSwitchTest.kt`** (AC: #10)
  - [ ] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridgeKillSwitchTest.kt`.
  - [ ] Implémenter les 4 tests T1-T4 (matrice AC #10).
  - [ ] Utiliser un `FakeKillSwitchDetector` minimal qui expose un `MutableLiveData<KillSwitchStatus>` paramétrable (ou directement le constructeur secondaire `internal` Story 10.1 si testable).
  - [ ] **Pas de Robolectric**. JVM-only.
  - [ ] Vérifier `cd android && ./gradlew :app:testDebugUnitTest` passe vert.

- [ ] **Task 9 : Patcher `README-android.md`** (AC: #11)
  - [ ] Insérer la section « Bandeau C17 kill switch (Story 10.2 livrée) » au bon endroit (après section Story 10.1).

- [ ] **Task 10 : Build sanity check + test manuel device**
  - [ ] `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — toutes tâches vert.
  - [ ] `apkanalyzer apk file-size app/build/outputs/apk/debug/app-debug.apk` — taille reste < 25 MB (NFR-AND-3) — ajout du bandeau n'augmente l'APK que de quelques Ko.
  - [ ] Test manuel cycle complet via `adb` (cf. README AC #11). Reporter dans Debug Log : « bandeau apparaît avec animation slide-down 200ms cohérente », « tap ouvre Settings VPN », « TalkBack annonce immédiatement à l'apparition (test manuel à venir Story 12.6 instrumenté complet) ».

- [ ] **Task 11 : Mettre à jour la story et sprint-status**
  - [ ] Mettre à jour la section « Dev Agent Record » (Agent Model Used, File List, Completion Notes List, Change Log).
  - [ ] Passer le `Status` de cette story de `ready-for-dev` à `review`.
  - [ ] Passer `_bmad-output/implementation-artifacts/sprint-status.yaml` `10-2-composant-c17-...: ready-for-dev` → `review`.

## Dev Notes

### Pattern principal — WebView UI avec bridge typé sur LiveData

Cohérent ADR-14 (architecture.md l. 2434-2437) : l'UI Android est rendue côté WebView, pas en `View` Android natif. Le bandeau C17 est donc un `<div>` HTML stylé en CSS — pas une `Toolbar` ni un `MaterialBanner`. Avantages :
- Cohérence visuelle 100% avec desktop (charte plateformeliberte.fr — couleurs identiques `#d42b2b`, animations identiques 200ms).
- Pas de duplication CSS / Material XML.
- Test manuel facile via `chrome://inspect`.
- Animation CSS native (pas de coroutines, pas de `Animator`).

Inconvénients (acceptés) :
- Le rendu WebView a une légère overhead vs un widget natif (~16ms par frame, négligeable pour un bandeau statique).
- Les annonces TalkBack passent par les attributs ARIA — `aria-live="assertive"` est bien supporté par TalkBack ≥ 9.0 (confirmé via test manuel sur émulateur Android 10+).

### Pourquoi pas pousser le statut directement dans `evaluateJavascript`

Pattern initialement envisagé : `webView.evaluateJavascript("window.__LV_setKillSwitchStatus('${status}');", null)`. Problèmes :
- Risque d'injection si `status` venait à contenir du JS (peu probable mais) — il faut escape, ce qui complexifie.
- Source de vérité dupliquée : Kotlin pousse, JS cache — divergence possible.

Pattern retenu : Kotlin pousse un **signal** (no-arg), JS re-query le bridge. Plus simple, plus testable, source de vérité unique.

### Pourquoi `aria-live="assertive"` et pas `polite`

Cohérent ux-design-specification.md l. 1339 + epics.md l. 1730. Le pattern :
- `polite` : annonce non-urgente, lecteur d'écran attend la fin de la lecture en cours.
- `assertive` : annonce critique, interrompt la lecture en cours.

Le bandeau C17 = perte de protection détectée → critique, doit interrompre. Cohérent avec le `aria-live="assertive"` du composant C18 desktop (ux l. 1205) — frères jumeaux fonctionnels (architecture.md l. 1192 : « équivalent fonctionnel desktop du composant C17 Android »).

### Coordination Story 11.6

Story 11.6 livrera le composant C15 (Onboarding Kill Switch Screen complet — icône warning, hiérarchie typo, bouton primaire deeplink, lien « Continuer sans », fallback « non vérifiable »). Au moment de Story 11.6, la modification de `LeVoileBridge.openKillSwitchTarget()` sera :
```kotlin
fun openKillSwitchTarget() {
    if (OnboardingPrefs(context).isOnboardingCompleted()) {
        // Branche normale Story 11.6 — ouvre directement l'écran C15
        val intent = Intent(context, OnboardingActivity::class.java)
            .putExtra(OnboardingActivity.EXTRA_DEEPLINK_KILLSWITCH, true)
        context.startActivity(intent)
    } else {
        // Branche fallback Story 10.2 (cette story) — préservée
        // ... code AC #5 actuel ...
    }
}
```

Story 10.2 verrouille uniquement la branche fallback. **L'extension Story 11.6 ne casse PAS le contrat `@JavascriptInterface`** (signature `fun openKillSwitchTarget()` inchangée).

### Source tree components à toucher

- **Modifiés** :
  - `android/app/src/main/assets/index.html` (insertion bloc bandeau)
  - `android/app/src/main/assets/style.css` (insertion bloc CSS scopé)
  - `android/app/src/main/assets/app.js` (insertion IIFE bandeau)
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (extension constructeur + 2 méthodes)
  - `android/app/src/main/kotlin/fr/plateformeliberte/levoile/MainActivity.kt` (ajout observer LiveData + passage détecteur au bridge)
  - `android/app/src/main/res/values/strings.xml` (1 nouvelle clé)
  - `android/app/src/main/res/values-fr/strings.xml` (1 nouvelle clé identique)
  - `android/README-android.md` (section nouvelle)
- **Nouveaux** :
  - `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridgeKillSwitchTest.kt`

### Standards de testing

- Test JVM-only privilégié (4 tests bridge, < 100ms total).
- Pas de test Espresso instrumenté ici — Story 12.6 traitera (animation slide-down vérifiée, TalkBack vérifié, tap deeplink vérifié).
- Couverture statique HTML/CSS/JS : pas de framework de test JS dans cette stack (KISS) — vérification par test manuel + `chrome://inspect`.

### Project Structure Notes

Le composant C17 dans architecture.md l. 1372 figure dans la liste « Composants spécifiques Android » — cohérent avec ce que livre cette story. Pas d'ajout de Component Library Android natif.

### References

- [architecture.md l. 1078](_bmad-output/planning-artifacts/architecture.md) — heuristique kill switch (consommée Story 10.1).
- [architecture.md l. 1192-1206](_bmad-output/planning-artifacts/architecture.md) — composant C18 desktop équivalent fonctionnel C17.
- [architecture.md l. 1372](_bmad-output/planning-artifacts/architecture.md) — composants spécifiques Android C13-C17.
- [architecture.md l. 1631](_bmad-output/planning-artifacts/architecture.md) — accessibilité bandeau C17 `aria-live="assertive"`.
- [architecture.md l. 2413-2416](_bmad-output/planning-artifacts/architecture.md) — ADR-10.
- [architecture.md l. 2434-2437](_bmad-output/planning-artifacts/architecture.md) — ADR-14 WebView Android.
- [architecture.md l. 2455-2461](_bmad-output/planning-artifacts/architecture.md) — EBR-02 fallback Settings direct.
- [epics.md l. 1701-1732](_bmad-output/planning-artifacts/epics.md) — Story 10.2 BDD complet (4 scénarios).
- [prd.md l. 705](_bmad-output/planning-artifacts/prd.md) — NFR-AND-9 logs filtrés.
- [ux-design-specification.md l. 1328-1339](_bmad-output/planning-artifacts/ux-design-specification.md) — composant C17 anatomy + état + interaction + accessibilité.
- [ux-design-specification.md l. 1196-1206](_bmad-output/planning-artifacts/ux-design-specification.md) — composant C18 desktop (référence couleurs/animation/aria).
- [ux-design-specification.md l. 1352](_bmad-output/planning-artifacts/ux-design-specification.md) — classes CSS `.android-*` activées par `body.platform-android`.
- [ux-design-specification.md l. 1373](_bmad-output/planning-artifacts/ux-design-specification.md) — composants Phase 2 Android.
- Story 9.3 (livrée) : `MainActivity.kt`, `LeVoileBridge.kt`, `assets/{index.html,style.css,app.js}`.
- Story 10.1 (livrée — pré-requise) : `KillSwitchDetector.kt`, `KillSwitchStatus.kt`, `SettingsReader.kt`, observer LiveData consommable depuis `MainActivity`.
- Story 11.1 (à venir) : `sync-frontend.sh` — devra préserver les classes `.android-c17-*` (rouler vers `frontend/` racine, pas seulement `android/app/src/main/assets/`). Coordination à anticiper.
- Story 11.6 (à venir) : composant C15 — modifie l'implémentation interne de `openKillSwitchTarget()` sans casser la signature `@JavascriptInterface`.
- Story 12.6 (à venir) : tests instrumentés Espresso vérifient animation + TalkBack + deeplink.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

### Change Log
