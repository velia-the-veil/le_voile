# Le Voile — Android

Module Gradle Android autonome du projet Le Voile. Arbre **complètement isolé** des arbres `windows/`, `linux/`, `relay/` (cohérent ADR-08 — isolation OS maximale).

## ⚠️ Important

**Ouvrir uniquement le sous-dossier `android/` dans Android Studio.** Ne jamais ouvrir le repo entier — Android Studio confond Gradle config avec le code Go racine (cf. architecture §Development Experience).

```
File → Open → /chemin/vers/le_voile/android   ← OK
File → Open → /chemin/vers/le_voile           ← ❌ ne pas faire
```

## Pré-requis

- **JDK 17** (Microsoft OpenJDK 17 ou Eclipse Temurin 17 testé). Variable `JAVA_HOME` définie.
- **Android SDK** avec packages :
  - `platform-tools`
  - `platforms;android-34` (compileSdk + targetSdk)
  - `build-tools;34.0.0`
  - `cmdline-tools;latest` (pour `apkanalyzer`, `sdkmanager`)
  - Optionnel pour tests instrumentés (Story 12.6) : `platforms;android-29`, `platforms;android-33`, `system-images;android-29;google_apis;x86_64`, `system-images;android-33;google_apis;x86_64`, `system-images;android-34;google_apis;x86_64` + AVD créé via `avdmanager`.
  - Variables `ANDROID_HOME` et `ANDROID_SDK_ROOT` définies.
- **Android NDK** (requis pour Story 9.2 — `gomobile bind`) :
  - `ndk;26.3.11579264` (NDK r26d) installable via `sdkmanager "ndk;26.3.11579264"`
  - Variable `ANDROID_NDK_HOME` pointant sur `$ANDROID_HOME/ndk/26.3.11579264/`
- **Android Studio Iguana+** recommandé pour le dev (auto-completion Gradle, logcat intégré, profiler) — non obligatoire pour build CLI.
- Pour Story 9.2+ : **Go 1.26+** + `gomobile` (`go install golang.org/x/mobile/cmd/gomobile@latest` puis `gomobile init`).

## Configuration projet

| Clé | Valeur | Source |
|---|---|---|
| `applicationId` | `fr.plateformeliberte.levoile` | `app/build.gradle.kts` |
| `namespace` (Kotlin) | `fr.plateformeliberte.levoile` | `app/build.gradle.kts` |
| `minSdk` | 29 (Android 10+) | `app/build.gradle.kts` |
| `targetSdk` | 34 (Android 14) | `app/build.gradle.kts` |
| `compileSdk` | 34 | `{app,levoile-core}/build.gradle.kts` |
| Java/Kotlin target | JVM 17 | `{app,levoile-core}/build.gradle.kts` |
| AGP / Kotlin / AndroidX / JUnit / Espresso | versions | `gradle/libs.versions.toml` |
| Gradle wrapper | 8.7 | `gradle/wrapper/gradle-wrapper.properties` |
| R8/ProGuard | activé en release (NFR-AND-11) | `app/build.gradle.kts` |

**Source de vérité versions plugins + dépendances** : [`gradle/libs.versions.toml`](./gradle/libs.versions.toml) (Gradle Version Catalog). Pour bumper AGP, Kotlin, AndroidX, etc., éditer ce TOML — les 3 build files (top-level + `app` + `levoile-core`) consomment automatiquement via `alias(libs.plugins.X)` et `libs.androidx.Y`. Aucune duplication de versions entre modules.

## Modules

- **`app/`** — module APK principal. Sources Kotlin, manifest, ressources, ProGuard rules. Contient (à terme) `MainActivity`, `LeVoileVpnService`, JS Bridge, etc.
- **`levoile-core/`** — module bibliothèque hôte du `.aar` produit par `gomobile bind` (Story 9.2). Frontière contractuelle Kotlin↔Go (cohérent ADR-09).

## Commandes

### Build

```bash
./gradlew assembleDebug          # APK debug → app/build/outputs/apk/debug/
./gradlew assembleRelease        # APK release (signé debug temporaire — Story 12.3 livrera le keystore réel)
./gradlew installDebug           # Installe sur device/émulateur connecté via adb
./gradlew clean                  # Nettoyage
```

### Tests + qualité

```bash
./gradlew test                   # Tests unitaires JUnit 4 (à venir Stories 9.3+)
./gradlew connectedAndroidTest   # Tests instrumentés Espresso (device/émulateur requis — Story 12.6)
./gradlew lint                   # Android lint
```

