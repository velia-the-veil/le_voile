---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
lastStep: 8
status: 'revised'
completedAt: '2026-03-08'
rewrittenAt: '2026-04-29'
rewrittenReason: 'Phase Android — extension du support à Android 10+ (API 29+) en parallèle de Windows + Linux. Stratégie : isolation OS maximale, duplication assumée — chaque OS a son arbre dédié. Seul le strict noyau protocole/crypto/registre/session est partagé via gomobile (compilé en .aar pour Android, importé en Go pour desktop). Tout le reste est dupliqué : capture (TUN/Wintun → VpnService Android), kill switch (WFP/nftables → réglage OS "VPN permanent" Android), service runner (kardianos → Foreground Service Android), UI host (webview/webview + fyne.io/systray → Android WebView + notification persistante), packaging (NSIS/nfpm → Gradle/AAB), distribution (GitHub releases + AUR → F-Droid + APK direct, pas de Google Play en MVP). Décisions transport (QUIC/HTTP3 sur 443 direct au relais), crypto (Ed25519 par relais + master key), registre signé, session tokens IP-bindés, anti-DPI inchangées. Stack relais zéro modification. Cible RAM Android < 60 MB (vs < 25 MB desktop) — overhead JVM + WebView accepté.'
previousRewrittenAt: '2026-04-19'
previousRewrittenReason: 'Pivot transport : retrait du fronting Cloudflare (incompatible ToS §2.8 non-HTML + origin-IP de toute façon exposée en NAT sortant). Le relais est désormais joint directement par DNS A record → VPS origin. Le handshake TLS + HTTP/3 camouflage DPI sans intermédiaire CDN. Session tokens bindés sur r.RemoteAddr au lieu de CF-Connecting-IP.'
inputDocuments: ['prd.md', 'prd-validation-report.md', 'codebase analysis 2026-04-02', 'architecture.md (2026-04-19 revision)']
workflowType: 'architecture'
project_name: 'bmad_vpn_le_voile_de_velia'
user_name: 'Akerimus'
date: '2026-04-29'
snapshot_ref: 'windows-stable-2026-04-15 (git tag) + backup/windows-stable (branch) ; pre-android-2026-04-29 (git tag) recommandé avant démarrage implémentation Android'
platforms_supported: ['Windows 10/11', 'Debian 11+/Ubuntu 22.04+', 'Fedora 38+/RHEL 9+', 'Arch Linux rolling', 'Alpine 3.18+', 'Android 10+ (API 29+)']
os_isolation_strategy: 'maximale ; duplication code par OS préférée à abstraction cross-OS partagée'
---

# Architecture Decision Document

_Ce document reflète l'architecture cible au 29 avril 2026 — après ajout du support **Android 10+ (API 29+)** en parallèle de Windows et Linux. Principe directeur : **isolation OS maximale** — chaque OS a son arbre de code dédié, la duplication est assumée et préférée à des abstractions cross-OS partagées. Seul le strict noyau protocole/crypto/registre/session est mutualisé (importé natif sur desktop, compilé en `.aar` via gomobile pour Android). Toutes les couches d'intégration OS sont indépendantes._

_Sur Android : `android.net.VpnService` remplace TUN/Wintun, le kill switch est délégué au réglage OS "VPN permanent + bloquer connexions sans VPN" (pas de WFP/nftables app-level — Android n'expose pas d'API firewall équivalente côté app), un Foreground Service avec notification persistante remplace `kardianos/service`, Android WebView remplace `webview/webview`, F-Droid + APK direct est l'unique canal de distribution MVP (pas de Google Play). Charte plateformeliberte.fr conservée via réutilisation des assets HTML/CSS/JS frontend dans la WebView Android._

_Sur Windows + Linux : aucune décision modifiée par cette révision. L'arbre desktop existant est intouché, l'arbre `android/` est ajouté en parallèle._

