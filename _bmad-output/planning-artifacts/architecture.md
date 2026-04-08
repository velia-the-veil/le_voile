---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
lastStep: 8
status: 'revised'
completedAt: '2026-03-08'
rewrittenAt: '2026-04-08'
rewrittenReason: 'Suppression mode portable, remplacement Wails v2 par webview/webview + fyne.io/systray (binaire UI unique), architecture 2 processus'
inputDocuments: ['prd.md', 'prd-validation-report.md', 'codebase analysis 2026-04-02']
workflowType: 'architecture'
project_name: 'bmad_vpn_le_voile_de_velia'
user_name: 'Akerimus'
date: '2026-04-08'
---

# Architecture Decision Document

_Ce document reflète l'architecture cible au 8 avril 2026 — après suppression du mode portable et remplacement de Wails v2 par webview/webview + fyne.io/systray._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
40 FRs organisés en 11 domaines :
- **Tunnel & Connexion (FR1-4)** — Établissement QUIC/HTTPS via Cloudflare, reconnexion auto avec backoff exponentiel, authentification Ed25519 par relais, session tokens signés (TTL 4h) avec circuit breaker
- **Protection DNS (FR5-8)** — Proxy DNS local UDP (127.0.0.1:53), forwarding DoH via tunnel, kill switch (arrêt du proxy quand tunnel coupé), blocklist DNS StevenBlack/hosts in-memory
- **IP Camouflage (FR27-30)** — Proxy HTTP CONNECT local (127.0.0.1:50113), tunneling trafic web via relais, session token Ed25519, rate limiting par IP (200 max), bandwidth limiting par IP (quota journalier), protection SSRF, volume tracker avec bypass direct pour domaines > 500 Mo
- **Protection Anti-Fuite (FR31-34)** — Détection fuites WebRTC via STUN Binding Requests (RFC 5389), politiques navigateur (Chromium registre + Firefox policies.json), vérification IP tunnel vs IP détectée, scheduler périodique
- **Interface Utilisateur (FR9-13b)** — Binaire UI unique (`levoile-ui`) combinant webview/webview (fenêtre 420×540px) + fyne.io/systray (icône tray) dans un seul processus, charte plateformeliberte.fr, sélecteur de pays avec drapeaux
- **Démarrage & Lifecycle (FR14-16)** — Service Windows (kardianos/service). UI tray persiste en arrière-plan, fenêtre webview ouverte/fermée à la demande
- **Relais Multi-VPS (FR17-19b)** — Relais HTTP/3 stateless avec DoH, STUN relay, CONNECT proxy, bandwidth limiting, organisés par pays, registre distribué signé Ed25519
- **Découverte & Sélection (FR23-26)** — Registre distribué signé (/.well-known/relay-registry.json), sélection par pays, failover automatique, latency measurement via /health
- **Extension Navigateur (FR37-40)** — Extension WebExtension (Chrome MV3 + Firefox MV2), routage par défaut via proxy Le Voile, bypass direct pour fichiers > 50 Mo, health check proxy, installation auto via browser policies
- **Distribution (FR20-22)** — Installeur NSIS (Windows uniquement), GoReleaser pour le packaging
- **Mise à jour (FR35-36)** — Vérification périodique GitHub releases, téléchargement rate-limited, vérification signature Ed25519, rollback

**Non-Functional Requirements:**
20 NFRs répartis en 4 axes :
- **Sécurité (NFR1-9)** — TLS 1.3 min, Ed25519 par relais + registre signé master key, zero persistence, résistance DPI (QUIC/HTTPS via Cloudflare), zero fuite DNS/WebRTC, restauration resolver, protection SSRF, validation source Cloudflare (CF-Connecting-IP), code auditable
- **Performance (NFR10-14)** — Latence DNS < 50ms, tunnel < 3s, reconnexion avec backoff (100ms → 30s), RAM < 20MB, CPU < 1%
- **Fiabilité (NFR15-18)** — Kill switch via arrêt proxy DNS, watchdog resolver 3s, crash-recovery DNS/proxy au redémarrage, failover transparent multi-relais
- **Confidentialité (NFR19-20)** — Aucun log IP client (ni relais ni proxy), IP hash uniquement dans session tokens

**Scale & Complexity:**
- Domaine principal : Application desktop + serveur réseau (cybersécurité/vie privée)
- Complexité : Élevée — protocoles réseau spécialisés, cryptographie, intégration OS profonde, multi-relais, proxy CONNECT, IPC multi-processus
- Composants architecturaux : ~18 packages internal/ (service, ui, ipc, ipchandler, et modules métier)

### Technical Constraints & Dependencies

- **Go 1.26** — Langage imposé (binaire unique cross-platform, bibliothèques crypto standard)
- **webview/webview** — Fenêtre desktop native utilisant WebView2 (Windows) / WebKitGTK (Linux) / WebKit (macOS). Remplace Wails v2 — plus léger, pas de contrainte sur le cycle de vie applicatif
- **fyne.io/systray** (v1.12.0) — Icône system tray, même processus que le webview (binaire UI unique)
- **kardianos/service** (v1.2.4) — Gestion service Windows SCM (install/uninstall/start/stop)
- **Architecture 2 processus** — Service (background) + UI (tray + webview) communiquant via IPC named pipes
- **Microsoft/go-winio** (v0.6.2) — Named pipes Windows pour IPC
- **quic-go** (v0.59.0) — Implémentation QUIC production-ready, HTTP/3 + TLS 1.3
- **BurntSushi/toml** (v1.5.0) — Configuration TOML
- **Cloudflare** — CDN intermédiaire pour le camouflage protocolaire (QUIC/HTTPS)
- **Ed25519** — Algorithme d'authentification par relais + signature du registre par master key
- **Multi-VPS** — Relais répartis géographiquement par pays, scalables horizontalement
- **Windows principal** — Validation MVP sur Windows (service SCM, UAC, registre, WinINET proxy)
- **Développeur unique + IA** — Contrainte ressource forte, architecture doit rester simple
- **NSIS** — Installeur Windows (installation service, tray autostart, shortcuts, extension deployment)

### Cross-Cutting Concerns Identified

- **Sécurité** — Chiffrement bout en bout, zero-log, résistance DPI, clé Ed25519 par relais, registre signé master key, session tokens signés, validation source CF, protection SSRF — touche tunnel, dns, relay, registry, httpproxy
- **Anti-Fuite** — Détection WebRTC via STUN, politiques navigateur Chromium/Firefox, kill switch DNS — touche leakcheck, stun, browser, dns
- **Routage Intelligent** — Extension navigateur bypass gros fichiers, volume tracker proxy local bypass > 500 Mo, cohabitation SysProxy + extension — touche extension/, browser, httpproxy
- **Fiabilité** — Kill switch (arrêt proxy DNS), watchdog resolver, reconnexion auto avec backoff, failover multi-relais — touche service, dns, tunnel, registry
- **Intégration OS** — Gestion DNS système (build tags), élévation UAC, politiques navigateur, system proxy WinINET, singleton mutex, service Windows SCM — varie par plateforme
- **Camouflage** — Trafic indiscernable HTTPS standard + proxy CONNECT transparent — touche tunnel, httpproxy, relay, Cloudflare
- **Statelessness** — Aucune persistence côté relais — touche architecture relais, registre, monitoring
- **Découverte** — Registre distribué signé, sélection géographique, failover, latency measurement — touche client, relais, déploiement
- **Mise à jour** — Auto-update via GitHub releases, vérification signature Ed25519, rollback, rate limiting — touche updater, distribution
- **IPC** — Communication inter-processus service ↔ UI via named pipes (Windows) ou Unix sockets — touche ipc, ipchandler, service, ui

## Starter Template Evaluation

### Primary Technology Domain

Application desktop Go + serveur réseau Go — pas de framework web, décisions portant sur structure projet, bibliothèques clés et outillage build.

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

**Build & Distribution :**

| Option | Verdict |
|---|---|
| **GoReleaser v2** | **Sélectionné** — cross-compilation, packaging multi-cible |
| **NSIS** | **Sélectionné** — installeur Windows (service + tray + desktop + shortcuts) |

### Selected Stack: Go + quic-go + webview/webview + fyne.io/systray + kardianos/service + GoReleaser + NSIS

**Rationale for Selection:**
- `quic-go` — Seule implémentation QUIC pure Go production-ready. TLS 1.3 intégré, HTTP/3 client+serveur
- `webview/webview` — Fenêtre desktop native via WebView2 (Windows). Même moteur que Wails v2, mais sans contrainte lifecycle. Le frontend HTML/CSS/JS existant est réutilisable quasi tel quel
- `fyne.io/systray` — Systray pur Go sur Windows. Même processus que le webview (binaire UI unique)
- `kardianos/service` — Service background Windows SCM avec install/uninstall/start/stop
- `NSIS` — Installeur Windows complet (service, UI autostart, shortcuts, extension deployment, uninstall propre)
- GoReleaser — Build multi-cible (service, UI, relay)

**Architectural Decisions Provided by Stack:**