### Audit APK

```bash
apkanalyzer apk file-size app/build/outputs/apk/release/app-release.apk
apkanalyzer apk download-size app/build/outputs/apk/release/app-release.apk
apkanalyzer manifest permissions app/build/outputs/apk/release/app-release.apk
```

#### Vérifier que les `.so` JNI gomobile sont bien packagés (Story 9.2 + Story 9.7)

R8/ProGuard ne strippe pas les natifs en théorie, mais une mauvaise configuration `packagingOptions` / `splits` / `ndk.abiFilters` pourrait silencieusement exclure une ABI. Avant chaque release, vérifier que les 2 ABIs ARM ciblées (Story 9.7 — cf. `app/build.gradle.kts` `defaultConfig.ndk.abiFilters` pour NFR-AND-3 < 25 MB) sont présentes :

```bash
jar -tf app/build/outputs/apk/release/app-release.apk | grep libgojni.so
# attendu (post-Story 9.7 — filtre ABI ARM uniquement) :
#   lib/arm64-v8a/libgojni.so
#   lib/armeabi-v7a/libgojni.so
```

`x86` et `x86_64` sont volontairement exclus (≈ 1 % du parc Android 2026, marché Chromebook négligeable + émulation native ARM via Houdini/native bridge couvre les corner cases). Si un device x86 réel doit être supporté à l'avenir, ré-ajouter ces ABIs au `abiFilters` mais documenter l'impact taille (~ +25 MB par ABI ajoutée).

Si une ABI ciblée manque, l'app crashera au runtime avec `UnsatisfiedLinkError: dlopen failed: library "libgojni.so" not found` sur les devices de cette ABI. Story 12.6 portera la matrice instrumentée Espresso qui valide ce check sur émulateurs API 29/33/34.

### Build du `.aar` du noyau Go partagé (Story 9.2)

Le `levoile-core/libs/levoile-core.aar` est produit par `gomobile bind` à partir des 5 packages exposés via shims sous `android/shims/` :

| Shim | Source de vérité | Surface re-exportée |
|---|---|---|
| `android/shims/protocol/` | (pas de package racine — shim canonique) | `Version()`, `FramingHeaderSize()` |
| `android/shims/auth/` | (pas de package racine — shim canonique) | `TokenHeaderName()`, `TokenTTLSeconds()`, `TokenRefreshThresholdSeconds()` |
| `android/shims/crypto/` | `internal/crypto` | `Ed25519PublicKeySize()`, `IsValidPublicKeyBase64(s)`, `ReleasePublicKeyCurrentBase64()` |
| `android/shims/registry/` | `internal/registry` | `ExtractCountryCode(id, domain)`, `SupportedCountryCount()` |
| `android/shims/leakcheck/` | `internal/leakcheck` (transitivement `internal/tunnel` + `quic-go`) | `DefaultSTUNServersJoined()`, `BuildBindingRequestSize()` |

**Localisation `android/shims/` (et non `android/internal/`)** : la règle Go `internal/` interdit l'import depuis le package `gobind` que gomobile génère dans son work dir temporaire. Story 9.7 enrichira la surface réelle des shims (handshake QUIC/HTTP3 complet).

#### Pré-requis (à faire UNE FOIS)

1. Go ≥ 1.26 installé (vérifier `go version`).
2. `gomobile` + `gobind` installés :
   ```
   go install golang.org/x/mobile/cmd/gomobile@latest
   go install golang.org/x/mobile/cmd/gobind@latest
   gomobile init    # télécharge l'Android NDK ~1.5 GB (5-15 min)
   ```
3. Dépendance `golang.org/x/mobile` ajoutée au `go.mod` racine (déjà fait Story 9.2 — requis par toolchain gomobile, pas embarqué dans les binaires desktop).

#### Build du `.aar`

Linux/macOS :
```
cd android
bash scripts/build-aar.sh
```

Windows (PowerShell — recommandé) :
```
cd android
pwsh scripts/build-aar.ps1
```

L'artefact produit : `android/app/libs/levoile-core.aar` (gitignoré). Le script affiche en sortie la taille + le SHA256 de l'`.aar`.