_Snapshot de l'état Windows-only initial : git tag `windows-stable-2026-04-15`, branche `backup/windows-stable`, binaires archivés dans `_snapshots/bmad_vpn_le_voile_windows-stable-2026-04-15/`. Snapshot pré-Android recommandé : tag `pre-android-2026-04-29` avant démarrage implémentation Android._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
36 FRs organisés en 10 domaines (FR37-40 extension navigateur retirés — capture L3 machine-wide rend l'extension redondante). Lecture par OS : chaque FR est mappé desktop (Windows + Linux) et Android avec adaptations OS spécifiées.
- **Tunnel & Connexion (FR1-4)** — Établissement QUIC/HTTPS direct au relais (DNS A record → VPS origin, sans CDN intermédiaire), reconnexion auto avec backoff exponentiel, authentification Ed25519 par relais, session tokens signés (TTL 4h) avec circuit breaker. **Identique sur les 3 OS** (logique fournie par le noyau Go partagé via gomobile sur Android)
- **Capture Trafic L3 (FR5-8 révisés + FR27-30 révisés)** — Encapsulation IP brut tunnelisée via QUIC/HTTP3 vers relais (relais = gateway NAT), DNS résolu côté relais, rate limiting par IP côté relais (200 max), bandwidth limiting par IP (quota journalier), protection SSRF, blocklist DNS appliquée côté relais (StevenBlack/hosts). **Capture par OS** : Wintun (Windows), TUN /dev/net/tun (Linux), `android.net.VpnService` (Android — file descriptor remis au noyau Go via JNI/gomobile). **Kill switch par OS** : WFP (Windows), nftables (Linux), réglage OS "VPN permanent + bloquer connexions sans VPN" (Android — pas d'API firewall app-level, le verrou est OS-natif)
- **Protection Anti-Fuite (FR31-34)** — Détection fuites WebRTC via STUN Binding Requests (RFC 5389) — la capture L3 rend les fuites structurellement impossibles, le check est une validation. Identique cross-OS, exécuté côté noyau Go partagé
- **Interface Utilisateur (FR9-13b)** — Charte plateformeliberte.fr, sélecteur de pays avec drapeaux. **UI host par OS** : binaire desktop unique (`levoile-ui`) combinant `webview/webview` (fenêtre 420×540px) + `fyne.io/systray` (Windows + Linux), Activité Android avec WebView Android plein-écran chargeant les mêmes assets HTML/CSS/JS (Android). Notification persistante du Foreground Service joue le rôle du tray (Android — pas de tray sur mobile)
- **Démarrage & Lifecycle (FR14-16)** — **Service par OS** : Windows SCM via kardianos/service, systemd Linux via kardianos/service, **Android Foreground Service** (classe Kotlin `LeVoileVpnService extends VpnService`) avec notification ongoing obligatoire (Android 8+) et exemption Battery Optimization demandée
- **Relais Multi-VPS (FR17-19b)** — Relais HTTP/3 stateless avec endpoint de tunneling IP (ingress/egress), bandwidth limiting, organisés par pays, registre distribué signé Ed25519. **Stack relais inchangée — zéro modification côté serveur pour le support Android**
- **Découverte & Sélection (FR23-26)** — Registre distribué signé (/.well-known/relay-registry.json), sélection par pays, failover automatique, latency measurement via /health. **Identique sur les 3 OS** (logique fournie par le noyau Go partagé)
- **Distribution (FR20-22)** — **Par OS** : NSIS (Windows), GoReleaser + nfpm `.deb`/`.rpm`/`.apk` + AUR (Linux), **F-Droid metadata + APK direct GitHub release (Android — pas de Google Play en MVP)**. Build Android via Gradle/AGP : `.aar` du noyau Go (gomobile bind) → APK signé v2/v3 + AAB optionnel pour future publication Play
- **Mise à jour (FR35-36)** — **Par OS** : auto-update GitHub releases pour desktop (vérification périodique 6h, signature Ed25519, rollback). **Android : pas d'auto-update intégré** — F-Droid gère les mises à jour côté store, les utilisateurs APK direct doivent vérifier manuellement (lien GitHub releases dans l'UI). Phase 2 envisageable : in-app update via API F-Droid ou check manuel + notif
- **~~Extension Navigateur (FR37-40)~~** — **SUPPRIMÉ** — Rendu redondant par la capture L3. Sur Android, la VpnService capture tout le trafic IP système sans configuration applicative — même cohérence que desktop

**Non-Functional Requirements:**
20 NFRs répartis en 4 axes :
- **Sécurité (NFR1-9)** — TLS 1.3 min, Ed25519 par relais + registre signé master key, zero persistence, résistance DPI (QUIC/HTTP/3 indistinguable d'un HTTPS navigateur), zero fuite DNS/WebRTC (capture L3 machine-wide), kill switch OS-level (nftables/WFP/réglage Android "VPN permanent"), protection SSRF, session token bindé à l'IP client (SHA256(r.RemoteAddr)), code auditable. **Sur Android : kill switch délégué à l'OS** — l'app ne peut pas installer de règles iptables/eBPF sans root, mais l'OS Android offre un toggle "VPN permanent + bloquer connexions sans VPN" qui est un kill switch kernel-level équivalent (et inviolable côté app)
- **Performance (NFR10-14)** — Latence DNS < 50ms (résolution côté relais via upstream proche), tunnel < 3s, reconnexion avec backoff (100ms → 30s), CPU < 1%. **RAM par cible** : `< 25 MB` desktop (Windows/Linux), **`< 60 MB` Android** (overhead JVM + WebView + AndroidX inévitable, conforme aux apps VPN Android standards)
- **Fiabilité (NFR15-18)** — Kill switch via règles firewall (indépendant du process client, persiste en cas de crash) ou via réglage OS Android (idem, kernel-level), watchdog TUN/VpnService interface 3s, crash-recovery au redémarrage service, failover transparent multi-relais. **Sur Android : `onRevoke()` + `onDestroy()` du `VpnService` doivent restaurer un état propre, le redémarrage est géré par `START_REDELIVER_INTENT` du Foreground Service**
- **Confidentialité (NFR19-20)** — Aucun log IP client (ni relais ni client), IP hash uniquement dans session tokens. Identique cross-OS

**Scale & Complexity:**
- Domaine principal : Application desktop + mobile + serveur réseau (cybersécurité/vie privée)
- Complexité : Élevée — protocoles réseau spécialisés, cryptographie, intégration OS profonde (TUN/Wintun/VpnService + firewall nftables/WFP/réglage Android), multi-relais, IPC multi-processus desktop, JNI bridge Android, encapsulation IP. **Triplication assumée** des couches d'intégration OS
- Composants architecturaux desktop : ~18 packages `internal/` (Go) — ajout `internal/tun/`, `internal/firewall/`, `internal/routing/` lors de la révision 2026-04-15
- **Composants Android** : module Kotlin/AndroidX (`android/app/src/main/`) avec `LeVoileVpnService`, `MainActivity` (héberge la WebView), `LeVoileBridge` (JNI vers `.aar`), `NotificationHelper`, `BootReceiver` (auto-start) ; un module Gradle séparé `android/levoile-core/` qui héberge l'`.aar` produit par gomobile depuis les packages Go partagés
- **Code Go partagé via gomobile** (frontière étroite, exposée à Android via `.aar`) : 5 shims gomobile-compatibles vivant sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` (cf. erratum Story 9.2 ci-dessous) qui ré-exposent une surface minimale en types primitifs. Les shims `protocol/` et `auth/` sont **canoniques** (pas de package racine équivalent — la logique correspondante vit dans `internal/tunnel/{pump.go,client.go}` côté desktop). Les shims `crypto/`, `registry/`, `leakcheck/` consomment en lecture les packages racine `internal/{crypto,registry,leakcheck,tunnel}` (en transitif). **Tout le reste reste desktop-only** — pas d'abstraction `Capture`/`Firewall`/`Service` partagée, chaque OS a sa propre implémentation complète et autonome
- Plateformes cibles : Windows 10/11, Debian 11+/Ubuntu 22.04+, Fedora 38+/RHEL 9+, Arch Linux rolling, Alpine 3.18+, **Android 10+ (API 29+ — couvre ~80% du parc actif, VpnService stable, Scoped Storage par défaut, TLS 1.3 garanti par OS)**
- **Hors scope explicite (Phase 1)** : iOS (effort App Store + Network Extension distinct, à reconsidérer Phase 3+), Android TV / Wear OS / Auto, Android < 10 (API < 29), macOS (Phase 2 si demande)

### Technical Constraints & Dependencies

#### Communs (noyau partagé via gomobile sur Android, natif sur desktop)

- **Go 1.26** — Langage du noyau (binaire desktop + `.aar` Android via gomobile). Bibliothèques crypto standard
- **quic-go** (v0.59.0) — Implémentation QUIC production-ready, HTTP/3 + TLS 1.3. Compatible gomobile (testé en production sur plusieurs apps Android)
- **BurntSushi/toml** (v1.5.0) — Configuration TOML (desktop). Sur Android : config exposée via SharedPreferences + JSON sérialisé, **pas de TOML côté Android** (parser Go reste utilisable mais non utilisé pour préfs runtime — les préfs Android sont gérées en Kotlin)
- **Cloudflare DoH** — Resolver DNS (1.1.1.1) pour bootstrap de résolution du domaine relais, avec fallback Quad9 (9.9.9.9). **Pas utilisé comme CDN fronting** — le relais est joint en direct (DNS A record → VPS). Identique cross-OS
- **Ed25519** — Algorithme d'authentification par relais + signature du registre par master key. Identique cross-OS
- **Multi-VPS** — Relais répartis géographiquement par pays, scalables horizontalement. Identique cross-OS
- **Capture L3 universelle** — Contrainte transverse : tous les OS doivent créer une interface virtuelle et router le trafic IP via elle. Implémentation par OS (voir sous-sections)
- **Développeur unique + IA** — Contrainte ressource forte, architecture doit rester simple — décision: **modèle gateway NAT côté relais** + **isolation OS maximale** (duplication code par OS plutôt qu'abstractions cross-OS partagées)

#### Windows-only

- **webview/webview** — Fenêtre desktop native utilisant WebView2. Plus léger que Wails v2
- **fyne.io/systray** (v1.12.0) — Icône system tray pur Go (pas de CGo)
- **kardianos/service** (v1.2.4) — Service Windows SCM
- **Architecture 2 processus** — Service (background, LocalSystem) + UI (tray + webview, user) communiquant via IPC named pipes
- **Microsoft/go-winio** (v0.6.2) — Named pipes Windows pour IPC
- **Bibliothèque TUN** — `golang.zx2c4.com/wireguard/tun` (Wintun via DLL signée Microsoft, embarquée dans le binaire service via `//go:embed`)
- **Firewall Windows** — Windows Filtering Platform (WFP) via API native `Fwpm*`
- **Wintun** — DLL signée Microsoft (distribuée avec WireGuard), embarquée dans le binaire service Windows et extraite au premier démarrage
- **Privilèges Windows** — Service SCM tourne en LocalSystem (tous privilèges réseau natifs)
- **NSIS** — Installeur Windows (installation service, tray autostart, shortcuts, Wintun DLL)

#### Linux-only

- **webview/webview** — Fenêtre desktop native utilisant WebKitGTK 6.0
- **fyne.io/systray** (v1.12.0) — Linux : dépend de `libayatana-appindicator3` (présent sur GNOME/KDE/XFCE via paquets standards). CGo accepté
- **kardianos/service** (v1.2.4) — Service systemd Linux
- **Architecture 2 processus** — Service (background, user `levoile` + capabilities) + UI (tray + webview, user) communiquant via IPC Unix sockets `/run/levoile/ipc.sock`
- **Bibliothèque TUN** — `golang.zx2c4.com/wireguard/tun` (TUN Linux via `/dev/net/tun`)
- **Firewall Linux** — `nftables` via shellout binaire `nft` (détection au démarrage : échec hard si absent, message clair "nftables required, install libnftables"). Pas de fallback iptables (dette technique refusée)
- **Capabilities Linux** — Binaire service requiert `CAP_NET_ADMIN` (création TUN) + `CAP_NET_RAW` (firewall). **Capabilities fournies via systemd `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW`** dans le unit file `levoile.service`, avec `User=levoile`. Pas de setcap sur le binaire
- **Stack TCP/IP userspace** — `gvisor.dev/gvisor/pkg/tcpip` (netstack) considéré puis rejeté — gateway NAT côté relais préféré (simplicité)
- **GoReleaser v2 + nfpm** — Build cross-compilation + packaging `.deb`/`.rpm`/`.apk` unifié. AUR via repo séparé (PKGBUILD manuel + `aur-publish` action)

#### Android-only (NOUVEAU)

- **Android 10+ (API 29+)** — Cible minimale. Couvre ~80% du parc actif, VpnService stable, Scoped Storage par défaut, TLS 1.3 garanti par OS, restrictions background relâchées pour Foreground Services VPN
- **Kotlin 1.9+** — Langage côté Android. Pas de Java directe (Kotlin uniquement, plus concis et nullsafe)
- **AndroidX** — Bibliothèques modernes officielles : `androidx.core`, `androidx.appcompat`, `androidx.webkit`, `androidx.activity`, `androidx.lifecycle`. Pas de `android.support.*` (legacy)
- **Android Gradle Plugin (AGP) 8.x** + **Gradle 8.x** — Build system
- **gomobile** (`golang.org/x/mobile/cmd/gomobile`) — Compile les packages Go partagés en `.aar` consommable par Gradle. **Frontière étroite** : 5 shims gomobile sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` ré-exposent une surface minimale (cf. erratum Story 9.2 ci-dessous — la règle Go `internal/` interdit l'import direct depuis le work dir gobind). Le `.aar` est régénéré par script de build dédié (`android/scripts/build-aar.sh` Linux/macOS + `build-aar.ps1` Windows) — pas une task Gradle directe (frontière build chain claire). Toolchain : ajout `golang.org/x/mobile` dans `go.mod` racine (indirect, build-time uniquement, non embarquée dans les binaires desktop puisqu'aucun code desktop ne l'importe)
- **`android.net.VpnService`** (Android API 14+) — API système pour créer une interface VPN. **Seul moyen** sur Android non-rooté de capturer le trafic IP. L'app appelle `VpnService.Builder` pour configurer (addresses, routes 0.0.0.0/0, DNS, MTU 1420), `prepare()` déclenche le prompt utilisateur de consentement, `establish()` retourne un `ParcelFileDescriptor` que l'on passe à la couche Go via JNI (`fd.detachFd()` côté Kotlin → `int` côté Go via gomobile binding)
- **Foreground Service** (Android 8+) — `LeVoileVpnService` étend `VpnService` (qui étend `Service`). Démarré via `startForegroundService()`, doit appeler `startForeground(notifId, notification)` dans les 5 secondes sinon ANR. Notification persistante non-dismissable affichant statut connexion + pays + bouton "Déconnecter"
- **Toggle "VPN permanent"** (Android 8+, robuste depuis 10+) — Réglage système accessible via `Settings.ACTION_VPN_SETTINGS`. L'utilisateur active manuellement "VPN permanent" + "Bloquer les connexions sans VPN" pour que l'OS bloque tout trafic hors-tunnel au niveau kernel. **C'est notre kill switch sur Android** — pas d'API app-level, le verrou est OS et inviolable côté app. L'UI guide l'utilisateur vers ce réglage avec un deeplink direct
- **Battery Optimization exemption** — `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` permission + intent OS pour demander à l'utilisateur d'exempter Le Voile du Doze mode. Sans ça, le tunnel se coupe en veille longue. **Pas critique sur Android 12+** (Foreground Service VPN exempté de Doze nativement) mais recommandé < Android 12
- **WebView Android** — `androidx.webkit.WebViewClientCompat` pour l'UI. Charge les assets HTML/CSS/JS via `WebViewAssetLoader` (résout `https://appassets.androidplatform.net/assets/...` vers `assets/` du module Android). JavaScript Bridge via `@JavascriptInterface` pour exposer les commandes natives (Connect, Disconnect, GetStatus, SelectCountry...) — **équivalent Android du serveur HTTP local desktop**. Pas de serveur HTTP loopback sur Android (Android l'autorise mais c'est plus lourd ; le bridge JS direct est suffisant et plus standard)
- **Notification API** (`androidx.core.app.NotificationCompat`) — Channel `levoile_vpn_status` (importance LOW, sans son), notification ongoing avec icône, statut, action "Déconnecter"
- **`BootReceiver`** (`android.content.BroadcastReceiver` + intent-filter `BOOT_COMPLETED`) — Auto-start du Foreground Service VPN au démarrage de l'appareil si l'utilisateur a activé "auto-start" dans les préférences. Permission `RECEIVE_BOOT_COMPLETED`
- **F-Droid metadata** (`android/fastlane/metadata/android/`) — Description, screenshots, changelog par version, fichier `levoile.yml` dans le repo F-Droid Data. Reproductible build obligatoire pour F-Droid (build hash déterministe)
- **Signature APK** — Clé v2/v3 dédiée Le Voile Android (distincte de la master key Ed25519 du registre — celle-ci sert à signer les artefacts release, pas l'APK). Stockée chiffrée hors-repo, importée via secret CI uniquement pour les builds release
- **`ProcessLifecycleOwner`** (androidx.lifecycle) — Détection foreground/background pour gérer le polling status (réduit en background)
- **Permissions runtime obligatoires** : `android.permission.FOREGROUND_SERVICE`, `android.permission.FOREGROUND_SERVICE_SPECIAL_USE` (Android 14+, type `vpn`), `android.permission.POST_NOTIFICATIONS` (Android 13+ — runtime prompt), `android.permission.RECEIVE_BOOT_COMPLETED`, `android.permission.INTERNET`, `android.permission.ACCESS_NETWORK_STATE`. **Pas de** : `READ_EXTERNAL_STORAGE`, `ACCESS_FINE_LOCATION`, `READ_PHONE_STATE` ou autres permissions sensibles — l'app n'en a pas besoin et leur absence renforce la posture confidentialité

### Cross-Cutting Concerns Identified

- **Sécurité** — Chiffrement bout en bout, zero-log, résistance DPI, clé Ed25519 par relais, registre signé master key, session tokens bindés à l'IP client (r.RemoteAddr), protection SSRF — touche tunnel, relay, registry, capture (TUN/Wintun/VpnService). **Identique cross-OS**
- **Anti-Fuite** — Détection WebRTC via STUN (validation — la capture L3 empêche structurellement les fuites), kill switch OS-level — touche leakcheck, stun, firewall (desktop) / réglage OS (Android)
- **Capture trafic machine-wide** — Capture tout IP (TCP/UDP/ICMP), encapsulation vers relais, pas de config par application. **Implémentation par OS** : Wintun (Windows) / TUN (Linux) / VpnService (Android). Frontière étroite vers le noyau Go partagé (read/write paquets IP)
- **Fiabilité** — Kill switch persistant (nftables/WFP/réglage OS Android), watchdog interface 3s, reconnexion auto avec backoff, failover multi-relais. **Sur Android** : `START_REDELIVER_INTENT` pour Foreground Service crash-recovery, restart automatique par OS si la VpnService est tuée par le système (rare en Foreground)
- **Intégration OS** — Trois implémentations complètes et indépendantes :
  - **Windows** : Wintun + WFP + winipcfg + UAC + SCM + named pipes + WebView2 + systray natif + NSIS
  - **Linux** : TUN /dev/net/tun + nftables + iproute2 + CAP_NET_ADMIN + systemd + Unix sockets + WebKitGTK + appindicator + nfpm/AUR
  - **Android** : VpnService + réglage OS "VPN permanent" + Routes système Android (gérées par VpnService.Builder) + Permissions runtime + Foreground Service + JNI bridge + WebView Android + Notification persistante + F-Droid/APK
- **Camouflage** — Trafic IP brut encapsulé dans HTTP/3 + TLS 1.3 vers le relais direct, indiscernable d'une connexion navigateur standard (ALPN h3, SNI = domaine relais). **Identique cross-OS** — pas de signature distincte sur le câble qui identifierait l'app comme VPN. (NB : sur Android l'OS affiche localement une icône clé dans la status bar — visible uniquement par l'utilisateur, jamais sur le réseau, donc neutre vis-à-vis du DPI)
- **Statelessness** — Aucune persistence côté relais (NAT table en RAM, TTL court). **Inchangé — le relais ignore quel OS est le client**
- **Découverte** — Registre distribué signé, sélection géographique, failover, latency measurement. **Identique cross-OS** (logique dans le noyau Go partagé `internal/registry/`)
- **Mise à jour** — Auto-update via GitHub releases (desktop), F-Droid + manuel (Android). Vérification signature Ed25519 toujours
- **IPC** — Communication inter-processus service ↔ UI **uniquement sur desktop** (named pipes Windows, Unix sockets Linux). **Sur Android : pas d'IPC** — l'app est mono-processus (Foreground Service + Activity dans le même `Application`), bridge JS↔Kotlin↔Go en in-process via JNI
- **Packaging triplé** — Win NSIS / Linux nfpm+AUR / Android Gradle. **Trois pipelines CI indépendants**, pas de mutualisation au-delà du noyau Go partagé

## Starter Template Evaluation

### Primary Technology Domain

Application **desktop Go (Windows + Linux)** + **app Android Kotlin/AndroidX avec noyau Go partagé via gomobile** + **serveur réseau Go (relais VPS)**. Pas de framework web, décisions portant sur structure projet, bibliothèques clés et outillage build, **dupliquées par OS quand applicable**.

### Starter Options Considered

**Bibliothèque QUIC :**

| Option | Version | Statut | Verdict |
|---|---|---|---|
| **quic-go** | v0.59.0 | Production-ready, 11.5k stars, utilisé par Cloudflare/Caddy/Tailscale | **Sélectionné** |
| golang.org/x/net/quic | v0.51.0 | Expérimental, API instable | Rejeté |

**Framework UI Desktop :**

| Option | Avantage | Inconvénient | Verdict |
|---|---|---|---|
| **webview/webview + fyne.io/systray** | Webview natif (WebView2 Windows) + systray dans un seul processus, léger (~15-20 Mo RAM), HTML/CSS/JS existant réutilisable, approche éprouvée (Lantern VPN) | Pas de binding Go↔JS intégré — serveur HTTP local embarqué requis | **Sélectionné** |
| Wails v2 | Go backend + HTML/CSS/JS frontend, bindings Go↔JS natifs | Conflit tray+desktop : Wails possède le lifecycle app, impossible d'avoir tray + fenêtre dans un seul processus. Nécessite 2 binaires séparés | **Rejeté — problème architectural tray+desktop** |
| Wails v3 | Résout le problème tray+desktop nativement | Alpha, instable, pas prêt pour la production | **Rejeté — alpha** |
| Electron | Éprouvé (Mullvad), tray+fenêtre natif | ~100+ Mo Chromium bundlé, surdimensionné, pas Go natif | Rejeté |
| Fyne | Pur Go, systray intégré v2.2+ | Styling limité, charte plateformeliberte.fr impossible à reproduire, réécriture frontend totale | Rejeté |
| Tauri v2 | Léger, moderne | Pas Go, ajoute Rust au stack | Rejeté |

**System Tray :**

| Option | Avantage | Inconvénient | Verdict |
|---|---|---|---|
| **fyne.io/systray** | Pur Go Windows (pas de CGo), API simple, éprouvé (getlantern/systray fork) | CGo requis Linux/macOS (dbus/appindicator) | **Sélectionné — même processus que webview** |

**Service OS :**

| Option | Avantage | Inconvénient | Verdict |
|---|---|---|---|
| **kardianos/service** | Cross-platform, Windows SCM intégré | Complexité IPC entre processus | **Sélectionné** |

**Bibliothèque TUN/Wintun :**

| Option | Avantage | Inconvénient | Verdict |
|---|---|---|---|
| **wireguard/tun** (`golang.zx2c4.com/wireguard/tun`) | Cross-platform unifié (TUN Linux + Wintun Windows), mature (production WireGuard), API cohérente, support IPv6 natif | Gros à build (inclut WG lui-même — à élaguer via imports sélectifs) | **Sélectionné** |
| songgao/water | Simple, bien connu | Support Wintun moins à jour, pas de terminaison IP stack intégrée | Rejeté |
| gvisor/pkg/tcpip (netstack) | Stack TCP/IP userspace complet, permet de parser les paquets client-side | Complexité massive, seulement utile si on veut éviter que le relais soit gateway NAT | Rejeté — gateway NAT côté relais préféré |

**Firewall (kill switch) :**

| OS | Option | Verdict |
|---|---|---|
| Linux | **nftables** via shellout `nft` + ruleset généré | **Sélectionné** — moderne, universel sur distros cibles |
| Linux | google/nftables (netlink pur Go) | Rejeté — ajoute complexité, gain marginal |
| Linux | iptables fallback | **Rejeté** — pas de fallback (dette technique refusée) |
| Windows | **Windows Filtering Platform (WFP)** via API `Fwpm*` | **Sélectionné** — kill switch persistant, filtrage kernel |
| Windows | Windows Firewall advfirewall (shellout netsh) | Rejeté — moins robuste, facilement contournable |

**Build & Distribution :**

| Option | Verdict |
|---|---|
| **GoReleaser v2** | **Sélectionné** — cross-compilation desktop + relay (Windows + Linux uniquement) |
| **nfpm** (via GoReleaser) | **Sélectionné** — génération unifiée .deb/.rpm/.apk Linux |
| **AUR (PKGBUILD manuel + GitHub Action `aur-publish`)** | **Sélectionné** — publication semi-automatique Arch User Repository |
| **NSIS** | **Sélectionné** — installeur Windows (service + tray + Wintun DLL + shortcuts) |
| **Gradle 8.x + AGP 8.x** | **Sélectionné — Android uniquement** — build APK/AAB, pas de mutualisation avec GoReleaser |
| **gomobile bind** | **Sélectionné** — produit `levoile-core.aar` consommé par Gradle. Régénéré via script dédié `android/scripts/build-aar.sh`, pas une task Gradle directe (évite couplage build chain) |

#### Stack Android (NOUVEAU — ajouté en parallèle, pas en remplacement)

**Noyau Go partagé (importable via `.aar`)** — cartographie réelle Story 9.2 (cf. erratum ADR-09 ci-dessous : 5 shims gomobile vivent sous `android/shims/`, pas dans `internal/`, à cause de la règle Go `internal/` qui interdit l'import depuis le work dir gobind hors module) :

| Shim gomobile | Rôle | Source Go consommée | Statut |
|---|---|---|---|
| `android/shims/protocol/` | Framing tunnel HTTP/3, encodage paquets IP | `internal/tunnel/pump.go` (Story 9.7) | Canonique — pas de package racine équivalent |
| `android/shims/auth/` | Session tokens Ed25519, IP hash, refresh | `internal/tunnel/client.go` (Story 9.7) | Canonique — pas de package racine équivalent |
| `android/shims/registry/` | Fetch + verify Ed25519 master key + sélection pays + failover | `internal/registry/` (lecture) | Wrapper d'un package racine |
| `android/shims/crypto/` | Ed25519 keypair, certificate pinning TLS | `internal/crypto/` (lecture) | Wrapper d'un package racine |
| `android/shims/leakcheck/` | STUN Binding Requests, validation IP | `internal/leakcheck/` + `internal/tunnel/` (transitif via quic-go) | Wrapper d'un package racine |
| `internal/tunnel/client.go` | Client HTTP/3 + reconnect + state machine | **Partiel** — exposé sans la pompe TUN (la pompe est OS-spécifique : un côté est Wintun/TUN/VpnService, l'autre côté est le stream QUIC). Sur Android, la pompe est en Kotlin (lit depuis `ParcelFileDescriptor`, écrit dans le stream Go via JNI) |

**Couches Android natives Kotlin (non partagées, équivalent fonctionnel des packages Go desktop) :**

| Composant Kotlin | Rôle | Équivalent desktop |
|---|---|---|
| `LeVoileVpnService` (Foreground Service étend `VpnService`) | Lifecycle service, création tunnel VPN, pompe paquets IP | `internal/service/` + `internal/tun/` desktop |
| `MainActivity` + WebView | Hôte UI, charge HTML/CSS/JS, JS Bridge | `cmd/ui/` + `webview/webview` desktop |
| `LeVoileBridge` (Kotlin singleton avec méthodes `@JavascriptInterface`) | Pont JS ↔ Kotlin ↔ Go (JNI gomobile) | `internal/ui/httpserver.go` + `internal/ipc/` desktop |
| `NotificationHelper` (gère la notification ongoing) | Indicateur statut + action déconnexion | `internal/ui/icons.go` + tray menu desktop |
| `BootReceiver` (BroadcastReceiver `BOOT_COMPLETED`) | Auto-start au boot | Autostart NSIS HKCU (Windows) / `levoile.desktop` autostart (Linux) |
| `KillSwitchHelper` (Kotlin — guide deeplink vers `Settings.ACTION_VPN_SETTINGS`) | Aide l'utilisateur à activer "VPN permanent" + "Bloquer connexions sans VPN" | `internal/firewall/` desktop (mais ici c'est l'OS qui applique la règle, pas l'app) |
| `VpnPreferences` (SharedPreferences wrapper) | Préférences runtime (pays préféré, auto-start, etc.) | `internal/config/` TOML desktop |
| `ConnectivityObserver` (`ConnectivityManager.NetworkCallback`) | Détection changements réseau (Wi-Fi → 4G), trigger reconnect | Watchdog TUN desktop |
| `BatteryOptimizationHelper` | Demande exemption Doze à l'utilisateur | N/A (desktop pas concerné) |

**Choix architecturaux Android refusés (pour mémoire) :**

| Option rejetée | Raison |
|---|---|
| Réécriture native Kotlin (OkHttp + Cronet/quiche pour QUIC, Bouncy Castle pour Ed25519) | Doublement maintenance, divergence inévitable de la logique protocole/crypto, contredit le principe de noyau partagé |
| Flutter / React Native | Stack additionnelle (Dart/JS) lourde, contredit "minimal dependencies" |
| Jetpack Compose (UI native) | Réécriture totale du frontend, perte cohérence visuelle cross-OS, charte plateformeliberte.fr non reproductible à l'identique |
| Serveur HTTP loopback Android (comme desktop) | JS Bridge `@JavascriptInterface` est plus standard, plus léger, et évite le risque port collision sur Android |
| Simulation kill switch app-level (reconnect-loop, drop sockets) | Inférieur au verrou OS-level "VPN permanent" — Android offre une solution kernel-level, l'utiliser |
| Google Play comme canal MVP | Play Integrity API contraignant, scrutin Google sur les VPN, ToS pouvant gêner. F-Droid + APK direct cohérent avec l'esprit logiciel libre |
| Support Android < 10 (API < 29) | API 29 = TLS 1.3 garanti OS, Scoped Storage, restrictions VPN stables. < 29 = corner cases sécu et VPN, effort disproportionné. ~80% du parc actif est >= 29 |
| Support Android TV / Wear OS / Android Auto | Cas d'usage trop différents (UI, lifecycle, contraintes), Phase 2+ |

### Selected Stack — DESKTOP : Go + quic-go + webview/webview + fyne.io/systray + kardianos/service + wireguard/tun + nftables/WFP + GoReleaser + nfpm + NSIS

### Selected Stack — ANDROID : Kotlin + AndroidX + Gradle/AGP 8.x + Android WebView + VpnService + Foreground Service + gomobile (`.aar` du noyau Go) + F-Droid metadata + APK signé v2/v3

**Rationale for Selection:**

_Desktop (inchangé) :_
- `quic-go` — Seule implémentation QUIC pure Go production-ready. TLS 1.3 intégré, HTTP/3 client+serveur
- `webview/webview` — Fenêtre desktop native via WebView2 (Windows) / WebKitGTK 6.0 (Linux). Frontend HTML/CSS/JS réutilisable sur les deux plateformes
- `fyne.io/systray` — Systray pur Go Windows. Linux via libayatana-appindicator3 (CGo, accepté car dépendance runtime standard sur toutes distros cibles)
- `kardianos/service` — Service background cross-platform : Windows SCM + systemd Linux avec install/uninstall/start/stop
- `wireguard/tun` — Interface TUN (Linux) + Wintun (Windows) avec API Go unifiée. Utilisée en production par WireGuard (millions d'installations)
- `nftables` — Standard Linux moderne pour kill switch. Pas de fallback iptables — simplifie la maintenance
- `WFP` (Windows Filtering Platform) — Filtrage kernel Windows pour kill switch, survit aux crashes du service
- `NSIS` — Installeur Windows complet (service, UI autostart, shortcuts, Wintun DLL, uninstall propre)
- `GoReleaser + nfpm` — Build cross-compilation + packaging Linux unifié (.deb, .rpm, .apk) depuis une seule config

_Android (nouveau) :_
- `Kotlin + AndroidX` — Stack Android moderne officielle Google. Kotlin obligatoire (Java refusé pour conciseness/nullsafety). AndroidX préféré aux APIs system pour stabilité forward-compat
- `Android WebView` (`androidx.webkit`) — Ressuscite la cohérence visuelle plateformeliberte.fr en chargeant les mêmes assets HTML/CSS/JS que desktop. `WebViewAssetLoader` sert les assets via virtual host HTTPS (`appassets.androidplatform.net`) — pas de loopback HTTP nécessaire
- `VpnService` — Seul moyen Android non-rooté de capturer le trafic IP. Modèle éprouvé par tous les VPN du Play Store et de F-Droid (NordVPN, ProtonVPN, Mullvad, Calyx, Orbot, etc.)
- `Foreground Service` — Obligatoire Android 8+ pour processus long-lived. La VpnService est nativement un Service donc elle hérite directement
- `gomobile` — Génère un `.aar` Java/Kotlin-friendly depuis Go. Maturité prouvée (Tailscale, Wireguard Android l'utilisent en production). `.aar` régénéré par script de build dédié (pas par Gradle directement) pour maintenir une frontière claire
- `F-Droid + APK direct` — Cohérent avec l'esprit logiciel libre. Pas de dépendance compte Google côté utilisateur, pas de soumission au scrutin Play Store sur les VPN. Build reproductible obligatoire pour F-Droid (renforce la confiance utilisateur)

**Architectural Decisions Provided by Stack:**

**Language & Runtime — Desktop:**
Go 1.26 + webview/webview (WebView2 Windows / WebKitGTK Linux) + fyne.io/systray + wireguard/tun — architecture 2 processus avec IPC

**Language & Runtime — Android:**
Kotlin 1.9 + AndroidX + JNI bridge vers `.aar` Go (gomobile) — architecture mono-processus (Foreground Service + Activity dans le même `Application`), bridge JS↔Kotlin↔Go in-process

**Styling Solution:**
HTML/CSS/JS — Charte visuelle plateformeliberte.fr **réutilisée à 100% entre desktop et Android** :
- Fond sombre : `#0b1526` / `#0e1e38`
- Bleu primaire : `#1a6fc4`, accents : `#2a8dff`
- Rouge alertes : `#d42b2b`
- Vert connecté : `#4ade80`, orange connecting : `#fb923c`, rouge risk : `#ff3c3c`
- Texte : `#f0f4ff` (principal), `#8a9bb8` (secondaire)
- Typographies : Bebas Neue (titres), Rajdhani (interface), Inter (corps) — woff2 embarqués
- Effets : gradients radiaux, ombres-lueur bleues, animations subtiles
- Adaptation Android : layout responsive (le frontend détecte `window.matchMedia('(max-width: 600px)')` ou viewport mobile et bascule sur layout vertical sans sidebar — sélecteur pays via menu déroulant ou bottom-sheet)

**Build Tooling — Desktop:**
GoReleaser v2 + nfpm — cibles de build multiplateformes :
- Windows : service (.exe), UI (.exe), installeur NSIS (.exe), Wintun DLL embarquée
- Linux : service (ELF) + UI (ELF) + paquets .deb, .rpm, .apk (via nfpm) — amd64 + arm64
- Linux (Arch) : PKGBUILD séparé publié sur AUR via GitHub Action
- Relais : ELF Linux (amd64 + arm64)
NSIS — installeur Windows (service, UI autostart, shortcuts, Wintun DLL)

**Build Tooling — Android (séparé, NOUVEAU) :**
Gradle 8 + AGP 8 — build Android isolé du build desktop, pas de mutualisation au-delà du noyau Go partagé :
- `android/scripts/build-aar.sh` (Linux/macOS) + `build-aar.ps1` (Windows) — invoke `gomobile bind -target=android -androidapi=29 -javapkg=fr.plateformeliberte.levoile.core -o android/levoile-core/libs/levoile-core.aar ./android/shims/protocol ./android/shims/auth ./android/shims/crypto ./android/shims/registry ./android/shims/leakcheck` (cf. erratum ADR-09 Story 9.2 : shims sous `android/shims/`, pas packages racine `internal/`). Produit `android/levoile-core/libs/levoile-core.aar` consommé par `:levoile-core` via `flatDir { dirs("libs") }` + `api(files("libs/levoile-core.aar"))` propagé transitivement à `:app`
- `android/gradlew assembleRelease` — produit APK signé v2/v3
- `android/gradlew bundleRelease` — produit AAB optionnel (pour future publication Play Store, non-MVP)
- ABI cibles : `arm64-v8a` (priorité), `armeabi-v7a` (legacy), `x86_64` (émulateur). Pas de splits APK pour MVP (un APK universel) — splits Phase 2 si la taille devient gênante
- F-Droid metadata maintenu dans `android/fastlane/metadata/android/`

**Testing Framework:**
- _Desktop_ : Go standard `testing` — pas de framework tiers. Tests unitaires, edge cases, e2e. 90+ fichiers de test
- _Android_ : JUnit 4 (tests unitaires Kotlin), Espresso (instrumentation UI), Mockito-Kotlin pour les mocks. **Pas de migration des tests Go desktop vers Android** — la couverture du noyau Go partagé est assurée par les tests Go existants (qui s'exécutent sur la machine de build, pas sur device)

**Code Organization (vue d'ensemble — détail dans la section Project Structure) :**

```
le_voile/
  # ---- Racine partagée (mono-repo) ----
  go.mod / go.sum
  .goreleaser.yaml                 # Desktop + relay UNIQUEMENT (pas Android)
  config.example.toml
  README.md
  
  # ---- Code partagé Go (noyau exposé via gomobile pour Android) ----
  internal/
    protocol/                      # Framing tunnel — partagé desktop + Android
    registry/                      # Discovery + verify Ed25519 — partagé
    auth/                          # Session tokens — partagé
    crypto/                        # Ed25519, pinning — partagé
    leakcheck/                     # STUN validation — partagé
    # ...autres packages internal/ desktop-only (tun, firewall, routing, tunnel/pump, dns, ipc, etc.)
  
  # ---- Desktop (Windows + Linux) ----
  cmd/
    client/main.go                 # Service cross-platform (kardianos/service)
    ui/main.go                     # UI : fyne.io/systray + webview/webview
    relay/main.go                  # Relais VPS HTTP/3
    ctl/main.go                    # CLI levoile-ctl
    genregistry/main.go            # Outil génération registre
  frontend/                        # Assets HTML/CSS/JS desktop (go:embed)
  installer/                       # NSIS script + assets (Windows)
  packaging/                       # nfpm configs, PKGBUILD (Arch), scripts post-install Linux
  deploy/                          # systemd units + install.sh pour relais
  assets/
  
  # ---- Android (NOUVEAU — arbre complet et indépendant) ----
  android/
    settings.gradle.kts
    build.gradle.kts               # Top-level Gradle config
    gradle.properties
    gradle/                        # Wrapper
    gradlew / gradlew.bat
    
    app/                           # Module APK
      build.gradle.kts
      proguard-rules.pro
      src/main/
        AndroidManifest.xml
        kotlin/fr/plateformeliberte/levoile/
          LeVoileApplication.kt
          LeVoileVpnService.kt     # VpnService + Foreground Service
          MainActivity.kt          # Hôte WebView
          bridge/
            LeVoileBridge.kt       # JS Bridge (@JavascriptInterface)
            GoCoreAdapter.kt       # Wrapper JNI vers .aar
          ui/
            NotificationHelper.kt
            KillSwitchHelper.kt    # Deeplink VPN settings
            BatteryOptimizationHelper.kt
          receivers/
            BootReceiver.kt
            ConnectivityObserver.kt
          prefs/
            VpnPreferences.kt      # SharedPreferences wrapper
          util/
            JniInteropUtil.kt
        assets/                    # HTML/CSS/JS (copiés depuis ../../frontend/ au build, voir build-aar.sh)
        res/                       # Ressources Android (icônes, strings.xml fr/en, themes)
      src/test/                    # Tests unitaires JUnit 4
      src/androidTest/             # Tests instrumentés Espresso
    
    levoile-core/                  # Module bibliothèque (héberge le .aar)
      build.gradle.kts
      libs/
        levoile-core.aar           # Produit par gomobile bind, gitignoré, généré au build
    
    scripts/
      build-aar.sh                 # gomobile bind → libs/levoile-core.aar
      build-aar.ps1                # Variante Windows pour devs
      sync-frontend.sh             # Copie ../../frontend/ → app/src/main/assets/
    
    fastlane/metadata/android/     # F-Droid metadata
      en-US/ / fr-FR/
        full_description.txt / short_description.txt / title.txt
        changelogs/
        images/
    
    keystore/                      # Gitignoré — clés signature APK (chiffrées + secret CI)
      .gitkeep
  
  # ---- CI/CD ----
  .github/workflows/
    release-desktop.yml            # Build Win + Linux + relay (GoReleaser)
    release-android.yml            # Build APK Android (Gradle), sign, upload F-Droid + GitHub release
    aur-publish.yml
  
  tools/
  docs/
```

**Development Experience:**

_Desktop :_
- `go run ./cmd/ui` pour le développement UI (tray + webview)
- `sudo go run ./cmd/client run` (Linux — CAP_NET_ADMIN requis) ou `go run ./cmd/client run` en admin CMD (Windows)
- `go run ./cmd/relay` pour le développement relais
- `go test ./...` pour les tests
- `go build ./cmd/ui` pour le build UI
- `goreleaser release --clean` pour builds multi-OS + paquets .deb/.rpm/.apk
- `goreleaser release --snapshot --skip=publish` pour un build local sans release

_Android (NOUVEAU) :_
- `cd android && ./scripts/build-aar.sh` (ou `.ps1`) — régénère le `.aar` depuis le noyau Go partagé. À lancer à chaque modification d'un package Go partagé
- `cd android && ./scripts/sync-frontend.sh` — copie `frontend/` → `android/app/src/main/assets/`. À lancer à chaque modification frontend
- `cd android && ./gradlew assembleDebug` — APK debug (signé clé debug Android)
- `cd android && ./gradlew assembleRelease` — APK release signé (requiert keystore CI)
- `cd android && ./gradlew installDebug` — déploie sur device/émulateur connecté via adb
- `cd android && ./gradlew test` — tests unitaires Kotlin
- `cd android && ./gradlew connectedAndroidTest` — tests instrumentés Espresso (device requis)
- Android Studio Iguana+ recommandé pour le dev Android (auto-completion Gradle, logcat intégré, profiler)
- **Important : ne jamais ouvrir le repo entier dans Android Studio** — ouvrir uniquement le dossier `android/` (sinon Android Studio confond Gradle config et code Go)

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Bloquent l'implémentation) :**
- **Capture trafic L3 unifiée** : TUN (Linux, `/dev/net/tun`) + Wintun (Windows, DLL signée) via `wireguard/tun`. Tout le trafic IP de la machine (TCP/UDP/ICMP/IPv4/IPv6) traverse l'interface virtuelle `levoile0`
- **Modèle gateway NAT côté relais** : le client envoie des paquets IP bruts encapsulés en HTTP/3 vers le relais. Le relais désencapsule, applique NAT (source IP = relais), forwarde sur Internet, gère la NAT table (TTL 120s UDP / 300s TCP), renvoie les réponses au client. Client = pas de netstack userspace (simplicité)
- **Kill switch firewall OS-level** : nftables (Linux) / WFP (Windows). Drop tout trafic sauf (a) paquets sortants vers IP relais sur port 443, (b) paquets sur interface TUN `levoile0`. Règles persistantes même si le service crash. Restauration propre au stop
- **Routage** : Route par défaut via TUN (`0.0.0.0/0 dev levoile0`) + route spécifique vers IP relais via gateway originale (évite routing loop). Linux : `ip route` + table dédiée (priorité). Windows : `winipcfg` + metric
- **Architecture 2 processus** : Service privilégié (CAP_NET_ADMIN Linux / LocalSystem Windows) + UI user (fyne.io/systray + webview/webview). IPC via named pipes (Windows) / Unix sockets (Linux `/run/levoile/ipc.sock` — service user `levoile`, UI via groupe `levoile`)
- **Protocole client ↔ relais** : HTTP/3 POST `/tunnel` — stream bidirectionnel persistant transportant les paquets IP bruts (framing : 2 octets taille + payload). Remplace `/dns-query` + `/connect` + `/stun-relay`. Session token Ed25519 dans le header Authorization
- **DNS** : Résolution côté relais (upstream Cloudflare 1.1.1.1 / Quad9 9.9.9.9, blocklist StevenBlack appliquée par le relais avant NAT-forward). Plus de proxy DNS local client
- **Limite connexions par relais** : 150 tunnels simultanés (clients distincts). 500 flux NAT concurrents par tunnel
- **Registre distribué signé** : chaque relais sert `/.well-known/relay-registry.json`, signé Ed25519 master key — inchangé
- **Sélection relais** : pays → failover auto → latency measurement via /health — inchangé
- **UI desktop** : webview/webview frameless 420×540px (charte plateformeliberte.fr) + fyne.io/systray dans un seul binaire — inchangé sur les deux OS

**Important Decisions (Façonnent l'architecture) :**
- Config client : TOML — `%AppData%/LeVoile/` (Windows) ou `~/.config/levoile/` (Linux user) + `/etc/levoile/config.toml` (service)
- Authentification : Unidirectionnelle — clé Ed25519 unique par relais, certificate pinning TLS — inchangé
- Session tokens : Ed25519 signés, TTL 4h, IP hash dans le payload, refresh automatique avec backoff exponentiel + circuit breaker (5 échecs consécutifs → déconnexion auto + kill switch maintenu)
- Rate limiting relais : 200 tunnels max par IP source (sync.Map + atomics), bandwidth limiter par IP (quota journalier 10 GiB) — conservé, adapté pour tunnels au lieu de CONNECT
- Anti-fuite : Détection WebRTC via STUN (3 serveurs Google + Cloudflare). **La capture L3 rend les fuites structurellement impossibles**, mais le check reste utile comme validation (détection mauvaise configuration / TUN down). Suppression des politiques navigateur (plus nécessaires)
- Blocklist DNS : Téléchargement StevenBlack/hosts **côté relais** (pas client). Stockage in-memory map sur chaque relais, refresh 24h. NXDOMAIN filtering dans le DNS resolver du relais
- Routes & Firewall par OS : Interface Go `NetworkManager` + build tags :
  - Linux : nftables + iproute2 (`ip rule`, `ip route`)
  - Windows : WFP + winipcfg (routes + firewall filters)
- Packaging Linux : GoReleaser + nfpm pour .deb/.rpm/.apk. PKGBUILD Arch séparé. Dépendances déclarées : `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0`, `libayatana-appindicator3-1`, `nftables`, `iproute2`. Post-install : `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW (dans le systemd unit)`, `systemctl enable --now levoile.service`
- Déploiement relais VPS : scp + systemd (inchangé, mais handler `/tunnel` ajouté au binaire relais)
- Monitoring : /health endpoint anonyme sur chaque relais — deux compteurs de concurrence : `connections` (requêtes courtes /verify, /ip, /dns-query) et `tunnels` (streams /tunnel long-lived, plafond MaxTunnels=150). Champs additionnels : nat_entries, uptime, ram_mb, cpu_pct, country, relay_id, compteurs rate-limit agrégés
- Auto-update : Vérification périodique GitHub releases (6h), signature Ed25519 checksums, rollback, rate limiting downloads. Linux : remplacement atomique binaire + `systemctl restart levoile.service`. Windows : inchangé
- Installeur NSIS Windows : Installation service SCM, UI autostart registre HKCU, shortcuts, Wintun DLL dans `Program Files/LeVoile/wintun.dll`, désinstallation propre (clear WFP filters, remove routes, delete TUN adapter)
- Installeur Linux : paquets natifs via `apt install levoile` / `dnf install levoile` / `yay -S levoile` / `apk add levoile`. Le paquet installe le service systemd + l'UI + setcap. L'UI est lancée au login via autostart XDG (`~/.config/autostart/levoile.desktop`)

**Deferred Decisions (Post-MVP) :**
- Authentification client (tokens/clés client) — Phase 3 si restriction d'accès
- CI/CD automatisé — GitHub Actions (requis pour publication AUR propre + APK Android)
- Registre dynamique (API de gestion) — Phase 2, remplacer le JSON statique
- Support macOS — utan (utun interface) via wireguard/tun supporte déjà macOS, différé à la demande
- Support iOS — Phase 3+ (effort App Store + Network Extension distinct, ne réutilise pas l'arbre Android)
- Publication Google Play — Phase 2+ si traction (build AAB existe déjà, signature additionnelle Play App Signing à mettre en place)
- Auto-update intégré Android — Phase 2 (vérification GitHub releases + notification dans l'UI Android, F-Droid gère le reste)
- Split tunneling par domaine ou par app — hors scope MVP. **Note Android** : `VpnService.Builder.addAllowedApplication()` / `addDisallowedApplication()` permettent un split par-package natif Android, à exposer côté UI Phase 2 si demande
- Toggle Tor-over-VPN ou intégration Orbot — hors scope, mais combinable côté utilisateur (Le Voile actif + Orbot en VpnService prioritaire = mutuellement exclusifs sur Android — un seul VpnService actif à la fois)
- Support Android TV / Wear OS / Auto — Phase 2+ si demande

### Configuration & Données

- **Aucune base de données** — Ni client ni relais. Cohérent avec zero-log
- **Config client (user)** : TOML dans `%AppData%/LeVoile/` (Windows) ou `~/.config/levoile/` (Linux)
- **Config service (Linux)** : `/etc/levoile/config.toml` (lecture seule pour user, modifiable via UI qui passe par IPC → service signe les écritures)
- **Contenu config** : Relay domain + clé publique Ed25519, pays préféré, auto-start, STUN server, update settings (GitHub owner/repo, rate limit, interval), registry settings (URL, master key, refresh interval), UI http server port, TUN interface name (`levoile0`), TUN MTU (1420 par défaut)
- **Registre relais** : Fichier JSON signé déployé sur chaque relais, servi via `/.well-known/relay-registry.json`. Contient version, master public key, entries avec ID, domain, public key, signature, added timestamp. Vérifié Ed25519 master key côté client — inchangé
- **Cache registre client** : Fichier JSON local persisté par `internal/registry/cache.go`. Fallback si aucun relais ne répond au démarrage
- **État NAT côté relais** : Table in-memory `(client_session, src_ip:port, dst_ip:port, protocol) → (nat_port, last_seen)`. Éviction par TTL (UDP 120s, TCP 300s selon état). Jamais persisté
- **Authentification** : Unidirectionnelle — chaque relais possède sa propre paire Ed25519. Certificate pinning TLS — inchangé

### Communication & Protocole

- **Client ↔ Relais (Tunnel IP)** : HTTP/3 via quic-go/http3 en connexion directe au VPS relais. POST `https://{relay-domain}/tunnel` avec Upgrade pour stream bidirectionnel persistant transportant les paquets IP bruts. Framing : `[2 octets big-endian: length][payload IP packet]`. Header `Authorization: Bearer {session_token}` à l'ouverture du stream. Limite pratique MTU : 1420 octets (QUIC overhead pris en compte). Trafic indiscernable d'une connexion HTTPS/HTTP/3 standard côté DPI
- **Protocole de désencapsulation** : Le relais reçoit le paquet IP → parse header (IPv4/IPv6 + protocole TCP/UDP/ICMP) → NAT source (substitue IP source par IP relais, alloue port NAT) → forwarde sur Internet via socket raw ou socket userspace selon protocole
- **DNS** : Les requêtes DNS (UDP 53 / TCP 53) capturées par la TUN arrivent au relais comme tout autre paquet IP. Le relais les intercepte : soit les résout en interne via upstream (Cloudflare 1.1.1.1 / Quad9 fallback) en appliquant la blocklist, soit les forwarde si on laisse l'upstream décider (décision d'implémentation : résolution interne pour appliquer la blocklist et zero-log)
- **Client ↔ Relais (Verify)** : GET `https://{relay-domain}/verify` — challenge-response, émission session token Ed25519 TTL 4h. Inchangé
- **Client ↔ Relais (IP)** : GET `https://{relay-domain}/ip` — retourne l'IP visible du client (pour le statut desktop). Inchangé
- **Session Token** : Obtenu via GET `/verify`, signé Ed25519, contient IP hash SHA256 + timestamp + TTL 4h. Refresh automatique avec backoff exponentiel (100ms → 30s) + circuit breaker (5 échecs consécutifs → déconnexion auto + kill switch reste actif)
- **Client → Registre** : GET `https://{any-relay}/.well-known/relay-registry.json` au démarrage. Inchangé
- **Communication Service ↔ UI** : IPC via named pipes (Windows `\\.\pipe\levoile`) ou Unix sockets (Linux `/run/levoile/ipc.sock`, permissions `0660` user `levoile` groupe `levoile`). Protocole JSON ligne par ligne. Max 4 Ko par message
- **Communication UI interne** : Serveur HTTP local embarqué (127.0.0.1:{port configurable}). Inchangé
- **IPC Actions** : GetStatus, Connect, SelectCountry, GetRegistry, GetLeakStatus, TriggerLeakCheck, GetUpdateStatus, CheckUpdate, GetAutoStart, SetAutoStart, **GetKillSwitchMode, SetKillSwitchMode** (normal / dégradé), **ForceReconnect**, Quit. **Supprimé** : GetBlocklist, SetBlocklist (blocklist gérée côté relais), GetHTTPProxy, SetHTTPProxy (plus de proxy local), GetBrowserPolicies, SetBrowserPolicies (plus d'extension)
- **Limite par relais** : 150 tunnels simultanés. 200 tunnels max par IP source. Bandwidth limit 10 GiB/jour + 1 GiB/heure (quota volume) par IP
- **Gestion STUN** : Les Binding Requests générés par les navigateurs sont capturés par la TUN → arrivent au relais → relayés vers le serveur STUN cible. Retours réguliers (pas de handler `/stun-relay` dédié). Le module `internal/leakcheck` continue à émettre ses propres checks pour validation

### Architecture par OS — vue triple

**Principe directeur** : isolation OS maximale. Chaque OS a son propre arbre d'implémentation autonome, sa propre sémantique de processus, son propre lifecycle, son propre système de packaging. Seul le noyau protocole/crypto/registre/session est mutualisé.

| Aspect | Windows + Linux (Desktop) | Android |
|---|---|---|
| **Modèle processus** | 2 processus (service privilégié + UI user) | 1 processus (Foreground Service + Activity dans le même `Application`) |
| **IPC** | Named pipes (Win) / Unix sockets (Linux), JSON ligne par ligne | Aucun — bridge JS↔Kotlin↔Go in-process (JNI gomobile + `@JavascriptInterface`) |
| **Capture L3** | Wintun (Win) / TUN (Linux) via `wireguard/tun` | `android.net.VpnService` — file descriptor passé à Go via JNI |
| **Kill switch** | WFP (Win) / nftables (Linux) — règles app-level installées par le service | Réglage OS "VPN permanent + bloquer connexions sans VPN" — kernel-level Android, pas d'API app pour l'installer ; l'app guide l'utilisateur via deeplink `Settings.ACTION_VPN_SETTINGS` |
| **Service lifecycle** | kardianos/service (SCM Win / systemd Linux) | Foreground Service Android (`startForegroundService` + `startForeground` < 5s, `START_REDELIVER_INTENT`) |
| **Élévation** | UAC (Win) / `CAP_NET_ADMIN` via systemd `AmbientCapabilities=` (Linux) | Permissions runtime (`POST_NOTIFICATIONS`, `FOREGROUND_SERVICE_SPECIAL_USE`) + consentement utilisateur VpnService au premier lancement |
| **UI host** | webview/webview (WebView2 Win / WebKitGTK Linux) + fyne.io/systray | Android WebView (`androidx.webkit`) plein-écran dans `MainActivity` + Notification ongoing du Foreground Service |
| **Communication frontend ↔ logique** | Serveur HTTP local (127.0.0.1:port) → IPC → service | JS Bridge (`@JavascriptInterface`) → Kotlin → JNI → Go (in-process) |
| **Préférences runtime** | TOML (`%AppData%/LeVoile/` Win / `~/.config/levoile/` Linux) | SharedPreferences Android (XML privé, scope app uniquement) |
| **Auto-start** | NSIS HKCU (Win) / `levoile.desktop` autostart XDG (Linux) | `BootReceiver` BroadcastReceiver `BOOT_COMPLETED` + permission `RECEIVE_BOOT_COMPLETED` (opt-in utilisateur) |
| **Singleton instance** | Mutex nommé (Win) / flock `~/.local/state/levoile/ui.lock` (Linux) | Foreground Service est singleton par nature (un seul `LeVoileVpnService` actif via `Service.START_STICKY` + check de l'instance) |
| **Recovery au crash** | Watchdog firewall + TUN, restart par SCM/systemd | `START_REDELIVER_INTENT` redélivre l'intent original au Service redémarré, `onRevoke()` propre si l'utilisateur change de VPN |
| **Distribution** | NSIS (Win) / `.deb`/`.rpm`/`.apk`/AUR (Linux) | F-Droid + APK direct GitHub release (pas Google Play en MVP) |
| **Signature artefacts** | Ed25519 master key sur les binaires + Authenticode optionnel (Win) + signatures paquets Linux | Signature APK v2/v3 dédiée (clé Android distincte) + Ed25519 sur le SHA256 de l'APK pour les downloads directs |

#### Desktop : Architecture 2 Processus & Intégration OS

Deux processus communiquant via IPC :

1. **levoile-service** (`cmd/client/`) — Service privilégié (Windows SCM / systemd Linux) via kardianos/service
   - Orchestrateur principal (`internal/service/service.go`)
   - Gère le lifecycle de tous les composants : tunnel QUIC, TUN/Wintun interface, firewall (nftables/WFP), routing, registry discovery, failover, leak check, updater
   - Expose un serveur IPC pour communication avec l'UI
   - Sous-commandes : install, uninstall, start, stop, run
   - Flags CLI : -config, -relay-domain, -relay-pubkey, -insecure
   - Linux : tourne en user `levoile` avec capabilities `CAP_NET_ADMIN,CAP_NET_RAW` (setcap au post-install) ou via systemd `AmbientCapabilities=`
   - Windows : tourne en LocalSystem (privilèges natifs complets)

2. **levoile-ui** (`cmd/ui/`) — UI unique user-space : fyne.io/systray + webview/webview
   - **Tray** (fyne.io/systray, thread principal) : Icône avec menu contextuel (Ouvrir la fenêtre, Quitter). Polling status service via IPC toutes les 2s. Icônes embarquées via `//go:embed`
   - **Webview** (webview/webview) : Fenêtre 420×540px, ouverte/fermée à la demande depuis le menu tray "Ouvrir". WebView2 (Windows) / WebKitGTK 6.0 (Linux)
   - **Serveur HTTP local** (net/http, 127.0.0.1:{port}) : Sert les assets frontend embarqués. Expose une API REST JSON qui proxie les commandes vers le service via IPC
   - Singleton : mutex nommé Windows / lock file `~/.local/state/levoile/ui.lock` (flock) Linux
   - `-H windowsgui` dans ldflags Windows (pas de console). Linux : pas besoin (pas de console attachée par défaut si lancé via .desktop)
   - La fenêtre webview se ferme sans quitter l'app — le tray persiste. "Quitter" depuis le tray arrête l'UI (pas le service — le service reste contrôlé par systemd/SCM)

#### Composants Communs

- **Service / Orchestrateur** (`internal/service/`) : Package central qui orchestre le lifecycle. Implémente `kardianos/service.Interface`. Gère : tunnel, TUN interface, firewall, routing, registry discovery, failover, leak check scheduling, updater. Expose les méthodes appelées par l'IPC handler
- **IPC** (`internal/ipc/`) : Client/serveur avec transport platform-specific (named pipes Windows via go-winio, Unix sockets Linux). Protocole JSON ligne par ligne, max 4 Ko par message
- **IPC Handler** (`internal/ipchandler/`) : Dispatcher centralisé qui route les requêtes IPC vers les méthodes du service. Gère la persistence config avec mutex de sérialisation
- **TUN/Wintun** (`internal/tun/`) : Création/destruction interface virtuelle `levoile0`. API unifiée via `wireguard/tun`. Lecture/écriture paquets IP. Gestion MTU (1420 par défaut). Windows : extraction `wintun.dll` embarquée au premier démarrage. Build tags pour spécificités OS
- **Tunnel IP** (`internal/tunnel/`) : HTTP/3 client QUIC, certificate pinning, session tokens, reconnexion backoff, state machine. **Étendu** : goroutine de pompe paquets TUN → stream HTTP/3, et stream HTTP/3 → TUN. Framing taille+payload. Statistiques volume
- **Firewall (kill switch)** (`internal/firewall/`) : Interface `Firewall` avec `Activate(relayIP, tunName)` / `Deactivate()` / `IsActive()`. Build tags :
  - Linux : génère ruleset nftables (table `inet levoile` dans family `inet`, chain `output` priority 0 hook output, règles : accept `oif levoile0`, accept `ip daddr {relayIP} udp dport 443`, accept loopback, drop tout le reste). Application via `nft -f`. Détection `nft` absent = échec bloquant. Après application du ruleset, vérification via `nft list ruleset` que les règles sont réellement chargées. Si absent (module kernel `nf_tables` non chargé), tentative `modprobe nf_tables`. Si toujours absent → échec service avec message : "nftables kernel module unavailable, cannot start Le Voile". Le post-install du paquet Linux inclut `modprobe nf_tables` + test de fumée
  - Windows : Windows Filtering Platform (WFP) — création d'une provider + sublayer dédiés, filters BLOCK sur toutes les interfaces sauf la TUN et le flow vers relayIP:443
- **Routes** (`internal/routing/`) : Interface `RouteManager` avec `AddDefault(tunName)` / `AddRelayRoute(relayIP, origGateway)` / `Cleanup()`. Build tags :
  - Linux : `ip route add 0.0.0.0/0 dev levoile0 table 51820`, `ip rule add from all lookup 51820 priority 100`, `ip route add {relayIP}/32 via {origGateway}`
  - Windows : `winipcfg` — ajout route par défaut via TUN avec metric basse, route spécifique vers relayIP via gateway originale avec metric haute
- **Privilège / Capabilities** (`internal/elevation/`) : Windows : token check UAC. Linux : check `CAP_NET_ADMIN` via `PR_CAPBSET_READ`, erreur claire si absent ("run as root or install via package which sets setcap")
- **Sélection relais** (`internal/registry/`) : Discovery orchestrator (fetch → verify → cache → default relay), failover automatique, latency measurement via /health, sélection par pays, cache local persistent. Inchangé
- **Détection fuites WebRTC** (`internal/leakcheck/` + `internal/stun/`) : STUN Binding Requests (RFC 5389) vers 3 serveurs (Google ×2, Cloudflare). **Rôle redéfini** : validation que la TUN capture bien le trafic. Le check émet un STUN request via l'OS (qui passe par la TUN) ; la réponse doit venir avec l'IP du relais, pas l'IP ISP. Si différent = TUN down ou fuite (alerte UI + log). Suppression du handler `/stun-relay` dédié et du composant STUN Interceptor/Relayer (TURN drop devenu moot)
- **Singleton UI** (`internal/ui/singleton_*.go`) : Windows : mutex nommé. Linux : flock sur `~/.local/state/levoile/ui.lock` — singleton **par utilisateur** (chaque user Linux peut lancer sa propre instance UI). Le service est machine-wide : toutes les UIs sur le même système contrôlent le même service, les actions d'un user affectent tous les users. Documenter dans le README : "Le Voile est un VPN machine-wide — sur un système multi-user Linux, tous les utilisateurs partagent le même tunnel."
- **Serveur HTTP local UI** (`internal/ui/httpserver.go`) : Sert les assets frontend embarqués et expose l'API REST JSON pour le webview. Écoute loopback uniquement. Proxie les commandes vers le service via IPC client
- **Auto-update** (`internal/updater/`) : Inchangé côté logique. Linux : après remplacement binaire → `systemctl restart levoile.service` via dbus ou shellout. Windows : inchangé (restart service SCM)
- **Config** (`internal/config/`) : TOML schema avec sections relay, client, stun, update, registry, tun (name, mtu), firewall (enable_killswitch), ui. **Supprimé** : sections blocklist (côté relais maintenant), http_proxy, browser_policies. Auto-discovery path. Atomic writes. Build tags par OS
- **Crypto** (`internal/crypto/`) : Ed25519 key pair, TLS self-signed cert, certificate pinning. **Toutes les comparaisons utilisent `crypto/subtle.ConstantTimeCompare`** (NFR9c). Chaîne de confiance master key avec clé de rotation embarquée (NFR22h) — structure de 2 clés publiques (current + next), dual-signature transitoire 6 mois
- **Config HMAC Integrity** (`internal/config/integrity.go`) : HMAC-SHA256 de la config TOML calculé au premier démarrage avec clé dérivée machine-local (clé stockée dans keyring système ou fichier 0600). Vérifié à chaque démarrage. Écart = refus démarrer + alerte (NFR9j)
- **DoH Bootstrap** (`internal/registry/doh_resolver.go`) : Résolution DNS initiale du domaine relais via DoH (Cloudflare `https://cloudflare-dns.com/dns-query` + fallback Quad9). Protège contre DNS poisoning au bootstrap avant que le tunnel ne soit établi (NFR9i)
- **DNS cache flush** (`internal/dns/flush.go`) : Flush du cache resolver système au disconnect. Build tags :
  - Windows : `ipconfig /flushdns` via shellout
  - Linux : détection au runtime du resolver actif (systemd-resolved → `resolvectl flush-caches`, nscd → `nscd -i hosts`, dnsmasq → SIGHUP, sinon no-op)
- **Integrity Checker** (`internal/integrity/`) : Vérification hash SHA256 du binaire au démarrage contre valeur signée Ed25519 embarquée via `//go:embed`. Signature générée par GoReleaser post-build. Échec = log syslog/Event Log + refus de démarrer. Protège contre remplacement post-install. Implémenté via `os.Executable()` + `io.Copy(sha256.New(), file)` + `ed25519.Verify()`
- **CLI Control Tool** (`cmd/ctl/main.go` — binaire `levoile-ctl`) : Outil CLI privilégié pour opérations avancées. Communique avec le service via IPC (même socket que l'UI). Authentification par token machine-local. Commandes : `killswitch off`, `killswitch on`, `status`, `reconnect`, `logs`. Installé dans `/usr/bin/levoile-ctl` (Linux) / `Program Files/LeVoile/levoile-ctl.exe` (Windows). **Pas d'équivalent Android** (pas de CLI sur Android)

#### Android : Architecture mono-processus & Intégration OS

L'app Android est mono-processus : `LeVoileApplication` héberge `MainActivity` (UI) + `LeVoileVpnService` (Foreground Service VPN) + le bridge JNI vers le `.aar` Go. Pas d'IPC — tout est in-process.

**Composants Android (Kotlin/AndroidX) :**

1. **`LeVoileApplication`** (`android/app/src/main/kotlin/.../LeVoileApplication.kt`) — Singleton `Application`. Initialise le bridge Go (chargement `.aar` via `System.loadLibrary("gojni")` géré transparent par gomobile), crée le notification channel, observe le lifecycle global (`ProcessLifecycleOwner`).

2. **`MainActivity`** (`...MainActivity.kt`) — Activité unique hôte de la WebView. Configure `WebView`, `WebViewClient`, `WebChromeClient`, `WebViewAssetLoader` (sert les assets HTML/CSS/JS depuis `assets/` via `https://appassets.androidplatform.net/assets/...`), enregistre `LeVoileBridge` via `webView.addJavascriptInterface(bridge, "LeVoile")`. Au premier lancement, déclenche le flux de consentement VpnService :
   ```kotlin
   val intent = VpnService.prepare(this)
   if (intent != null) startActivityForResult(intent, REQ_VPN_PREPARE)
   else onActivityResult(REQ_VPN_PREPARE, RESULT_OK, null)
   ```
   Bouton "Connecter" appelle `startForegroundService(Intent(this, LeVoileVpnService::class.java).setAction(ACTION_CONNECT))`.

3. **`LeVoileVpnService`** (`...LeVoileVpnService.kt`) — Étend `android.net.VpnService` (qui étend `android.app.Service`). Cycle de vie :
   - `onStartCommand(intent, flags, startId)` : selon `intent.action` (`ACTION_CONNECT` / `ACTION_DISCONNECT`), démarre ou stoppe le tunnel. Retourne `START_REDELIVER_INTENT` pour qu'un crash redémarre avec l'intent original
   - **Connect** :
     1. `startForeground(NOTIF_ID, buildOngoingNotification("Connexion en cours..."))` — DOIT être appelé < 5s après `onStartCommand` sinon ANR
     2. Sélection relais via `GoCoreAdapter.selectRelay(preferredCountry)` (appelle le noyau Go via JNI)
     3. Construction interface VPN :
        ```kotlin
        val builder = Builder()
            .setSession("Le Voile")
            .addAddress("10.6.6.2", 32)         // IP virtuelle locale tunnel
            .addRoute("0.0.0.0", 0)              // Route tout via VPN
            .addRoute("::", 0)                   // IPv6
            .addDnsServer("10.6.6.1")            // DNS servi par le relais (résolu côté relais)
            .setMtu(1420)
            .setBlocking(true)
            .setUnderlyingNetworks(null)         // Système choisit Wi-Fi/cellulaire
        val pfd = builder.establish() ?: error("VpnService.establish() returned null — user revoked consent")
        ```
     4. `pfd.detachFd()` retourne un `Int` (file descriptor brut) que l'on passe au noyau Go via `GoCoreAdapter.startTunnel(fd)`. Le côté Go ouvre un `os.File` sur ce fd et démarre les pompes (lecture paquets IP de la VpnService → encodage protocole → stream HTTP/3 vers relais ; et inverse). **Attention** : la pompe lecture/écriture côté VPN est en Kotlin (lecture du fd via `FileInputStream(ParcelFileDescriptor.adoptFd(fd).fileDescriptor)`) car l'API Android Kotlin est plus simple ; la pompe envoie les paquets au Go via la méthode `GoCoreAdapter.writePacket(buf, len)` exposée par gomobile. Inversement le Go appelle `GoCoreAdapter.readPacketCallback(buf)` quand il reçoit du relais. Un benchmark validera si la traversée JNI par paquet n'est pas un goulot — sinon batching ou pompe full-Go via fd partagé
   - **Disconnect** : `GoCoreAdapter.stopTunnel()`, ferme le fd, `stopForeground(STOP_FOREGROUND_REMOVE)`, `stopSelf()`
   - **`onRevoke()`** : appelée par l'OS si l'utilisateur change de VPN ou désactive Le Voile dans Settings → flow Disconnect propre
   - **`onDestroy()`** : nettoyage final, garantit fd fermé

4. **`LeVoileBridge`** (`...bridge/LeVoileBridge.kt`) — Pont JS↔Kotlin. Méthodes annotées `@JavascriptInterface` exposées au frontend via `webView.addJavascriptInterface(bridge, "LeVoile")` :
   ```kotlin
   class LeVoileBridge(private val context: Context) {
       @JavascriptInterface fun connect(): String { /* startForegroundService ACTION_CONNECT */ }
       @JavascriptInterface fun disconnect(): String { /* startService ACTION_DISCONNECT */ }
       @JavascriptInterface fun getStatus(): String { /* JSON status: connected/connecting/disconnected, IP, country, latency */ }
       @JavascriptInterface fun selectCountry(code: String): String
       @JavascriptInterface fun getRegistry(): String  // JSON liste pays + relais (depuis cache + fetch)
       @JavascriptInterface fun checkLeak(): String
       @JavascriptInterface fun openVpnSettings()  // Deeplink Settings.ACTION_VPN_SETTINGS — pour activer "VPN permanent"
       @JavascriptInterface fun openBatteryOptimizationSettings()
       @JavascriptInterface fun isAlwaysOnEnabled(): Boolean  // Vérifie si l'utilisateur a bien activé "VPN permanent" + "Bloquer connexions sans VPN"
       @JavascriptInterface fun getPreferences(): String      // JSON SharedPreferences
       @JavascriptInterface fun setPreference(key: String, value: String)
       @JavascriptInterface fun quit()
   }
   ```
   Le frontend JS appelle directement `window.LeVoile.connect()` etc. — équivalent fonctionnel des `fetch('/api/...')` desktop.

5. **`GoCoreAdapter`** (`...bridge/GoCoreAdapter.kt`) — Wrapper autour des classes générées par gomobile depuis le `.aar`. Centralise tous les appels JNI vers le noyau Go pour éviter de disperser les imports `gomobile_levoile.*` partout. Expose une API Kotlin idiomatique (suspend functions, `Result<T>`, exceptions traduites).

6. **`NotificationHelper`** (`...ui/NotificationHelper.kt`) — Construit la notification persistante du Foreground Service. Channel `levoile_vpn_status` (importance LOW). Contenu : icône, "Le Voile · Connecté · Allemagne" + sous-texte "IP: 1.2.3.4", action "Déconnecter" (PendingIntent vers `LeVoileVpnService` action `DISCONNECT`). Pas de son, pas de vibration.

7. **`KillSwitchHelper`** (`...ui/KillSwitchHelper.kt`) — Méthodes pour ouvrir le réglage système "VPN permanent" via `Intent(Settings.ACTION_VPN_SETTINGS)` et instructions UI (string dans `strings.xml` fr/en) expliquant à l'utilisateur les deux switches à activer. **Le switch "VPN permanent" + "Bloquer les connexions sans VPN" EST notre kill switch sur Android** — l'app ne peut pas l'activer programmatiquement (intentionnel par Google pour empêcher abus), seul l'utilisateur le peut. Au premier setup, l'UI affiche un onboarding obligatoire qui guide vers ce réglage.

8. **`BatteryOptimizationHelper`** (`...ui/BatteryOptimizationHelper.kt`) — Vérifie si Le Voile est exempté du Doze (`PowerManager.isIgnoringBatteryOptimizations(packageName)`), demande l'exemption via `Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).setData(Uri.parse("package:$packageName"))`. **Sur Android 12+ : pas obligatoire** (Foreground Service VPN est exempté nativement) ; **Android 10-11 : recommandé** sinon le tunnel se coupe en veille longue.

9. **`BootReceiver`** (`...receivers/BootReceiver.kt`) — `BroadcastReceiver` avec intent-filter `BOOT_COMPLETED`. Si la préférence `auto_start` est activée, démarre `LeVoileVpnService` avec `ACTION_CONNECT`. Permission `RECEIVE_BOOT_COMPLETED` déclarée dans le manifest.

10. **`ConnectivityObserver`** (`...receivers/ConnectivityObserver.kt`) — `ConnectivityManager.NetworkCallback` enregistré pendant la durée du tunnel. Détecte changements (Wi-Fi → 4G, perte connexion) → trigger `GoCoreAdapter.notifyNetworkChange()` qui force la reconnexion QUIC immédiate (au lieu d'attendre le timeout TCP).

11. **`VpnPreferences`** (`...prefs/VpnPreferences.kt`) — Wrapper typé autour de `SharedPreferences`. Clés : `auto_start: Bool`, `preferred_country: String`, `skip_quit_modal: Bool`, `kill_switch_warned: Bool` (l'utilisateur a vu l'onboarding kill switch), `last_relay_id: String`, `battery_optimization_warned: Bool`. **Pas de TOML sur Android** — la config TOML reste desktop-only, Android utilise SharedPreferences (plus standard, scope app, chiffré au repos sur Android 10+ via Direct Boot).

**Permissions Android déclarées dans `AndroidManifest.xml` :**

```xml
<uses-permission android:name="android.permission.INTERNET"/>
<uses-permission android:name="android.permission.ACCESS_NETWORK_STATE"/>
<uses-permission android:name="android.permission.FOREGROUND_SERVICE"/>
<uses-permission android:name="android.permission.FOREGROUND_SERVICE_SPECIAL_USE"/>
<uses-permission android:name="android.permission.POST_NOTIFICATIONS"/>
<uses-permission android:name="android.permission.RECEIVE_BOOT_COMPLETED"/>
<uses-permission android:name="android.permission.REQUEST_IGNORE_BATTERY_OPTIMIZATIONS"/>
<!-- BIND_VPN_SERVICE est implicite via la déclaration <service> avec android:permission -->
```

```xml
<service
    android:name=".LeVoileVpnService"
    android:permission="android.permission.BIND_VPN_SERVICE"
    android:foregroundServiceType="vpn|specialUse"
    android:exported="false">
    <intent-filter>
        <action android:name="android.net.VpnService"/>
    </intent-filter>
    <property
        android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"
        android:value="vpn"/>
</service>
```

**Interactions critiques Kotlin ↔ Go (in-process, JNI via gomobile) :**

| Direction | Méthode | Quand |
|---|---|---|
| Kotlin → Go | `GoCoreAdapter.startTunnel(fd: Int, relayId: String, sessionToken: String)` | Au connect, après `VpnService.Builder.establish()` |
| Kotlin → Go | `GoCoreAdapter.stopTunnel()` | Au disconnect / `onRevoke()` |
| Kotlin → Go | `GoCoreAdapter.fetchRegistry(): String` | Au démarrage / refresh 6h |
| Kotlin → Go | `GoCoreAdapter.requestSessionToken(relayDomain: String, relayPubKey: String): String` | Avant connect |
| Kotlin → Go | `GoCoreAdapter.notifyNetworkChange()` | Sur callback `ConnectivityObserver` |
| Kotlin → Go | `GoCoreAdapter.writePacket(packet: ByteArray)` | Pompe : paquet IP lu depuis VpnService fd vers relais |
| Go → Kotlin (callback) | `PacketCallback.onPacketReceived(packet: ByteArray)` | Pompe : paquet IP arrivé du relais à écrire sur VpnService fd |
| Go → Kotlin (callback) | `StatusCallback.onStateChange(state: String)` | Connecté / Déconnecté / Reconnexion |

**Les callbacks Go → Kotlin sont implémentés via interfaces gomobile** (`ifacestmt` côté Go), enregistrées une fois au startup via `GoCoreAdapter.setCallbacks(packetCb, statusCb)`. Pas de polling Go → Kotlin.

### Infrastructure & Déploiement

- **Relais VPS** (`cmd/relay/`) : Binaire Go autonome HTTP/3 (port 443) avec TLS. Handlers : **Tunnel IP** (/tunnel — stream bidirectionnel paquets IP, NAT, DNS blocklist intégrée), Verify (/verify — session token), IP detection (/ip), health (/health), registry (/.well-known/relay-registry.json). **Supprimé** : /dns-query, /connect, /stun-relay (tous absorbés par /tunnel). Build tag pour traçabilité. Flags : -addr, -cert, -key, -upstream, -fallback, -signing-key, -registry-file, -cf-insecure, -blocklist-url
- **NAT côté relais** : Table NAT en RAM (`sync.Map` keyed par `(session, 5-tuple)`). Alloc port NAT via pool range 10000-60000 avec eviction TTL. Socket userspace ou raw selon protocole (TCP : dial/accept normal, UDP : `net.ListenUDP`, ICMP : raw socket ou abandon au MVP)
- **Relay DNSSEC Validator** (`internal/relay/dns_resolver.go`) : Les réponses DNS upstream sont validées DNSSEC avant forwarding. Cloudflare 1.1.1.1 et Quad9 9.9.9.9 supportent nativement DNSSEC. Réponse SERVFAIL si validation échoue (NFR9f)
- **Relay Session Validator** (`internal/relay/verify_handler.go`) : À chaque ouverture de stream /tunnel, vérification que SHA256(r.RemoteAddr) == IPHash du session token. Rejet HTTP 401 si différent (NFR9d) — le client reçoit son IP-hash au /verify et le relais la vérifie au /tunnel sur le même socket direct.
- **Relay TLS Config** : Serveur TLS obligatoirement TLS 1.3, ciphersuites modernes uniquement. Certificat Let's Encrypt servi directement depuis le VPS, pas de terminaison TLS intermédiaire (NFR9e)
- **TUN Packet Integrity** (`internal/tun/integrity.go`) : Tagging des paquets émis par le pump tunnel (metadata in-memory + checksum), détection de paquets arrivant sur la TUN sans avoir transité par le pump = injection externe. Paquets non tagués ignorés + log (NFR9g)
- **Registre signé** : Inchangé
- **Déploiement relais** : `deploy/install.sh` — Crée user system `levoile`, copie binaire /opt/levoile, cert/key avec permissions 0600, systemd service avec CAP_NET_BIND_SERVICE + CAP_NET_ADMIN (pour NAT si raw sockets), ProtectSystem=strict, ProtectHome=true, NoNewPrivileges=true, restart always
- **Transport** : HTTP/3 direct vers le VPS relais (DNS A record → origin). **Pas de fronting CDN** (Cloudflare ToS §2.8 interdit le non-HTML, coût Enterprise prohibitif, et l'IP origin est de toute façon exposée au DNS sortant NAT — gain sécurité marginal). Les rate-limiters utilisent `r.RemoteAddr` (host portion) comme identité client (clientIP helper dans `internal/relay/middleware.go`)
- **Signature installeur Windows** : Le .exe NSIS final est signé Ed25519 (master key) + optionnellement Authenticode (signature commerciale, à évaluer selon budget). Checksums SHA256 des binaires internes affichés dans la page de téléchargement
- **Installeur Windows NSIS** (`installer/levoile.nsi`) :
  - Install : UAC admin → kill instances → stop/unregister old service → copy binaries (service + UI) + icons + **wintun.dll** → register service SCM → UI autostart HKCU → shortcuts desktop/Start menu → launch UI
  - Uninstall : kill instances → stop/unregister service → **clear WFP filters + remove TUN adapter** → remove shortcuts → cleanup
  - **Supprimé** : déploiement extension Chrome/Firefox, restore WinINET proxy, kill browsers, remove extension files/profiles
- **Packaging Linux** (`packaging/`) :
  - `nfpm-deb.yaml` / `nfpm-rpm.yaml` / `nfpm-apk.yaml` — config par format
  - Dépendances runtime (Debian/Ubuntu) : `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0, libayatana-appindicator3-1, nftables, iproute2`
  - Dépendances runtime (Fedora/RHEL) : `webkit2gtk4.1, libayatana-appindicator-gtk3, nftables, iproute`
  - Dépendances runtime (Arch) : `webkit2gtk-4.1 libayatana-appindicator nftables iproute2`
  - Dépendances runtime (Alpine) : `webkit2gtk-4.1 libayatana-appindicator nftables iproute2`
  - **Signature des paquets** : Tous les paquets .deb, .rpm, .apk sont signés Ed25519 par la master key Le Voile. Les repos (si publiés) exposent la clé publique. Le PKGBUILD AUR embarque un SHA256 du tarball GitHub release + vérification de signature. Les commits du repo AUR sont signés GPG par la clé du mainteneur
  - Scripts post-install : `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW (dans le systemd unit)`, `systemctl daemon-reload`, `systemctl enable --now levoile.service` (si systemd détecté)
  - Scripts pre-remove : `systemctl disable --now levoile.service`
  - Fichiers installés : `/usr/bin/levoile-service`, `/usr/bin/levoile-ui`, `/usr/lib/systemd/system/levoile.service`, `/etc/levoile/config.toml` (skelette), `/usr/share/applications/levoile.desktop`, `/usr/share/icons/hicolor/*/apps/levoile.png`
  - PKGBUILD Arch (`packaging/arch/PKGBUILD`) : cible l'archive .tar.gz release GoReleaser, install dans les mêmes chemins. Publication AUR via GitHub Action `ays-publish`
- **CI/CD** :
  - Build local pour dev (`goreleaser release --snapshot --skip=publish`)
  - GitHub Actions pour release : cross-compile, signatures Ed25519 checksums, upload GitHub release, trigger AUR publish
  - **Security gates obligatoires** (NFR22d/e/f) : `go vet`, `gosec -severity medium`, `govulncheck ./...`, `go test -race ./...`. Build bloqué si échec. Fuzzing `go test -fuzz` hebdomadaire sur parsers critiques (IP packets, STUN, TOML, registre JSON)
  - Renovate bot pour mises à jour dépendances automatiques avec scan vulnérabilités
  - `ldflags="-s -w"` appliqué à tous les binaires release (NFR9h)
  - **Supprimé** : pre-build hook CRX (plus d'extension)
- **Monitoring** : Endpoint `/health` sur chaque relais — métriques anonymes : `{"status":"ok","connections":12,"tunnels":42,"nat_entries":1840,"uptime":"3d12h","ram_mb":220,"cpu_pct":3.4,"country":"de","relay_id":"de-01","rejected_ip_limit_total":0,"rejected_daily_quota_total":0,"throttled_hourly_quota_total":0}`. Aucun log de requêtes, aucune IP client. `tunnels` remplace `connections`, ajout `nat_entries`

### Decision Impact Analysis

**Séquence d'implémentation Android (révision Phase Android — 2026-04-29) :**

_État existant (desktop stable 2026-04-19)_ : noyau Go partagé + arbres Windows + Linux complets et stables, relais inchangé.

_Séquence Android_ :
1. Tag git `pre-android-2026-04-29` pour snapshot avant démarrage
2. Refactor mineur du noyau Go partagé : extraction explicite des packages destinés à gomobile (`internal/protocol/`, `internal/registry/`, `internal/auth/`, `internal/crypto/`, `internal/leakcheck/`). Vérification qu'aucun de ces packages n'importe de packages OS-spécifiques (`internal/tun/`, `internal/firewall/`, etc.). Si import croisé détecté → restructuration locale (jamais de nouvelle abstraction cross-OS partagée — reste desktop-only)
3. Création arbre `android/` complet (squelette Gradle + AndroidManifest + structure Kotlin)
4. Script `android/scripts/build-aar.sh` + variante `.ps1` : invoke gomobile bind pour produire `levoile-core.aar`
5. Wrapper `GoCoreAdapter.kt` autour des classes générées par gomobile (API Kotlin idiomatique)
6. Implémentation `LeVoileVpnService` (VpnService + Foreground Service + lifecycle + pompes paquets)
7. Implémentation `MainActivity` + WebView + `WebViewAssetLoader` + `LeVoileBridge` (JS interface)
8. Sync frontend : script `android/scripts/sync-frontend.sh` copie `frontend/` → `android/app/src/main/assets/`. Adaptation CSS/JS responsive mobile (media queries, layout vertical sans sidebar)
9. `NotificationHelper`, `KillSwitchHelper`, `BatteryOptimizationHelper` (UI helpers Android-spécifiques)
10. `BootReceiver` + `ConnectivityObserver` + `VpnPreferences`
11. Onboarding obligatoire au premier lancement : prompt VpnService consent + guide deeplink "VPN permanent" + (Android 10-11) demande exemption Battery Optimization
12. Tests : JUnit 4 unitaires sur les helpers Kotlin, Espresso instrumentés sur le flow connect/disconnect (avec mock VpnService)
13. F-Droid metadata (`fastlane/metadata/android/`) : descriptions fr/en, screenshots, changelog
14. CI Android : workflow `release-android.yml` — gomobile bind, gradle assembleRelease, signature avec keystore secret CI, upload APK GitHub release, génération `index-v1.json` F-Droid (ou push vers repo `fdroiddata`)
15. Validation e2e : Android 10 (Pixel 3 émul.), Android 12 (Pixel 6 émul.), Android 14 (émul. récent), test kill switch (Wi-Fi → 4G → désactivation Wi-Fi avec "VPN permanent" actif → vérifier blocage), test Doze mode (Battery Optimization off → tunnel maintenu en veille longue), test fuite DNS, test rotation appareil
16. Documentation onboarding utilisateur Android (README + page documentation)

**Dépendances inter-composants Android :**
- `GoCoreAdapter` dépend de `levoile-core.aar` (build artefact)
- `LeVoileVpnService` dépend de `GoCoreAdapter` + `NotificationHelper` + `VpnPreferences`
- `MainActivity` dépend de `LeVoileBridge` + `KillSwitchHelper` + `BatteryOptimizationHelper`
- `LeVoileBridge` dépend de `LeVoileVpnService` (intents start/stop) + `VpnPreferences` (prefs CRUD)
- `BootReceiver` dépend de `VpnPreferences` (lecture flag auto_start) + `LeVoileVpnService` (start)
- `ConnectivityObserver` dépend de `GoCoreAdapter.notifyNetworkChange()`
- **Pas de dépendance vers les packages desktop-only** (`internal/tun/`, `internal/firewall/`, `internal/routing/`, `internal/ui/`, `internal/ipc/`, `cmd/client/`, `cmd/ui/`, `installer/`, `packaging/`, etc.) — l'arbre Android n'a aucune connaissance de ces composants

**Ordre de démarrage Android (équivalent du desktop Connect) :**
```
User tap "Connecter":
  1. MainActivity → check VpnService.prepare() → si null OK, sinon prompt consent
  2. MainActivity → startForegroundService(ACTION_CONNECT)
  3. LeVoileVpnService.onStartCommand → startForeground(notif) [< 5s]
  4. LeVoileVpnService → GoCoreAdapter.fetchRegistry() (si cache stale)
  5. LeVoileVpnService → GoCoreAdapter.selectRelay(preferredCountry)
  6. LeVoileVpnService → GoCoreAdapter.requestSessionToken(relay)
  7. LeVoileVpnService → VpnService.Builder...establish() → ParcelFileDescriptor
  8. LeVoileVpnService → GoCoreAdapter.startTunnel(fd, relayId, sessionToken)
  9. Pompes Kotlin (lecture fd) ↔ JNI ↔ Go (encode + stream HTTP/3)
  10. StatusCallback → state = Connected → notification mise à jour
```

**Ordre de démarrage Connect — détail kill switch Android** :
- **Pas de Activate firewall app-level** (impossible Android sans root)
- Le verrou kill switch est porté par l'OS via "VPN permanent" — l'utilisateur l'a activé une fois lors de l'onboarding
- Si "VPN permanent" + "Bloquer connexions sans VPN" activés : entre le boot et le Connect complet, l'OS bloque tout trafic. **Pas de fenêtre de fuite contrairement au desktop** (qui assume cette fenêtre par ADR-05)
- Si l'utilisateur n'a PAS activé "VPN permanent" : trafic libre entre disconnect et connect — comportement par défaut Android, c'est sa responsabilité (l'UI le rappelle)

**Séquence d'implémentation (révision Linux + TUN — à partir du snapshot Windows stable) :**

_État existant (Windows stable 2026-04-15)_ : crypto, config, IPC, tunnel QUIC, DNS proxy+kill switch+watchdog, relais (DoH + CONNECT + STUN), registry, httpproxy, blocklist, leakcheck, stun, browser, elevation, updater, service, ipchandler, UI webview+systray, frontend, extension, installeur NSIS.

_Séquence de révision_ :
1. Ajouter dépendances : `golang.zx2c4.com/wireguard/tun`, embed `wintun.dll`
2. Nouveau package `internal/tun/` — création/destruction interface `levoile0`, read/write paquets IP, build tags Linux/Windows
3. Nouveau package `internal/firewall/` — interface `Firewall` + impls nftables (Linux) + WFP (Windows). Detection `nft` ou APIs WFP au démarrage
4. Nouveau package `internal/routing/` — ajout/retrait route par défaut + route spécifique relais, build tags
5. Refactor `internal/tunnel/` — remplacer handlers DoH/CONNECT par pompe bidirectionnelle paquets IP ↔ HTTP/3 stream `/tunnel`. Framing 2-byte length
6. Refactor `cmd/relay/` — nouveau handler `/tunnel` avec NAT table + socket dialer + DNS resolver interne + blocklist intégrée. Retrait `/dns-query`, `/connect`, `/stun-relay`
7. Refactor `internal/leakcheck/` — validation TUN au lieu de relay STUN. Retrait `internal/stun/interceptor.go` + `relayer.go`
8. Refactor `internal/service/` — orchestration révisée : TUN + firewall + routing avant tunnel. Ordre de shutdown inversé
9. Refactor `internal/elevation/` — ajout check Linux CAP_NET_ADMIN
10. Simplification `internal/config/` — retrait sections blocklist (client), http_proxy, browser_policies. Ajout sections tun, firewall
11. Suppression `internal/httpproxy/`, `internal/browser/`, `extension/`, `tools/crxgen/`
12. Refactor `internal/dns/` — suppression `proxy.go`, `kill_switch.go`, `reuseaddr_*.go`, `dnscache_*.go`. Conservation éventuelle de `check_*.go` pour validation
13. Ajout `internal/ui/singleton_linux.go` (flock) + adaptation systray Linux (libayatana-appindicator). Retrait `sysproxy_windows.go`
14. Support service systemd (kardianos/service le gère nativement — tester install/uninstall Linux)
15. Packaging : `.goreleaser.yaml` étendu avec targets Linux + nfpm configs. PKGBUILD Arch. Scripts post-install (setcap, systemctl)
16. Installeur NSIS : retrait déploiement extension/restore proxy ; ajout Wintun DLL + création TUN adapter + setup WFP
17. CI/CD GitHub Actions : build multi-OS, signatures, release, AUR publish
18. Validation e2e : build Ubuntu 24.04, Fedora 40, Arch, Alpine 3.19, Debian 12, Windows 11 — test kill switch, fuite DNS, IPv6, reconnexion

**Dépendances inter-composants (révisées) :**
- Tunnel dépend de crypto (pinning, session tokens) + tun (read/write paquets)
- TUN dépend de rien (package racine)
- Firewall dépend de rien (interface pure, impls OS)
- Routing dépend de rien (interface pure, impls OS)
- Registry dépend de crypto (signatures Ed25519)
- Leakcheck dépend de stun (parsing) + tunnel (état connexion)
- Service dépend de : tun, firewall, routing, tunnel, registry, leakcheck, updater, config, elevation
- Ordre de démarrage : elevation check → tun create → routing setup → firewall activate → tunnel connect
- Ordre de shutdown : tunnel disconnect → firewall deactivate → routing cleanup → tun destroy
- Rationale : simplicité. Fenêtre de fuite au boot acceptée (cible grand public, protection à l'usage actif)
- IPC handler dépend de service
- UI dépend de IPC client + singleton + webview + serveur HTTP local
- Updater dépend de crypto + service (restart)

## Implementation Patterns & Consistency Rules

### Points de Conflit Identifiés

20 zones où des agents IA pourraient diverger, toutes résolues ci-dessous. **Patterns triplés** : desktop (Go) + Android (Kotlin) + zone partagée gomobile. Les agents IA Android suivent les conventions Kotlin/AndroidX standards Google + les règles spécifiques ci-dessous ; les agents IA desktop suivent les conventions Go inchangées.

### Naming Patterns

**Packages Go (desktop + noyau partagé — inchangé) :**
- Noms courts, minuscules, un mot si possible : `tunnel`, `tun`, `firewall`, `routing`, `dns`, `crypto`, `relay`, `registry`, `elevation`, `leakcheck`, `stun`, `updater`, `service`, `ui`, `ipc`, `ipchandler`, `config`
- Pas de underscores dans les noms de packages
- **Supprimés** : `httpproxy`, `blocklist` (client), `browser`, `watchdog` (la surveillance devient interne à firewall/tun)

**Fichiers Go :**
- `snake_case.go` : `tun_device.go`, `firewall_nft.go`, `firewall_wfp.go`, `route_manager.go`, `nat_table.go`, `bandwidth_limiter.go`
- Build tags : `tun_linux.go`, `tun_windows.go`, `firewall_linux.go`, `firewall_windows.go`, `routing_linux.go`, `routing_windows.go`, `elevation_linux.go`, `elevation_windows.go`, `paths_linux.go`, `paths_windows.go`
- Stubs : `singleton_stub.go` (non-supported platforms)
- Tests co-localisés : `manager_test.go` à côté de `manager.go`
- Tests edge cases : `*_edge_test.go` (scénarios limites)
- Tests e2e : `e2e_test.go` (intégration)
- Tests platform-specific : `*_windows_test.go`, `*_linux_test.go`

**Fonctions & Types :**
- Exportés : `PascalCase` — `DNSManager`, `ConnectHandler`, `NewRelay`, `VolumeTracker`
- Privés : `camelCase` — `parseResponse`, `retryCount`, `dialRelay`
- Constantes exportées : `PascalCase` — `MaxConnections = 150`, `VolumeThreshold`, `DailyQuotaBytes`
- Constructeurs : `New` + nom du type — `NewDNSManager()`, `NewServer()`, `NewProgram()`

**Tests Go :**
- Nommage : `TestDNSManager_SetResolver`, `TestKillSwitch_Activate`, `TestVolumeTracker_Bypass`
- Table-driven tests quand > 2 cas
- Pas de framework tiers — `testing` standard + `t.Helper()` pour les helpers
- `t.Cleanup()` pour le teardown

**Naming Android (Kotlin/AndroidX — NOUVEAU) :**
- **Package** : `fr.plateformeliberte.levoile` (root) avec sous-packages `bridge`, `ui`, `receivers`, `prefs`, `util`
- **Classes** : `PascalCase`, suffixes selon le rôle Android — `LeVoileVpnService`, `MainActivity`, `LeVoileApplication`, `BootReceiver`, `LeVoileBridge`, `GoCoreAdapter`, `NotificationHelper`, `KillSwitchHelper`
- **Fichiers Kotlin** : un fichier = une classe principale, `PascalCase.kt` (matche le nom de la classe). Extensions/helpers in-file regroupés sous le top-level
- **Fonctions** : `camelCase`, verbes — `connectVpn()`, `buildOngoingNotification()`, `requestVpnPermission()`
- **Constantes** : `UPPER_SNAKE_CASE` dans un companion object — `const val NOTIF_ID = 1001`, `const val ACTION_CONNECT = "fr.plateformeliberte.levoile.ACTION_CONNECT"`
- **Resources** : `snake_case` — `R.drawable.ic_levoile`, `R.string.vpn_status_connected`, `R.layout.activity_main`, `R.id.web_view`
- **Strings** : Clés `snake_case`, valeurs en `<string>` dans `res/values/strings.xml` (anglais par défaut) + `res/values-fr/strings.xml` (français). **Tous les textes utilisateur DOIVENT passer par `R.string.*`** — jamais de hardcode
- **Async / Coroutines** : `viewModelScope` ou `lifecycleScope` pour les coroutines liées à un lifecycle. `Dispatchers.IO` pour les appels JNI Go bloquants. **Pas de `GlobalScope`** (sauf cas explicitement justifiés)
- **Suffixe `Helper`** pour les classes utilitaires sans état (`NotificationHelper`, `KillSwitchHelper`, `BatteryOptimizationHelper`)
- **Suffixe `Service`** pour les classes étendant `Service` (`LeVoileVpnService`)
- **Suffixe `Receiver`** pour les `BroadcastReceiver` (`BootReceiver`)
- **Suffixe `Observer`** pour les `LifecycleObserver` ou `NetworkCallback` (`ConnectivityObserver`)

**Tests Android :**
- **Unitaires (`src/test/`)** : `JUnit 4` + Mockito-Kotlin. Nommage `MyClassTest.kt`, méthodes `fun \`should compute X when Y\`()` (backticks autorisés Kotlin tests)
- **Instrumentés (`src/androidTest/`)** : `Espresso` + `androidx.test.ext`. Nommage `MyClassInstrumentedTest.kt`. Mock `VpnService` via `ServiceTestRule` ou tests sur émulateur avec consentement automatique
- Pas de mocks pour le `.aar` Go — tests d'intégration utilisent le vrai `.aar` (de toute façon, le noyau Go est testé indépendamment côté Go)

### Format Patterns

**IPC Protocol (JSON ligne par ligne) :**
```go
// internal/ipc/messages.go
type Request struct {
    Action string `json:"action"`
    Value  string `json:"value,omitempty"`
}
type Response struct {
    Status  string `json:"status"`
    Message string `json:"message,omitempty"`
    IP      string `json:"ip,omitempty"`
    Country string `json:"country,omitempty"`
    // ... autres champs optionnels
}
```

**API REST locale (serveur HTTP embarqué dans l'UI, remplace les bindings Wails v2) :**
```
// internal/ui/httpserver.go — endpoints REST JSON sur 127.0.0.1:{port}

GET  /api/status          → StatusResponse (état tunnel, IP, pays, latence)
GET  /api/registry        → RegistryResponse (liste pays + relais)
POST /api/connect         → connecte via IPC
POST /api/country         → {code: "is"} sélectionne le pays via IPC
GET  /api/leak-status     → état détection fuites
POST /api/leak-check      → déclenche vérification fuites
GET  /api/update-status   → état mise à jour
POST /api/check-update    → déclenche vérification mise à jour
POST /api/quit            → arrêt propre via IPC
GET  /api/settings        → préférences UI (skip_quit_modal, etc.)
POST /api/settings        → persiste préférences dans config TOML
GET  /                    → sert index.html (assets frontend embarqués)
GET  /assets/*            → sert CSS/JS/fonts/images embarqués

// Le frontend utilise fetch() au lieu de window.go.main.App.Method()
// Exemple : fetch('/api/status').then(r => r.json())
```

**Config TOML (client — révisée) :**
```toml
[relay]
domain = "relay.levoile.dev"
public_key_ed25519 = "rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk="
# insecure = true  # dev only

[client]
auto_start = true
skip_quit_modal = false
preferred_country = "is"

[tun]
name = "levoile0"
mtu = 1420

[firewall]
killswitch = true  # drop tout sauf TUN + IP relais

[stun]
default_server = "stun.l.google.com:19302"

[update]
enabled = true
github_owner = "velia-the-veil"
github_repo = "le_voile"
check_interval = "6h"
rate_limit_kbps = 500

[registry]
enabled = true
url = "https://relay.levoile.dev/.well-known/relay-registry.json"
master_public_key = "..."
refresh_interval = "6h"

[ui]
http_port = 50114
```

**Sections supprimées** : `[blocklist]` (côté relais maintenant), `[http_proxy]` (plus de proxy local), `[browser_policies]` (plus d'extension).

**Préférences Android (SharedPreferences — équivalent fonctionnel TOML, mais format Android natif) :**

```kotlin
// android/app/src/main/kotlin/fr/plateformeliberte/levoile/prefs/VpnPreferences.kt
class VpnPreferences(context: Context) {
    private val prefs = context.getSharedPreferences("levoile_prefs", Context.MODE_PRIVATE)

    var autoStart: Boolean
        get() = prefs.getBoolean("auto_start", false)
        set(v) = prefs.edit().putBoolean("auto_start", v).apply()

    var preferredCountry: String
        get() = prefs.getString("preferred_country", "is") ?: "is"
        set(v) = prefs.edit().putString("preferred_country", v).apply()

    var skipQuitModal: Boolean
        get() = prefs.getBoolean("skip_quit_modal", false)
        set(v) = prefs.edit().putBoolean("skip_quit_modal", v).apply()

    var killSwitchOnboardingShown: Boolean
        get() = prefs.getBoolean("kill_switch_warned", false)
        set(v) = prefs.edit().putBoolean("kill_switch_warned", v).apply()

    var batteryOptOnboardingShown: Boolean
        get() = prefs.getBoolean("battery_optimization_warned", false)
        set(v) = prefs.edit().putBoolean("battery_optimization_warned", v).apply()

    var lastRelayId: String?
        get() = prefs.getString("last_relay_id", null)
        set(v) = prefs.edit().putString("last_relay_id", v).apply()
}
```

**Pas d'équivalent du `[relay]` config TOML sur Android** : la `relay_domain` + `master_public_key` sont compilées en dur dans le `.aar` (constantes Go) — l'utilisateur Android ne configure pas le relais bootstrap (cohérent avec UX mobile : zéro configuration). Le registre signé fournit ensuite la liste complète.

**Pas d'équivalent du `[ui]` http_port sur Android** : pas de serveur HTTP loopback, le bridge JS est in-process.

**Registre JSON signé (relay-registry.json) :**
```json
{
  "version": 1,
  "master_public_key": "base64...",
  "relays": [
    {
      "id": "relay-es-01",
      "domain": "es.levoile.dev",
      "public_key": "base64...",
      "signature": "base64...",
      "added": "2026-04-09T22:22:36Z"
    },
    {
      "id": "relay-gb-01",
      "domain": "gb.levoile.dev",
      "public_key": "base64...",
      "signature": "base64...",
      "added": "2026-04-09T22:22:36Z"
    },
    {
      "id": "relay-de-01",
      "domain": "de.levoile.dev",
      "public_key": "base64...",
      "signature": "base64...",
      "added": "2026-04-09T22:22:36Z"
    }
  ]
}
```

**Health Endpoint JSON :**
```json
{"status":"ok","connections":12,"tunnels":42,"nat_entries":1840,"uptime":"3d12h","ram_mb":220,"cpu_pct":3.4,"country":"de","relay_id":"de-01","rejected_ip_limit_total":0,"rejected_daily_quota_total":0,"throttled_hourly_quota_total":0}
```

### Error Handling Patterns

**Côté Go (desktop + noyau partagé) :**
- Wrapping systématique : `fmt.Errorf("tunnel: connect: %w", err)`
- Préfixe = nom du package : `tunnel:`, `dns:`, `service:`, `ipc:`
- Erreurs sentinelles pour les cas récupérables : `var ErrInvalidKey = errors.New(...)`, `var ErrPinningFailed = errors.New(...)`
- Jamais de `panic` sauf bug critique — toujours retourner `error`
- **Logging relais** : `fmt.Fprintf(os.Stderr, ...)` — messages opérationnels uniquement, jamais de données utilisateur
- **Logging client desktop** : Pas de framework log. Erreurs propagées vers le service → IPC → frontend pour affichage

**Côté Android (Kotlin) — NOUVEAU :**
- Erreurs Go remontées via gomobile sont des `Exception` Java côté Kotlin (gomobile mappe `error` Go → `Exception` Java). **Toujours wrapper en `Result<T>` ou exceptions custom Kotlin** dans `GoCoreAdapter` pour ne pas leak les types gomobile dans le reste de l'app
- Exceptions custom : `LeVoileVpnException` (sealed class) avec sous-types : `RelayUnavailable`, `SessionTokenInvalid`, `VpnPermissionDenied`, `KillSwitchNotConfigured`, `NetworkUnreachable`
- **Logging Android** : `android.util.Log` avec tag par classe (`Log.w("LeVoileVpnService", "tunnel error", e)`). En release, niveau VERBOSE/DEBUG masqué via ProGuard ou check `BuildConfig.DEBUG`. **Jamais logger d'IP, de payload paquet, de token** — même en debug (toute fuite via `adb logcat` est inacceptable)
- Crash reporting : **aucun crash reporter externe** (pas de Firebase Crashlytics, Sentry, etc.) — incompatible avec la promesse zéro-log. Les crashes restent locaux, l'utilisateur peut envoyer manuellement un bug report depuis le menu (export texte, sans IP)
- **Messages utilisateur en français + anglais** : `strings.xml` (en) par défaut + `values-fr/strings.xml` (fr) — Android sélectionne automatiquement selon locale système. Statuts affichés au frontend toujours via `R.string.*` (jamais de hardcode dans le bridge)

### Relay Selection & Failover Patterns (inchangés)

- **Découverte** (`internal/registry/discoverer.go`) : Orchestration fetch → verify (Ed25519 master key) → cache → default relay. Refresh background toutes les 6h
- **Sélection par pays** : L'utilisateur choisit un pays dans l'UI → IPC SelectCountry → service → discoverer filtre les relais actifs pour ce pays
- **Failover automatique** (`internal/registry/failover.go`) : En cas d'échec connexion, bascule vers le relais suivant dans le pool. Retry logic. Pendant le failover, le kill switch firewall reste actif — aucune fuite possible entre deux connexions
- **Latency measurement** (`internal/registry/latency.go`) : Mesure RTT via GET /health sur chaque relais. Tri par latence. Cache + mesures fraîches
- **Country metadata** (`internal/registry/countries.go`) : Map pays → nom affiché + drapeau emoji. Extraction code pays depuis relay ID ou domain
- **Cache local** (`internal/registry/cache.go`) : Persistence fichier JSON des relais vérifiés. Source de vérité quand aucun relais ne répond
- **Bootstrap** : Premier lancement → relay domain par défaut dans config (relay.levoile.dev)

### Android — VpnService / Foreground Service / JNI Bridge Patterns (NOUVEAU)

- **Lifecycle VpnService** :
  - `onStartCommand(intent, flags, startId)` : router selon `intent.action` (`ACTION_CONNECT` / `ACTION_DISCONNECT` / `null` = restart). Toujours retourner `START_REDELIVER_INTENT` pour qu'un crash redémarre avec le dernier intent
  - `onCreate()` : init `NotificationHelper`, init `GoCoreAdapter` callbacks
  - `onDestroy()` : nettoyage final, `stopForeground(STOP_FOREGROUND_REMOVE)`, `GoCoreAdapter.stopTunnel()` idempotent
  - `onRevoke()` : appelée par OS si l'utilisateur change de VPN ou désactive Le Voile dans Settings → flow Disconnect propre
  - Toujours `startForeground(notifId, notification)` < 5 secondes après `onStartCommand` sinon ANR + kill par OS
  - **Pattern singleton** : check `if (instance != null) { handle existing }` au début de `onStartCommand` pour éviter doubles instances
- **Pattern JNI bridge in-process** :
  - Tous les appels Kotlin → Go passent par `GoCoreAdapter` (singleton dans `LeVoileApplication`)
  - `GoCoreAdapter` cache l'instance gomobile et expose une API Kotlin (suspend functions, Result<T>)
  - **Aucune classe Kotlin hors `GoCoreAdapter` n'importe directement le package gomobile généré** — frontière étroite garantie
  - Callbacks Go → Kotlin : interfaces enregistrées une fois au startup via `GoCoreAdapter.setCallbacks(packetCb, statusCb, errorCb)`. Pas de polling
- **Pattern pompe paquets VpnService ↔ Go** :
  - Côté Kotlin : `Thread { while (running) { val n = fis.read(buf); GoCoreAdapter.writePacket(buf, n) } }` (lecture bloquante du fd VpnService)
  - Côté Go : reçoit le paquet via gomobile, encode dans le protocole tunnel, écrit sur le stream HTTP/3
  - Inversement : Go reçoit du stream HTTP/3, décode, appelle `packetCb.onPacketReceived(packet)` → Kotlin écrit sur le fd VpnService via `fos.write(packet)`
  - **Benchmark à valider** : si le coût JNI par paquet est > 5% CPU à 100 Mbps, batcher les paquets (envoyer N paquets par appel JNI) ou passer le fd directement à Go via gomobile et faire toute la pompe en Go (nécessite que gomobile expose `int` fd → `os.NewFile(uintptr(fd), "vpn")` côté Go)
- **Pattern Foreground Service notification** :
  - Channel `levoile_vpn_status` créé une fois dans `LeVoileApplication.onCreate()` (importance LOW, sans son ni vibration)
  - Notification builder dans `NotificationHelper.buildOngoingNotification(state, country, ip)`
  - Action "Déconnecter" : `PendingIntent` vers `LeVoileVpnService` action `DISCONNECT` avec `FLAG_IMMUTABLE` (Android 12+)
  - Mise à jour notification à chaque changement d'état (sans recréer la notif, juste `notify(notifId, updatedNotif)`)
- **Pattern kill switch UX** :
  - Au premier lancement, `MainActivity.checkAndPromptKillSwitch()` :
    1. Si `KillSwitchHelper.isAlwaysOnEnabled()` retourne true → OK, marquer `killSwitchOnboardingShown = true`
    2. Sinon → afficher modal obligatoire qui explique en français "Pour que Le Voile bloque les fuites quand le tunnel est interrompu, activez 'VPN permanent' + 'Bloquer les connexions sans VPN' dans Réglages Android"
    3. Bouton "Ouvrir Réglages" → `startActivity(Intent(Settings.ACTION_VPN_SETTINGS))`
    4. Au retour de `MainActivity.onResume()`, re-check. Si toujours non activé → option "Continuer sans" (warning persistant dans l'UI principale) ou "Réessayer"
  - `KillSwitchHelper.isAlwaysOnEnabled()` : impossible à query directement avec API publique Android → on utilise un heuristique (`Settings.Global.getString(contentResolver, "always_on_vpn_app")` matche package name + `Settings.Global.getInt("always_on_vpn_lockdown")` == 1). **Heuristique fragile** : Android peut casser ces accès dans futures versions, prévoir fallback "non vérifiable"
- **Pattern responsive WebView** :
  - Frontend détecte mobile via `window.matchMedia('(max-width: 600px)')` ou `navigator.userAgent.includes('Android')`
  - Layout vertical, sélecteur pays via menu déroulant ou bottom-sheet (vs sidebar desktop)
  - Tactile-friendly : boutons min 48dp, pas de hover effects (CSS `@media (hover: none)`)
- **Pattern coroutines Kotlin** :
  - Appels JNI bloquants → toujours sur `Dispatchers.IO`
  - Updates UI (notification, WebView) → `Dispatchers.Main`
  - Pas de `runBlocking` en production (sauf init `Application`)
  - `viewModelScope` ou `lifecycleScope` pour cleanup automatique au destroy

### Patterns OS-isolation (NOUVEAU — règle directrice)

**Règle structurelle** : un agent IA travaillant sur un OS donné ne doit JAMAIS modifier le code des autres OS pour factoriser. Si tu identifies de la duplication entre Windows et Linux et Android — **c'est intentionnel**, ne propose pas de DRY refactor.

**Frontière du noyau partagé Go** :
- Liste fermée des packages exposés à Android via gomobile (voir Section "Stack Android" → "Noyau Go partagé")
- Ces packages ne doivent JAMAIS importer un package OS-spécifique (`internal/tun/`, `internal/firewall/`, `internal/ui/`, etc.)
- Lint check possible : un test Go qui parse les imports de chaque package partagé et fail s'il référence un package OS-spécifique

**Si une fonctionnalité doit exister sur les 3 OS** :
1. **Default** : 3 implémentations indépendantes (Go pour Win, Go pour Linux, Kotlin pour Android). Chacune suit les conventions de son OS
2. **Exception** : si la logique est purement métier (parsing, calcul, crypto, sérialisation) sans dépendance OS → ajouter au noyau partagé. **Justification obligatoire dans un ADR** avant ajout

**Exceptions ADR-08 connues — fichiers racine partagés que les stories Android *peuvent* toucher** :

Bien que la règle générale interdise aux stories Android de modifier quoi que ce soit hors `android/`, deux fichiers racine sont par nécessité partagés. Toute story Android qui les modifie doit le déclarer explicitement dans son périmètre + sa file list :

| Fichier racine | Quand modifié | Pourquoi (ADR-09 cohérent) | Borne |
|---|---|---|---|
| `go.mod` + `go.sum` | Story 9.2 (introduction `golang.org/x/mobile`), puis bump occasionnel suivant les besoins gomobile | `gomobile bind` exige que les packages cibles vivent dans un module Go avec `golang.org/x/mobile` listé en `require`. Sans ça, aucun build Android possible. La dépendance est annotée `// indirect` car elle n'est pas importée par le code racine — elle est build-time uniquement et n'apparaît pas dans les binaires desktop (validable via `go list -deps` sur les packages desktop) | Aucune autre dépendance ne doit être ajoutée. Aucun module existant ne doit être retiré. Les bumps transitifs (`golang.org/x/{crypto,net,sys,text,mod,tools}`) entraînés par `go get golang.org/x/mobile@latest` sont acceptés sous condition que `go test ./internal/...` racine + `go vet ./...` (racine + windows + linux + relay) restent verts |
| `android/shims/*` qui importent `internal/{crypto,registry,leakcheck,tunnel}` | Toute story qui élargit la surface JNI re-exposée à Kotlin | Les 5 packages partagés via gomobile (`internal/{protocol,auth,crypto,registry,leakcheck}`) sont définitionellement consommés depuis `android/shims/*` — c'est l'unique frontière de packaging Android. Lecture seule pour les packages desktop (`internal/tunnel` etc.) — pas de PR Android qui modifie un fichier sous `internal/` | Chaque shim ne re-expose qu'une liste fermée de fonctions documentée dans la file de la story. Le script `android/scripts/verify-shared-imports.sh` (Story 9.2) fail si un shim importe un package OS-spécifique (`internal/tun/`, `internal/firewall/`, etc.) |

Toute autre modif racine par une story Android est une violation ADR-08 et doit être refusée en code review.

### TUN / Firewall / Routing Patterns (Desktop — Windows + Linux)

- **Lifecycle TUN** (`internal/tun/`) :
  - `New(name, mtu) (*Device, error)` crée l'interface (`/dev/net/tun` Linux, Wintun adapter Windows)
  - `Read()` / `Write()` blocking I/O sur paquets IP bruts
  - `Close()` — détruit l'interface proprement
  - Jamais partagée entre goroutines — une goroutine lecture, une goroutine écriture (pattern wireguard-go)
- **Lifecycle Firewall** (`internal/firewall/`) :
  - `Activate(relayIP net.IP, tunName string) error` — applique les règles
  - `Deactivate() error` — retire les règles (idempotent)
  - `IsActive() (bool, error)` — inspection des règles actuelles
  - Linux : génération du ruleset nftables en template Go, application atomique via `nft -f -` (flush table `inet levoile` puis apply)
  - Windows : WFP provider + sublayer créés au premier Activate, filters ajoutés/retirés à chaque Activate/Deactivate
  - **Règle de base** : Activate AVANT le tunnel, Deactivate APRÈS la fermeture du tunnel
- **Lifecycle Routing** (`internal/routing/`) :
  - `Setup(tunName, relayIP, origGateway) error` / `Teardown() error`
  - Capture la route par défaut originale avant modification (pour restauration + route spécifique relais)
- **Ordre strict d'orchestration (service) — modèle simple** :
  ```
  Connect:
    elevation.Check() → tun.New() → routing.Setup() → firewall.Activate(relayIP, tunName) → tunnel.Connect()
  
  Disconnect:
    tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()
  ```
  **Garantie** : une fois la connexion établie, le firewall bloque toute fuite. **Fenêtre de fuite au boot acceptée** (quelques secondes entre démarrage système et Connect complet) — le produit vise le grand public, la protection commence à l'usage actif des services.
- **Watchdog TUN** : goroutine qui ping l'interface toutes les 3s (check `ip link show levoile0` / Wintun status). Si TUN disparue (ex: user admin l'a tuée) → déclenche reconnect complet avec activation kill switch

### UI Patterns Desktop (webview/webview + fyne.io/systray — processus unique)

- **Charte visuelle** : Reproduit plateformeliberte.fr — fond sombre `#0b1526`, bleu primaire `#1a6fc4`, accents `#2a8dff`, alertes `#d42b2b`
- **Fenêtre webview** : 420×540px, ouverte/fermée à la demande depuis le tray. Navigate vers `http://127.0.0.1:{port}/`
- **Layout** : Sidebar 150px (sélecteur pays) + Main panel (statut connexion centré)
- **Sélecteur de pays** : Liste avec drapeaux emoji + nom du pays + nombre de relais + indicateur actif
- **Statut connexion** : Dot animé (vert connecté steady, orange connecting pulse 1.5s, rouge déconnecté), pays, IP visible, relay ID, latence, uptime
- **Bouton** : Connect uniquement (visible quand déconnecté, masqué quand connecté). L'utilisateur quitte via le bouton X ou le tray → Quitter
- **Modal** : Confirmation quitter avec "Ne plus afficher" checkbox, persisté config TOML
- **Polling** : Frontend JS poll `fetch('/api/status')` toutes les 2s, `fetch('/api/registry')` toutes les 30s
- **Tray (même processus)** :
  - fyne.io/systray sur le thread principal (bloquant — requis par systray)
  - Menu contextuel : Ouvrir la fenêtre (ouvre/montre la fenêtre webview), Quitter
  - Polling status service via IPC toutes les 2s
  - Icônes tray embarquées via `//go:embed` : connected.ico / connecting.ico / disconnected.ico
  - Proxy système WinINET : set au connect, restore au quit (ForceDisable en fallback)
  - Singleton Windows : mutex nommé pour empêcher instances multiples
- **Lifecycle** :
  - Lancement → tray démarre sur main thread, serveur HTTP local démarre en goroutine
  - "Ouvrir" depuis tray → crée/montre la fenêtre webview
  - Fermer la fenêtre (X) → shutdown complet : déconnexion tunnel, restauration proxy WinINET, arrêt service, exit tray. Le raccourci bureau relance le tout (run as admin)
  - "Quitter" depuis tray → arrêt serveur HTTP, libération webview, sortie processus

### UI Patterns Android (WebView Android + Foreground Service notification — NOUVEAU)

- **Charte visuelle** : Identique desktop — assets HTML/CSS/JS partagés via `android/scripts/sync-frontend.sh`. Adaptations responsive uniquement
- **Activité unique** (`MainActivity`) : héberge un `WebView` plein-écran. Pas de fenêtre flottante (concept inexistant Android), pas de fenêtre 420×540 fixée — le layout s'adapte
- **WebViewAssetLoader** : sert les assets locaux via `https://appassets.androidplatform.net/assets/...` — virtual host HTTPS-like, plus sécurisé que `file://` (Android le restreint progressivement)
- **JS Bridge** :
  - Exposé via `webView.addJavascriptInterface(LeVoileBridge(this), "LeVoile")`
  - Frontend appelle `window.LeVoile.connect()`, `window.LeVoile.getStatus()`, etc.
  - **Toutes les méthodes `@JavascriptInterface` retournent String (JSON) ou primitives Boolean/Int** — gomobile et JS Bridge gèrent mal les types complexes
- **Sélecteur de pays** : sur mobile, drop-down ou bottom-sheet plutôt que sidebar
- **Statut connexion** : même composant centré (dot animé + pays + IP + relay ID + latence + uptime)
- **Bouton Connect** : visible quand déconnecté, masqué quand connecté. Quitter l'app : bouton retour Android (back) ou home — l'app passe en background, le Foreground Service continue. **Pas de bouton "Quitter" dans l'UI** : pour vraiment arrêter, l'utilisateur déconnecte via la notification ou via Réglages Android > Apps > Le Voile > Forcer l'arrêt
- **Notification persistante du Foreground Service (joue le rôle du tray)** :
  - Channel `levoile_vpn_status` (importance LOW)
  - Title : "Le Voile · Connecté" / "Le Voile · Déconnecté" / "Le Voile · Reconnexion..."
  - Subtitle : pays + IP visible
  - Action principale : "Déconnecter" (PendingIntent → `LeVoileVpnService.ACTION_DISCONNECT`)
  - Tap sur la notif : ouvre `MainActivity` (PendingIntent vers MainActivity)
  - Icône : `R.drawable.ic_levoile_status` (mono-couleur Android style)
- **Polling status (équivalent du polling 2s desktop)** :
  - WebView appelle `setInterval(() => fetch... no wait, c'est window.LeVoile.getStatus()` toutes les 2s
  - **Optimisation Android** : quand l'app passe en background (`MainActivity.onPause`), le polling JS continue mais l'UI WebView est pausée par le système — pas d'impact perf
  - Quand back en foreground (`onResume`), refresh immédiat puis reprise polling normal
- **Lifecycle** :
  - Premier lancement → onboarding obligatoire (consentement VpnService + activation "VPN permanent" guidée + opt-in Battery Optimization)
  - Fermeture app (back/home) → l'Activity est détruite, le Foreground Service continue (notification persistante reste)
  - Tap notification → re-création Activity (la WebView re-crée son état via `onSaveInstanceState`)
  - Disconnect via notification action → Service stoppe, notification disparaît, app revient à l'état "Déconnecté"
- **Configuration changes** : `MainActivity` déclare `android:configChanges="orientation|screenSize|smallestScreenSize|keyboardHidden|navigation"` pour éviter recréation Activity sur rotation/changement clavier (préserve l'état WebView)
- **Permissions runtime** : `POST_NOTIFICATIONS` (Android 13+) demandée au premier lancement de `LeVoileVpnService` via `ActivityCompat.requestPermissions`. Si refusée → notif foreground masquée mais service continue (Android l'autorise pour les FGS en cours, juste pas de notification visible)

### ~~Extension Navigateur Patterns~~ — **SUPPRIMÉ**

Retrait complet — la capture L3 (TUN/Wintun/VpnService) capture tout le trafic navigateur sans configuration. Bypass > 50 Mo abandonné.

### Concurrency Patterns

**Côté Go (desktop + noyau partagé) :**
- `context.Context` passé en premier argument partout (timeout, cancellation)
- Channels pour la communication inter-goroutines (état tunnel → service, watchdog → DNS)
- `sync.Mutex` pour état partagé simple (config save, compteurs)
- `atomic.Int64` pour le compteur de connexions relais
- `sync.Map` + atomics pour rate limiting lock-free (IP limiter, bandwidth limiter)
- Jamais de goroutines orphelines — toujours gérées via context ou `sync.WaitGroup`
- Pattern standard : `go func() { defer wg.Done(); ... }()`
- Semaphore pattern via buffered channel (STUN relayer : 20 slots)
- UI Desktop : tray sur main thread (bloquant), serveur HTTP + IPC polling en goroutines, webview créé/détruit à la demande

**Côté Kotlin (Android) :**
- **Coroutines** au lieu de threads bruts : `viewModelScope`, `lifecycleScope`, `CoroutineScope(Dispatchers.IO + SupervisorJob())` pour le scope du Service
- **Dispatchers** :
  - `Dispatchers.Main` : UI updates (WebView, notification builder finale)
  - `Dispatchers.IO` : appels JNI Go bloquants, lecture/écriture fd VpnService, accès SharedPreferences
  - `Dispatchers.Default` : transformations CPU (rare dans cette app)
- **Pompes paquets** : threads Java classiques (`Thread { while(running) { ... } }`) car la lecture `FileInputStream(fd).read()` est bloquante kernel et ne profite pas des coroutines. Threads daemon, interrompus à `stopTunnel()`
- **Pas de `runBlocking`** en production (sauf init `Application.onCreate()`)
- **Service scope** : `CoroutineScope(Dispatchers.IO + SupervisorJob())` créé dans `LeVoileVpnService.onCreate`, cancelé dans `onDestroy` (cleanup automatique des coroutines en cours)
- **Mutex Kotlin** (`kotlinx.coroutines.sync.Mutex`) pour exclusion mutuelle async (vs `java.util.concurrent.locks` bloquants)
- **Atomics** : `AtomicReference<TunnelState>` pour l'état tunnel partagé entre threads pompe + coroutines UI

**Frontière JNI (gomobile) :**
- Les appels Kotlin → Go (JNI) sont **synchrones et bloquants** (gomobile ne supporte pas suspend functions natifs). Wrapper en `suspend fun` côté Kotlin via `withContext(Dispatchers.IO) { goCore.callMethod() }`
- Les callbacks Go → Kotlin (interfaces enregistrées) s'exécutent sur un thread Go. **Bridger vers `Dispatchers.Main` ou `Dispatchers.IO` côté Kotlin** : `override fun onPacketReceived(buf: ByteArray) { scope.launch(Dispatchers.IO) { ... } }`. Ne JAMAIS faire d'opération bloquante dans le callback direct (bloquerait le thread Go)

### Assets

**Desktop :**
- Icônes tray dans `internal/ui/icons/` (embedded via `//go:embed icons`) — `.ico` pour Windows, `.png` pour Linux (connected/connecting/disconnected + levoile)
- **Wintun DLL** dans `internal/tun/wintun/` (embedded via `//go:embed wintun/wintun.dll` — uniquement inclus sur build Windows via build tag). Extraite au premier démarrage dans `%ProgramData%/LeVoile/wintun.dll`
- Fonts frontend dans `frontend/assets/fonts/` — BebasNeue, Rajdhani, Inter (woff2)
- Frontend assets dans `frontend/` (embedded via `//go:embed` dans `internal/ui/embed.go`, servis par le serveur HTTP local)
- Icône application Linux dans `assets/icons/hicolor/` (256×256, 128×128, 64×64, 48×48, 32×32, 16×16) pour le standard freedesktop
- **Supprimé** : extension CRX/XPI

**Android (NOUVEAU — duplication assumée des assets visuels en multi-densité) :**
- Icône app dans `android/app/src/main/res/mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}/ic_launcher.png` + `ic_launcher_foreground.png` (adaptive icon Android 8+) + `ic_launcher_background.xml` (couleur charte)
- Icône notification dans `android/app/src/main/res/drawable/ic_levoile_status.xml` (vector drawable mono-couleur — Android exige icônes notif blanches/transparentes)
- Frontend HTML/CSS/JS dans `android/app/src/main/assets/` — copié depuis `frontend/` au build via `android/scripts/sync-frontend.sh` (gitignoré pour éviter divergence). **Fonts woff2 réutilisées tel quel** (Android WebView les supporte)
- Strings localisés dans `android/app/src/main/res/values/strings.xml` (en) + `values-fr/strings.xml` (fr)
- Themes Material You compatible : `android/app/src/main/res/values/themes.xml` — couleur primaire `#1a6fc4` matching charte
- **`levoile-core.aar` produit par gomobile** : `android/levoile-core/libs/levoile-core.aar` — gitignoré, regen à chaque modif noyau partagé
- **Pas de Wintun DLL Android** (concept inexistant)
- **Pas d'icône freedesktop** (concept Linux uniquement)

### Enforcement Guidelines

**Tous les agents IA Desktop (Go) DOIVENT :**
- Suivre les conventions de nommage Go ci-dessus sans exception
- Utiliser `context.Context` comme premier paramètre de toute fonction bloquante ou réseau
- Wrapper les erreurs avec le préfixe du package
- Ne jamais logger de données utilisateur côté relais (IP, DNS queries, contenu paquets)
- Utiliser les build tags pour le code spécifique OS (Linux / Windows)
- Co-localiser les tests avec le code source
- Communiquer service ↔ UI exclusivement via IPC (jamais d'import direct entre ces packages)
- Toujours passer par le registry discoverer/failover pour obtenir un relais
- Respecter la charte visuelle plateformeliberte.fr dans tout le frontend
- Toute logique d'orchestration passe par `internal/service/` — les packages internes ne s'appellent pas entre eux directement (sauf via interfaces)
- L'IPC handler (`internal/ipchandler/`) est le seul point d'entrée pour les requêtes UI vers le service
- Messages utilisateur affichés au frontend en français
- **Respecter l'ordre strict Connect/Disconnect** : elevation → tun → routing → firewall → tunnel / tunnel → firewall → routing → tun
- **Ne jamais désactiver le firewall sans reconnect immédiat** — le kill switch doit survivre aux déconnexions/failover
- **Parser un paquet IP** : toujours valider version (4/6), longueur, checksum avant forwarding

**Tous les agents IA Android (Kotlin) DOIVENT (NOUVEAU) :**
- Suivre les conventions Kotlin / AndroidX standards Google + les règles ci-dessus
- Tous les textes utilisateur passent par `R.string.*` (strings.xml en + values-fr/strings.xml fr) — jamais de hardcode
- Tous les appels JNI vers Go passent par `GoCoreAdapter` — aucune classe hors `GoCoreAdapter` n'importe les types gomobile générés
- Coroutines dispatchées : appels JNI sur `Dispatchers.IO`, UI sur `Dispatchers.Main`
- Tous les Intents inter-app utilisent `FLAG_IMMUTABLE` (Android 12+ requis)
- `LeVoileVpnService.startForeground()` appelé < 5 secondes après `onStartCommand` sans exception
- Pompe paquets : threads daemon, interrompus à `stopTunnel()`, jamais de leak
- **Onboarding kill switch obligatoire** au premier lancement — pas de bypass possible (sauf "Continuer sans" explicite avec warning persistant)
- **Aucun crash reporter externe** (pas de Firebase, Sentry, etc.) — incompatible promesse zéro-log
- **Aucun analytics, télémétrie, beacon** — Le Voile Android n'envoie AUCUNE donnée hors du tunnel utilisateur
- Permissions runtime demandées juste-à-temps (pas au démarrage app)

**Tous les agents IA — Règles cross-OS (NOUVEAU) :**
- **Isolation OS maximale** : ne JAMAIS proposer de refactor "DRY cross-OS" qui partagerait du code OS-spécifique. Si tu vois de la duplication entre Win/Linux/Android — c'est intentionnel
- **Frontière noyau partagé Go étroite** : avant d'ajouter un nouveau package au `.aar` Android (gomobile bind), justifier dans un ADR. Par défaut, ne pas exposer
- **Tests de la frontière** : un test Go vérifie que les packages exposés gomobile n'importent pas de packages OS-spécifiques (`internal/tun/`, `internal/firewall/`, etc.)
- **Documentation par OS** : un changement OS-spécifique doit mentionner explicitement l'OS cible dans le commit message + dans les commentaires si nécessaire

**Anti-Patterns à éviter :**

_Go (desktop + noyau partagé) :_
- `panic` pour des erreurs récupérables
- Goroutines sans context ni WaitGroup
- `log.Fatal` / `log.Println` côté client
- Mutex imbriqués (risque deadlock)
- Imports circulaires entre packages `internal/`
- Import direct de `internal/service` depuis `internal/ui` (passer par IPC)
- Connexion directe à un relais sans passer par le registry discoverer
- Hardcoder des endpoints de relais (sauf le domaine par défaut dans config)
- **Activer la TUN sans activer le firewall** — crée une fuite potentielle pendant la fenêtre de connexion
- **Utiliser iptables** (même en fallback) — nftables exclusif
- **Réintroduire un proxy L7 local** (HTTP CONNECT, DNS UDP 127.0.0.1:53) — tout passe par TUN maintenant
- **Réintroduire une extension navigateur** — redondant avec la capture L3
- **Importer un package OS-spécifique depuis un package partagé gomobile** (`internal/tun/` depuis `internal/protocol/` etc.) — casse le build Android

_Kotlin (Android) :_
- `runBlocking` en production
- `GlobalScope` pour des coroutines liées à un lifecycle
- Importer directement les types gomobile générés en dehors de `GoCoreAdapter`
- Logger des données utilisateur (IP, payload, token) — même en debug
- Crash reporters externes (Firebase Crashlytics, Sentry, Bugsnag, etc.)
- Analytics ou télémétrie de toute nature
- Bloquer le main thread (UI) avec un appel JNI ou IO
- **Tenter de simuler un kill switch app-level** (reconnect-loop, drop sockets) au lieu d'utiliser le verrou OS "VPN permanent"
- Forcer l'utilisateur à donner Battery Optimization exemption avant Connect (Android 12+ pas nécessaire — laisser optionnel)
- Demander `READ_EXTERNAL_STORAGE`, `ACCESS_FINE_LOCATION`, `READ_PHONE_STATE` ou autres permissions sensibles non nécessaires
- Hardcoder les textes utilisateur (toujours via `R.string.*`)
- **Refactor cross-OS** : proposer une factorisation Kotlin/Java + Go en interface partagée pour réduire duplication — refusé par principe (voir feedback memory)

## Project Structure & Boundaries

### Complete Project Directory Structure

```
le_voile/
├── go.mod
├── go.sum
├── .goreleaser.yaml                   # Windows (service+UI+NSIS) + Linux (.deb/.rpm/.apk) + relay
├── config.example.toml                # Template config
├── .gitignore
├── LICENSE
├── README.md
│
├── cmd/
│   ├── client/
│   │   ├── main.go                    # Service cross-platform (SCM Windows + systemd Linux)
│   │   └── main_test.go
│   ├── ui/
│   │   └── main.go                    # UI : fyne.io/systray + webview/webview + HTTP local
│   ├── relay/
│   │   ├── main.go                    # Relais VPS HTTP/3 (port 443) — tunnel IP handler
│   │   └── main_test.go
│   ├── ctl/                           # NOUVEAU — CLI opérationnel (levoile-ctl)
│   │   ├── main.go                    # killswitch off/on, status, reconnect, logs
│   │   └── main_test.go
│   └── genregistry/
│       └── main.go                    # Outil génération registre signé
│
├── internal/
│   ├── service/
│   │   ├── service.go                 # Orchestrateur principal — lifecycle tous composants
│   │   ├── service_test.go
│   │   ├── service_edge_test.go
│   │   └── e2e_recovery_test.go
│   │
│   ├── tun/                           # NOUVEAU — interface TUN/Wintun
│   │   ├── device.go                  # Interface Device (Read/Write/Close)
│   │   ├── device_test.go
│   │   ├── device_linux.go            # /dev/net/tun via wireguard/tun
│   │   ├── device_linux_test.go
│   │   ├── device_windows.go          # Wintun via wireguard/tun
│   │   ├── device_windows_test.go
│   │   └── wintun/
│   │       └── wintun.dll             # DLL signée Microsoft (embed windows only)
│   │
│   ├── firewall/                      # NOUVEAU — kill switch OS-level
│   │   ├── firewall.go                # Interface Firewall (Activate/Deactivate/IsActive)
│   │   ├── firewall_test.go
│   │   ├── firewall_linux.go          # nftables via nft shellout
│   │   ├── firewall_linux_test.go
│   │   ├── firewall_windows.go        # Windows Filtering Platform (WFP)
│   │   ├── firewall_windows_test.go
│   │   ├── ruleset.nft.tmpl           # Template ruleset nftables
│   │   └── e2e_test.go
│   │
│   ├── routing/                       # NOUVEAU — routes système
│   │   ├── routing.go                 # Interface RouteManager
│   │   ├── routing_test.go
│   │   ├── routing_linux.go           # ip route + ip rule + table dédiée
│   │   ├── routing_windows.go         # winipcfg
│   │   └── e2e_test.go
│   │
│   ├── ipc/
│   │   ├── messages.go                # Request/Response JSON
│   │   ├── client.go / client_test.go
│   │   ├── server.go / server_test.go
│   │   ├── pipe_windows.go            # Named pipes (go-winio)
│   │   ├── pipe_linux.go              # Unix sockets /run/levoile/ipc.sock
│   │   └── e2e_test.go
│   │
│   ├── ipchandler/
│   │   ├── handler.go                 # Dispatcher IPC → service methods
│   │   └── handler_test.go
│   │
│   ├── ui/
│   │   ├── ui.go                      # Point d'entrée UI : systray + webview + HTTP server
│   │   ├── httpserver.go              # Serveur HTTP local (assets + API REST JSON)
│   │   ├── embed.go                   # //go:embed frontend
│   │   ├── icons.go                   # //go:embed icons
│   │   ├── singleton_windows.go       # Mutex nommé
│   │   ├── singleton_linux.go         # flock ~/.local/state/levoile/ui.lock
│   │   └── icons/
│   │       ├── connected.ico / .png
│   │       ├── connecting.ico / .png
│   │       ├── disconnected.ico / .png
│   │       └── levoile.ico / .png
│   │
│   ├── config/
│   │   ├── config.go                  # TOML schema révisé (sections tun, firewall ajoutées)
│   │   ├── config_test.go
│   │   ├── discover.go                # Auto-discovery config path
│   │   ├── paths_windows.go           # %AppData%/LeVoile/
│   │   └── paths_linux.go             # ~/.config/levoile/ + /etc/levoile/
│   │
│   ├── crypto/                        # Inchangé
│   │   ├── ed25519.go / ed25519_test.go
│   │   ├── tls.go / tls_test.go
│   │   └── pinning.go / pinning_test.go
│   │
│   ├── tunnel/
│   │   ├── client.go                  # HTTP/3 client + stream /tunnel (paquets IP ↔ QUIC)
│   │   ├── client_test.go
│   │   ├── pump.go                    # Pompe TUN ↔ HTTP/3 (framing 2-byte length)
│   │   ├── pump_test.go
│   │   ├── reconnect.go               # Backoff + circuit breaker
│   │   ├── reconnect_test.go
│   │   └── state.go / state_test.go
│   │
│   ├── dns/                           # Réduit — plus de proxy UDP local
│   │   ├── check.go                   # Validation résolveur système (optionnel)
│   │   ├── check_linux.go
│   │   ├── check_windows.go
│   │   ├── flush.go                   # NOUVEAU — flush cache DNS au disconnect
│   │   ├── flush_linux.go             # resolvectl / nscd / dnsmasq SIGHUP
│   │   ├── flush_windows.go           # ipconfig /flushdns
│   │   └── flush_test.go
│   │
│   ├── integrity/                     # NOUVEAU — self-integrity check binaire
│   │   ├── check.go                   # SHA256(self) vs Ed25519 signature embarquée
│   │   ├── check_test.go
│   │   └── signature_embed.go         # //go:embed signature.bin (générée par GoReleaser)
│   │
│   ├── elevation/
│   │   ├── elevation.go               # Interface + détection
│   │   ├── elevation_windows.go       # UAC token check
│   │   ├── elevation_linux.go         # CAP_NET_ADMIN via PR_CAPBSET_READ
│   │   └── elevation_test.go
│   │
│   ├── registry/                      # Inchangé
│   │   ├── registry.go / client.go / discoverer.go / cache.go / failover.go / latency.go / countries.go
│   │   └── *_test.go + e2e_test.go
│   │
│   ├── leakcheck/                     # Simplifié — validation TUN uniquement
│   │   ├── webrtc.go                  # STUN Binding Request emitted via OS (passe TUN)
│   │   ├── webrtc_test.go
│   │   ├── xor_mapped.go              # XOR-MAPPED-ADDRESS parsing (conservé)
│   │   ├── xor_mapped_test.go
│   │   └── scheduler.go / scheduler_test.go
│   │
│   ├── stun/                          # Réduit — juste parsing
│   │   ├── stun.go                    # Constants + classification
│   │   ├── parser.go
│   │   └── parser_test.go
│   │   # Supprimés : interceptor.go, relayer.go (plus de relay STUN dédié)
│   │
│   ├── updater/                       # Inchangé, avec restart systemctl sur Linux
│   │   ├── updater.go / checker.go / downloader.go / verify.go / installer.go / rollback.go / version.go
│   │   └── *_test.go
│   │
│   └── relay/
│       ├── server.go                  # HTTP/3 + TCP HTTPS dual stack
│       ├── server_test.go
│       ├── tunnel_handler.go          # NOUVEAU — /tunnel stream bidirectionnel + NAT + DNS
│       ├── tunnel_handler_test.go
│       ├── nat_table.go               # NOUVEAU — NAT table in-memory (5-tuple → port)
│       ├── nat_table_test.go
│       ├── dns_resolver.go            # NOUVEAU — DNS interne avec upstream + blocklist
│       ├── dns_resolver_test.go
│       ├── blocklist.go               # NOUVEAU — StevenBlack/hosts côté relais
│       ├── blocklist_test.go
│       ├── verify_handler.go          # /verify — session token issuance
│       ├── ip_handler.go              # /ip
│       ├── health.go                  # /health (tunnels + nat_entries)
│       ├── middleware.go              # LimitMiddleware + IPLimitMiddleware + clientIP helper (host from r.RemoteAddr)
│       ├── limiter.go / ip_limiter.go / bandwidth_limiter.go
│       └── e2e_test.go
│       # Supprimés : doh_handler.go, stun_handler.go, connect_handler.go (fusionnés dans tunnel_handler)
│
├── frontend/
│   ├── index.html                     # Point d'entrée UI (420×540)
│   ├── src/
│   │   ├── style.css                  # Charte plateformeliberte.fr
│   │   └── app.js                     # Polling fetch('/api/status') 2s, registry 30s
│   └── assets/
│       └── fonts/                     # Bebas Neue, Rajdhani, Inter (woff2)
│
├── installer/                         # Windows NSIS
│   ├── levoile.nsi                    # Script complet (install/uninstall) — ajout Wintun DLL + WFP cleanup
│   ├── build.ps1
│   ├── levoile.ico
│   ├── config-default.toml
│   └── build/
│
├── packaging/                         # NOUVEAU — paquets Linux
│   ├── nfpm-deb.yaml
│   ├── nfpm-rpm.yaml
│   ├── nfpm-apk.yaml
│   ├── arch/
│   │   ├── PKGBUILD                   # AUR
│   │   └── .SRCINFO
│   ├── systemd/
│   │   └── levoile.service            # Unit file client (ExecStart, CAP_NET_ADMIN)
│   ├── desktop/
│   │   └── levoile.desktop            # XDG application entry
│   ├── postinstall.sh                 # setcap + systemctl enable
│   └── preremove.sh                   # systemctl disable
│
├── deploy/                            # Relais VPS uniquement
│   ├── install.sh                     # Installation relais Linux
│   └── levoile-relay.service          # Systemd unit relais
│
├── assets/
│   └── icons/
│       └── hicolor/                   # Linux freedesktop icon theme
│           ├── 16x16/ / 32x32/ / 48x48/ / 64x64/ / 128x128/ / 256x256/
│           └── apps/levoile.png
│
├── tools/
│   └── gen_icons.go                   # Outil génération icônes
│   # Supprimé : crxgen/ (plus d'extension)
│
├── android/                           # NOUVEAU — Arbre Android complet et indépendant
│   ├── .gitignore                     # Spécifique Android : .gradle/, build/, *.aar, local.properties, keystore/*
│   ├── settings.gradle.kts            # include(":app", ":levoile-core")
│   ├── build.gradle.kts               # Top-level Gradle config — versions plugins
│   ├── gradle.properties              # JVM args, AndroidX flags, Kotlin code style
│   ├── gradlew / gradlew.bat
│   ├── gradle/
│   │   └── wrapper/
│   │       ├── gradle-wrapper.jar
│   │       └── gradle-wrapper.properties
│   │
│   ├── app/                           # Module APK principal
│   │   ├── build.gradle.kts           # Config app : applicationId, signingConfigs, dependencies
│   │   ├── proguard-rules.pro         # Rules ProGuard release
│   │   ├── consumer-rules.pro
│   │   └── src/
│   │       ├── main/
│   │       │   ├── AndroidManifest.xml
│   │       │   ├── kotlin/fr/plateformeliberte/levoile/
│   │       │   │   ├── LeVoileApplication.kt        # Singleton Application
│   │       │   │   ├── LeVoileVpnService.kt         # VpnService + Foreground Service
│   │       │   │   ├── MainActivity.kt              # Hôte WebView + onboarding
│   │       │   │   ├── bridge/
│   │       │   │   │   ├── LeVoileBridge.kt         # JS Bridge (@JavascriptInterface)
│   │       │   │   │   ├── GoCoreAdapter.kt         # Wrapper unique vers .aar gomobile
│   │       │   │   │   └── PacketCallback.kt        # Interface callback Go → Kotlin
│   │       │   │   ├── ui/
│   │       │   │   │   ├── NotificationHelper.kt    # Channel + ongoing notification
│   │       │   │   │   ├── KillSwitchHelper.kt      # Deeplink Settings VPN + check always-on
│   │       │   │   │   └── BatteryOptimizationHelper.kt
│   │       │   │   ├── receivers/
│   │       │   │   │   ├── BootReceiver.kt          # Auto-start au boot (BOOT_COMPLETED)
│   │       │   │   │   └── ConnectivityObserver.kt  # NetworkCallback → reconnect
│   │       │   │   ├── prefs/
│   │       │   │   │   └── VpnPreferences.kt        # SharedPreferences typé
│   │       │   │   └── util/
│   │       │   │       └── JniInteropUtil.kt        # Conversions ByteArray ↔ JNI
│   │       │   ├── assets/
│   │       │   │   ├── index.html                   # Copié depuis ../../../../frontend/ par sync-frontend.sh
│   │       │   │   ├── style.css
│   │       │   │   ├── app.js
│   │       │   │   └── fonts/
│   │       │   │       ├── BebasNeue-Regular.woff2
│   │       │   │       ├── Rajdhani-*.woff2
│   │       │   │       └── Inter-*.woff2
│   │       │   └── res/
│   │       │       ├── drawable/
│   │       │       │   ├── ic_levoile_status.xml    # Vector mono-couleur pour notif
│   │       │       │   └── ic_levoile_logo.xml      # Vector logo
│   │       │       ├── mipmap-mdpi/ic_launcher.png  # Adaptive icon multi-densité
│   │       │       ├── mipmap-hdpi/
│   │       │       ├── mipmap-xhdpi/
│   │       │       ├── mipmap-xxhdpi/
│   │       │       ├── mipmap-xxxhdpi/
│   │       │       ├── mipmap-anydpi-v26/
│   │       │       │   └── ic_launcher.xml          # Adaptive icon Android 8+
│   │       │       ├── values/
│   │       │       │   ├── strings.xml              # Strings anglais (default)
│   │       │       │   ├── colors.xml               # Couleurs charte plateformeliberte.fr
│   │       │       │   ├── themes.xml               # Theme Material You + couleurs charte
│   │       │       │   └── styles.xml
│   │       │       ├── values-fr/
│   │       │       │   └── strings.xml              # Strings français
│   │       │       └── xml/
│   │       │           └── network_security_config.xml  # Cleartext traffic disabled, TLS 1.3 enforced
│   │       ├── test/
│   │       │   └── kotlin/fr/plateformeliberte/levoile/
│   │       │       ├── prefs/VpnPreferencesTest.kt          # JUnit 4
│   │       │       ├── ui/KillSwitchHelperTest.kt
│   │       │       └── bridge/GoCoreAdapterTest.kt          # Mock le .aar via interface
│   │       └── androidTest/
│   │           └── kotlin/fr/plateformeliberte/levoile/
│   │               ├── MainActivityInstrumentedTest.kt      # Espresso
│   │               └── LeVoileVpnServiceInstrumentedTest.kt
│   │
│   ├── levoile-core/                   # Module bibliothèque pour le .aar gomobile
│   │   ├── build.gradle.kts            # Configuration consommation .aar (flatDir libs/)
│   │   └── libs/
│   │       ├── .gitkeep
│   │       └── levoile-core.aar        # GITIGNORÉ — produit par gomobile bind
│   │
│   ├── scripts/
│   │   ├── build-aar.sh                # gomobile bind → libs/levoile-core.aar (Linux/macOS dev)
│   │   ├── build-aar.ps1               # Variante Windows pour devs
│   │   ├── sync-frontend.sh            # Copie ../../frontend/ → app/src/main/assets/
│   │   ├── sync-frontend.ps1           # Variante Windows
│   │   └── verify-shared-imports.sh    # Lint check : packages partagés gomobile n'importent pas de packages OS-spécifiques
│   │
│   ├── fastlane/
│   │   └── metadata/
│   │       └── android/
│   │           ├── en-US/
│   │           │   ├── title.txt
│   │           │   ├── short_description.txt
│   │           │   ├── full_description.txt
│   │           │   ├── changelogs/
│   │           │   │   └── 1.txt
│   │           │   └── images/
│   │           │       ├── icon.png
│   │           │       ├── featureGraphic.png
│   │           │       └── phoneScreenshots/
│   │           └── fr-FR/
│   │               ├── title.txt
│   │               ├── short_description.txt
│   │               ├── full_description.txt
│   │               └── changelogs/1.txt
│   │
│   ├── keystore/                       # GITIGNORÉ — clés signature APK
│   │   └── .gitkeep
│   │
│   └── README-android.md               # Guide dev Android (build .aar, sync frontend, debug)
│
├── .github/
│   └── workflows/
│       ├── release-desktop.yml         # Build Win + Linux + relay (GoReleaser) — ex release.yml
│       ├── release-android.yml         # NOUVEAU — Build APK Android (Gradle), sign, F-Droid metadata
│       └── aur-publish.yml             # Publication AUR automatique
│
└── docs/
    ├── validation-e2e.md               # Guide validation bout en bout desktop (Windows + Linux)
    └── validation-e2e-android.md       # NOUVEAU — Guide validation bout en bout Android (Pixel 3/6/14)
```

**Ajouts par rapport à la révision précédente (2026-04-19)** :
- `android/` — Arbre Android complet et autonome (module Kotlin/AndroidX + module gomobile + scripts build + F-Droid metadata + tests instrumentés)
- `.github/workflows/release-android.yml` — Pipeline CI Android indépendant
- `docs/validation-e2e-android.md` — Guide validation Android

**Supprimés par rapport à la révision précédente (2026-04-08)** :
- `internal/httpproxy/` (proxy CONNECT local)
- `internal/blocklist/` (blocklist client — maintenant côté relais)
- `internal/browser/` (déploiement extension + détection navigateurs)
- `internal/watchdog/` (remplacé par watchdog interne TUN/firewall)
- `extension/` (WebExtension Chrome/Firefox)
- `tools/crxgen/` (générateur CRX)
- `scripts/test-extension-install.ps1`
- Fichiers `internal/dns/proxy.go`, `kill_switch.go`, `dnscache_*.go`, `reuseaddr_*.go`
- Fichiers `internal/stun/interceptor*.go`, `relayer*.go`
- Fichiers `internal/ui/sysproxy*.go` (WinINET)
- Fichiers `internal/relay/doh_handler.go`, `stun_handler.go`, `connect_handler.go`

### Architectural Boundaries

**Frontière IPC (Service ↔ UI) :**
- Communication exclusivement via named pipes (Windows) ou Unix sockets
- Protocole JSON ligne par ligne, max 4 Ko par message
- Le service est l'autorité : l'UI est un client IPC pur
- Aucun import Go direct entre `internal/service` et `internal/ui`
- L'IPC handler (`internal/ipchandler/`) est le seul dispatcher côté service

**Frontière Réseau (Client ↔ Relais) :**
- Points de contact : POST `/tunnel` (stream bidirectionnel paquets IP), GET `/verify` (session token), GET `/ip`, GET `/health`, GET `/.well-known/relay-registry.json`
- Tout passe en HTTPS/HTTP/3 direct vers `https://{relay-domain}` (DNS A record = VPS origin). Pas d'intermédiaire CDN — ToS Cloudflare §2.8 interdit le proxying non-HTML, et l'IP origin est de toute façon exposée via le NAT sortant donc le gain sécurité d'un fronting serait marginal vs le coût (Enterprise + dépendance tiers)
- Le registry discoverer est le seul composant qui détermine quel relais contacter
- Le handler `/tunnel` valide : session token Ed25519, IP client (constant-time compare `SHA256(r.RemoteAddr) == IPHash` du token), rate limit par IP (IPLimiter), bandwidth limit par IP (BWLimiter). Pour chaque paquet IP forwardé : SSRF check (bloque réseaux privés destination)
- `/health` accessible publiquement mais ne contient que des métriques anonymes

**Frontière Capture L3 (OS ↔ Le Voile) :**
- Interface virtuelle `levoile0` (TUN Linux / Wintun Windows) — point d'entrée unique pour tout le trafic IP machine
- Le service lit/écrit des paquets IP bruts — aucune inspection du payload applicatif
- Route par défaut système pointe vers `levoile0` (metric basse), route spécifique vers IP relais via gateway originale
- Firewall OS-level (nftables/WFP) drop tout trafic ne respectant pas la politique — kill switch structurel
- MTU : 1420 (après overhead QUIC/HTTP3)

**Frontière Découverte (Client ↔ Registre) :**
- GET `/.well-known/relay-registry.json` sur n'importe quel relais retourne la liste complète signée
- Vérification Ed25519 master key obligatoire côté client
- Le cache local est le fallback si aucun relais ne répond
- Le registre est en lecture seule côté client

**Frontière Webview (Frontend JS ↔ UI Go — même processus) :**
- Communication via serveur HTTP local embarqué (API REST JSON sur 127.0.0.1:{port})
- Le frontend utilise `fetch()` vers `/api/*` — pas de bindings Go↔JS directs
- Le serveur HTTP local (`internal/ui/httpserver.go`) proxie les commandes vers le service via IPC client — pas d'accès direct aux composants internes
- Endpoints : /api/status, /api/connect, /api/country, /api/registry, /api/leak-status, /api/settings, etc.

**Frontière OS Desktop (TUN / Firewall / Routing) :**
- 3 interfaces Go : `tun.Device`, `firewall.Firewall`, `routing.RouteManager` — contrats uniques pour Windows + Linux
- Implémentations OS isolées derrière build tags — jamais d'imports croisés
- Chaque implémentation OS est autonome et testable indépendamment
- Le service orchestre les 3 dans un ordre strict (tun → routing → firewall)
- **Pas étendu à Android** — l'arbre Android n'utilise pas ces interfaces, il a sa propre stack VpnService Kotlin

**Frontière Android (NOUVEAU — VpnService / Foreground Service / JNI Bridge) :**
- 1 classe Kotlin centrale par responsabilité : `LeVoileVpnService` (capture L3 + lifecycle), `MainActivity` (UI host), `LeVoileBridge` (JS↔Kotlin), `GoCoreAdapter` (Kotlin↔Go JNI)
- L'arbre `android/` est complètement indépendant du reste du repo : il consomme **uniquement** le `.aar` produit par gomobile depuis les packages Go partagés (frontière étroite, contractuelle)
- Pas de tunneling vers desktop : aucun fichier de l'arbre `android/` n'est inclus dans les binaires Windows/Linux, et inversement
- Le `.aar` est régénéré explicitement par script — pas par Gradle ni par Go build, frontière build chain claire

**Frontière Noyau Partagé Go ↔ Android (gomobile) — NOUVEAU :**
- Liste fermée de shims gomobile sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` (cf. erratum ADR-09 Story 9.2 — la spec d'origine référençait des packages racine `internal/{protocol,auth,registry,crypto,leakcheck}` exposés directement, contournée à cause de la règle Go `internal/` interdisant l'import depuis le work dir gobind hors module). Tout autre code Go reste desktop-only
- Les shims `crypto`, `registry`, `leakcheck` consomment en **lecture** les packages racine `internal/{crypto,registry,leakcheck,tunnel}` (transitif). Aucune modification du code racine
- Les shims `protocol` et `auth` sont **canoniques** (pas de package racine équivalent — la logique vit dans `internal/tunnel/{pump.go,client.go}`, intégrée Story 9.7)
- Aucun shim ni package racine consommé n'importe de package OS-spécifique (`internal/tun/`, `internal/firewall/`, `internal/routing/`, `internal/ui/`, `internal/ipc/`, `internal/wfp/`, `internal/nftables/`) — vérifié par `android/scripts/verify-shared-imports.sh`
- L'API exposée gomobile utilise des types primitifs ou simples : `String`, `Int`, `Boolean`, `ByteArray`, structs aplatis. Pas de types Go complexes (channels, contexts, interfaces) qui ne traversent pas JNI proprement
- Les callbacks Go → Kotlin passent par interfaces gomobile enregistrées une seule fois au startup

**Frontière Relais (Stateless vis-à-vis des clients) :**
- Aucun état persisté entre les requêtes (pas de disque)
- État en RAM uniquement : NAT table (5-tuple → port NAT + TTL), compteurs tunnels/bandwidth, DNS cache court — tout volatile
- Le relais ne connaît ni les identités clients ni leurs requêtes passées
- Le fichier registry.json est statique, déployé avec le binaire

### Requirements to Structure Mapping

Lecture par OS (Desktop vs Android) — chaque FR a deux mappings.

| FR | Desktop (Windows + Linux) | Android |
|---|---|---|
| **FR1-4 (Tunnel)** | `internal/tunnel/`, `internal/crypto/` | `levoile-core.aar` (depuis shims `android/shims/{protocol,auth,crypto}/` consommant `internal/{crypto,tunnel}` racine — cf. erratum Story 9.2) + `LeVoileVpnService` (lifecycle) |
| **FR5-8 (Capture L3 + DNS + Kill Switch)** | `internal/tun/`, `internal/tunnel/pump.go`, `internal/firewall/`, `internal/routing/`, `internal/relay/dns_resolver.go`, `internal/relay/blocklist.go` | `LeVoileVpnService` (VpnService + pompes paquets), `KillSwitchHelper` (deeplink OS toggle), DNS résolu côté relais inchangé |
| **FR9-13b (Interface Utilisateur)** | `frontend/`, `internal/ui/` | `frontend/` (assets partagés via sync), `MainActivity` + `WebView` + `LeVoileBridge`, `NotificationHelper` (rôle du tray) |
| **FR14-16 (Démarrage & Lifecycle)** | `internal/service/`, `internal/elevation/`, `cmd/client/` | `LeVoileVpnService` (lifecycle Foreground Service), `BootReceiver` (auto-start) |
| **FR17-19b (Relais Multi-VPS)** | `internal/relay/`, `cmd/relay/`, `deploy/` | **Inchangé** — relais commun, l'arbre Android consomme les mêmes relais |
| **FR20-22 (Distribution)** | `.goreleaser.yaml`, `installer/` (NSIS), `packaging/` (nfpm + AUR) | `android/app/build.gradle.kts` (Gradle), `android/fastlane/metadata/` (F-Droid), `.github/workflows/release-android.yml` |
| **FR23-26 (Découverte & Sélection)** | `internal/registry/` | `levoile-core.aar` (depuis `internal/registry/`) |
| **FR27-30 (IP Camouflage)** | `internal/tun/`, `internal/tunnel/`, `internal/relay/tunnel_handler.go`, etc. | `LeVoileVpnService` + pompes Kotlin↔Go, relais inchangé |
| **FR31-34 (Anti-Fuite)** | `internal/leakcheck/`, `internal/stun/` (parsing) + `internal/tun/` + `internal/firewall/` | `levoile-core.aar` (depuis `internal/leakcheck/`) — validation. Prévention structurelle via VpnService (capture totale) + verrou OS "VPN permanent" |
| **FR35-36 (Mise à jour)** | `internal/updater/` | F-Droid (gestion store), pas d'auto-update intégré MVP — Phase 2 |
| **~~FR37-40 (Extension Navigateur)~~** | **SUPPRIMÉ** | **SUPPRIMÉ** |

**Cross-Cutting :**

_Desktop :_
- Kill switch (FR8) → `internal/firewall/` (nftables Linux / WFP Windows)
- Reconnexion auto (FR2) → `internal/tunnel/reconnect.go` — kill switch firewall reste actif pendant les reconnexions
- Failover multi-relais → `internal/registry/failover.go` + `internal/tunnel/` (kill switch maintenu entre relais)
- Session tokens → `internal/tunnel/client.go` + `internal/relay/verify_handler.go`
- Anti-fuite WebRTC → `internal/leakcheck/` (validation) + `internal/tun/` + `internal/firewall/` (prévention structurelle)
- Démarrage (FR14) → `internal/service/` + `internal/elevation/` (check capabilities Linux / UAC Windows)
- Découverte registre → `internal/registry/` + `internal/relay/` (sert le JSON)
- IPC → `internal/ipc/` + `internal/ipchandler/`

_Android (NOUVEAU) :_
- Kill switch (FR8) → délégué OS via réglage "VPN permanent + bloquer connexions sans VPN". `KillSwitchHelper` guide l'utilisateur. **Pas de code app-level pour le kill switch** — par design Android
- Reconnexion auto (FR2) → noyau Go partagé `internal/tunnel/reconnect.go` (via `.aar`) — déclenchée par `ConnectivityObserver` sur changement réseau
- Failover multi-relais → noyau Go partagé `internal/registry/failover.go` (via `.aar`)
- Session tokens → noyau Go partagé `internal/auth/` (via `.aar`)
- Anti-fuite WebRTC → noyau Go partagé `internal/leakcheck/` (validation) + VpnService (capture totale = prévention structurelle)
- Démarrage → `LeVoileVpnService.onStartCommand` + consentement utilisateur `VpnService.prepare()`
- Découverte registre → noyau Go partagé `internal/registry/` (via `.aar`)
- "IPC" → JS Bridge (`@JavascriptInterface`) + JNI gomobile, **in-process**, pas d'IPC réelle

### Data Flow

#### Desktop (Windows + Linux)

**Flux principal (tout trafic IP — DNS, HTTP, HTTPS, QUIC, UDP, ICMP) :**
```
[Utilisateur] → [Application sur le PC (navigateur, mail, jeu, etc.)]
                        ↓ (connexion TCP/UDP/ICMP quelconque)
              [Socket OS → stack réseau kernel]
                        ↓ (routing table : défaut → levoile0)
              [Interface TUN/Wintun "levoile0"]
                        ↓ (paquet IP brut)
              [internal/tun/Device.Read()]
                        ↓ (goroutine pompe)
              [internal/tunnel/pump.go]
                        ↓ (framing 2-byte length + payload)
              [HTTP/3 stream POST /tunnel sur quic-go]
                        ↓ (trafic indiscernable HTTPS)
              [Connexion HTTP/3 directe → {relay-domain} = VPS origin]
                        ↓
              [Relais VPS — internal/relay/tunnel_handler.go]
                        ↓ (parse IP header)
              [SSRF check (destinations privées bloquées)]
              [DNS ? → internal/relay/dns_resolver.go + blocklist → upstream 1.1.1.1/9.9.9.9]
              [Autre ? → NAT allocation via internal/relay/nat_table.go]
                        ↓ (substitution source IP = relais, source port = port NAT)
                        ↓ (forwarding via socket)
              [Internet → serveur destination]
                        ↓ (réponse)
              [Relais : NAT reverse lookup → 5-tuple client]
                        ↓ (paquet IP encapsulé)
              [HTTP/3 stream → client (retour direct)]
                        ↓ (internal/tunnel/pump.go → internal/tun/Device.Write())
              [levoile0 → kernel → application]
```

**Flux IPC (interaction utilisateur) :**
```
[Utilisateur clique "Connecter" dans webview]
        ↓
[frontend/app.js → fetch('/api/connect', {method: 'POST'})]
        ↓ (HTTP local)
[internal/ui/httpserver.go → ipc.Client.Send(ActionConnect)]
        ↓ (named pipe / Unix socket)
[internal/ipc/server.go → dispatch]
        ↓
[internal/ipchandler/handler.go → Handle(prg, req)]
        ↓
[internal/service/service.go → Start tunnel, DNS, proxy]
        ↓ (response)
[Chemin inverse → JSON → frontend updateUI()]
```

**Flux Connect (activation complète) :**
```
[User clique "Connecter" dans webview]
        ↓
[frontend/app.js → fetch('/api/connect', POST)]
        ↓
[internal/ui/httpserver.go → ipc.Client.Send(ActionConnect)]
        ↓
[internal/ipchandler/handler.go → service.Connect()]
        ↓
[internal/service/service.go — orchestration stricte]
  1. internal/elevation/ — check CAP_NET_ADMIN (Linux) / UAC (Windows) → erreur bloquante si absent
  2. internal/registry/ — sélection relais (pays préféré ou meilleur latence)
  3. internal/tun/ — création interface "levoile0" avec MTU 1420
  4. internal/routing/ — route par défaut via TUN + route spécifique vers IP relais via gateway originale
  5. internal/firewall/ — Activate(relayIP, tunName) : nftables ruleset / WFP filters
  6. internal/tunnel/ — QUIC handshake + certificate pinning Ed25519 + obtention session token via /verify
  7. internal/tunnel/pump.go — démarrage goroutines pompe TUN ↔ HTTP/3 stream /tunnel
  8. État = Connected, IPC notifie UI
```

**Flux Disconnect :**
```
[User clique "Quitter" ou switch pays ou crash relais]
        ↓
[internal/service/service.go — ordre inverse]
  1. internal/tunnel/ — arrêt pompe + fermeture stream /tunnel + close QUIC conn
  2. internal/firewall/ — Deactivate() : flush règles nftables / WFP filters retirées
  3. internal/routing/ — Teardown : restauration route par défaut originale
  4. internal/tun/ — Close() : destruction interface "levoile0"
  5. État = Disconnected, IPC notifie UI
```

**Flux découverte (registre) :**
```
[Service démarrage] → [internal/registry/discoverer.go]
                        ↓ (GET /.well-known/relay-registry.json)
              [HTTPS direct → n'importe quel relais du registre]
                        ↓ (JSON signé)
              [internal/registry/registry.go → VerifyAll(master key)]
                        ↓ (relais vérifiés)
              [internal/registry/cache.go → persistence locale]
                        ↓ (IPC GetRegistry → UI)
              [Utilisateur choisit un pays dans l'UI]
                        ↓ (IPC SelectCountry → service)
              [internal/registry/failover.go → sélection relais]
              [Connexion au relais sélectionné]
```

**Flux failover :**
```
[Tunnel actif sur relais de-01]
        ↓ (timeout / erreur / perte QUIC)
[internal/tunnel/reconnect.go → backoff exponentiel]
        ↓ (5 échecs consécutifs → circuit breaker)
[internal/registry/failover.go → bascule relais suivant]
        ↓ (fermeture QUIC actuelle, nouvelle connexion)
        ↓ (IMPORTANT : firewall reste actif, TUN reste up, routing inchangé)
        ↓ (mise à jour route spécifique vers nouvelle IP relais)
[Notification via IPC → UI mise à jour]
```

**Flux anti-fuite WebRTC (validation TUN) :**
```
[internal/leakcheck/scheduler.go → check périodique]
        ↓ (skip si tunnel déconnecté)
[internal/leakcheck/webrtc.go → STUN Binding Request vers stun.l.google.com]
        ↓ (paquet UDP émis par l'OS → capturé par TUN → encapsulé vers relais)
[Relais → NAT → forward vers serveur STUN → réponse]
        ↓
[XOR-MAPPED-ADDRESS → IP publique détectée]
        ↓
[Comparaison IP détectée vs IP relais attendue (issue de /ip ou registry)]
        ↓ (si identique = OK, TUN capture bien le trafic)
        ↓ (si différente = ALERT : TUN bypass détecté — bug kernel/firewall)
[Notification via IPC → UI alerte rouge]
```

**Pourquoi plus de politique navigateur** : la capture L3 intercepte tous les paquets UDP générés par WebRTC (ICE candidates, STUN probes), empêchant structurellement la fuite. Le check ci-dessus sert uniquement à vérifier que la chaîne fonctionne bien.

#### Android (NOUVEAU)

**Flux principal (tout trafic IP système — DNS, HTTP, HTTPS, QUIC, UDP) :**
```
[Utilisateur] → [Application Android (navigateur, mail, jeu, etc.)]
                        ↓ (connexion TCP/UDP quelconque)
              [Socket OS Android → kernel networking]
                        ↓ (routing : VpnService active sur l'app process via uid binding)
              [Interface VPN tunX (créée par VpnService.Builder)]
                        ↓ (paquet IP brut écrit sur fd VPN)
              [Pompe Kotlin : FileInputStream(fd).read(buf) — thread daemon]
                        ↓ (JNI gomobile)
              [GoCoreAdapter.writePacket(buf, len) → noyau Go .aar]
                        ↓ (encodage protocole tunnel)
              [internal/protocol/ + internal/tunnel/client.go (in .aar)]
                        ↓ (framing 2-byte length + payload)
              [HTTP/3 stream POST /tunnel sur quic-go (in .aar)]
                        ↓ (trafic indiscernable HTTPS/HTTP3)
              [Connexion HTTP/3 directe → {relay-domain} = VPS origin]
                        ↓
              [Relais VPS — internal/relay/tunnel_handler.go]
                        ↓ (parse IP header, NAT, forward Internet)
              [Internet → serveur destination]
                        ↓ (réponse)
              [Relais : NAT reverse → paquet IP encapsulé]
              [HTTP/3 stream → client Android]
                        ↓ (in .aar : décodage protocole)
              [GoCoreAdapter callback PacketCallback.onPacketReceived(buf)]
                        ↓ (JNI Go → Kotlin)
              [Pompe Kotlin écriture : FileOutputStream(fd).write(buf)]
                        ↓
              [Interface VPN tunX → kernel Android → application]
```

**Flux JS Bridge (interaction utilisateur Android) :**
```
[Utilisateur tap "Connecter" dans WebView]
        ↓
[Frontend app.js → window.LeVoile.connect()]
        ↓ (JS Bridge synchrone)
[LeVoileBridge.connect() — méthode @JavascriptInterface, dispatched sur Dispatchers.IO]
        ↓
[startForegroundService(Intent(this, LeVoileVpnService::class.java).setAction(ACTION_CONNECT))]
        ↓
[LeVoileVpnService.onStartCommand → flow Connect (voir Decision Impact Analysis)]
        ↓
[StatusCallback.onStateChange("Connected") → bridge → window.LeVoile.notifyState()]
        ↓
[Frontend updateUI() — reflète état]
```

**Flux Connect Android (séquence détaillée) :**
```
1. MainActivity → val intent = VpnService.prepare(this)
2. Si intent != null → startActivityForResult(intent, REQ) → user accepte → onActivityResult
   Si intent == null → utilisateur a déjà donné consentement
3. MainActivity → checkAndPromptKillSwitch() → guide vers Settings.ACTION_VPN_SETTINGS si pas déjà actif
4. MainActivity → bridge.connect() → startForegroundService(ACTION_CONNECT)
5. LeVoileVpnService.onStartCommand reçoit l'intent
6. startForeground(NOTIF_ID, ongoingNotification) — < 5 secondes mandatoire
7. coroutineScope.launch(Dispatchers.IO) {
     val registry = goCoreAdapter.fetchRegistry()        // ou cache
     val relay = goCoreAdapter.selectRelay(prefCountry)
     val token = goCoreAdapter.requestSessionToken(relay)
     val builder = Builder()
       .setSession("Le Voile")
       .addAddress("10.6.6.2", 32)
       .addRoute("0.0.0.0", 0)
       .addRoute("::", 0)
       .addDnsServer("10.6.6.1")
       .setMtu(1420)
       .setBlocking(true)
     val pfd = builder.establish() ?: throw VpnPermissionDenied()
     val fd = pfd.detachFd()
     goCoreAdapter.startTunnel(fd, relay.id, token)       // Go ouvre os.File sur fd, démarre pompes
     // pompes Kotlin lecture fd → JNI Go writePacket si batched paquets vers Go
     // callback Go onPacketReceived → Kotlin écriture fd
     statusCallback.onStateChange("Connected")
   }
8. UI WebView reçoit notification status → updateUI()
```

**Flux Disconnect Android :**
```
[User tap "Déconnecter" dans WebView ou notification action]
        ↓
[bridge.disconnect() ou notif PendingIntent → LeVoileVpnService action DISCONNECT]
        ↓
[LeVoileVpnService.onStartCommand(action=DISCONNECT)]
  1. goCoreAdapter.stopTunnel()                       // ferme stream HTTP/3, stoppe pompes Go
  2. fermeture fd VPN (ParcelFileDescriptor.close())
  3. stopForeground(STOP_FOREGROUND_REMOVE)
  4. stopSelf()
        ↓
[StatusCallback.onStateChange("Disconnected") → bridge → frontend updateUI()]
```

**Flux failover Android :**
```
[Tunnel actif sur relais de-01]
        ↓ (timeout / erreur QUIC / changement réseau Wi-Fi → 4G via ConnectivityObserver)
[noyau Go in .aar : reconnect backoff exponentiel]
        ↓ (5 échecs consécutifs → circuit breaker)
[noyau Go : registry.failover.go bascule relais suivant]
        ↓ (fermeture QUIC actuelle, nouvelle connexion HTTP/3)
        ↓ (IMPORTANT : VpnService reste actif, fd VPN inchangé, OS continue à router via VPN)
        ↓ (kill switch OS "VPN permanent" : si configuré, l'OS bloque tout pendant la transition)
[StatusCallback.onStateChange("Reconnecting") puis "Connected"]
[UI WebView reçoit notification → indicateur orange pulse → vert]
```

**Flux découverte registre Android :**
Identique desktop sauf que tout est exécuté dans le `.aar` Go côté Android. L'`UI` reçoit le résultat via `bridge.getRegistry()` et `RegistryCallback.onUpdated()`.

**Flux anti-fuite Android :**
La VpnService capture **tout** le trafic IP de toutes les apps (sauf si l'utilisateur a configuré allowlist/disallowlist). Le check `internal/leakcheck/` exécuté côté Go via `.aar` envoie un STUN Binding Request qui passe par la VpnService → relais → STUN externe → réponse → comparaison IP. Si l'IP détectée == IP relais → OK. Sinon → alerte UI rouge.

**Flux kill switch Android (réglage OS) :**
```
[Utilisateur active "VPN permanent" + "Bloquer connexions sans VPN" dans Settings]
        ↓
[OS Android stocke always_on_vpn_app=fr.plateformeliberte.levoile + lockdown=1]
        ↓
À tout moment (boot, crash, désactivation manuelle Le Voile) :
        ↓
[OS Android : si Le Voile non-actif → bloque TOUT trafic au niveau kernel]
        ↓
[Quand Le Voile redémarre et reconnecte → OS débloque uniquement le trafic via VPN tun]
        ↓
[L'app Le Voile n'a aucun moyen de désactiver ce verrou — seul l'utilisateur peut]
```

## Architecture Validation Results

### Coherence Validation

**Decision Compatibility :** Aucun conflit détecté

_Desktop (Win + Linux) — inchangé :_
- quic-go v0.59.0 + HTTP/3 + TLS 1.3 — nativement intégré
- webview/webview + fyne.io/systray — même processus, tray sur main thread, webview créé/détruit à la demande. Windows : WebView2. Linux : WebKitGTK 6.0 + libayatana-appindicator3
- wireguard/tun + nftables/WFP — capture L3 unifiée, kill switch robuste indépendant du process client
- Ed25519 Go standard + clé par relais + registre signé master key — sécurité renforcée
- kardianos/service (cross-platform SCM + systemd) + IPC named pipes/Unix sockets
- Tunnel IP /tunnel + NAT côté relais + SSRF + bandwidth limiting — défense en profondeur
- Registre distribué signé + cache local — résilient, pas de SPOF
- Failover auto + kill switch firewall persistant — aucune fuite entre basculements
- GoReleaser + nfpm + NSIS — distribution Windows + Linux (4 familles de paquets) complète
- TOML config + cache registre JSON — rôles distincts sans conflit

_Android (NOUVEAU) :_
- gomobile + AndroidX + Kotlin 1.9 — stack standard, compatible (Tailscale Android, WireGuard Android l'utilisent en production)
- VpnService Android + Foreground Service Android 8+ — combinaison standard pour tous les VPN apps Android, pas de conflit lifecycle
- WebView Android + WebViewAssetLoader + JS Bridge `@JavascriptInterface` — pattern recommandé Google, sécurisé (pas de loading file://)
- Réglage OS "VPN permanent + bloquer connexions sans VPN" — kill switch kernel-level Android natif, plus robuste que toute solution app-level (qui de toute façon est impossible sans root)
- F-Droid + APK direct — distribution sans Google Play, cohérent avec esprit logiciel libre
- Noyau Go partagé via gomobile + Kotlin natif pour tout le reste — pas de conflit, frontière étroite et contractuelle
- Permissions runtime minimales (INTERNET, ACCESS_NETWORK_STATE, FOREGROUND_SERVICE, POST_NOTIFICATIONS, RECEIVE_BOOT_COMPLETED, FOREGROUND_SERVICE_SPECIAL_USE) — surface d'attaque minimale

_Cross-OS :_
- Pas d'incompatibilité protocolaire — Android consomme le même protocole tunnel que desktop (`internal/protocol/`)
- Relais inchangé — un relais ne distingue pas un client Android d'un client desktop, traite tout le monde de manière identique
- Registre signé même format JSON, même vérification Ed25519 — partage trivial via `.aar`

**Pattern Consistency :** Vérifié

_Desktop :_
- snake_case cohérent à travers TOML config, registre JSON et health endpoint
- Error wrapping fmt.Errorf("pkg: action: %w", err) — chaîne cohérente
- context.Context partout + reconnexion auto + failover — lifecycle correctement câblé
- IPC — communication UI → service strictement via messages JSON, pas d'import direct
- Ordre strict d'orchestration (elevation → tun → routing → firewall → tunnel) imposé par service + documenté dans patterns

_Android (NOUVEAU) :_
- Conventions Kotlin/AndroidX standards Google
- Strings i18n via `R.string.*` (en + fr), aucun hardcode
- Coroutines dispatchées (IO pour JNI/IO, Main pour UI), pas de runBlocking
- Tous les Intents inter-app FLAG_IMMUTABLE
- `LeVoileVpnService.startForeground` < 5s mandatoire respecté
- `GoCoreAdapter` est le seul wrapper JNI — pas de dispersion gomobile types
- Onboarding kill switch obligatoire au premier lancement
- Aucun crash reporter, aucune télémétrie, aucun analytics

_Cross-OS :_
- Charte visuelle plateformeliberte.fr réutilisée à 100% (assets HTML/CSS/JS partagés)
- Protocole tunnel identique (même framing, même session tokens, même registry signé)
- Sémantique état tunnel identique (Connected / Connecting / Disconnected / Reconnecting)

**Structure Alignment :** Vérifié

_Desktop :_
- Chaque frontière architecturale a un package internal/ dédié
- Registry (avec failover, latency, discoverer, cache, countries) isole la logique multi-relais
- Service centralise l'orchestration de tous les composants
- IPC handler centralise le dispatch des requêtes UI
- Frontend dans frontend/ — séparation nette Go/Web, servi par HTTP local embarqué
- Build tags couvrent Linux + Windows dans tun/, firewall/, routing/, config/, elevation/, ipc/, ui/
- Direction des dépendances empêche les imports circulaires
- Nouveaux packages (tun/firewall/routing) orthogonaux — pas de dépendances croisées entre eux

_Android (NOUVEAU) :_
- Arbre `android/` complètement isolé du reste du repo (consomme uniquement `.aar` produit par gomobile)
- Frontière `GoCoreAdapter` → `.aar` = unique point JNI, classes Kotlin séparées par responsabilité
- `LeVoileVpnService` orchestre Connect/Disconnect, équivalent fonctionnel de `internal/service/` desktop
- Pas d'imports croisés Kotlin → Go hors `GoCoreAdapter`
- Tests Kotlin co-localisés (`src/test/`, `src/androidTest/`)
- Modules Gradle : `app` (APK) + `levoile-core` (host du `.aar`) — séparation claire
- Pas de partage de code Java/Kotlin avec desktop (par design : isolation OS maximale)

_Cross-OS :_
- Frontière noyau partagé Go étroite et contrôlée (5 packages exposés gomobile, vérification automatique)
- Aucun fichier de l'arbre `android/` n'est inclus dans les binaires desktop, et inversement

### Requirements Coverage Validation

**Functional Requirements : 50/50 couverts sur Desktop, 60/60 couverts sur Android** (FR37-40 extension retirés depuis 2026-04-15 ; FR-AND-1..10 ajoutés en révision PRD 2026-04-30)

> **Note count post-2026-04-30** : PRD compte désormais 50 FRs Phase 1 (Desktop + Relais) + 10 FRs Phase 2 Android (FR-AND-1..10) = 60 FRs au total. Les sous-sections ci-dessous décrivent la couverture par OS au moment de la révision architecture (2026-04-29) — les FR-AND-* spécifiques Android Phase 2 sont couverts par les composants `android/` détaillés dans cette même section.

Voir tableau Desktop vs Android dans la section "Requirements to Structure Mapping" ci-dessus.

**Non-Functional Requirements :**

_Desktop (Win + Linux) — 20/20 couverts (inchangé) :_
- Sécurité (NFR1-9) : TLS 1.3 via quic-go, Ed25519 par relais + registre signé master key, certificate pinning, zero persistence, HTTP/3 camouflage, SSRF protection côté relais, session token bindé à l'IP client (SHA256(r.RemoteAddr)), code auditable, kill switch firewall kernel-level
- Performance (NFR10-14) : Go natif, capture L3 zéro-copy (lecture TUN vers stream QUIC), failover transparent, bandwidth limiting, RAM < 25MB
- Fiabilité (NFR15-18) : Kill switch firewall persistant (survit au crash service), watchdog TUN 3s, failover multi-relais (firewall maintenu), crash-recovery firewall au restart, circuit breaker
- Confidentialité (NFR19-20) : Zero log IP client, IP hash uniquement dans session tokens, NAT table en RAM volatile

_Android — 20/20 couverts (NOUVEAU) :_
- Sécurité (NFR1-9) :
  - TLS 1.3 via quic-go (in `.aar`) ✅
  - Ed25519 par relais + registre signé master key (in `.aar` via `internal/crypto/`) ✅
  - Certificate pinning (in `.aar` via `internal/crypto/pinning.go`) ✅
  - Zero persistence côté client : SharedPreferences contient prefs UI uniquement, aucune trace de trafic. Cache registre Android : oui mais public (signé) ✅
  - HTTP/3 camouflage : identique desktop ✅
  - SSRF protection : identique (côté relais) ✅
  - Session token IP-bindé : identique (in `.aar` via `internal/auth/`) ✅
  - **Kill switch kernel-level : assuré par le réglage OS Android "VPN permanent" — équivalent fonctionnel à WFP/nftables, robuste car porté par l'OS, inviolable côté app** ✅
  - Code auditable : Kotlin + Go, build reproductible F-Droid ✅
- Performance (NFR10-14) :
  - Latence DNS < 50ms : identique (résolu côté relais) ✅
  - Tunnel < 3s : identique ✅
  - Reconnexion backoff : identique (in `.aar`) ✅
  - **RAM < 60 MB Android** (vs < 25 MB desktop) : overhead JVM + WebView + AndroidX + buffers VPN. À benchmarker sur Pixel 3 (low-end de la gamme cible API 29) ✅
  - CPU < 1% : à valider — la pompe paquets via JNI peut coûter plus cher que la pompe full-Go desktop. Benchmark à 100 Mbps obligatoire avant release. Si > 5%, batcher les paquets ou passer le fd directement à Go (pompe full-Go côté Android via fd) ✅
- Fiabilité (NFR15-18) :
  - Kill switch persistant : assuré par OS "VPN permanent" — survit aux crashes app, redémarrages, mises à jour ✅
  - Watchdog interface : `ConnectivityObserver` détecte changements réseau, reconnect immédiat ✅
  - Crash-recovery : `START_REDELIVER_INTENT` redémarre le Foreground Service avec dernier intent. Si l'app est tuée par OOM killer, Android redémarre le service automatiquement (contrainte VPN persistant) ✅
  - Failover multi-relais : identique (in `.aar`) ✅
- Confidentialité (NFR19-20) :
  - Zero log IP client : aucun log applicatif d'IP, payload, token. `Log.w` masqué en release par ProGuard ou check `BuildConfig.DEBUG` ✅
  - **Aucune télémétrie, aucun crash reporter, aucun analytics** — engagement plus strict que beaucoup d'apps Android grand public ✅
  - Permissions runtime minimales — pas de location, pas de contacts, pas de storage ✅

**Coverage globale post-PRD 2026-04-30 :** 50 FRs Desktop × 2 OS (Win/Linux) + 10 FRs Android Phase 2 = 110 mappings ; 43 NFRs Desktop × 2 OS + 11 NFR-AND-* Android = 97 mappings. Tous couverts.

### Implementation Readiness Validation

**Decision Completeness :**
- Desktop : Décisions critiques documentées (révisions 2026-04-15 + 2026-04-19 stables)
- Android (NOUVEAU) : Décisions critiques documentées (révision 2026-04-29). Implémentation à démarrer sur la base du snapshot pré-Android (`pre-android-2026-04-29`)

**Structure Completeness :**
- Desktop : ~18 packages internal/ (ajout tun/firewall/routing, retrait httpproxy/blocklist/browser/watchdog), 4+1 entry points cmd/ (client, ui, relay, ctl + genregistry), frontend HTML/CSS/JS, installeur NSIS, packaging Linux (nfpm + AUR), déploiement systemd relais
- Android (NOUVEAU) : Arbre `android/` complet — 2 modules Gradle (`app`, `levoile-core`), ~13 classes Kotlin principales, scripts build (`build-aar.sh`/`.ps1`, `sync-frontend.sh`/`.ps1`, `verify-shared-imports.sh`), F-Droid metadata fr/en, tests JUnit + Espresso

**Pattern Completeness :**
- Desktop : Naming, formats, erreurs, concurrence, IPC, sélection relais, failover, UI, session tokens, tunnel IP encapsulation, firewall rulesets, ordre strict Connect/Disconnect — tous spécifiés
- Android (NOUVEAU) : Naming Kotlin/AndroidX, JSON via JS Bridge, erreurs Kotlin (sealed classes), coroutines + dispatchers, JNI bridge pattern, Foreground Service lifecycle, kill switch UX onboarding, responsive WebView, anti-patterns Android — tous spécifiés
- Cross-OS : règle d'isolation OS maximale documentée + ADRs

### Gap Analysis Results

**Gaps critiques : 0**

**Gaps mineurs Desktop (inchangé, acceptables pour le MVP Linux) :**
- Config TOML par défaut (génération à la première exécution) → détail d'implémentation
- Registre dynamique (API de gestion) → Phase 2
- Support ICMP dans le handler /tunnel du relais — peut être abandonné au MVP
- Support IPv6 complet — à valider lors de l'implémentation
- Gestion distros Linux sans libayatana-appindicator3 (ex: GNOME pur vanilla) — mitigation : fallback CLI-only ou dépendance stricte

**Gaps mineurs Android (NOUVEAU — acceptables pour MVP Android) :**
- **Benchmark performance pompe paquets JNI** : à valider sur Pixel 3 (low-end API 29). Si CPU > 5% à 100 Mbps, batcher ou pompe full-Go via fd
- **Détection "VPN permanent" actif** : heuristique via `Settings.Global.getString("always_on_vpn_app")` — fragile car non documenté API publique. Fallback prévu : assumer non-vérifiable et faire confiance à l'utilisateur
- **Auto-update Android intégré** : non-MVP, F-Droid suffit (utilisateurs APK direct doivent vérifier manuellement). Phase 2 envisageable
- **Build reproductible F-Droid** : effort initial pour atteindre hash déterministe (Android Gradle peut générer des outputs non-stables). Référence : guide F-Droid Reproducible Builds. Sans ça, F-Droid acceptera l'app mais le badge "Reproducible Build" manquera
- **Test Doze mode automatisé** : Android n'expose pas d'API standard pour forcer Doze en test. Validation manuelle ou via `adb shell dumpsys deviceidle force-idle`
- **Permission `FOREGROUND_SERVICE_SPECIAL_USE` Android 14+** : nécessite déclaration `<property>` dans manifest avec sous-type `vpn`. Documentée mais à vérifier que Google ne demande pas justification supplémentaire pour distribution F-Droid (en principe non — uniquement Play Store)
- **Émulateurs Android pour CI** : tests instrumentés nécessitent device ou émulateur. Solution : Firebase Test Lab (gratuit jusqu'à un quota) ou émulateurs GitHub Actions Linux KVM
- **Localisation au-delà fr/en** : Phase 2 si demande utilisateur

**Écart PRD ↔ Code (résolu en révision PRD 2026-04-30) :**
- ✅ Suppression FR37-40 (extension) — actée 2026-04-15
- ✅ Reformulation FR5-8 (capture L3 unifiée Win/Linux ; VpnService Android via FR-AND-1)
- ✅ Reformulation FR27-30 (tunnel IP) — actée 2026-04-15
- ✅ Support Android 10+ (API 29+) intégré au PRD §7 + §9 « Phase 2 — Android » (FR-AND-1..10) avec contraintes Foreground Service, kill switch OS-délégué, F-Droid + APK direct
- ✅ NFR-AND-1 split RAM (< 60 MB Android vs < 25 MB desktop NFR13)
- ✅ NFR-AND-8 zéro télémétrie/analytics/crash reporter Android explicitement numéroté
- ✅ Permissions Android énumérées (PRD §5 + NFR-AND-7)

### Architecture Completeness Checklist

**Requirements Analysis**
- [x] Contexte projet analysé en profondeur
- [x] Complexité et échelle évaluées
- [x] Contraintes techniques identifiées
- [x] Préoccupations transversales cartographiées

**Architectural Decisions**
- [x] Décisions critiques documentées avec versions
- [x] Stack technologique entièrement spécifié et implémenté
- [x] Patterns d'intégration définis (IPC, API REST locale, proxy)
- [x] Considérations de performance adressées

**Implementation Patterns**
- [x] Conventions de nommage établies
- [x] Patterns de structure définis
- [x] Patterns de communication spécifiés (IPC JSON, API REST locale)
- [x] Patterns de processus documentés (service, UI unique)

**Project Structure**
- [x] Structure de répertoires complète définie (~18 packages, 3+1 entry points)
- [x] Frontières de composants établies (IPC, réseau, proxy, OS, relais)
- [x] Points d'intégration cartographiés
- [x] Mapping exigences → structure complet (40 FR + 20 NFR)

### Architecture Readiness Assessment

**Statut global : ARCHITECTURE RÉVISÉE — PHASE ANDROID À IMPLÉMENTER (Desktop stable)**

**Niveau de confiance : Élevé**

**Changements majeurs (2026-04-29 — Phase Android) :**
- **Ajout support Android 10+ (API 29+)** — arbre `android/` complet et autonome. Stack Kotlin + AndroidX + Gradle/AGP 8 + WebView Android + VpnService + Foreground Service
- **Stratégie : isolation OS maximale, duplication assumée** — Android ne factorise pas les couches OS desktop, chaque OS a son arbre dédié
- **Noyau Go partagé via gomobile** — frontière étroite : 5 packages exposés (`internal/protocol/`, `internal/registry/`, `internal/auth/`, `internal/crypto/`, `internal/leakcheck/`). Régénéré par script de build dédié, pas par Gradle directement
- **Capture L3 Android via VpnService** — file descriptor remis au noyau Go via JNI. Pas de TUN brut accessible Android non-rooté
- **Kill switch Android = réglage OS "VPN permanent + bloquer connexions sans VPN"** — l'app guide l'utilisateur via deeplink, ne peut pas l'activer programmatiquement (par design Android)
- **UI Android via WebView** — assets HTML/CSS/JS partagés avec desktop (sync au build), responsive mobile, JS Bridge `@JavascriptInterface` au lieu de serveur HTTP loopback
- **Distribution Android = F-Droid + APK direct GitHub release** — pas de Google Play en MVP
- **Pas d'auto-update Android intégré MVP** — F-Droid gère, Phase 2 si besoin
- **Notification persistante Foreground Service joue le rôle du tray** (concept tray inexistant Android)
- **Pas d'IPC sur Android** — mono-processus (Foreground Service + Activity dans le même Application), bridge JS↔Kotlin↔Go in-process
- **Préférences SharedPreferences sur Android** (pas TOML)
- **Aucune télémétrie, aucun crash reporter, aucun analytics côté Android** — engagement plus strict que apps grand public Android typiques
- **Cible RAM Android < 60 MB** (vs < 25 MB desktop) — overhead JVM + WebView + AndroidX accepté
- **Snapshot de préservation pré-Android recommandé** : tag git `pre-android-2026-04-29` avant démarrage implémentation

**Changements 2026-04-19 (conservés) :**
- **Pivot transport** : retrait fronting Cloudflare, accès direct DNS A → VPS origin. Session tokens bindés `r.RemoteAddr` au lieu de `CF-Connecting-IP`

**Changements 2026-04-15 (conservés) :**
- **Ajout support Linux** — Debian/Ubuntu (.deb), Fedora/RHEL (.rpm), Arch (AUR), Alpine (.apk) via GoReleaser + nfpm
- **Bascule capture L3 unifiée** : TUN (Linux `/dev/net/tun`) + Wintun (Windows DLL signée) via `wireguard/tun`. Capture machine-wide sans configuration applicative
- **Modèle gateway NAT côté relais** : le relais désencapsule les paquets IP, applique NAT, forwarde sur Internet. Client = pas de stack TCP/IP userspace (simplicité)
- **Kill switch firewall OS-level desktop** : nftables (Linux) + Windows Filtering Platform (Windows). Persiste aux crashes du service
- **Suppression extension navigateur** (FR37-40) — redondante avec la capture L3 machine-wide
- **Suppression proxy local HTTP CONNECT et proxy DNS UDP local**
- **Nouveaux packages desktop** : internal/tun/, internal/firewall/, internal/routing/
- **Refactor relais** : handler unifié `/tunnel`
- **Abandon du bypass > 50 Mo**
- **Snapshot de préservation** : tag git `windows-stable-2026-04-15` + branche `backup/windows-stable` + binaires archivés

**Traçabilité brainstorming (session 2026-03-08) :**
- #1 Notification tray native → FR13 (tray + IP dans fenêtre webview)
- #2 Proxy STUN via tunnel → **absorbé par la capture L3** (tout STUN passe par TUN structurellement, plus besoin d'un composant dédié)
- #3 Warrant Canary → Phase 3
- #4 Découverte dynamique relais → FR23/FR24 registre signé
- #5 Page santé publique → Phase 3
- #6 Suppression page web vérification IP → UI webview + tray (intégré MVP)
- #7 Double distribution (portable) → **abandonné lors de la révision 2026-04-08**
- #8 Blocklist communautaire → FR8b (désormais côté relais)
- #9 Certificate pinning → NFR2 + internal/crypto/pinning.go
- #10 Sélection auto relais optimal → FR26 (latency measurement)
- #11 Rollback auto update → FR36
- #12 Auto-test fuite périodique → FR31-34 (scheduler leakcheck + reconnexion auto dans FR34 révisé)

**Changements 2026-04-08 (conservés) :**
- Suppression du mode portable
- Wails v2 → webview/webview + fyne.io/systray
- Fusion internal/tray/ + internal/desktop/ → internal/ui/
- API REST locale

**Forces clés :**

_Desktop (inchangé) :_
- **Capture L3 machine-wide** — aucune configuration applicative requise, tout le trafic IP capté
- **Kill switch robuste** — règles firewall kernel-level, indépendantes du process client, survivent au crash
- **Anti-fuite structurelle** — DNS, WebRTC, IPv6 ne peuvent pas fuir
- Architecture 2 processus simplifiée — service privilégié + UI user, IPC propre
- Architecture multi-relais résiliente — registre signé Ed25519, failover automatique
- Zero-log par design
- Camouflage protocolaire — paquets IP encapsulés dans HTTP/3 + TLS 1.3 direct
- UI riche via webview/webview — charte plateformeliberte.fr
- Packaging Linux natif — apt/dnf/pacman/apk + AUR
- Code cross-platform unifié desktop — même `wireguard/tun` Win + Linux

_Android (NOUVEAU) :_
- **Capture machine-wide via VpnService** — tout le trafic IP du téléphone passe par le tunnel, sans config par app
- **Kill switch OS-level** — réglage Android "VPN permanent + bloquer connexions sans VPN", kernel-level, inviolable côté app, survit aux crashes et redémarrages
- **Anti-fuite structurelle** identique desktop (DNS résolu côté relais, IPv6 capturé)
- **Zero télémétrie / crash reporter / analytics** — posture confidentialité plus stricte que la plupart des apps Android grand public
- **Permissions runtime minimales** — INTERNET, ACCESS_NETWORK_STATE, FOREGROUND_SERVICE, POST_NOTIFICATIONS, RECEIVE_BOOT_COMPLETED, FOREGROUND_SERVICE_SPECIAL_USE. Aucune permission sensible (storage, location, contacts)
- **F-Droid + APK direct** — pas de Google Play, pas de compte Google requis utilisateur
- **Build reproductible F-Droid** — renforce confiance utilisateur (avec effort initial pour atteindre l'objectif)
- **Charte visuelle réutilisée** — les mêmes assets HTML/CSS/JS que desktop (cohérence multi-plateforme)
- **Noyau protocole/crypto/registry partagé via gomobile** — code éprouvé desktop directement réutilisé Android, pas de divergence

_Cross-OS :_
- **Isolation OS maximale assumée** — chaque OS évolue à son rythme, pas de couplage cross-OS sur les couches d'intégration. Maintenance simplifiée long terme
- **Frontière noyau Go partagé étroite et contractuelle** — change rarement, vérifié par lint
- **Relais inchangé** — un seul backend pour 3 OS, économies opérationnelles

**Risques identifiés :**

_Desktop (inchangé) :_
- Performance tunnel : latence additionnelle encapsulation IP dans HTTP/3
- Complexité NAT côté relais
- IPv6 : à valider
- Distros sans appindicator (GNOME Wayland pur)

_Android (NOUVEAU) :_
- **Performance pompe paquets JNI** — coût traversée Kotlin↔Go par paquet potentiellement gênant à 100+ Mbps. Mitigation : batching ou pompe full-Go via fd. Benchmark obligatoire avant release
- **Détection "VPN permanent" actif** — heuristique fragile (`Settings.Global` non-publique). Risque casse Android future. Mitigation : fallback "non vérifiable", l'utilisateur fait confiance à son setup
- **Battery Optimization Doze** — Android 10-11 peut tuer le tunnel en veille longue sans exemption. Mitigation : opt-in utilisateur lors de l'onboarding (non-obligatoire mais recommandé)
- **Build reproductible F-Droid** — atteindre hash déterministe nécessite config Gradle pointue (timestamps, ordering, etc.). Mitigation : guide F-Droid + tests automatisés
- **Émulateurs CI** — tests instrumentés Android nécessitent device/émulateur, complexifie pipeline GitHub Actions. Mitigation : Firebase Test Lab ou KVM Linux
- **Conflit avec d'autres VpnService** — un seul VpnService actif simultanément. Si l'utilisateur lance Orbot ou autre VPN, Le Voile sera désactivé par OS sans préavis. Documentation utilisateur requise
- **Localisation Android limitée à fr/en** — autres langues Phase 2

_Cross-OS :_
- **Triple maintenance** — chaque ajout fonctionnel requiert souvent triplication. Accepté par directive isolation OS, mais exige discipline
- **Drift potentiel UX** entre desktop et Android — le frontend HTML/CSS/JS est partagé mais doit s'adapter au mobile (responsive). Tests visuels manuels nécessaires

**Améliorations futures (post-MVP) :**
- Registre dynamique avec API de gestion (Phase 2)
- CI/CD GitHub Actions complet (build multi-arch + publication AUR + APK F-Droid)
- Support macOS (utun — `wireguard/tun` le gère déjà)
- Support iOS (Phase 3+ — effort App Store + Network Extension distinct)
- Publication Google Play Android (Phase 2+ — AAB déjà buildable, Play App Signing à mettre en place)
- Auto-update Android intégré (Phase 2)
- Split tunneling Android par-app via `VpnService.Builder.addAllowedApplication()` (Phase 2)
- Alignement PRD avec la nouvelle architecture (incluant Android)
- Support ICMP (ping, traceroute) côté relais via raw sockets — Phase 2
- Support Android TV / Wear OS / Auto — Phase 2+ si demande

## Architecture Decision Records (ADRs)

Décisions structurantes — format condensé. ADR-01 à ADR-07 issus du pivot 2026-04-15. ADR-08 à ADR-14 issus de la Phase Android 2026-04-29.

**ADR-01: Gateway NAT côté relais** (vs netstack userspace client / TAP L2)
- Le relais parse les paquets IP, applique NAT, forwarde via sockets système. Client = pas de stack TCP/IP userspace
- Justification : complexité client minimale, modèle éprouvé (OpenVPN, WireGuard servers), maintenance single-dev soutenable
- Conséquence : ICMP (ping, traceroute) différé Phase 2 — acceptable car TCP/UDP couvrent 99% du trafic utilisateur grand public

**ADR-02: Suppression totale de l'extension navigateur**
- Validée lors de la décision initiale du pivot
- Justification : capture L3 rend l'extension architecturalement redondante ; élimine maintenance MV3/MV2/CRX/XPI
- Conséquence : bypass > 50 Mo abandonné (les relais absorbent la bande passante, 10 GiB/jour/IP)

**ADR-03: nftables exclusif sur Linux (pas de fallback iptables)**
- Backend firewall unique sur Linux. Si `nft` absent ou `nf_tables` kernel module indisponible → échec service explicite
- Justification : distros MVP cibles (Debian 11+, Ubuntu 22+, Fedora 38+, Arch, Alpine 3.18+) supportent toutes nftables. Fallback = doublement code + bugs + tests
- Conséquence : Ubuntu 18.04, Debian 10, CentOS 7 non supportés (fin de vie)

**ADR-04: Capabilities Linux via systemd `AmbientCapabilities=`** (vs setcap sur binaire)
- `User=levoile` + `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW` dans le unit `levoile.service`. Pas de setcap au post-install
- Justification : capabilities persistent aux updates binaire sans réapplication manuelle, plus robuste que setcap (qui est perdu à chaque remplacement de binaire)
- Conséquence : dépendance à systemd sur Linux (acceptable — Alpine a systemd optionnel, OpenRC devra attendre Phase 2 si demande)

**ADR-05: Ordre de démarrage simple** (pas de 2 phases lockdown)
- Séquence Connect : elevation → tun → routing → firewall → tunnel. Pas de lockdown initial au boot service
- Justification : **produit grand public, pas cible hackers**. Protection commence à l'usage actif des services, pas au boot système. Fenêtre de fuite de quelques secondes entre démarrage OS et Connect complet est acceptée
- Conséquence : apps auto-lancées (clients cloud, AV, Windows Update) peuvent émettre du trafic pendant les 5-10 premières secondes post-boot. Simplicité architecturale privilégiée

**ADR-06: DNS résolu côté relais** (vs DoH client via tunnel)
- Le relais embarque un resolver DNS avec upstream (Cloudflare/Quad9) + blocklist StevenBlack/hosts
- Justification : simplicité client, blocklist centralisée (mise à jour sans push client), latence optimale
- Conséquence : le "voir les noms" est inhérent à tout VPN. Modèle de confiance = opérateur relais, cohérent avec industrie

**ADR-07: wireguard/tun** (vs songgao/water / implémentation maison)
- Bibliothèque TUN/Wintun `golang.zx2c4.com/wireguard/tun` — on importe UNIQUEMENT le module interface, pas le protocole WireGuard
- Justification : même auteur que Wintun, maturité production (WireGuard Windows, millions d'installs), API unifiée Linux+Windows
- Conséquence : risque de confusion avec le protocole WireGuard (on fait du QUIC/HTTP3, pas du WireGuard). Documenté explicitement en interne

---

_ADRs Phase Android (2026-04-29) :_

**ADR-08: Isolation OS maximale, duplication assumée** (vs abstractions cross-OS partagées)
- Chaque OS (Windows, Linux, Android) a son propre arbre de code complet et autonome. Seul le strict noyau protocole/crypto/registre/session est mutualisé (5 packages Go via gomobile pour Android, packages natifs pour desktop)
- Justification : maintenance long-terme par un développeur unique + IA. Une abstraction `Capture`/`Firewall`/`Service` partagée Windows/Linux/Android serait artificielle (les sémantiques OS divergent fondamentalement) et coûteuse à maintenir. Cohérent avec l'évolution récente du repo (split installers/goreleaser per-OS, suppression Makefile racine partagé)
- Conséquence : triplication de code visible (lifecycle service, capture L3, kill switch, UI host, packaging). Acceptée comme prix de la clarté et de l'autonomie de chaque arbre

**ADR-09: gomobile pour le noyau Go partagé Android** (vs réécriture native Kotlin / réécriture WASM)
- `golang.org/x/mobile/cmd/gomobile bind -target=android` produit un `.aar` consommable par Gradle. 5 shims gomobile sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` ré-exposent une surface minimale en types primitifs. Les shims `crypto`, `registry`, `leakcheck` consomment en lecture les packages racine `internal/{crypto,registry,leakcheck,tunnel}`. Les shims `protocol` et `auth` sont canoniques (pas de package racine équivalent — la logique correspondante vit dans `internal/tunnel/{pump.go,client.go}`)
- Justification : maturité production éprouvée (Tailscale Android, WireGuard Android). Réutilise 100% de la logique protocole/crypto déjà testée desktop. Réécriture Kotlin native = doublement de maintenance + divergence inévitable. WASM = stack inutile pour cet usage
- Conséquence : dépendance à gomobile (maintenu par Google mais cadence release lente), surface JNI à monitorer côté perf, nécessité d'un script de build dédié (`android/scripts/build-aar.{sh,ps1}`) — pas une task Gradle directe pour garder une frontière claire. Ajout `golang.org/x/mobile` dans `go.mod` racine (indirect build-time, non embarqué dans binaires desktop)

**Erratum Story 9.2 (2026-05-02) — cartographie réelle vs spec d'origine** : la spec d'origine de cet ADR (rédigée 2026-04-29) référençait 5 packages racine `internal/{protocol,registry,auth,crypto,leakcheck}` exposés directement à gomobile. À l'implémentation Story 9.2, deux divergences ont émergé :
1. **`internal/protocol/` et `internal/auth/` n'existent pas** dans le repo — leur logique vit dans `internal/tunnel/{pump.go,client.go}` (decision pivot 2026-04-19). Solution : créer 2 shims canoniques `android/shims/{protocol,auth}/` qui exposent une surface gomobile-compatible minimale alimentée à terme par `internal/tunnel/` (Story 9.7).
2. **La règle Go `internal/`** interdit l'import depuis le work dir temporaire gobind (hors module). Solution : étendre la stratégie shim aux 3 packages racine restants (`internal/{crypto,registry,leakcheck}`) → 5 shims sous `android/shims/` (et NON `android/internal/`, qui serait soumis à la même règle Go interne).

La frontière contractuelle reste **étroite** (5 shims, types primitifs uniquement). Verify-shared-imports.sh (Story 9.2 AC#5) lint en CI les imports cross-OS interdits sur les shims + packages racine consommés.

**ADR-10: VpnService Android + kill switch via réglage OS "VPN permanent"** (vs simulation kill switch app-level)
- Capture L3 via `android.net.VpnService` (seule API non-rootée). Kill switch délégué à l'OS via le réglage utilisateur "VPN permanent + bloquer connexions sans VPN" dans Settings Android. L'app guide l'utilisateur via deeplink `Settings.ACTION_VPN_SETTINGS` et un onboarding obligatoire au premier lancement
- Justification : Android non-rooté ne permet PAS d'installer de règles iptables/eBPF côté app. Le réglage OS est kernel-level, inviolable côté app, persiste aux crashes/redémarrages — supérieur à toute simulation app-level (reconnect-loop, drop sockets, etc.)
- Conséquence : dépendance à un réglage utilisateur explicite. Risque de bypass si l'utilisateur ne l'active pas. Mitigation : onboarding obligatoire + warning persistant dans l'UI principale si non-activé. Détection "VPN permanent actif" via heuristique non-publique (`Settings.Global`) — fragile, fallback "non vérifiable"

**ADR-11: F-Droid + APK direct comme canaux MVP Android** (vs Google Play immédiat)
- Distribution Android via F-Droid (catalogue officiel logiciel libre) + APK direct sur GitHub releases. Pas de Google Play en MVP
- Justification : F-Droid + APK direct cohérent avec l'esprit logiciel libre du projet, pas de compte Google requis utilisateur, pas de scrutin Play Store sur les VPN (Google a des ToS spécifiques aux VPN apps), build reproductible obligatoire pour F-Droid (renforce confiance utilisateur)
- Conséquence : reach plus limité que Play Store, pas de mises à jour automatiques pour les utilisateurs APK direct (F-Droid gère ses utilisateurs). Phase 2+ : publication Play Store si traction suffisante (AAB déjà buildable, Play App Signing à mettre en place)

**ADR-12: Android API 29+ (Android 10+) comme cible minimale** (vs API 26+ ou API 33+)
- `minSdk = 29` (Android 10+). Couvre ~80% du parc actif fin 2026
- Justification API 29+ : TLS 1.3 garanti par OS (NFR1 satisfait sans hack), Scoped Storage par défaut (sécurité renforcée), VpnService stable et bien documenté, restrictions VPN cohérentes. < 29 introduit des corner cases sécu/VPN qui ne valent pas l'effort
- Justification refus API 33+ : trop restrictif, perd ~30% du parc inutilement
- Conséquence : utilisateurs Android 8/9 non supportés. Acceptable car ils peuvent utiliser desktop ou rester sur l'OS d'origine (parc en déclin)

**ADR-13: Notification persistante Foreground Service comme rôle du tray Android** (vs widget / activité résidente / shortcut)
- Le Foreground Service `LeVoileVpnService` affiche une notification ongoing non-dismissable (channel `levoile_vpn_status`, importance LOW) avec statut + pays + IP + action "Déconnecter" (PendingIntent FLAG_IMMUTABLE). Pas de widget homescreen, pas de raccourci système
- Justification : pattern standard Android pour services long-lived. Obligatoire depuis Android 8+ pour Foreground Service. Notification persiste à la fermeture de l'Activity, joue exactement le rôle du tray desktop. Widget = effort additionnel, raccourci = redondant
- Conséquence : la notification ne peut pas être supprimée par l'utilisateur tant que le service tourne. C'est intentionnel et conforme aux guidelines Android — tous les VPN apps fonctionnent ainsi

**ADR-14: WebView Android + assets HTML/CSS/JS partagés desktop** (vs Jetpack Compose / Flutter / réécriture native)
- UI Android = `MainActivity` + `WebView` plein écran chargeant les mêmes assets HTML/CSS/JS que desktop (synchronisés au build via `android/scripts/sync-frontend.sh`). JS Bridge `@JavascriptInterface` pour exposer les commandes natives (Connect, GetStatus, etc.). Charte plateformeliberte.fr réutilisée à 100%, layout responsive mobile (media queries)
- Justification : cohérence visuelle cross-OS forte, réutilisation maximale du frontend, pas de réécriture UI. Compose = réécriture totale + perte charte. Flutter/RN = stack additionnelle (Dart/JS) lourde, contredit "minimal dependencies"
- Conséquence : performance WebView légèrement inférieure à du natif (acceptable pour une UI peu interactive), dépendance à `androidx.webkit` (stable), surface d'attaque WebView (mitigée par `WebViewAssetLoader` + JS Bridge restrictif)

**ADR-15: Aucune télémétrie / crash reporter / analytics côté Android** (vs Firebase Crashlytics / Sentry minimal)
- Pas de Firebase Crashlytics, pas de Sentry, pas de Bugsnag, pas d'analytics maison. Crashes restent locaux. Bug reports via export texte manuel (sans IP, sans identifiant)
- Justification : un crash reporter externe contredit la promesse zéro-log et zéro-tracking. Même un Sentry self-hosted introduit une surface (qui héberge ? quelle juridiction ? etc.). Cohérent avec posture confidentialité du projet
- Conséquence : perte de visibilité sur les crashes en production. Mitigation : tests instrumentés robustes pré-release + bug reports utilisateur volontaires + canal F-Droid issues GitHub. Acceptable car l'engagement zéro-log prime sur le monitoring

**ADR-16: Assets web Android = sources Android-natives versionnées** (vs sync depuis `windows/frontend/`)
- Date : 2026-05-03 — formalisation post-implémentation Story 11.1 (Option 2)
- Décision : les fichiers `android/app/src/main/assets/web/{index.html, style.css, app.js, style-android.css}` sont **versionnés directement** dans le repo comme assets Android-natifs. Le script `android/scripts/sync-frontend.sh` ne synchronize PAS depuis `windows/frontend/` — il sert d'**idempotency check** (vérifie présence des fichiers requis pour préparer Story 12.2 CI Android). Le `.gitignore` `assets/.gitignore` reste vide commenté (anti-régression au cas où une story future réintroduirait un sync)
- Justification :
  1. Le frontend `windows/frontend/` est lourdement Windows-spécifique : `.titlebar` custom (C1), sidebar pays (C2/C3), serveur HTTP local `/api/*`, modal `quit-modal` (C8) — la majorité du markup est inutilisable Android
  2. Story 10.2 (bandeau C17) a été livrée DIRECTEMENT dans les assets Android (`assets/index.html` + `style.css` + `app.js`) — un sync depuis `windows/frontend/` aurait écrasé/cassé ce composant
  3. Cohérent ADR-08 (isolation OS maximale) + memory `feedback_os_isolation` : « duplication code Win/Linux/Android préférée à abstraction partagée »
  4. La duplication de markup entre `windows/frontend/` et `android/app/src/main/assets/web/` est faible (composants partagés visuellement = `style.css` palette + typo, mais markup divergent : Android = AppBar 56dp + bottom-sheet, desktop = titlebar + sidebar). La maintenance double est acceptable vs. une abstraction de templating qui serait sur-conçue pour 4 fichiers
- Conséquence :
  - Le AC #1 et AC #2 originaux Story 11.1 (script qui copie + sed find/replace + `.gitignore` actif `web/*`) sont OBSOLÈTES — voir révision Story 11.1 post-implémentation
  - Toute évolution UI Android passe par éditer directement `android/app/src/main/assets/web/*` (pas via `windows/frontend/`)
  - Aucun ADR cross-OS n'impose la duplication des composants visuels — chaque OS reste libre de son markup tant que la palette CSS commune (couleurs, typo) reste cohérente
- Surface impactée : Story 11.1 (script + `.gitignore`), toutes les stories 11.3/11.4/11.6/11.7 qui ont enrichi les assets Android directement

## Epic-Boundary Records (EBR)

Mini-décisions formalisant les frontières inter-epics établies lors du découpage Phase 2 Android (élicitation comparative 2026-05-02). Documentent comment des fonctionnalités initialement contiguës ont été splittées entre epics pour préserver l'indépendance de livraison de chaque epic. Référencés dans `epics.md` §FR Coverage Map et §Epic List Phase 2.

**EBR-01 : Split du composant C16 (notification persistante Android) entre Epic 9 et Epic 11**

- **Décision :** la notification persistante du Foreground Service (FR-AND-2, composant UX C16) est livrée en deux étapes : (1) version MVP « statut texte minimal » dans Epic 9 Story 9.6 — titre `« Le Voile · {État} »`, texte secondaire vide ; (2) enrichissement dynamique pays + IP visible + action contextuelle dans Epic 11 Story 11.7
- **Justification :** Epic 9 doit pouvoir être livré et testable en isolation (foundation Android : VpnService + Foreground Service + bridge gomobile + UI placeholder). Lier la notification finale au SelectCountry/relayState couplerait Epic 9 à Epic 11. Le statut texte minimal suffit à valider la promesse fonctionnelle « notification ongoing pendant tunnel actif »
- **Conséquence :** Epic 9 livre un produit Android techniquement utilisable (tunnel + notification basique) ; Epic 11 enrichit l'expérience sans casser de contrat. La notification ne change jamais d'identité visuelle (même channel, même icône, même `setOngoing(true)`) — seul son contenu textuel s'enrichit
- **Surface impactée :** Story 9.6 (notification MVP), Story 11.7 (enrichissement pays + IP + action dynamiques)

**EBR-02 : Migration de FR-AND-3 (onboarding « VPN permanent ») d'Epic 10 vers Epic 11**

- **Décision :** l'onboarding obligatoire au premier lancement avec deeplink `Settings.ACTION_VPN_SETTINGS` (FR-AND-3, composant UX C15) appartient à Epic 11 et non Epic 10. Le composant C17 « Bandeau alerte kill switch » et l'heuristique `Settings.Global.always_on_vpn_app` (Story 10.1) restent dans Epic 10
- **Justification :** Epic 11 livre la séquence Léa #9 navigable de bout en bout (UI mobile + onboarding + bridge). Garder l'onboarding dans Epic 10 obligerait à coupler Epic 10 à un OnboardingActivity et à une persistence SharedPreferences `onboarding_completed`, alors qu'Epic 10 doit rester focalisé sur la couche détection/protection (heuristique + bandeau + détection autre VPN + audit télémétrie + filtrage logs)
- **Conséquence sur l'indépendance d'Epic 10 :** Story 10.2 (Bandeau alerte C17) reçoit un fallback explicite — si Epic 11 n'est pas encore livré, le tap sur le bandeau ouvre directement `Intent(Settings.ACTION_VPN_SETTINGS)` (panneau VPN Android natif) plutôt que d'ouvrir le composant C15 d'Epic 11. Cette branche garantit qu'Epic 10 reste autonomement déployable
- **Re-sizing post-EBR-02 :** Epic 11 passe de « moyen » à « lourd » (8 stories) avec l'ajout de l'OnboardingActivity (Story 11.5) et du composant C15 enrichi (Story 11.6) ; Epic 10 reste « moyen » (5 stories)
- **Surface impactée :** Story 10.2 (fallback Settings direct), Story 11.5 (OnboardingActivity placeholder), Story 11.6 (composant C15 complet)

**EBR-03 : (à formaliser)**

- **Statut :** référencé dans `epics.md` §Epic List Phase 2 (ligne 526) sans définition complète dans les artefacts publiés
- **Action requise :** Akerimus à fournir le contexte de cette troisième frontière issue de l'élicitation comparative 2026-05-02 (notes de session non publiées dans le repo). Hypothèses possibles à confirmer/infirmer : frontière entre Story 11.7 (enrichissement notification dynamique) et Story 9.5 (Foreground Service lifecycle) sur la responsabilité du payload status ; ou frontière entre Epic 12 (audit reproductibilité APK) et Epic 9 (build `.aar` reproductible Story 9.2) ; ou autre. Sans confirmation, EBR-03 reste un placeholder dans le coverage map epics.md
- **Mitigation immédiate :** la référence textuelle « EBR-03 » dans epics.md ligne 526 ne bloque aucune story (aucune Story ne dépend de la définition EBR-03 pour son AC). Documentation à compléter au prochain refactor cross-epic ou avant retrospective Phase 2 Android