**Language & Runtime:**
Go 1.26 + webview/webview (WebView2 sur Windows) + fyne.io/systray — architecture 2 processus avec IPC

**Styling Solution:**
HTML/CSS/JS via webview/webview — Charte visuelle plateformeliberte.fr :
- Fond sombre : `#0b1526` / `#0e1e38`
- Bleu primaire : `#1a6fc4`, accents : `#2a8dff`
- Rouge alertes : `#d42b2b`
- Vert connecté : `#4ade80`, orange connecting : `#fb923c`, rouge risk : `#ff3c3c`
- Texte : `#f0f4ff` (principal), `#8a9bb8` (secondaire)
- Typographies : Bebas Neue (titres), Rajdhani (interface), Inter (corps) — woff2 embarqués
- Effets : gradients radiaux, ombres-lueur bleues, animations subtiles

**Build Tooling:**
GoReleaser v2 — 3 cibles de build (service Win, UI Win, relay Linux)
NSIS — installeur Windows avec service registration, shortcuts, extension deployment

**Testing Framework:**
Go standard `testing` — pas de framework tiers. Tests unitaires, edge cases, e2e. 90+ fichiers de test

**Code Organization:**

```
le_voile/
  go.mod / go.sum
  .goreleaser.yaml                 # 3 cibles de build (service, UI, relay)
  config.example.toml
  cmd/
    client/main.go                 # Service Windows (kardianos/service)
    ui/main.go                     # UI unique : fyne.io/systray + webview/webview
    relay/main.go                  # Relais VPS HTTP/3
    genregistry/main.go            # Outil génération registre
  internal/
    ~18 packages (détaillés ci-dessous)
  frontend/                        # Assets HTML/CSS/JS (embarqués via go:embed, servis par HTTP local)
  extension/                       # Extension navigateur WebExtension
  installer/                       # NSIS script + assets
  deploy/                          # systemd + install.sh pour relais Linux
  assets/icons/                    # Icônes tray (ico)
  tools/crxgen/                    # Outil génération CRX signé
```

**Development Experience:**
- `go run ./cmd/ui` pour le développement UI (tray + webview)
- `go run ./cmd/client run` pour le service en mode interactif
- `go run ./cmd/relay` pour le développement relais
- `go test ./...` pour les tests
- `go build ./cmd/ui` pour le build UI
- `goreleaser release --clean` pour les builds de release

## Core Architectural Decisions

### Decision Priority Analysis