Taille typique :
- **Story 9.2** : ~13 MB (constantes pure-data uniquement — la chaîne build gomobile est validée mais aucune logique tunnel exposée).
- **Story 9.7** : ~24 MB (full surface fonctionnelle exposée : `Protocol.{connect,writePacket,close,setPacketCallback,setStatusCallback}` + `Auth.{issueSessionToken,refreshSessionToken,validateSessionToken}` + dépendances transitives `internal/tunnel/{client,pump,reconnect}.go` + `quic-go`/HTTP3 + facade additif `internal/tunnel/gomobile_facade.go`). Les natives libs `libgojni.so` 4 ABIs sont embarquées dans le `.aar`, mais `app/build.gradle.kts` `defaultConfig.ndk.abiFilters` filtre à `arm64-v8a` + `armeabi-v7a` au packaging APK (NFR-AND-3 < 25 MB).

**Note placement** : le `.aar` est consommé directement par `:app` via `implementation(files("libs/levoile-core.aar"))`. AGP interdit qu'un module `android.library` (ici `:levoile-core`) bundle un `.aar` local en dépendance fichier — l'AAR de sortie serait cassée. Le module `:levoile-core` reste comme placeholder pour les futurs wrappers Kotlin idiomatiques (Story 9.7 — `GoCoreAdapter` etc.).

#### Quand re-builder

À chaque modification d'un des 5 shims `android/shims/*` ou des packages racine `internal/{crypto,registry,leakcheck,tunnel}`. Après modification puis `bash scripts/build-aar.sh`, exécuter `./gradlew clean assembleDebug` pour que Gradle recharge le nouveau `.aar` (Gradle ne détecte pas toujours le changement d'un fichier dans `files(...)` sans `clean`).

#### Vérifier la frontière ADR-09 (imports cross-OS)

```
cd android
bash scripts/verify-shared-imports.sh
```

Le script vérifie que les 7 packages partagés (4 shims qui importent `internal/*` + 3 packages racine + `internal/tunnel`) n'importent aucun package OS-spécifique (`internal/tun/`, `internal/firewall/`, `internal/captive/`, `internal/httpproxy/`, etc.). À exécuter avant tout PR touchant aux packages partagés.

#### Smoke test JUnit (Story 9.2)

```
cd android
./gradlew :app:testDebugUnitTest
```

Le test `LeVoileCoreSmokeTest.kt` vérifie que les 5 classes Java générées par gomobile (`fr.plateformeliberte.levoile.core.{protocol.Protocol, auth.Auth, crypto.Crypto, registry.Registry, leakcheck.Leakcheck}`) sont résolvables au compile-time + class-loading runtime JVM, **sans** déclencher le chargement JNI complet (qui requiert un device Android — porté par Story 9.7).

### Lancement de l'app debug (Story 9.3 livrée)

Après `./gradlew assembleDebug` :

```
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.MainActivity
```

L'écran affiche « Le Voile · Démarrage… » + un dot de statut. Le polling JS appelle `window.LeVoile.getStatus()` toutes les 2 s — observable via le DOM (`#status-dot.textContent` passe de `…` à `placeholder`).

#### Inspection via `chrome://inspect` (debug builds uniquement)

`MainActivity.onCreate` appelle `WebView.setWebContentsDebuggingEnabled(true)` **uniquement** en `BuildConfig.DEBUG` (suffixe app id `.debug`, fix M-2 code-review 9.3). Sur un APK debug attaché à un device/émulateur :

1. Brancher le device en USB (ou démarrer un émulateur) — vérifier `adb devices`.
2. Chrome desktop → `chrome://inspect/#devices` → la WebView apparaît sous l'app `fr.plateformeliberte.levoile.debug` → cliquer **inspect**.
3. Console DevTools : `document.body.classList.contains('platform-android')` doit retourner `true` après le premier `onPageFinished`.
4. Pour activer le log verbeux du polling, taper `window.LeVoileDebug = true` dans la console DevTools (par défaut **aucun** `console.log` n'est émis — frontière NFR-AND-8 zéro télémétrie / pollution logcat, fix M-6 code-review 9.3).

**Sur un APK release**, `setWebContentsDebuggingEnabled` n'est jamais appelé : la WebView n'apparaît PAS dans `chrome://inspect`. Comportement intentionnel.

**À ce stade, l'app n'établit pas encore de tunnel** — `LeVoileVpnService` est livré Stories 9.4-9.5, l'intégration `.aar` Story 9.7. Cette story livre uniquement la coquille UI :

- WebView plein écran chargé via `WebViewAssetLoader` (host virtuel `https://appassets.androidplatform.net/` — pas de `file://`).
- Bridge JS↔Kotlin minimal (`window.LeVoile.getStatus()` retourne un JSON placeholder figé).
- Marqueur responsive `body.platform-android` injecté au `onPageFinished` (préparation Story 11.1).
- `configChanges` déclaré dans le manifest pour préserver l'état WebView sur rotation portrait↔paysage.

Les assets HTML/CSS/JS (`app/src/main/assets/{index.html,style.css,app.js}`) sont **manuscrits committés** — pas de sync depuis `frontend/` racine dans cette story (Story 11.1 livrera `sync-frontend.sh` qui les remplacera).

### Capture L3 via VpnService (Story 9.4 livrée)

Le service `LeVoileVpnService` (sous `app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/`)
crée l'interface TUN Android via `VpnService.Builder.establish()` et démarre
les threads de pump paquets IP. À ce stade :

- **PacketRelay** est stubbé (`NoOpPacketRelay`) — les paquets sortants sont droppés
  en silence, aucun paquet entrant n'est jamais reçu. Story 9.7 le réécrira pour
  pousser vers le noyau Go partagé via `GoCoreAdapter` (handshake QUIC/HTTP3 +
  stream `/tunnel`).
- **Foreground Service lifecycle** : `startForeground(NOTIF_ID, ...)` est appelé
  en moins de 5 s avec une notification stub minimaliste (channel
  `levoile_vpn_status_stub`). Story 9.6 livrera la notification finale (titre
  dynamique pays/IP, action « Déconnecter », channel `levoile_vpn_status`).
- **MainActivity** ne déclenche pas encore `VpnService.prepare()`. Story 9.5
  branchera l'orchestration complète UI ↔ Service via Intents `ACTION_CONNECT`
  / `ACTION_DISCONNECT`.

Test manuel post-9.4 (sans 9.5/9.7), nécessite émulateur ou device :

```
# 1. Installer l'APK debug
adb install -r app/build/outputs/apk/debug/app-debug.apk

# 2. Accepter le consent VpnService manuellement (avant Story 9.5, ouvrir
#    Réglages → VPN → Le Voile → Activer ; ou via une activité de debug
#    locale qui invoque VpnService.prepare()).

# 3. Démarrer le service (action ACTION_CONNECT)
adb shell am start-foreground-service \
  -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
  -a fr.plateformeliberte.levoile.action.CONNECT

# 4. Vérifier l'interface TUN créée + le service Foreground actif
adb shell ip link show | grep tun
adb shell dumpsys activity services | grep -A 20 LeVoileVpnService

# 5. Stopper le service (action ACTION_DISCONNECT)
adb shell am start-foreground-service \
  -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
  -a fr.plateformeliberte.levoile.action.DISCONNECT
```

**À ce stade, l'app n'établit pas encore de tunnel chiffré** —
`LeVoileVpnService` crée la TUN et démarre les pumps mais ne relaie aucun
paquet (`NoOpPacketRelay`). Le tunnel chiffré QUIC/HTTP3 vers les relais est
livré Story 9.7.

### Lifecycle Foreground Service + OEM agressifs (Story 9.5 livrée)

Le service `LeVoileVpnService` tourne en Foreground Service (notification persistante non-dismissable, channel `levoile_vpn_status` `IMPORTANCE_LOW` livré Story 9.6) et est exempt du Doze mode Android 8+. Sur la majorité des devices Android stock (Pixel, Samsung One UI récent), le tunnel reste actif quand l'écran s'éteint, même plusieurs heures, sans intervention utilisateur supplémentaire.

#### Action « Déconnecter » de la notification

Tap sur l'action « Déconnecter » de la notification persistante :

1. Déclenche un `PendingIntent.getService(... FLAG_IMMUTABLE)` ciblant `LeVoileVpnService.ACTION_DISCONNECT` (livré Story 9.6 — `NotificationHelper.buildDisconnectAction()`).
2. Le service appelle `disconnectInternal()` → `cleanupSync()` qui arrête les pumps, ferme le `ParcelFileDescriptor`, libère l'interface TUN, notifie `VpnState.DISCONNECTED` à la notif (visible le temps d'un repaint).
3. **Délai de 5 secondes** (`STOP_FOREGROUND_DELAY_MS`) pendant lesquelles la notification reste affichée — UX cohérente : laisse le temps à l'utilisatrice de voir le retrait progressif (le titre passe de « Le Voile · Connecte » à « Le Voile · Deconnecte ») et évite un clignotement notif présente/absente sur des reconnects rapides.
4. Au terme du délai, `stopForeground(STOP_FOREGROUND_REMOVE)` retire la notification, puis `stopSelf()` termine le service. Android GC l'instance.