**Critical Decisions (Bloquent l'implémentation) :**
- Architecture 2 processus : Service background (kardianos/service) + UI unique (fyne.io/systray + webview/webview). IPC via named pipes (Windows) ou Unix sockets
- Protocole client ↔ relais : HTTP/3 DoH (RFC 8484) + CONNECT proxy + STUN relay
- Kill switch DNS : Arrêt du proxy DNS local quand tunnel coupé (pas de redirect vers 127.0.0.1)
- Proxy DNS local : UDP listener 127.0.0.1:53, forward DoH via tunnel, blocklist NXDOMAIN
- Limite connexions par relais : 150 simultanées (DoH)
- Registre distribué signé : chaque relais sert `/.well-known/relay-registry.json`, signé Ed25519 master key
- Sélection relais : pays → failover auto → latency measurement via /health
- UI desktop : webview/webview frameless 420×540px (charte plateformeliberte.fr) + fyne.io/systray dans un seul binaire

**Important Decisions (Façonnent l'architecture) :**
- Config client : TOML — %AppData%/LeVoile/ (Windows) ou ~/.config/levoile/ (Unix)
- Authentification : Unidirectionnelle — clé Ed25519 unique par relais, certificate pinning TLS
- Session tokens : Ed25519 signés, TTL 4h, IP hash dans le payload, refresh automatique avec backoff exponentiel + circuit breaker (5 échecs DoH consécutifs → déconnexion auto)
- IP Camouflage : Proxy HTTP CONNECT local (127.0.0.1:50113), volume tracker par domaine (bypass direct > 500 Mo, cooldown 24h)
- Rate limiting relais : 200 connexions CONNECT max par IP (sync.Map + atomics), bandwidth limiter par IP (quota journalier)
- Extension navigateur : WebExtension (Chrome MV3 + Firefox MV2) — routage proxy par défaut, bypass direct fichiers > 50 Mo, health check proxy 5s, CRX/XPI pré-signés et embarqués dans le binaire
- Cohabitation SysProxy + Extension : SysProxy WinINET (HKCU) pour les apps hors navigateur, extension pour le routage intelligent navigateur
- Anti-fuite : Détection WebRTC via STUN (3 serveurs Google + Cloudflare), politiques navigateur Chromium (registre Windows) + Firefox (policies.json)
- Blocklist DNS : Téléchargement StevenBlack/hosts, stockage in-memory map, swap atomique, NXDOMAIN filtering dans le proxy DNS
- DNS par OS : Interface Go `DNSManager` + build tags (windows: netsh, linux: resolv.conf/resolvectl, darwin: networksetup)
- Déploiement relais : scp + systemd (registre JSON déployé avec chaque binaire)
- Monitoring : /health endpoint anonyme sur chaque relais (status, connections, uptime, RAM, CPU, country, relay_id)
- Auto-update : Vérification périodique GitHub releases (6h), signature Ed25519 checksums, rollback, rate limiting downloads
- Installeur NSIS : Installation service SCM, UI autostart registre HKCU, shortcuts desktop/Start menu, extension deployment Chrome/Firefox via policies, désinstallation propre (restore proxy, kill browsers, remove policies)

**Deferred Decisions (Post-MVP) :**
- Authentification client (tokens/clés client) — Phase 3 si restriction d'accès
- CI/CD automatisé — GitHub Actions quand le projet sera stabilisé
- Registre dynamique (API de gestion) — Phase 2, remplacer le JSON statique

### Configuration & Données

- **Aucune base de données** — Ni client ni relais. Cohérent avec zero-log
- **Config client** : TOML dans %AppData%/LeVoile/ (Windows) ou ~/.config/levoile/ (Unix)
- **Contenu config** : Relay domain + clé publique Ed25519, pays préféré, auto-start, STUN server, update settings (GitHub owner/repo, rate limit, interval), blocklist enable/interval, registry settings (URL, master key, refresh interval), HTTP proxy enable/port, browser policies enable, UI http server port
- **Registre relais** : Fichier JSON signé déployé sur chaque relais, servi via `/.well-known/relay-registry.json`. Contient version, master public key, entries avec ID, domain, public key, signature, added timestamp. Vérifié Ed25519 master key côté client
- **Cache registre client** : Fichier JSON local persisté par `internal/registry/cache.go`. Fallback si aucun relais ne répond au démarrage (cold start resilience)
- **Authentification** : Unidirectionnelle — chaque relais possède sa propre paire Ed25519. Certificate pinning TLS : le client vérifie que le cert du relais contient la bonne clé publique. Pas d'identité client au MVP

### Communication & Protocole

- **Client ↔ Relais (DoH)** : HTTP/3 via quic-go/http3. Requêtes DNS en POST HTTPS DoH (RFC 8484) sur `https://{relay-domain}/dns-query`. Trafic indiscernable de requêtes HTTPS normales via Cloudflare. Upstream failover : Cloudflare 1.1.1.1 primary, Quad9 9.9.9.9 fallback, recovery probing
- **Client ↔ Relais (CONNECT)** : POST `https://{relay-domain}/connect` avec header Authorization (session token). Le relais établit la connexion TCP sortante et relaie bidirectionnellement via HTTP/3 streaming. Validation : session token Ed25519, source Cloudflare IP (CF-Connecting-IP), rate limit par IP (200 max), bandwidth limit par IP (quota journalier), SSRF protection (bloque réseaux privés)
- **Client ↔ Relais (STUN)** : POST `https://{relay-domain}/stun-relay` — relais de STUN Binding Requests pour la détection de fuites WebRTC
- **Client ↔ Relais (IP)** : GET `https://{relay-domain}/ip` — retourne l'IP visible du client (pour le statut desktop)
- **Session Token** : Obtenu via GET `/verify` (challenge-response), signé Ed25519, contient IP hash SHA256 + timestamp + TTL 4h. Refresh automatique avec backoff exponentiel (100ms → 30s) + circuit breaker (5 échecs DoH consécutifs → déconnexion auto)
- **Proxy Local** : HTTP CONNECT proxy sur `127.0.0.1:50113`. Intercepte le trafic navigateur/apps, tunnelise via le handler CONNECT du relais. Volume tracker : bypass direct pour domaines dépassant 500 Mo (cooldown 24h), exception pour CDN partagés (Akamai, Cloudflare, Fastly — full FQDN)
- **Proxy DNS Local** : UDP listener sur `127.0.0.1:53` (+ `[::1]:53`). Forward DoH via tunnel. Blocklist : NXDOMAIN pour domaines StevenBlack/hosts
- **Client → Registre** : GET `https://{any-relay}/.well-known/relay-registry.json` au démarrage. Réponse JSON signée avec liste complète des relais. Vérification Ed25519 master key. Fallback sur le cache local si aucun relais ne répond. Refresh toutes les 6h en arrière-plan
- **Communication Service ↔ UI** : IPC via named pipes (Windows `\\.\pipe\levoile`) ou Unix sockets. Protocole JSON ligne par ligne. Messages : Request (Action + Value) → Response (Status + champs optionnels). Max 4 Ko par message
- **Communication UI interne** : Serveur HTTP local embarqué (127.0.0.1:{port configurable}) servant les assets frontend et exposant une API REST JSON pour le webview. Remplace les bindings Go↔JS de Wails v2. Le frontend utilise `fetch()` vers ce serveur local au lieu de `window.go.main.App.Method()`
- **IPC Actions** : GetStatus, Connect, Disconnect, SelectCountry, GetRegistry, GetLeakStatus, TriggerLeakCheck, GetUpdateStatus, CheckUpdate, GetAutoStart, SetAutoStart, GetBlocklist, SetBlocklist, GetHTTPProxy, SetHTTPProxy, GetBrowserPolicies, SetBrowserPolicies, Quit
- **Limite par relais (DoH)** : 150 connexions simultanées via atomic.Int64. Au-delà : middleware rejection
- **Limite par relais (CONNECT)** : 200 connexions max par IP source (sync.Map + atomics) + bandwidth limiting par IP (quota journalier)

### Architecture 2 Processus & Intégration OS

Deux processus communiquant via IPC named pipes :

1. **levoile-service.exe** (`cmd/client/`) — Service Windows SCM (kardianos/service)
   - Orchestrateur principal (`internal/service/service.go`)
   - Gère le lifecycle de tous les composants : tunnel QUIC, proxy DNS, kill switch, watchdog, blocklist, HTTP proxy, registry discovery, failover, leak check, updater, browser policies
   - Expose un serveur IPC pour communication avec l'UI
   - Sous-commandes : install, uninstall, start, stop, run
   - Flags CLI : -config, -relay-domain, -relay-pubkey, -insecure

2. **levoile-ui.exe** (`cmd/ui/`) — UI unique : fyne.io/systray + webview/webview
   - **Tray** (fyne.io/systray, thread principal) : Icône avec menu contextuel (Ouvrir, Connecter/Déconnecter, Pays, Quitter). Polling status service via IPC toutes les 2 secondes. Icônes embarquées via `//go:embed`. Gestion proxy système WinINET (HKCU)
   - **Webview** (webview/webview) : Fenêtre 420×540px, ouverte/fermée à la demande depuis le menu tray "Ouvrir". Utilise WebView2 (Windows). Affiche le frontend HTML/CSS/JS servi par le serveur HTTP local embarqué
   - **Serveur HTTP local** (net/http, 127.0.0.1:{port}) : Sert les assets frontend embarqués (go:embed). Expose une API REST JSON qui proxie les commandes vers le service via IPC. Endpoints : /api/status, /api/connect, /api/disconnect, /api/country, /api/registry, /api/leak-status, etc.
   - Singleton via mutex nommé Windows (empêche instances multiples)
   - `-H windowsgui` dans ldflags (pas de console)
   - La fenêtre webview se ferme sans quitter l'app — le tray persiste. "Quitter" depuis le tray arrête tout

#### Composants Communs

- **Service / Orchestrateur** (`internal/service/`) : Package central qui orchestre le lifecycle de tous les composants. Implémente `kardianos/service.Interface`. Gère : tunnel, DNS proxy, kill switch, watchdog, blocklist, HTTP proxy, registry discovery, failover, leak check scheduling, updater, browser policies. Expose les méthodes appelées par l'IPC handler
- **IPC** (`internal/ipc/`) : Client/serveur avec transport platform-specific (named pipes Windows via go-winio, Unix sockets). Protocole JSON ligne par ligne, max 4 Ko par message
- **IPC Handler** (`internal/ipchandler/`) : Dispatcher centralisé qui route les requêtes IPC vers les méthodes du service. Gère la persistence config avec mutex de sérialisation
- **Gestion DNS système** (`internal/dns/`) : Interface `DNSManager` avec implémentations par OS via build tags
  - Windows : `netsh interface ip set dns` + flush DNS cache via DnsFlushResolverCache
  - Linux : `/etc/resolv.conf` ou `resolvectl`
  - macOS : `networksetup -setdnsservers`
- **Proxy DNS local** (`internal/dns/proxy.go`) : UDP listener 127.0.0.1:53, forward DoH via tunnel. Blocklist NXDOMAIN filtering (extraction nom DNS du wire format). SO_REUSEADDR pour coexistence
- **Kill switch DNS** (`internal/dns/kill_switch.go`) : Arrêt du proxy DNS local quand tunnel coupé → plus aucune résolution possible tant que le tunnel n'est pas rétabli
- **Watchdog** (`internal/watchdog/`) : Goroutine surveillant le resolver DNS toutes les 3 secondes. Vérifie que le resolver système pointe vers 127.0.0.1. Corrige le drift causé par des processus externes
- **Sélection relais** (`internal/registry/`) : Discovery orchestrator (fetch → verify → cache → default relay), failover automatique, latency measurement via /health, sélection par pays, cache local persistent
- **Proxy HTTP CONNECT** (`internal/httpproxy/`) : Serveur proxy local sur 127.0.0.1:50113, écoute loopback uniquement. Volume tracker par domaine : bypass direct si > 500 Mo (cooldown 24h, exception CDN partagés). Connection draining à l'arrêt
- **Détection fuites WebRTC** (`internal/leakcheck/` + `internal/stun/`) : STUN Binding Requests (RFC 5389) vers 3 serveurs (Google ×2, Cloudflare). XOR-MAPPED-ADDRESS parsing. Scheduler périodique avec skip quand kill switch actif ou tunnel déconnecté. Transaction ID validation anti-spoofing
- **STUN Interceptor & Relayer** (`internal/stun/`) : Intercepte les paquets STUN sur les ports appropriés. Drop les messages TURN (hérite le masquage IP du serveur TURN). Relay des Binding Requests via tunnel. Semaphore-based concurrency control (20 slots)
- **Protection navigateur** (`internal/browser/`) : Extension CRX/XPI embarquées via `//go:embed`. Déploiement via browser policies (registre Windows pour Chrome, policies.json pour Firefox). Parsing protobuf CRX header pour dérivation Extension ID. Génération dynamique updates.xml Chrome
- **Proxy système** (`internal/ui/sysproxy_windows.go`) : Configuration WinINET proxy (HKCU) au connect, restauration au disconnect. Mécanisme de récupération orphelin en cas de crash
- **Singleton** (`internal/ui/singleton_windows.go`) : Mutex nommé Windows pour empêcher les instances multiples de l'UI
- **Serveur HTTP local** (`internal/ui/httpserver.go`) : Sert les assets frontend embarqués et expose l'API REST JSON pour le webview. Écoute loopback uniquement. Proxie les commandes vers le service via IPC client
- **Blocklist DNS** (`internal/blocklist/`) : Téléchargement StevenBlack/hosts (10 Mo max), parsing format hosts, stockage in-memory map, swap atomique. Refresh périodique configurable
- **Élévation privilèges** (`internal/elevation/`) : Détection admin Windows (token check). Utilisé par le service uniquement
- **Auto-update** (`internal/updater/`) : Checker GitHub releases API, downloader rate-limited, verifier Ed25519 signatures, installer atomic binary replacement, rollback previous version. Semver comparison, failed version tracking, platform-specific binary selection. Cycle timeout 10 min, retry network 30 min, retry integrity 1h, max 3 retries
- **Config** (`internal/config/`) : TOML schema avec sections relay, client, stun, update, blocklist, registry, http_proxy, browser_policies, ui. Auto-discovery path (CLI flag → default). Atomic writes. Platform-specific paths via build tags
- **Crypto** (`internal/crypto/`) : Ed25519 key pair generation/export/import/sign/verify. TLS self-signed cert generation. Certificate pinning (verify cert public key matches pinned Ed25519 key)

### Infrastructure & Déploiement

- **Relais VPS** (`cmd/relay/`) : Binaire Go autonome HTTP/3 (port 443) avec TLS. Handlers : DoH (/dns-query avec upstream failover), STUN relay (/stun-relay), CONNECT proxy (/connect avec session token + CF IP + rate limit + bandwidth limit + SSRF), IP detection (/ip), health (/health), registry (/.well-known/relay-registry.json). Build tag pour traçabilité. Flags : -addr, -cert, -key, -upstream, -fallback, -signing-key, -registry-file, -cf-insecure
- **Registre signé** : JSON avec version, master public key, entries signées individuellement. Client vérifie chaque entry avec la master key. Deployed as static file on each relay
- **Déploiement relais** : `deploy/install.sh` — Crée user system `levoile`, copie binaire /opt/levoile, cert/key avec permissions 0600, systemd service avec CAP_NET_BIND_SERVICE, ProtectSystem=strict, ProtectHome=true, NoNewPrivileges=true, restart always
- **Cloudflare** : Sous-domaines proxiés via Cloudflare (orange cloud). Trafic QUIC/HTTP3 client → Cloudflare CDN → VPS. Cloudflare gère le certificat TLS côté CDN. CF-Connecting-IP header pour identifier le client réel
- **Installeur NSIS** (`installer/levoile.nsi`) :
  - Install : UAC admin → kill instances → stop/unregister old service → copy binaries (service + UI) + icons → register service SCM → UI autostart HKCU → shortcuts desktop/Start menu → deploy extension Chrome/Firefox via policies → launch UI
  - Uninstall : kill instances → stop/unregister service → restore WinINET proxy → broadcast WM_SETTINGCHANGE → kill browsers → remove extension files/profiles → delete policy registry keys → remove shortcuts → cleanup
- **CI/CD** : Build local pour le MVP. GoReleaser en local (`goreleaser release --clean`). Pre-build hook : génération CRX signé via `tools/crxgen`. 3 cibles (service Win, UI Win, relay Linux)
- **Monitoring** : Endpoint `/health` sur chaque relais — métriques anonymes : `{"status":"ok","connections":42,"uptime":"3d12h","ram_mb":180,"cpu_pct":2.1,"country":"is","relay_id":"is-01"}`. Aucun log de requêtes, aucune IP client

### Decision Impact Analysis

**Séquence d'implémentation (telle que réalisée) :**
1. Structure projet + go.mod + dépendances
2. Module crypto (Ed25519 génération/vérification, TLS cert, certificate pinning)
3. Module config (TOML schema, discovery, paths OS)
4. Module IPC (client/server, named pipes/Unix sockets, protocole JSON)
5. Module tunnel (HTTP/3 client QUIC, certificate pinning, session tokens, reconnexion backoff, state machine)
6. Module DNS (interface + implémentations OS, proxy UDP local, kill switch, watchdog, DNS cache flush)
7. Relais stateless (HTTP/3 server, DoH avec upstream failover, STUN relay, /connect handler, /health, /verify, /ip, /.well-known/relay-registry.json, limiters, CF IP validator)
8. Module registry (modèle signé Ed25519, client fetch+verify, discoverer, cache, failover manager, latency checker, countries)
9. Module HTTP proxy (serveur local 127.0.0.1:50113, tunneling via relais, volume tracker bypass)
10. Module blocklist (téléchargement StevenBlack/hosts, parsing, manager refresh)
11. Module anti-fuite (leakcheck STUN + XOR-MAPPED-ADDRESS, scheduler, interceptor, relayer)
12. Module browser (extension embed CRX/XPI, deployment policies Chrome/Firefox, CRX header parsing)
13. Module elevation (UAC Windows, sudo Unix)
14. Module updater (checker GitHub, downloader rate-limited, verifier Ed25519, installer atomic, rollback)
15. Service orchestrateur (`internal/service/`) — lifecycle tous composants, IPC server, rollback detection
16. IPC handler (`internal/ipchandler/`) — dispatcher centralisé
17. UI unique (`internal/ui/`) — fyne.io/systray + webview/webview + serveur HTTP local, polling IPC, sysproxy WinINET, singleton
18. Frontend HTML/CSS/JS (charte plateformeliberte.fr, polling status 2s via fetch API REST local)
19. Extension navigateur (background.js, manifests Chrome MV3 + Firefox MV2, proxy PAC/onRequest, health check, bypass)
20. Installeur NSIS (service SCM, shortcuts, extension deployment)
21. Intégration bout en bout + distribution

**Dépendances inter-composants :**
- Tunnel dépend de crypto (certificate pinning Ed25519, session tokens)
- DNS proxy dépend de tunnel (forward DoH) + blocklist (NXDOMAIN filtering)
- Kill switch dépend de DNS proxy (stop/start)
- Watchdog dépend de DNS manager (vérification/restauration resolver)
- HTTP proxy dépend de tunnel (connexion CONNECT via relais)
- Registry dépend de crypto (verification signatures Ed25519 master key)
- Leakcheck dépend de stun (parsing) + tunnel (relay STUN, état connexion)
- Browser dépend de extension assets embarqués (CRX/XPI)
- Service dépend de tous les composants (tunnel, DNS, proxy, watchdog, blocklist, registry, httpproxy, leakcheck, browser, updater, config)
- IPC handler dépend de service (dispatch vers méthodes)
- UI dépend de IPC client (communication service) + sysproxy (WinINET) + webview + serveur HTTP local
- Updater dépend de crypto (vérification signature checksums)

## Implementation Patterns & Consistency Rules

### Points de Conflit Identifiés

20 zones où des agents IA pourraient diverger, toutes résolues ci-dessous.

### Naming Patterns

**Packages Go :**
- Noms courts, minuscules, un mot si possible : `tunnel`, `dns`, `crypto`, `watchdog`, `relay`, `registry`, `httpproxy`, `blocklist`, `browser`, `elevation`, `leakcheck`, `stun`, `updater`, `service`, `tray`, `desktop`, `ipc`, `ipchandler`, `config`
- Pas de underscores dans les noms de packages

**Fichiers Go :**
- `snake_case.go` : `dns_manager.go`, `kill_switch.go`, `connect_handler.go`, `volume_tracker.go`, `bandwidth_limiter.go`
- Build tags : `manager_windows.go`, `manager_linux.go`, `manager_darwin.go`, `paths_unix.go`, `elevation_unix.go`
- Stubs : `singleton_stub.go`, `sysproxy_stub.go`, `dnscache_other.go`, `reuseaddr_other.go`
- Tests co-localisés : `manager_test.go` à côté de `manager.go`
- Tests edge cases : `*_edge_test.go` (scénarios limites)
- Tests e2e : `e2e_test.go` (intégration)
- Tests platform-specific : `*_windows_test.go`, `*_linux_test.go`, `*_darwin_test.go`

**Fonctions & Types :**
- Exportés : `PascalCase` — `DNSManager`, `ConnectHandler`, `NewRelay`, `VolumeTracker`
- Privés : `camelCase` — `parseResponse`, `retryCount`, `dialRelay`
- Constantes exportées : `PascalCase` — `MaxConnections = 150`, `VolumeThreshold`, `DailyQuotaBytes`
- Constructeurs : `New` + nom du type — `NewDNSManager()`, `NewServer()`, `NewProgram()`

**Tests :**
- Nommage : `TestDNSManager_SetResolver`, `TestKillSwitch_Activate`, `TestVolumeTracker_Bypass`
- Table-driven tests quand > 2 cas
- Pas de framework tiers — `testing` standard + `t.Helper()` pour les helpers
- `t.Cleanup()` pour le teardown

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
POST /api/disconnect      → déconnecte via IPC
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

**Config TOML :**
```toml
[relay]
domain = "relay.levoile.dev"
public_key_ed25519 = "rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk="
# insecure = true  # dev only

[client]
auto_start = true
skip_quit_modal = false
preferred_country = "is"

[stun]
default_server = "stun.l.google.com:19302"

[update]
enabled = true
github_owner = "velia-the-veil"
github_repo = "le_voile"
check_interval = "6h"
rate_limit_kbps = 500

[blocklist]
enabled = true
update_interval = "24h"

[registry]
enabled = true
url = "https://relay.levoile.dev/.well-known/relay-registry.json"
master_public_key = "..."
refresh_interval = "6h"

[http_proxy]
enabled = true
port = 50113

[browser_policies]
enabled = true

[ui]
http_port = 50114
```

**Registre JSON signé (relay-registry.json) :**
```json
{
  "version": 1,
  "master_public_key": "base64...",
  "relays": [
    {
      "id": "is-01",
      "domain": "relay.levoile.dev",
      "public_key": "base64...",
      "signature": "base64...",
      "added": "2026-03-16T10:00:00Z"
    }
  ]
}
```

**Health Endpoint JSON :**
```json
{"status":"ok","connections":42,"uptime":"3d12h","ram_mb":180,"cpu_pct":2.1,"country":"is","relay_id":"is-01"}
```

### Error Handling Patterns

- Wrapping systématique : `fmt.Errorf("tunnel: connect: %w", err)`
- Préfixe = nom du package : `tunnel:`, `dns:`, `service:`, `ipc:`
- Erreurs sentinelles pour les cas récupérables : `var ErrInvalidKey = errors.New(...)`, `var ErrPinningFailed = errors.New(...)`
- Jamais de `panic` sauf bug critique — toujours retourner `error`
- **Logging relais** : `fmt.Fprintf(os.Stderr, ...)` — messages opérationnels uniquement, jamais de données utilisateur
- **Logging client** : Pas de framework log. Erreurs propagées vers le service → IPC → frontend pour affichage
- **Messages utilisateur en français** : Statuts affichés au frontend en français ("Connecté", "Reconnexion en cours...", "Déconnecté")

### Relay Selection & Failover Patterns

- **Découverte** (`internal/registry/discoverer.go`) : Orchestration fetch → verify (Ed25519 master key) → cache → default relay. Refresh background toutes les 6h
- **Sélection par pays** : L'utilisateur choisit un pays dans l'UI → IPC SelectCountry → service → discoverer filtre les relais actifs pour ce pays
- **Failover automatique** (`internal/registry/failover.go`) : En cas d'échec connexion, bascule vers le relais suivant dans le pool. Retry logic
- **Latency measurement** (`internal/registry/latency.go`) : Mesure RTT via GET /health sur chaque relais. Tri par latence. Cache + mesures fraîches
- **Country metadata** (`internal/registry/countries.go`) : Map pays → nom affiché + drapeau emoji. Extraction code pays depuis relay ID ou domain
- **Cache local** (`internal/registry/cache.go`) : Persistence fichier JSON des relais vérifiés. Source de vérité quand aucun relais ne répond
- **Bootstrap** : Premier lancement → relay domain par défaut dans config (relay.levoile.dev)

### UI Patterns (webview/webview + fyne.io/systray — processus unique)

- **Charte visuelle** : Reproduit plateformeliberte.fr — fond sombre `#0b1526`, bleu primaire `#1a6fc4`, accents `#2a8dff`, alertes `#d42b2b`
- **Fenêtre webview** : 420×540px, ouverte/fermée à la demande depuis le tray. Navigate vers `http://127.0.0.1:{port}/`
- **Layout** : Sidebar 150px (sélecteur pays) + Main panel (statut connexion centré)
- **Sélecteur de pays** : Liste avec drapeaux emoji + nom du pays + nombre de relais + indicateur actif
- **Statut connexion** : Dot animé (vert connecté steady, orange connecting pulse 1.5s, rouge déconnecté), pays, IP visible, relay ID, latence, uptime
- **Bouton** : Connect/Disconnect toggle. Désactivé pendant transition d'état
- **Modal** : Confirmation quitter avec "Ne plus afficher" checkbox, persisté config TOML
- **Polling** : Frontend JS poll `fetch('/api/status')` toutes les 2s, `fetch('/api/registry')` toutes les 30s
- **Tray (même processus)** :
  - fyne.io/systray sur le thread principal (bloquant — requis par systray)
  - Menu contextuel : Ouvrir (ouvre/montre la fenêtre webview), Connecter/Déconnecter, Pays (sous-menu), Quitter
  - Polling status service via IPC toutes les 2s
  - Icônes tray embarquées via `//go:embed` : connected.ico / connecting.ico / disconnected.ico
  - Proxy système WinINET : set au connect, restore au disconnect
  - Singleton Windows : mutex nommé pour empêcher instances multiples
- **Lifecycle** :
  - Lancement → tray démarre sur main thread, serveur HTTP local démarre en goroutine
  - "Ouvrir" depuis tray → crée/montre la fenêtre webview
  - Fermer la fenêtre → la détruit, le tray persiste. Réouverture → nouvelle fenêtre webview
  - "Quitter" depuis tray → arrêt serveur HTTP, libération webview, sortie processus

### Extension Navigateur Patterns

- **Chrome MV3** : `chrome.proxy.settings.set()` avec PAC script dynamique. Route tout via `PROXY 127.0.0.1:50113; DIRECT`. Exceptions loopback + bypass set
- **Firefox MV2** : `browser.proxy.onRequest` listener direct. Retourne config proxy par requête. ID : `levoile@plateformeliberte.fr`, strict_min_version: 142.0
- **Health check proxy** : Fetch périodique (5s) vers `http://127.0.0.1:50113/`. Si unreachable → switch DIRECT (fail-safe). Auto-recovery quand proxy revient
- **Bypass gros fichiers** : `onHeadersReceived` vérifie Content-Length > 50 Mo → ajoute au bypass set (TTL 120s) → cancel + re-download via `chrome.downloads.download()` / direct
- **Déploiement** : CRX/XPI pré-signés embarqués dans binaire (`//go:embed`). Extension ID dérivé cryptographiquement du CRX public key (SHA256 → hex). Chrome : updates.xml généré dynamiquement. Firefox : policy registry key

### Concurrency Patterns

- `context.Context` passé en premier argument partout (timeout, cancellation)
- Channels pour la communication inter-goroutines (état tunnel → service, watchdog → DNS)
- `sync.Mutex` pour état partagé simple (config save, compteurs)
- `atomic.Int64` pour le compteur de connexions relais
- `sync.Map` + atomics pour rate limiting lock-free (IP limiter, bandwidth limiter)
- Jamais de goroutines orphelines — toujours gérées via context ou `sync.WaitGroup`
- Pattern standard : `go func() { defer wg.Done(); ... }()`
- Semaphore pattern via buffered channel (STUN relayer : 20 slots)
- UI : tray sur main thread (bloquant), serveur HTTP + IPC polling en goroutines, webview créé/détruit à la demande

### Assets

- Icônes tray dans `internal/ui/icons/` (embedded via `//go:embed icons`) — connected.ico, connecting.ico, disconnected.ico, levoile.ico
- Extension CRX/XPI dans `internal/browser/extension_assets/build/` (embedded via `//go:embed all:extension_assets`)
- Fonts frontend dans `frontend/assets/fonts/` — BebasNeue, Rajdhani, Inter (woff2)
- Frontend assets dans `frontend/` (embedded via `//go:embed` dans `internal/ui/embed.go`, servis par le serveur HTTP local)

### Enforcement Guidelines

**Tous les agents IA DOIVENT :**
- Suivre les conventions de nommage Go ci-dessus sans exception
- Utiliser `context.Context` comme premier paramètre de toute fonction bloquante ou réseau
- Wrapper les erreurs avec le préfixe du package
- Ne jamais logger de données utilisateur côté relais (IP, contenu DNS)
- Utiliser les build tags pour le code spécifique OS
- Co-localiser les tests avec le code source
- Communiquer service ↔ UI exclusivement via IPC (jamais d'import direct entre ces packages)
- Toujours passer par le registry discoverer/failover pour obtenir un relais
- Respecter la charte visuelle plateformeliberte.fr dans tout le frontend
- Toute logique d'orchestration passe par `internal/service/` — les packages internes ne s'appellent pas entre eux directement (sauf via interfaces)
- L'IPC handler (`internal/ipchandler/`) est le seul point d'entrée pour les requêtes UI vers le service
- Messages utilisateur affichés au frontend en français

**Anti-Patterns à éviter :**
- `panic` pour des erreurs récupérables
- Goroutines sans context ni WaitGroup
- `log.Fatal` / `log.Println` côté client
- Mutex imbriqués (risque deadlock)
- Imports circulaires entre packages `internal/`
- Import direct de `internal/service` depuis `internal/ui` (passer par IPC)
- Connexion directe à un relais sans passer par le registry discoverer
- Hardcoder des endpoints de relais (sauf le domaine par défaut dans config)

## Project Structure & Boundaries

### Complete Project Directory Structure

```
le_voile/
├── go.mod
├── go.sum
├── .goreleaser.yaml                   # 3 cibles de build (service Win, UI Win, relay Linux)
├── config.example.toml                # Template config
├── .gitignore
├── LICENSE
├── README.md
│
├── cmd/
│   ├── client/
│   │   ├── main.go                    # Service Windows SCM (kardianos/service)
│   │   └── main_test.go
│   ├── ui/
│   │   └── main.go                    # UI unique : fyne.io/systray + webview/webview + HTTP local
│   ├── relay/
│   │   ├── main.go                    # Relais VPS HTTP/3 (port 443)
│   │   └── main_test.go
│   └── genregistry/
│       └── main.go                    # Outil génération registre signé
│
├── internal/
│   ├── service/
│   │   ├── service.go                 # Orchestrateur principal — lifecycle tous composants (1340 lignes)
│   │   ├── service_test.go
│   │   ├── service_edge_test.go
│   │   ├── service_53_edge_test.go
│   │   └── e2e_recovery_test.go
│   │
│   ├── ipc/
│   │   ├── messages.go                # Request/Response JSON + constantes actions/statuts
│   │   ├── client.go                  # IPC client (connect, send, receive)
│   │   ├── client_test.go
│   │   ├── client_edge_test.go
│   │   ├── server.go                  # IPC server (listen, dispatch)
│   │   ├── server_test.go
│   │   ├── server_edge_test.go
│   │   ├── pipe_windows.go            # Named pipes (go-winio)
│   │   ├── pipe_unix.go               # Unix sockets
│   │   ├── pipe_windows_test.go
│   │   ├── pipe_unix_test.go
│   │   └── e2e_test.go
│   │
│   ├── ipchandler/
│   │   ├── handler.go                 # Dispatcher IPC → service methods
│   │   ├── handler_test.go
│   │   └── handler_edge_test.go
│   │
│   ├── ui/
│   │   ├── ui.go                      # Point d'entrée UI : systray main thread + webview + HTTP server
│   │   ├── ui_test.go
│   │   ├── httpserver.go              # Serveur HTTP local (assets + API REST JSON)
│   │   ├── httpserver_test.go
│   │   ├── embed.go                   # //go:embed frontend — assets HTML/CSS/JS
│   │   ├── icons.go                   # //go:embed icons — ICO files
│   │   ├── sysproxy_windows.go        # WinINET proxy management
│   │   ├── sysproxy_windows_test.go
│   │   ├── sysproxy_stub.go           # No-op pour Unix
│   │   ├── singleton_windows.go       # Mutex nommé anti-doublon
│   │   ├── singleton_stub.go          # No-op pour Unix
│   │   └── icons/
│   │       ├── connected.ico
│   │       ├── connecting.ico
│   │       ├── disconnected.ico
│   │       └── levoile.ico
│   │
│   ├── config/
│   │   ├── config.go                  # TOML schema, Load/Save atomic
│   │   ├── config_test.go
│   │   ├── discover.go                # Auto-discovery config path
│   │   ├── discover_test.go
│   │   ├── paths_windows.go           # %AppData%/LeVoile/
│   │   └── paths_unix.go              # ~/.config/levoile/
│   │
│   ├── crypto/
│   │   ├── ed25519.go                 # Key pair gen, import/export, sign/verify
│   │   ├── ed25519_test.go
│   │   ├── tls.go                     # Self-signed X.509 cert generation
│   │   ├── tls_test.go
│   │   ├── pinning.go                 # Certificate pinning Ed25519
│   │   └── pinning_test.go
│   │
│   ├── tunnel/
│   │   ├── client.go                  # HTTP/3 client QUIC, cert pinning, session tokens
│   │   ├── client_test.go
│   │   ├── client_edge_test.go
│   │   ├── reconnect.go               # Reconnexion auto backoff + circuit breaker
│   │   ├── reconnect_test.go
│   │   ├── reconnect_edge_test.go
│   │   ├── state.go                   # Machine d'état : connected/connecting/disconnected
│   │   └── state_test.go
│   │
│   ├── dns/
│   │   ├── manager.go                 # Interface DNSManager
│   │   ├── manager_test.go
│   │   ├── manager_windows.go         # netsh interface ip set dns
│   │   ├── manager_windows_test.go
│   │   ├── manager_linux.go           # resolv.conf / resolvectl
│   │   ├── manager_linux_test.go
│   │   ├── manager_darwin.go          # networksetup
│   │   ├── manager_darwin_test.go
│   │   ├── proxy.go                   # UDP DNS proxy 127.0.0.1:53, DoH forward, blocklist NXDOMAIN
│   │   ├── proxy_test.go
│   │   ├── kill_switch.go             # Stop/start proxy DNS
│   │   ├── kill_switch_test.go
│   │   ├── kill_switch_edge_test.go
│   │   ├── check_windows.go           # DNS validation Windows
│   │   ├── check_windows_test.go
│   │   ├── check_linux.go             # DNS validation Linux
│   │   ├── check_linux_test.go
│   │   ├── check_darwin.go            # DNS validation macOS
│   │   ├── check_darwin_test.go
│   │   ├── dnscache_windows.go        # DnsFlushResolverCache
│   │   ├── dnscache_other.go          # No-op
│   │   ├── reuseaddr_windows.go       # SO_REUSEADDR
│   │   ├── reuseaddr_other.go
│   │   ├── e2e_test.go
│   │   └── e2e_windows_test.go
│   │
│   ├── watchdog/
│   │   ├── watchdog.go                # Surveillance resolver DNS (3s interval)
│   │   └── watchdog_test.go
│   │
│   ├── registry/
│   │   ├── registry.go                # Modèle registre signé, Parse, VerifyAll
│   │   ├── registry_test.go
│   │   ├── client.go                  # Fetch + verify registre depuis URL
│   │   ├── client_test.go
│   │   ├── discoverer.go              # Orchestration : fetch → verify → cache → default
│   │   ├── discoverer_test.go
│   │   ├── cache.go                   # Persistence locale relais vérifiés
│   │   ├── cache_test.go
│   │   ├── failover.go                # Bascule relais en cas d'échec
│   │   ├── failover_test.go
│   │   ├── latency.go                 # Mesure RTT via /health
│   │   ├── latency_test.go
│   │   ├── countries.go               # Metadata pays (nom, drapeau emoji)
│   │   ├── countries_test.go
│   │   └── e2e_test.go
│   │
│   ├── httpproxy/
│   │   ├── server.go                  # HTTP proxy local 127.0.0.1:50113
│   │   ├── server_test.go
│   │   ├── connect_handler.go         # CONNECT tunneling + plain HTTP
│   │   ├── connect_handler_test.go
│   │   ├── volume_tracker.go          # Per-domain volume tracking, bypass > 500 Mo
│   │   ├── volume_tracker_test.go
│   │   └── e2e_test.go
│   │
│   ├── blocklist/
│   │   ├── downloader.go              # HTTP fetch StevenBlack/hosts (10 Mo max)
│   │   ├── downloader_test.go
│   │   ├── parser.go                  # Parse hosts format → domain map
│   │   ├── parser_test.go
│   │   ├── manager.go                 # Thread-safe manager, refresh périodique
│   │   └── manager_test.go
│   │
│   ├── browser/
│   │   ├── detect.go                  # Interface détection navigateurs
│   │   ├── detect_windows.go          # Détection Chrome/Firefox/Edge Windows
│   │   ├── detect_linux.go
│   │   ├── detect_darwin.go
│   │   ├── manager.go                 # Orchestration déploiement extensions
│   │   ├── manager_windows.go         # Registry policies Chrome, policies.json Firefox
│   │   ├── manager_linux.go
│   │   ├── manager_darwin.go
│   │   ├── manager_test.go
│   │   ├── manager_windows_test.go
│   │   ├── extension_embed.go         # //go:embed all:extension_assets — CRX/XPI + CRX header parsing
│   │   ├── lock_windows.go            # File locking Windows
│   │   ├── lock_linux.go
│   │   ├── lock_darwin.go
│   │   ├── e2e_test.go
│   │   ├── e2e_windows_test.go
│   │   └── extension_assets/
│   │       ├── src/
│   │       │   ├── manifest.json
│   │       │   ├── manifest_firefox.json
│   │       │   ├── background.js
│   │       │   └── icons/ (16, 48, 128px)
│   │       └── build/
│   │           ├── levoile.crx         # Chrome extension signée
│   │           └── levoile.xpi         # Firefox add-on signé
│   │
│   ├── elevation/
│   │   ├── elevation_windows.go       # UAC token check
│   │   └── elevation_test.go
│   │
│   ├── leakcheck/
│   │   ├── webrtc.go                  # STUN Binding Request leak checker
│   │   ├── webrtc_test.go
│   │   ├── webrtc_edge_test.go
│   │   ├── xor_mapped.go             # XOR-MAPPED-ADDRESS parsing (RFC 5389)
│   │   ├── xor_mapped_test.go
│   │   ├── xor_mapped_edge_test.go
│   │   ├── scheduler.go               # Periodic leak check runner
│   │   └── scheduler_test.go
│   │
│   ├── stun/
│   │   ├── stun.go                    # STUN header constants, IsSTUN/IsBindingRequest/IsTURN
│   │   ├── parser.go                  # Parse STUN header
│   │   ├── parser_test.go
│   │   ├── interceptor.go            # UDP listener, packet classification
│   │   ├── interceptor_test.go
│   │   ├── interceptor_53_test.go
│   │   ├── interceptor_edge_test.go
│   │   ├── relayer.go                 # Relay Binding Requests via tunnel
│   │   ├── relayer_test.go
│   │   ├── relayer_53_test.go
│   │   └── relayer_edge_test.go
│   │
│   ├── updater/
│   │   ├── updater.go                 # Orchestrateur check + download
│   │   ├── updater_test.go
│   │   ├── checker.go                 # GitHub Releases API
│   │   ├── checker_test.go
│   │   ├── downloader.go             # Download rate-limited
│   │   ├── downloader_test.go
│   │   ├── verify.go                  # Vérification signature Ed25519 checksums
│   │   ├── verify_test.go
│   │   ├── installer.go               # Atomic binary replacement
│   │   ├── installer_test.go
│   │   ├── rollback.go                # Restore previous version
│   │   ├── rollback_test.go
│   │   ├── version.go                 # Semver parsing + comparison
│   │   └── version_test.go
│   │
│   └── relay/
│       ├── server.go                  # HTTP/3 + TCP HTTPS dual stack
│       ├── server_test.go
│       ├── server_edge_test.go
│       ├── doc.go                     # Package documentation
│       ├── doh_handler.go             # DoH proxy, upstream failover + recovery probing
│       ├── doh_handler_test.go
│       ├── doh_handler_edge_test.go
│       ├── stun_handler.go            # STUN relay handler
│       ├── stun_handler_test.go
│       ├── stun_handler_edge_test.go
│       ├── connect_handler.go         # CONNECT proxy (token + CF IP + rate + BW + SSRF)
│       ├── connect_handler_test.go
│       ├── verify_handler.go          # /verify — challenge-response, session token issuance
│       ├── verify_handler_test.go
│       ├── verify_handler_edge_test.go
│       ├── ip_handler.go              # /ip — client IP detection
│       ├── ip_handler_test.go
│       ├── health.go                  # /health — métriques anonymes
│       ├── health_test.go
│       ├── cfip.go                    # Cloudflare IP validator + refresh + fallback
│       ├── cfip_test.go
│       ├── limiter.go                 # Atomic connection counter (150 max)
│       ├── limiter_test.go
│       ├── middleware.go              # Connection limiting middleware
│       ├── middleware_test.go
│       ├── ip_limiter.go              # Per-IP connection limiter (200 max, sync.Map)
│       ├── ip_limiter_test.go
│       ├── bandwidth_limiter.go       # Per-IP daily bandwidth quota
│       ├── bandwidth_limiter_test.go
│       └── e2e_test.go
│
├── frontend/
│   ├── index.html                     # Point d'entrée UI (420×540)
│   ├── src/
│   │   ├── style.css                  # Charte plateformeliberte.fr
│   │   └── app.js                     # Polling fetch('/api/status') 2s, registry 30s, country selector
│   └── assets/
│       └── fonts/                     # Bebas Neue, Rajdhani, Inter (woff2)
│
├── extension/
│   ├── background.js                  # Proxy PAC (Chrome) + proxy.onRequest (Firefox)
│   ├── manifest.json                  # Base manifest
│   ├── manifest_chrome.json           # Chrome MV3
│   ├── manifest_firefox.json          # Firefox MV2 (gecko ID + strict_min_version)
│   ├── levoile.pem                    # Signing key
│   ├── .amo-upload-uuid               # Firefox AMO upload ID
│   └── icons/                         # 16, 48, 128px
│
├── installer/
│   ├── levoile.nsi                    # NSIS script complet (install/uninstall)
│   ├── build.ps1                      # PowerShell build script
│   ├── build.sh                       # Bash build script
│   ├── levoile.ico                    # Icône installeur
│   ├── config-default.toml            # Config par défaut pour distribution
│   └── build/                         # Artefacts compilés
│
├── deploy/
│   ├── install.sh                     # Installation relais Linux
│   └── levoile-relay.service          # Systemd unit (CAP_NET_BIND_SERVICE, ProtectSystem=strict)
│
├── assets/
│   └── icons/
│       ├── connected.ico
│       ├── connecting.ico
│       └── disconnected.ico
│
├── tools/
│   ├── crxgen/
│   │   └── main.go                    # Générateur CRX signé
│   └── gen_icons.go                   # Outil génération icônes
│
├── scripts/
│   └── test-extension-install.ps1     # Test installation extension
│
└── docs/
    └── validation-e2e.md              # Guide validation bout en bout
```

### Architectural Boundaries

**Frontière IPC (Service ↔ UI) :**
- Communication exclusivement via named pipes (Windows) ou Unix sockets
- Protocole JSON ligne par ligne, max 4 Ko par message
- Le service est l'autorité : l'UI est un client IPC pur
- Aucun import Go direct entre `internal/service` et `internal/ui`
- L'IPC handler (`internal/ipchandler/`) est le seul dispatcher côté service

**Frontière Réseau (Client ↔ Relais) :**
- Points de contact : POST `/dns-query` (DoH), POST `/connect` (CONNECT proxy), GET `/verify` (session token), POST `/stun-relay` (STUN), GET `/ip`, GET `/.well-known/relay-registry.json`
- Tout passe via Cloudflare (`https://{relay-domain}`) — jamais d'accès direct au VPS
- Le registry discoverer est le seul composant qui détermine quel relais contacter
- Le handler CONNECT valide : session token Ed25519, source Cloudflare IP (CF-Connecting-IP), rate limit par IP, bandwidth limit par IP, SSRF (bloque réseaux privés)
- `/health` accessible publiquement mais ne contient que des métriques anonymes

**Frontière Proxy Local (Apps/Navigateurs ↔ Le Voile) :**
- Proxy HTTP CONNECT sur `127.0.0.1:50113` — écoute loopback uniquement
- Proxy DNS UDP sur `127.0.0.1:53` — écoute loopback uniquement
- Les navigateurs/apps se connectent au proxy local, qui tunnelise via le relais
- Le proxy ne fait que relayer — aucune inspection de contenu
- Volume tracker : bypass direct si domaine dépasse 500 Mo (cooldown 24h)

**Frontière Découverte (Client ↔ Registre) :**
- GET `/.well-known/relay-registry.json` sur n'importe quel relais retourne la liste complète signée
- Vérification Ed25519 master key obligatoire côté client
- Le cache local est le fallback si aucun relais ne répond
- Le registre est en lecture seule côté client

**Frontière Webview (Frontend JS ↔ UI Go — même processus) :**
- Communication via serveur HTTP local embarqué (API REST JSON sur 127.0.0.1:{port})
- Le frontend utilise `fetch()` vers `/api/*` — pas de bindings Go↔JS directs
- Le serveur HTTP local (`internal/ui/httpserver.go`) proxie les commandes vers le service via IPC client — pas d'accès direct aux composants internes
- Endpoints : /api/status, /api/connect, /api/disconnect, /api/country, /api/registry, /api/leak-status, /api/settings, /api/quit, etc.

**Frontière OS (DNS Manager) :**
- Interface `DNSManager` dans `manager.go` — contrat unique
- Implémentations OS isolées derrière build tags — jamais d'imports croisés
- Chaque implémentation OS est autonome et testable indépendamment

**Frontière Relais (Stateless) :**
- Aucun état persisté entre les requêtes
- Compteurs en RAM uniquement (connexions, bandwidth) — volatile
- Le relais ne connaît ni les clients ni leurs requêtes passées
- Le fichier registry.json est statique, déployé avec le binaire

### Requirements to Structure Mapping

**FR1-4 (Tunnel & Connexion) →** `internal/tunnel/`, `internal/crypto/`
**FR5-8 (Protection DNS) →** `internal/dns/`, `internal/blocklist/`, `internal/watchdog/`
**FR9-13b (Interface Utilisateur) →** `frontend/`, `internal/ui/`
**FR14-16 (Démarrage & Lifecycle) →** `internal/service/`, `internal/elevation/`, `cmd/client/`
**FR17-19b (Relais Multi-VPS) →** `internal/relay/`, `cmd/relay/`, `deploy/`
**FR20-22 (Distribution) →** `.goreleaser.yaml`, `installer/`
**FR23-26 (Découverte & Sélection) →** `internal/registry/`
**FR27-30 (IP Camouflage) →** `internal/httpproxy/`, `internal/relay/connect_handler.go`, `internal/relay/ip_limiter.go`, `internal/relay/bandwidth_limiter.go`, `internal/relay/cfip.go`, `internal/relay/verify_handler.go`
**FR31-34 (Anti-Fuite) →** `internal/leakcheck/`, `internal/stun/`, `internal/browser/`
**FR35-36 (Mise à jour) →** `internal/updater/`
**FR37-40 (Extension Navigateur) →** `extension/`, `internal/browser/`

**Cross-Cutting :**
- Kill switch (FR8) → `internal/dns/kill_switch.go` + `internal/watchdog/`
- Reconnexion auto (FR2) → `internal/tunnel/reconnect.go`
- Failover multi-relais → `internal/registry/failover.go` + `internal/tunnel/`
- Session tokens → `internal/tunnel/client.go` + `internal/relay/verify_handler.go`
- Anti-fuite WebRTC → `internal/leakcheck/` + `internal/stun/` + `internal/browser/`
- Extension navigateur → `extension/` + `internal/browser/` (installation auto via policies)
- Démarrage (FR14) → `internal/service/` + `internal/elevation/`
- Découverte registre → `internal/registry/` + `internal/relay/` (sert le JSON)
- IPC → `internal/ipc/` + `internal/ipchandler/`

### Data Flow

**Flux principal (requête DNS) :**
```
[Utilisateur] → [Application sur le PC]
                        ↓ (requête DNS)
              [Resolver système → 127.0.0.1:53]
                        ↓
              [internal/dns/proxy.go (UDP listener)]
                        ↓ (blocklist check)
              [Bloqué ? → NXDOMAIN]
              [Autorisé ? → internal/tunnel/client.go]
                        ↓ (POST HTTPS DoH RFC 8484)
              [Cloudflare CDN ({relay-domain})]
                        ↓ (QUIC/HTTP3)
              [Relais VPS (stateless)]
                        ↓ (forward DoH → Cloudflare 1.1.1.1 / Quad9 9.9.9.9)
                        ↓ (réponse DNS)
              [Chemin inverse → Utilisateur]
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

**Flux IP camouflage (trafic web) :**
```
[Navigateur/App] → [HTTP CONNECT proxy 127.0.0.1:50113]
                        ↓ (requête CONNECT host:port)
              [internal/httpproxy/connect_handler.go]
                        ↓ (volume check — bypass si > 500 Mo)
              [Bypass ? → connexion directe]
              [Tunnel ? → internal/tunnel/client.go]
                        ↓ (POST /connect + session token Ed25519)
              [Cloudflare CDN ({relay-domain})]
                        ↓ (validation source CF IP)
              [Relais → connect_handler]
                        ↓ (vérif token + SSRF check + IP rate limit + bandwidth limit)
                        ↓ (résolution DNS → dial IP directe)
              [Serveur destination (ex: example.com:443)]
                        ↓ (relay bidirectionnel streaming)
              [Chemin inverse → Navigateur]
```

**Flux découverte (registre) :**
```
[Service démarrage] → [internal/registry/discoverer.go]
                        ↓ (GET /.well-known/relay-registry.json)
              [Cloudflare CDN → Relais quelconque]
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
[Tunnel actif sur relais is-01]
        ↓ (timeout / erreur / perte QUIC)
[internal/tunnel/reconnect.go → backoff exponentiel]
        ↓ (5 échecs DoH consécutifs → circuit breaker)
[internal/registry/failover.go → bascule relais suivant]
        ↓ (reconnexion transparente)
[Notification via IPC → UI mise à jour]
```

**Flux extension navigateur (routage intelligent) :**
```
[Navigateur avec extension Le Voile installée]
        ↓ (toute requête HTTP/HTTPS)
[background.js → health check proxy alive?]
        ↓ NO → route DIRECT (fail-safe)
        ↓ YES → check bypass set
              ↓ dans bypass → route DIRECT
              ↓ pas dans bypass → route via PROXY 127.0.0.1:50113
[onHeadersReceived → vérifie Content-Length]
        ↓ (si > 50 Mo)
[Ajoute hostname/URL au bypass set (TTL 120s)]
[Cancel + re-download direct]
```

**Flux anti-fuite WebRTC :**
```
[internal/leakcheck/scheduler.go → check périodique]
        ↓ (skip si kill switch actif ou tunnel déconnecté)
[internal/leakcheck/webrtc.go → STUN Binding Request]
        ↓ (3 serveurs : Google ×2, Cloudflare)
[XOR-MAPPED-ADDRESS → IP publique détectée]
        ↓
[Comparaison IP détectée vs IP tunnel attendue]
        ↓ (si différentes = fuite)
[internal/browser/ → politiques Chromium/Firefox]
        ↓ (désactivation WebRTC)
[Callback recovery quand fuite résolue]
```

## Architecture Validation Results

### Coherence Validation

**Decision Compatibility :** Aucun conflit détecté
- quic-go v0.59.0 + HTTP/3 + TLS 1.3 — nativement intégré
- webview/webview + fyne.io/systray — même processus, tray sur main thread, webview créé/détruit à la demande
- webview/webview + charte CSS plateformeliberte.fr — reproduction fidèle via HTML/CSS/JS natif
- Ed25519 Go standard + clé par relais + registre signé master key — sécurité renforcée
- kardianos/service + IPC named pipes — service SCM + communication inter-processus
- Proxy CONNECT + validation CF source + SSRF + bandwidth limiting — défense en profondeur
- Registre distribué signé + cache local — résilient, pas de SPOF
- Failover auto + latency measurement — compatible avec HTTP/3 + reconnexion auto
- Anti-fuite WebRTC (STUN + politiques navigateur) — couverture complète
- GoReleaser multi-cible + NSIS — distribution Windows complète
- TOML config + cache registre JSON — rôles distincts sans conflit
- Volume tracker proxy + extension bypass — routage intelligent multicouche

**Pattern Consistency :** Vérifié
- snake_case cohérent à travers TOML config, registre JSON et health endpoint
- Error wrapping fmt.Errorf("pkg: action: %w", err) — chaîne cohérente
- context.Context partout + reconnexion auto + watchdog + failover — lifecycle correctement câblé
- IPC — communication UI → service strictement via messages JSON, pas d'import direct

**Structure Alignment :** Vérifié
- Chaque frontière architecturale a un package internal/ dédié
- Registry (avec failover, latency, discoverer, cache, countries) isole la logique multi-relais
- Service centralise l'orchestration de tous les composants
- IPC handler centralise le dispatch des requêtes UI
- Frontend dans frontend/ — séparation nette Go/Web, servi par HTTP local embarqué
- Build tags couvrent les 3 OS dans dns/, config/, elevation/, browser/, ipc/, ui/
- Direction des dépendances empêche les imports circulaires

### Requirements Coverage Validation

**Functional Requirements : 40/40 couverts**

| FR | Composant architectural |
|---|---|
| FR1-4 (Tunnel) | internal/tunnel/, internal/crypto/ |
| FR5-8 (DNS + Blocklist) | internal/dns/, internal/blocklist/, internal/watchdog/ |
| FR9-13b (Interface Utilisateur) | frontend/, internal/ui/ |
| FR14-16 (Lifecycle) | internal/service/, internal/elevation/, cmd/client/ |
| FR17-19b (Relais Multi-VPS) | internal/relay/, cmd/relay/, deploy/ |
| FR20-22 (Distribution) | .goreleaser.yaml, installer/ |
| FR23-26 (Découverte & Sélection) | internal/registry/ |
| FR27-30 (IP Camouflage) | internal/httpproxy/, internal/relay/connect_handler.go, ip_limiter.go, bandwidth_limiter.go, cfip.go, verify_handler.go |
| FR31-34 (Anti-Fuite) | internal/leakcheck/, internal/stun/, internal/browser/ |
| FR35-36 (Mise à jour) | internal/updater/ |
| FR37-40 (Extension Navigateur) | extension/, internal/browser/ |

**Non-Functional Requirements : 20/20 couverts**
- Sécurité (NFR1-9) : TLS 1.3 via quic-go, Ed25519 par relais + registre signé master key, certificate pinning, zero persistence, HTTP/3 DoH camouflage, SSRF protection, validation source CF (CF-Connecting-IP), code auditable
- Performance (NFR10-14) : Go natif, proxy DNS local + CONNECT, failover transparent, bandwidth limiting
- Fiabilité (NFR15-18) : Kill switch via arrêt proxy DNS, watchdog 3s, failover multi-relais, crash-recovery DNS/proxy, circuit breaker (5 échecs → déconnexion)
- Confidentialité (NFR19-20) : Zero log IP client, IP hash uniquement dans session tokens, compteurs RAM volatile

### Implementation Readiness Validation

**Decision Completeness :** Décisions critiques documentées. Migration Wails v2 → webview/webview à implémenter
**Structure Completeness :** ~18 packages internal/, 3 entry points cmd/ (client, ui, relay) + genregistry, frontend HTML/CSS/JS, extension navigateur, installeur NSIS, déploiement systemd
**Pattern Completeness :** Naming, formats, erreurs, concurrence, IPC, sélection relais, failover, UI, session tokens, proxy CONNECT, volume tracker, extension health check — tous implémentés

### Gap Analysis Results

**Gaps critiques : 0**

**Gaps mineurs (acceptables pour le MVP) :**
- Config TOML par défaut (génération à la première exécution) → détail d'implémentation
- Registre dynamique (API de gestion pour ajouter/retirer des relais sans redéployer) → Phase 2
- CI/CD automatisé (GitHub Actions) → post-MVP

**Écart PRD ↔ Code :**
- Le PRD devra être mis à jour pour refléter la nouvelle architecture (suppression portable, webview/webview + systray au lieu de Wails v2)

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

**Statut global : ARCHITECTURE RÉVISÉE — MIGRATION UI EN COURS**

**Niveau de confiance : Élevé**

**Changements majeurs (2026-04-08) :**
- Suppression du mode portable — simplification du projet, focus Windows installé uniquement
- Remplacement Wails v2 par webview/webview + fyne.io/systray — résout le conflit tray+desktop en un seul processus
- Fusion cmd/tray/ + cmd/desktop/ → cmd/ui/ (binaire unique)
- Fusion internal/tray/ + internal/desktop/ → internal/ui/ (package unique avec serveur HTTP local)
- Ajout API REST locale remplaçant les bindings Wails v2

**Forces clés :**
- Architecture 2 processus simplifiée — service SCM background + UI unique (tray + webview), communication IPC propre
- Architecture multi-relais résiliente — registre signé Ed25519, failover automatique, latency measurement, cache local
- Zero-log par design — aucune donnée à compromettre, architecture stateless côté relais
- Camouflage protocolaire — HTTP/3 DoH via Cloudflare indiscernable du trafic web
- UI riche via webview/webview — charte plateformeliberte.fr fidèlement reproduite, frontend HTML/CSS/JS existant réutilisé
- Approche UI éprouvée — même pattern que Lantern VPN (getlantern/systray + webview, millions d'utilisateurs)
- Routage intelligent multicouche — extension navigateur (bypass > 50 Mo) + volume tracker proxy (bypass > 500 Mo) + cohabitation SysProxy
- Anti-fuite complète — STUN detection + browser policies + kill switch DNS
- Installeur NSIS simplifié — 2 binaires au lieu de 3 (service + UI)

**Améliorations futures (post-MVP) :**
- Registre dynamique avec API de gestion (Phase 2)
- CI/CD GitHub Actions
- Alignement PRD avec la nouvelle architecture