Idempotence : si un second tap « Déconnecter » survient pendant les 5 s, `disconnectInternal()` annule le runnable précédent (`Handler.removeCallbacksAndMessages(null)`) avant d'en re-poster un — pas de double `stopSelf()` sur Service en cours d'arrêt.

Garde-fou `onDestroy()` : si Android détruit le Service pendant les 5 s (ex. force-stop par l'utilisatrice via Réglages → Apps), `onDestroy()` annule le runnable + appelle `cleanupSync()` + `stopForeground` synchronement — évite la race « stopForeground sur Service détruit » (UB Android).

#### Singleton Service

`LeVoileVpnService.instance` (companion `@Volatile internal var`) est assigné dans `onCreate` et libéré dans `onDestroy`. Android garantit déjà au plus une instance active de Service par classe dans un même process — ce singleton est exposé pour les diagnostics introspectifs et un câblage futur Story 11.2 si nécessaire (le pattern `Intent.action` reste préféré pour les commandes Connect/Disconnect, cf. architecture.md l. 134 « pas d'IPC interne »).

#### Helpers `MainActivity.requestVpnStart` / `requestVpnStop` (dormants — Story 11.2)

`MainActivity` expose deux helpers `internal` qui orchestrent le flow consent VpnService + démarrage service :

```
requestVpnStart(country: String? = null) :
    1. VpnService.prepare(this) → si non null, lance le popup système consent
       via vpnConsentLauncher (ActivityResultLauncher)
    2. Au retour RESULT_OK : startForegroundService(ACTION_CONNECT + EXTRA_COUNTRY)

requestVpnStop() :
    startForegroundService(ACTION_DISCONNECT)
```

Ces helpers sont **DORMANTS** dans le scope Story 9.5 — aucun appelant côté UI. **Story 11.2** câblera depuis `LeVoileBridge.connect(country)` / `LeVoileBridge.disconnect()` (méthodes `@JavascriptInterface` qui castent leur Context en MainActivity et invoquent ces helpers).

Pour tester manuellement avant Story 11.2 :

```bash
# Connect (consent à accorder manuellement la 1re fois)
adb shell am start-foreground-service \
  -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
  -a fr.plateformeliberte.levoile.action.CONNECT

# Disconnect — la notif disparaîtra après 5 s
adb shell am start-foreground-service \
  -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
  -a fr.plateformeliberte.levoile.action.DISCONNECT
```

#### OEM agressifs (Xiaomi / Huawei / Oppo / Vivo)

Sur stock Android (Pixel, Samsung One UI), le Foreground Service VPN tourne en arrière-plan sans souci grâce à l'exemption Doze native + le pattern `foregroundServiceType="specialUse"` (Story 9.4) qui signale un usage long-running légitime au scheduler OS.

**Cas problématiques** : Xiaomi (MIUI), Huawei (EMUI/HarmonyOS), Oppo/Realme (ColorOS), Vivo (FuntouchOS) appliquent des heuristiques propriétaires de battery save qui peuvent **tuer même les Foreground Services** quand l'écran reste éteint plusieurs minutes. Symptôme : la notification disparaît pendant la nuit, le tunnel se ferme silencieusement, l'utilisatrice se retrouve sans VPN au matin.

**Recommandation utilisateur** (à documenter dans l'onboarding Stories 11.5/11.6, éventuellement détecté + bandeau d'avertissement Story 10.2 si OEM agressif identifié via `Build.MANUFACTURER`) :

| OEM | Chemin Settings (libellés FR — peuvent varier selon ROM/version) |
|---|---|
| **Xiaomi (MIUI)** | Réglages → Apps → Le Voile → Apps en arrière-plan auto-start ✓. Réglages → Batterie → Économiseur de batterie → Le Voile → Aucune restriction. (Optionnel : désactiver « MIUI Optimization » dans Options développeur pour stabilité maximum.) |
| **Huawei (EMUI/HarmonyOS)** | Réglages → Apps → Lancement → Le Voile → Manage manually → toggles Auto-launch ✓ + Secondary launch ✓ + Run in background ✓. |
| **Oppo / Realme (ColorOS)** | Réglages → Batterie → Optimisation batterie → Le Voile → Pas d'optimisation. + Réglages → Privacy permissions → Le Voile → Permettre exécution en arrière-plan ✓. |
| **Vivo (FuntouchOS)** | i Manager → Battery → High Background Power Consumption → Le Voile → Allow ✓. |
| **Pixel / Samsung / autres stock Android** | Réglages → Apps → Le Voile → Batterie → Sans restriction. (Pas obligatoire en pratique sur stock — Foreground Service VPN exempté nativement Android 12+.) |

**Pourquoi Le Voile ne le fait pas automatiquement ?** L'API publique `Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` (NFR-AND-7) permet de _demander_ à l'utilisatrice l'exemption Doze, mais ne couvre que la partie standard Android. Les heuristiques OEM-spécifiques au-delà du Doze ne sont contrôlables que par l'utilisatrice dans des écrans spécifiques à chaque ROM (intentionnel — Google n'autorise pas l'app à les manipuler programmatiquement). Story 10.x / 11.x ajoutera potentiellement un detect heuristique + bandeau dédié si on identifie au runtime un device d'OEM agressif (`Build.MANUFACTURER` matche `xiaomi|huawei|honor|oppo|realme|vivo`).

### Notification persistante MVP (Story 9.6 livrée)

Le `NotificationHelper` (sous `app/src/main/kotlin/fr/plateformeliberte/levoile/ui/`)
centralise la création de la notification ongoing affichée par
`LeVoileVpnService` lors de `startForeground(...)`. À ce stade :

- **Channel** : `levoile_vpn_status` (`IMPORTANCE_LOW` — silencieux, pas de
  heads-up). Le channel stub `levoile_vpn_status_stub` livré Story 9.4 est
  supprimé automatiquement par `NotificationHelper.ensureChannel()` au
  premier démarrage post-9.6 (idempotent — silencieux pour les nouveaux
  installs).
- **Titre** : « Le Voile · {État} » avec {État} ∈ {Connecté, Reconnexion…,
  Déconnecté, Erreur} (cf. enum `VpnState` sous `ui/`).
- **Sous-texte** : vide (l'enrichissement pays + IP visible arrive
  Story 11.7 — voir EBR-01 dans `epics.md`).
- **Action « Déconnecter »** : `PendingIntent.getService(... FLAG_IMMUTABLE)`
  → `LeVoileVpnService.ACTION_DISCONNECT` (livré Story 9.4).
- **Tap sur le corps** : `PendingIntent.getActivity(... FLAG_IMMUTABLE)` →
  `MainActivity` (livrée Story 9.3, `FLAG_ACTIVITY_SINGLE_TOP` pour préserver
  l'état WebView).
- **Icône** : `R.drawable.ic_levoile_status` (vector mono-couleur 24dp,
  reteinté via `setColor(R.color.primary_blue)` du builder).

Test manuel post-9.6 (sans Story 9.7 livrée), nécessite émulateur ou device :

```
# 1. Installer l'APK debug
adb install -r app/build/outputs/apk/debug/app-debug.apk

# 2. (Android 13+) Accorder POST_NOTIFICATIONS manuellement :
#    Settings → Apps → Le Voile → Notifications → Activer
#    (la demande runtime appartient à Story 11.5 onboarding).

# 3. Démarrer le service (consent VpnService prérequis — voir Story 9.4)
adb shell am start-foreground-service \
  -n fr.plateformeliberte.levoile.debug/fr.plateformeliberte.levoile.vpn.LeVoileVpnService \
  -a fr.plateformeliberte.levoile.action.CONNECT

# 4. Vérifier la notif persistante
#    - Barre de statut : icône cadenas + titre « Le Voile · Connecté »
#    - Pull-down panel : action « DÉCONNECTER » visible
#    - Tap sur l'action → service s'arrête, notif disparaît brièvement
#      (état DISCONNECTED affiché le temps d'un repaint avant retrait)
#    - Tap sur le corps → MainActivity revient au premier plan

# 5. Vérifier le channel via UI Settings
#    Settings → Apps → Le Voile → Notifications
#    → un seul channel « Statut Le Voile » (le stub ne doit plus apparaître)
```

**À ce stade, l'état affiché reste statique « Connecté » dès que le service
démarre** — le câblage temps-réel de l'état vers la notif (passage par
`RECONNECTING` puis `CONNECTED` lors d'un vrai handshake QUIC/HTTP3, `ERROR`
sur échec) est livré Story 9.7 (intégration noyau Go via `.aar`).
L'enrichissement pays + IP visible dans le sous-texte est livré Story 11.7.

#### Note pattern Foreground Service `specialUse` + subtype `vpn`

Android 14 (API 34) requiert un `foregroundServiceType` déclaré dans le
manifest pour tout Foreground Service. Le SDK build-tools 34.0.0 N'accepte
PAS la valeur littérale `"vpn"` dans l'enum `foregroundServiceType` (réservée
au runtime via `setForegroundServiceType` + `ServiceInfo.FOREGROUND_SERVICE_TYPE_VPN`).

Pattern retenu (cohérent architecture.md l. 664-666) :

```xml
<service
    android:name=".vpn.LeVoileVpnService"
    android:permission="android.permission.BIND_VPN_SERVICE"
    android:foregroundServiceType="specialUse"
    android:exported="false">
    <intent-filter>
        <action android:name="android.net.VpnService" />
    </intent-filter>
    <property
        android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"
        android:value="vpn" />
</service>
```

La permission applicative correspondante est `FOREGROUND_SERVICE_SPECIAL_USE`
(remplace l'ancien commentaire Story 9.1 qui annonçait `dataSync`).
`BIND_VPN_SERVICE` reste le verrou sécurité système attaché au tag `<service>`.

### Story 9.7 livrée — surface noyau Go exposée à Kotlin

Le `.aar` produit par `bash scripts/build-aar.sh` (ou `pwsh scripts/build-aar.ps1`)
expose désormais la **surface fonctionnelle complète** au-dessus du noyau Go
partagé (handshake QUIC/HTTP3 + pump IP via `internal/tunnel/` racine, exposé
via le facade additif `internal/tunnel/gomobile_facade.go`) :

- `fr.plateformeliberte.levoile.core.protocol.Protocol` :
  - `connect(relayDomain, pinnedKeyB64)` — handshake QUIC/HTTP3 + pinning
    Ed25519 + obtention session token via `/verify`. Démarre la goroutine
    de pump bidirectionnel (paquets IP ↔ stream HTTP/3 `/tunnel`).
  - `writePacket(packet)` — encapsulation framing 2 octets BE + écriture
    sur stream `/tunnel`. Back-pressure : drop silencieux si file pleine
    (TCP/QUIC retransmettent).
  - `close()` — fermeture session QUIC + arrêt pompe. Idempotent.
  - `setPacketCallback(cb)` — callback Go → Kotlin pour paquets entrants.
  - `setStatusCallback(cb)` — callback Go → Kotlin pour transitions d'état
    (`connecting` / `connected` / `disconnected` / `error`).
  - `isSessionOpen()` — diagnostic.

- `fr.plateformeliberte.levoile.core.auth.Auth` :
  - `issueSessionToken(relayDomain, relayPubKeyB64)` — émet token Ed25519
    (TTL 4h) sans démarrer la pompe. Réutilise le token courant si une
    session globale est déjà ouverte sur le même relais.
  - `refreshSessionToken()` — refresh proactif via `Client.RefreshSessionToken`
    (single-flight + backoff exponentiel + circuit breaker — héritage desktop).
  - `validateSessionToken(token)` — guard rapide TTL + match avec session
    active (la validation Ed25519 / IP hash réelle est faite par le relais à
    chaque requête `/tunnel`).

Côté Kotlin, le **seul point d'entrée** est le singleton
[`GoCoreAdapter`](app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/GoCoreAdapter.kt) :

- Toutes les méthodes JNI bloquantes sont enveloppées en `suspend fun` +
  `withContext(Dispatchers.IO)` (cf. architecture.md l. 1213-1214).
- Un `Mutex` Coroutines sérialise les mutateurs (`connect`/`disconnect`/
  `writePacket`) — gomobile n'est pas thread-safe sur les méthodes
  mutatrices d'une même session.
- Les exceptions Java propagées par gomobile sont attrapées et
  re-encapsulées en `LeVoileCoreException` — **aucun type gomobile généré
  ne fuit hors de cet objet** (frontière étroite, architecture l. 1059-1060).
- Vérification : `grep -r "fr.plateformeliberte.levoile.core" app/src/main/kotlin/`
  doit lister UNIQUEMENT `GoCoreAdapter.kt`.

L'implémentation [`GoBackedPacketRelay`](app/src/main/kotlin/fr/plateformeliberte/levoile/vpn/GoBackedPacketRelay.kt)
livrée en parallèle est l'adapteur prêt à brancher dans `LeVoileVpnService`
(remplace `NoOpPacketRelay` Story 9.4) : pompe out (Kotlin → Go) via
`GoCoreAdapter.writePacket(...)`, pompe in (Go → Kotlin) via
`PacketCallback` qui enqueue dans le `packetSink` du Service. La bascule
réelle dans `LeVoileVpnService.packetRelay` est tracée Story 9.5
(injection DI + lifecycle complet UI ↔ Service).

**À ce stade**, `GoCoreAdapter.connect(relayDomain, pinnedKey)` est
fonctionnellement complet — il établirait réellement un tunnel s'il était
appelé. Mais aucune classe n'invoque encore cette méthode (c'est Story 9.5
qui branchera UI → Service → adapter). Test fonctionnel complet sur
émulateur API 29/33/34 = Story 12.6 (matrice Espresso instrumentée).

#### Régression desktop : zéro

Le build desktop (`go build ./...` + `go test ./internal/tunnel/...`) reste
intact. Les modifications côté Go se limitent à un nouveau fichier
[`internal/tunnel/gomobile_facade.go`](../internal/tunnel/gomobile_facade.go)
qui contient **uniquement** des fonctions wrapper additives + son fichier
de tests `gomobile_facade_test.go`. Vérifier via
`git diff internal/tunnel/{client,pump,types,state,reconnect}.go` qui doit
être vide. Pattern facade strict — cohérent feedback `os_isolation`.

#### Filtres ABI APK release (NFR-AND-3 < 25 MB)

Le `.aar` Story 9.7 embarque les natives libs `libgojni.so` pour 4 ABIs
(x86, x86_64, armeabi-v7a, arm64-v8a) — sans filtre, l'APK release pèse
~47 MB (hors NFR). `app/build.gradle.kts` `defaultConfig.ndk.abiFilters`
limite à `arm64-v8a` + `armeabi-v7a` (≈99% du parc Android 2026), descendant
l'APK à ~23 MB (sous le seuil NFR). Marché Chromebook x86 négligeable +
émulation native ARM via Houdini/native bridge couvre les corner cases.

### Sync frontend desktop (Story 11.1 à venir)

```bash
bash scripts/sync-frontend.sh    # Copie ../frontend/ → app/src/main/assets/
```

## Permissions (NFR-AND-7)

Liste stricte déclarée dans `app/src/main/AndroidManifest.xml` :

- `INTERNET`
- `FOREGROUND_SERVICE`
- `FOREGROUND_SERVICE_DATA_SYNC` (API 34+) — conservée pour cohabitation avec un futur `WorkManager` (auto-update check 24h Story 12.5)
- `FOREGROUND_SERVICE_SPECIAL_USE` (API 34+) — Story 9.4, requise par `foregroundServiceType="specialUse"` du `LeVoileVpnService`
- `POST_NOTIFICATIONS` (API 33+)

Permission injectée automatiquement par AGP 8+ (custom, surface d'attaque nulle, invisible utilisateur) :

- `fr.plateformeliberte.levoile[.debug].DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION`

`BIND_VPN_SERVICE` n'est pas une `<uses-permission>` mais est attachée au tag `<service>` de `LeVoileVpnService` (Story 9.4 livrée).

## Notes

- **Icône launcher** : placeholder vectoriel (« LV ») dans `app/src/main/res/{mipmap-anydpi-v26,drawable}/`. Icône finale produite par Story 12.x.
- **Aucun fichier hors `android/` ne doit être modifié par les stories Android.** Les seuls liens autorisés sortant : (a) `gomobile bind` lit `internal/{crypto,registry,leakcheck,tunnel}` racine **sans modifier**, (b) `sync-frontend.sh` lit `frontend/` racine **sans modifier**.
- **Exception calculée ADR-08 : `go.mod` + `go.sum` racine.** Story 9.2 a ajouté `golang.org/x/mobile` (annoté `// indirect`) au `go.mod` racine — requis par le toolchain `gomobile bind`, sans ça aucun build Android possible. Cette modif a entraîné des bumps transitifs sur `golang.org/x/{crypto,net,sys,text,mod,tools}` (patches stables, sans breaking change). Régression desktop validée : `go test ./internal/...` (9/9 packages OK) + `go vet ./...` (racine + `windows/` + `linux/` + `relay/`) restent verts. Détails complets dans la file de Story 9.2 (`_bmad-output/implementation-artifacts/9-2-script-build-aar-sh-gomobile-bind-du-noyau-go-partage.md`, voir §Completion Notes point 6 et File List). Architecture : voir `architecture.md` §Patterns OS-isolation → "Exceptions ADR-08 connues".
- **Aucune télémétrie / crash reporter / analytics** (NFR-AND-8). Story 10.4 ajoutera l'audit Gradle CI bloquant.
