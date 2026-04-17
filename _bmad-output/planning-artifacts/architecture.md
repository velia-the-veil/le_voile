---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
lastStep: 8
status: 'revised'
completedAt: '2026-03-08'
rewrittenAt: '2026-04-15'
rewrittenReason: 'Ajout support Linux + bascule capture L3 unifiée (TUN Linux / Wintun Windows) + suppression extension navigateur + kill switch firewall (nftables / WFP). Révision 2026-04-08 conservée: webview/webview + fyne.io/systray.'
previousRewrittenAt: '2026-04-08'
previousRewrittenReason: 'Suppression mode portable, remplacement Wails v2 par webview/webview + fyne.io/systray (binaire UI unique), architecture 2 processus'
inputDocuments: ['prd.md', 'prd-validation-report.md', 'codebase analysis 2026-04-02', 'architecture.md (2026-04-08 revision)']
workflowType: 'architecture'
project_name: 'bmad_vpn_le_voile_de_velia'
user_name: 'Akerimus'
date: '2026-04-15'
snapshot_ref: 'windows-stable-2026-04-15 (git tag) + backup/windows-stable (branch)'
---

# Architecture Decision Document

_Ce document reflète l'architecture cible au 15 avril 2026 — après ajout du support Linux (Debian/Ubuntu, Fedora/RHEL, Arch, Alpine) et bascule unifiée vers capture L3 (TUN sur Linux, Wintun sur Windows) en remplacement du modèle proxy local (HTTP CONNECT + DNS UDP 127.0.0.1:53). L'extension navigateur est supprimée (devenue redondante avec la capture L3 machine-wide)._

_Snapshot de l'état stable Windows-only précédent : git tag `windows-stable-2026-04-15`, branche `backup/windows-stable`, binaires archivés dans `_snapshots/bmad_vpn_le_voile_windows-stable-2026-04-15/`._

## Project Context Analysis

### Requirements Overview

**Functional Requirements:**
36 FRs organisés en 10 domaines (FR37-40 extension navigateur retirés — capture L3 machine-wide rend l'extension redondante) :
- **Tunnel & Connexion (FR1-4)** — Établissement QUIC/HTTPS via Cloudflare, reconnexion auto avec backoff exponentiel, authentification Ed25519 par relais, session tokens signés (TTL 4h) avec circuit breaker
- **Capture Trafic L3 (FR5-8 révisés + FR27-30 révisés)** — Interface TUN/Wintun virtuelle, encapsulation IP brut tunnelisée via QUIC/HTTP3 vers relais (relais = gateway NAT), DNS résolu côté relais (plus de proxy DNS local), kill switch firewall (nftables Linux / WFP Windows) drop tout sauf TUN + IP relais, rate limiting par IP côté relais (200 max), bandwidth limiting par IP (quota journalier), protection SSRF, blocklist DNS appliquée côté relais (StevenBlack/hosts)
- **Protection Anti-Fuite (FR31-34)** — Détection fuites WebRTC via STUN Binding Requests (RFC 5389) — WebRTC ne peut plus fuir puisque tout le trafic UDP/STUN passe aussi par la TUN, mais check de validation conservé. Vérification IP tunnel vs IP détectée, scheduler périodique
- **Interface Utilisateur (FR9-13b)** — Binaire UI unique (`levoile-ui`) combinant webview/webview (fenêtre 420×540px) + fyne.io/systray (icône tray) dans un seul processus, charte plateformeliberte.fr, sélecteur de pays avec drapeaux
- **Démarrage & Lifecycle (FR14-16)** — Service Windows (kardianos/service SCM) + Service Linux (kardianos/service systemd). UI tray persiste en arrière-plan, fenêtre webview ouverte/fermée à la demande
- **Relais Multi-VPS (FR17-19b)** — Relais HTTP/3 stateless avec endpoint de tunneling IP (ingress/egress), bandwidth limiting, organisés par pays, registre distribué signé Ed25519
- **Découverte & Sélection (FR23-26)** — Registre distribué signé (/.well-known/relay-registry.json), sélection par pays, failover automatique, latency measurement via /health
- **Distribution (FR20-22)** — Installeur NSIS (Windows), paquets natifs Linux via GoReleaser + nfpm : .deb (Debian/Ubuntu), .rpm (Fedora/RHEL), AUR (Arch), .apk (Alpine)
- **Mise à jour (FR35-36)** — Vérification périodique GitHub releases, téléchargement rate-limited, vérification signature Ed25519, rollback
- **~~Extension Navigateur (FR37-40)~~** — **SUPPRIMÉ** — Rendu redondant par la capture L3. La TUN/Wintun capture tout le trafic IP du système (navigateurs inclus) sans configuration. Le bypass > 50 Mo est abandonné (impossible à faire en L3 sans inspecter les paquets)

**Non-Functional Requirements:**
20 NFRs répartis en 4 axes :
- **Sécurité (NFR1-9)** — TLS 1.3 min, Ed25519 par relais + registre signé master key, zero persistence, résistance DPI (QUIC/HTTPS via Cloudflare), zero fuite DNS/WebRTC (capture L3 machine-wide), kill switch firewall OS-level (nftables/WFP), protection SSRF, validation source Cloudflare (CF-Connecting-IP), code auditable
- **Performance (NFR10-14)** — Latence DNS < 50ms (résolution côté relais via upstream proche), tunnel < 3s, reconnexion avec backoff (100ms → 30s), RAM < 25MB (hausse acceptable vs 20MB — stack TCP/IP userspace ou équivalent), CPU < 1%
- **Fiabilité (NFR15-18)** — Kill switch via règles firewall (indépendant du process client, persiste en cas de crash), watchdog TUN interface 3s, crash-recovery firewall/TUN au redémarrage service, failover transparent multi-relais
- **Confidentialité (NFR19-20)** — Aucun log IP client (ni relais ni client), IP hash uniquement dans session tokens

**Scale & Complexity:**
- Domaine principal : Application desktop + serveur réseau (cybersécurité/vie privée)
- Complexité : Élevée — protocoles réseau spécialisés, cryptographie, intégration OS profonde (TUN/Wintun + firewall nftables/WFP), multi-relais, IPC multi-processus, encapsulation IP
- Composants architecturaux : ~18 packages internal/ — ajout `internal/tun/`, `internal/firewall/`, `internal/ipstack/` ; suppression `internal/httpproxy/`, `internal/browser/`, simplification `internal/dns/` (plus de proxy UDP local)
- Plateformes cibles : Windows 10/11, Debian 11+/Ubuntu 22.04+, Fedora 38+/RHEL 9+, Arch Linux rolling, Alpine 3.18+

### Technical Constraints & Dependencies

- **Go 1.26** — Langage imposé (binaire unique cross-platform, bibliothèques crypto standard)
- **webview/webview** — Fenêtre desktop native utilisant WebView2 (Windows) / WebKitGTK 6.0 (Linux) / WebKit (macOS différé). Plus léger que Wails v2, pas de contrainte sur le cycle de vie applicatif
- **fyne.io/systray** (v1.12.0) — Icône system tray. Windows : pur Go. Linux : dépend de `libayatana-appindicator3` (présent sur GNOME/KDE/XFCE via paquets standards)
- **kardianos/service** (v1.2.4) — Gestion service cross-platform : Windows SCM + systemd Linux (install/uninstall/start/stop)
- **Architecture 2 processus** — Service (background, privilégié) + UI (tray + webview, user) communiquant via IPC (named pipes Windows / Unix sockets Linux)
- **Microsoft/go-winio** (v0.6.2) — Named pipes Windows pour IPC (Windows only)
- **quic-go** (v0.59.0) — Implémentation QUIC production-ready, HTTP/3 + TLS 1.3
- **BurntSushi/toml** (v1.5.0) — Configuration TOML
- **Cloudflare** — CDN intermédiaire pour le camouflage protocolaire (QUIC/HTTPS)
- **Ed25519** — Algorithme d'authentification par relais + signature du registre par master key
- **Multi-VPS** — Relais répartis géographiquement par pays, scalables horizontalement
- **Capture L3 unifiée** — Contrainte nouvelle : le client doit créer une interface virtuelle et router le trafic IP via elle, au lieu d'intercepter au niveau application (proxy L7)
- **Bibliothèque TUN** — `golang.zx2c4.com/wireguard/tun` (utilisée par WireGuard, cross-platform : TUN Linux via `/dev/net/tun`, Wintun Windows via DLL signée Microsoft). Alternative considérée : `songgao/water` (plus simple mais moins mature pour Wintun)
- **Stack TCP/IP userspace** — `gvisor.dev/gvisor/pkg/tcpip` (netstack) pour terminaison des paquets IP côté client si nécessaire, ou forwarding direct si le relais agit comme gateway NAT (choix préféré — simplicité)
- **Firewall Linux** — `nftables` via shellout binaire `nft` (détection au démarrage : échec hard si absent, message clair "nftables required, install libnftables"). Pas de fallback iptables (dette technique refusée)
- **Firewall Windows** — Windows Filtering Platform (WFP) via `golang.zx2c4.com/wireguard/windows/conf/winipcfg` ou API native `Fwpm*` (wrapper Go : `fyne-io/winfw` ou implémentation custom)
- **Wintun** — DLL signée Microsoft (distribuée avec WireGuard), embarquée dans le binaire service Windows via `//go:embed` et extraite au premier démarrage
- **Capabilities Linux** — Binaire service requiert `CAP_NET_ADMIN` (création TUN) + `CAP_NET_RAW` (firewall). **Capabilities fournies via systemd `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW`** dans le unit file `levoile.service`, avec `User=levoile`. Pas de setcap sur le binaire (évite la problématique de perte des capabilities à chaque update). Les capabilities sont rattachées au processus au démarrage par systemd
- **Privilèges Windows** — Service SCM tourne en LocalSystem (tous privilèges réseau natifs)
- **GoReleaser v2 + nfpm** — Build cross-compilation + packaging .deb/.rpm/.apk unifié. AUR via repo séparé (PKGBUILD manuel ou `aur-publish` action)
- **NSIS** — Installeur Windows (installation service, tray autostart, shortcuts, Wintun DLL)
- **Développeur unique + IA** — Contrainte ressource forte, architecture doit rester simple — décision: **modèle gateway NAT côté relais** (pas de netstack userspace client) pour limiter la complexité

### Cross-Cutting Concerns Identified

- **Sécurité** — Chiffrement bout en bout, zero-log, résistance DPI, clé Ed25519 par relais, registre signé master key, session tokens signés, validation source CF, protection SSRF — touche tunnel, relay, registry, tun
- **Anti-Fuite** — Détection WebRTC via STUN (validation — la capture L3 empêche structurellement les fuites), kill switch firewall OS-level — touche leakcheck, stun, firewall
- **Capture trafic machine-wide** — TUN/Wintun capture tout IP (TCP/UDP/ICMP), encapsulation vers relais, pas de config par application — touche tun, firewall, routing
- **Fiabilité** — Kill switch firewall persistant (nftables/WFP), watchdog interface TUN, reconnexion auto avec backoff, failover multi-relais, règles firewall restaurées proprement au shutdown — touche service, firewall, tun, tunnel, registry
- **Intégration OS** — Interface TUN (build tags : Linux /dev/net/tun, Windows Wintun), firewall (nftables Linux, WFP Windows), routes (iproute2 Linux, winipcfg Windows), élévation (root/CAP Linux, UAC Windows), service (systemd/SCM), tray (dbus/appindicator Linux, systray natif Windows) — varie par plateforme
- **Camouflage** — Trafic IP brut encapsulé dans HTTP/3 via Cloudflare, indiscernable d'une connexion HTTPS standard — touche tunnel, relay, Cloudflare
- **Statelessness** — Aucune persistence côté relais (NAT table en RAM, TTL court) — touche architecture relais, registre, monitoring
- **Découverte** — Registre distribué signé, sélection géographique, failover, latency measurement — touche client, relais, déploiement
- **Mise à jour** — Auto-update via GitHub releases, vérification signature Ed25519, rollback, rate limiting — touche updater, distribution
- **IPC** — Communication inter-processus service ↔ UI via named pipes (Windows) ou Unix sockets (Linux) — touche ipc, ipchandler, service, ui
- **Packaging multi-distro** — .deb/.rpm/.apk/PKGBUILD, dépendances runtime (libayatana-appindicator3, libwebkit2gtk-6.0-0, nftables), scripts post-install (setcap, systemd enable) — touche goreleaser, nfpm, deploy

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
| **GoReleaser v2** | **Sélectionné** — cross-compilation, orchestration multi-cible |
| **nfpm** (via GoReleaser) | **Sélectionné** — génération unifiée .deb/.rpm/.apk |
| **AUR (PKGBUILD manuel + GitHub Action `aur-publish`)** | **Sélectionné** — publication semi-automatique Arch User Repository |
| **NSIS** | **Sélectionné** — installeur Windows (service + tray + Wintun DLL + shortcuts) |

### Selected Stack: Go + quic-go + webview/webview + fyne.io/systray + kardianos/service + wireguard/tun + nftables/WFP + GoReleaser + nfpm + NSIS

**Rationale for Selection:**
- `quic-go` — Seule implémentation QUIC pure Go production-ready. TLS 1.3 intégré, HTTP/3 client+serveur
- `webview/webview` — Fenêtre desktop native via WebView2 (Windows) / WebKitGTK 6.0 (Linux). Frontend HTML/CSS/JS réutilisable sur les deux plateformes
- `fyne.io/systray` — Systray pur Go Windows. Linux via libayatana-appindicator3 (CGo, accepté car dépendance runtime standard sur toutes distros cibles)
- `kardianos/service` — Service background cross-platform : Windows SCM + systemd Linux avec install/uninstall/start/stop
- `wireguard/tun` — Interface TUN (Linux) + Wintun (Windows) avec API Go unifiée. Utilisée en production par WireGuard (millions d'installations)
- `nftables` — Standard Linux moderne pour kill switch. Pas de fallback iptables — simplifie la maintenance
- `WFP` (Windows Filtering Platform) — Filtrage kernel Windows pour kill switch, survit aux crashes du service
- `NSIS` — Installeur Windows complet (service, UI autostart, shortcuts, Wintun DLL, uninstall propre)
- `GoReleaser + nfpm` — Build cross-compilation + packaging Linux unifié (.deb, .rpm, .apk) depuis une seule config

**Architectural Decisions Provided by Stack:**

**Language & Runtime:**
Go 1.26 + webview/webview (WebView2 Windows / WebKitGTK Linux) + fyne.io/systray + wireguard/tun — architecture 2 processus avec IPC

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
GoReleaser v2 + nfpm — cibles de build multiplateformes :
- Windows : service (.exe), UI (.exe), installeur NSIS (.exe), Wintun DLL embarquée
- Linux : service (ELF) + UI (ELF) + paquets .deb, .rpm, .apk (via nfpm) — amd64 + arm64
- Linux (Arch) : PKGBUILD séparé publié sur AUR via GitHub Action
- Relais : ELF Linux (amd64 + arm64)
NSIS — installeur Windows (service, UI autostart, shortcuts, Wintun DLL)

**Testing Framework:**
Go standard `testing` — pas de framework tiers. Tests unitaires, edge cases, e2e. 90+ fichiers de test

**Code Organization:**

```
le_voile/
  go.mod / go.sum
  .goreleaser.yaml                 # cibles Win + Linux (.deb/.rpm/.apk) + relay
  config.example.toml
  cmd/
    client/main.go                 # Service cross-platform (kardianos/service: SCM Windows + systemd Linux)
    ui/main.go                     # UI : fyne.io/systray + webview/webview
    relay/main.go                  # Relais VPS HTTP/3 avec tunnel IP gateway
    genregistry/main.go            # Outil génération registre
  internal/
    ~18 packages (détaillés ci-dessous) — ajout tun/, firewall/ ; retrait httpproxy/, browser/
  frontend/                        # Assets HTML/CSS/JS (embarqués via go:embed, servis par HTTP local)
  installer/                       # NSIS script + assets (Windows)
  packaging/                       # nfpm configs, PKGBUILD (Arch), scripts post-install Linux
  deploy/                          # systemd units + install.sh pour relais Linux VPS
  assets/
    icons/                         # Icônes tray (ico Windows, png Linux)
    wintun/                        # Wintun DLL (signée Microsoft, embarquée)
  tools/
    # (crxgen supprimé — plus d'extension)
```

**Development Experience:**
- `go run ./cmd/ui` pour le développement UI (tray + webview)
- `sudo go run ./cmd/client run` (Linux — CAP_NET_ADMIN requis) ou `go run ./cmd/client run` en admin CMD (Windows)
- `go run ./cmd/relay` pour le développement relais
- `go test ./...` pour les tests
- `go build ./cmd/ui` pour le build UI
- `goreleaser release --clean` pour builds multi-OS + paquets .deb/.rpm/.apk
- `goreleaser release --snapshot --skip=publish` pour un build local sans release

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
- CI/CD automatisé — GitHub Actions (requis pour publication AUR propre)
- Registre dynamique (API de gestion) — Phase 2, remplacer le JSON statique
- Support macOS — utan (utun interface) via wireguard/tun supporte déjà macOS, différé à la demande
- Split tunneling par domaine — hors scope (complexifie le client sans bénéfice clair vu l'abandon du bypass > 50 Mo)

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

- **Client ↔ Relais (Tunnel IP)** : HTTP/3 via quic-go/http3. POST `https://{relay-domain}/tunnel` avec Upgrade pour stream bidirectionnel persistant transportant les paquets IP bruts. Framing : `[2 octets big-endian: length][payload IP packet]`. Header `Authorization: Bearer {session_token}` à l'ouverture du stream. Limite pratique MTU : 1420 octets (QUIC overhead pris en compte). Trafic indiscernable d'une connexion HTTPS standard via Cloudflare
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

### Architecture 2 Processus & Intégration OS

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
- **CLI Control Tool** (`cmd/ctl/main.go` — binaire `levoile-ctl`) : Outil CLI privilégié pour opérations avancées. Communique avec le service via IPC (même socket que l'UI). Authentification par token machine-local. Commandes : `killswitch off`, `killswitch on`, `status`, `reconnect`, `logs`. Installé dans `/usr/bin/levoile-ctl` (Linux) / `Program Files/LeVoile/levoile-ctl.exe` (Windows)

### Infrastructure & Déploiement

- **Relais VPS** (`cmd/relay/`) : Binaire Go autonome HTTP/3 (port 443) avec TLS. Handlers : **Tunnel IP** (/tunnel — stream bidirectionnel paquets IP, NAT, DNS blocklist intégrée), Verify (/verify — session token), IP detection (/ip), health (/health), registry (/.well-known/relay-registry.json). **Supprimé** : /dns-query, /connect, /stun-relay (tous absorbés par /tunnel). Build tag pour traçabilité. Flags : -addr, -cert, -key, -upstream, -fallback, -signing-key, -registry-file, -cf-insecure, -blocklist-url
- **NAT côté relais** : Table NAT en RAM (`sync.Map` keyed par `(session, 5-tuple)`). Alloc port NAT via pool range 10000-60000 avec eviction TTL. Socket userspace ou raw selon protocole (TCP : dial/accept normal, UDP : `net.ListenUDP`, ICMP : raw socket ou abandon au MVP)
- **Relay DNSSEC Validator** (`internal/relay/dns_resolver.go`) : Les réponses DNS upstream sont validées DNSSEC avant forwarding. Cloudflare 1.1.1.1 et Quad9 9.9.9.9 supportent nativement DNSSEC. Réponse SERVFAIL si validation échoue (NFR9f)
- **Relay Session Validator** (`internal/relay/verify_handler.go`) : À chaque ouverture de stream /tunnel, vérification que SHA256(CF-Connecting-IP) == IPHash du session token. Rejet HTTP 401 si différent (NFR9d)
- **Relay TLS Config** : Serveur TLS obligatoirement TLS 1.3, ciphersuites modernes uniquement. Configuration Cloudflare côté Origin Certificate en mode "Full (Strict)" (NFR9e)
- **TUN Packet Integrity** (`internal/tun/integrity.go`) : Tagging des paquets émis par le pump tunnel (metadata in-memory + checksum), détection de paquets arrivant sur la TUN sans avoir transité par le pump = injection externe. Paquets non tagués ignorés + log (NFR9g)
- **Registre signé** : Inchangé
- **Déploiement relais** : `deploy/install.sh` — Crée user system `levoile`, copie binaire /opt/levoile, cert/key avec permissions 0600, systemd service avec CAP_NET_BIND_SERVICE + CAP_NET_ADMIN (pour NAT si raw sockets), ProtectSystem=strict, ProtectHome=true, NoNewPrivileges=true, restart always
- **Cloudflare** : Inchangé (CF-Connecting-IP continue d'identifier le client réel pour les limiteurs)
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

20 zones où des agents IA pourraient diverger, toutes résolues ci-dessous.

### Naming Patterns

**Packages Go :**
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

- Wrapping systématique : `fmt.Errorf("tunnel: connect: %w", err)`
- Préfixe = nom du package : `tunnel:`, `dns:`, `service:`, `ipc:`
- Erreurs sentinelles pour les cas récupérables : `var ErrInvalidKey = errors.New(...)`, `var ErrPinningFailed = errors.New(...)`
- Jamais de `panic` sauf bug critique — toujours retourner `error`
- **Logging relais** : `fmt.Fprintf(os.Stderr, ...)` — messages opérationnels uniquement, jamais de données utilisateur
- **Logging client** : Pas de framework log. Erreurs propagées vers le service → IPC → frontend pour affichage
- **Messages utilisateur en français** : Statuts affichés au frontend en français ("Connecté", "Reconnexion en cours...", "Déconnecté")

### Relay Selection & Failover Patterns (inchangés)

- **Découverte** (`internal/registry/discoverer.go`) : Orchestration fetch → verify (Ed25519 master key) → cache → default relay. Refresh background toutes les 6h
- **Sélection par pays** : L'utilisateur choisit un pays dans l'UI → IPC SelectCountry → service → discoverer filtre les relais actifs pour ce pays
- **Failover automatique** (`internal/registry/failover.go`) : En cas d'échec connexion, bascule vers le relais suivant dans le pool. Retry logic. Pendant le failover, le kill switch firewall reste actif — aucune fuite possible entre deux connexions
- **Latency measurement** (`internal/registry/latency.go`) : Mesure RTT via GET /health sur chaque relais. Tri par latence. Cache + mesures fraîches
- **Country metadata** (`internal/registry/countries.go`) : Map pays → nom affiché + drapeau emoji. Extraction code pays depuis relay ID ou domain
- **Cache local** (`internal/registry/cache.go`) : Persistence fichier JSON des relais vérifiés. Source de vérité quand aucun relais ne répond
- **Bootstrap** : Premier lancement → relay domain par défaut dans config (relay.levoile.dev)

### TUN / Firewall / Routing Patterns (nouveaux)

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

### UI Patterns (webview/webview + fyne.io/systray — processus unique)

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

### ~~Extension Navigateur Patterns~~ — **SUPPRIMÉ**

Retrait complet — la capture L3 (TUN/Wintun) capture tout le trafic navigateur sans configuration. Bypass > 50 Mo abandonné.

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

- Icônes tray dans `internal/ui/icons/` (embedded via `//go:embed icons`) — `.ico` pour Windows, `.png` pour Linux (connected/connecting/disconnected + levoile)
- **Wintun DLL** dans `internal/tun/wintun/` (embedded via `//go:embed wintun/wintun.dll` — uniquement inclus sur build Windows via build tag). Extraite au premier démarrage dans `%ProgramData%/LeVoile/wintun.dll`
- Fonts frontend dans `frontend/assets/fonts/` — BebasNeue, Rajdhani, Inter (woff2)
- Frontend assets dans `frontend/` (embedded via `//go:embed` dans `internal/ui/embed.go`, servis par le serveur HTTP local)
- Icône application Linux dans `assets/icons/hicolor/` (256×256, 128×128, 64×64, 48×48, 32×32, 16×16) pour le standard freedesktop
- **Supprimé** : extension CRX/XPI

### Enforcement Guidelines

**Tous les agents IA DOIVENT :**
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

**Anti-Patterns à éviter :**
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
│       ├── cfip.go                    # Cloudflare IP validator
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
├── .github/
│   └── workflows/
│       ├── release.yml                # Build multi-OS + signatures + GitHub release
│       └── aur-publish.yml            # Publication AUR automatique
│
└── docs/
    └── validation-e2e.md              # Guide validation bout en bout (Windows + Linux)
```

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
- Tout passe via Cloudflare (`https://{relay-domain}`) — jamais d'accès direct au VPS
- Le registry discoverer est le seul composant qui détermine quel relais contacter
- Le handler `/tunnel` valide : session token Ed25519, source Cloudflare IP (CF-Connecting-IP), rate limit par IP, bandwidth limit par IP. Pour chaque paquet IP forwardé : SSRF check (bloque réseaux privés destination)
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

**Frontière OS (TUN / Firewall / Routing) :**
- 3 interfaces Go : `tun.Device`, `firewall.Firewall`, `routing.RouteManager` — contrats uniques
- Implémentations OS isolées derrière build tags — jamais d'imports croisés
- Chaque implémentation OS est autonome et testable indépendamment
- Le service orchestre les 3 dans un ordre strict (tun → routing → firewall)

**Frontière Relais (Stateless vis-à-vis des clients) :**
- Aucun état persisté entre les requêtes (pas de disque)
- État en RAM uniquement : NAT table (5-tuple → port NAT + TTL), compteurs tunnels/bandwidth, DNS cache court — tout volatile
- Le relais ne connaît ni les identités clients ni leurs requêtes passées
- Le fichier registry.json est statique, déployé avec le binaire

### Requirements to Structure Mapping

**FR1-4 (Tunnel & Connexion) →** `internal/tunnel/`, `internal/crypto/`
**FR5-8 (Capture Trafic L3 + DNS + Kill Switch)** — ex-"Protection DNS" → `internal/tun/`, `internal/tunnel/pump.go`, `internal/firewall/`, `internal/routing/`, `internal/relay/dns_resolver.go`, `internal/relay/blocklist.go`
**FR9-13b (Interface Utilisateur) →** `frontend/`, `internal/ui/`
**FR14-16 (Démarrage & Lifecycle) →** `internal/service/`, `internal/elevation/`, `cmd/client/`
**FR17-19b (Relais Multi-VPS) →** `internal/relay/`, `cmd/relay/`, `deploy/`
**FR20-22 (Distribution) →** `.goreleaser.yaml`, `installer/` (Windows NSIS), `packaging/` (Linux .deb/.rpm/.apk/AUR)
**FR23-26 (Découverte & Sélection) →** `internal/registry/`
**FR27-30 (IP Camouflage)** — ex-"Proxy CONNECT" → `internal/tun/`, `internal/tunnel/`, `internal/relay/tunnel_handler.go`, `internal/relay/nat_table.go`, `internal/relay/ip_limiter.go`, `internal/relay/bandwidth_limiter.go`, `internal/relay/cfip.go`, `internal/relay/verify_handler.go`
**FR31-34 (Anti-Fuite) →** `internal/leakcheck/`, `internal/stun/` (parsing seulement). Couverture structurelle via `internal/tun/` + `internal/firewall/` (pas de fuite possible par design)
**FR35-36 (Mise à jour) →** `internal/updater/`
**~~FR37-40 (Extension Navigateur) →~~** **SUPPRIMÉ**

**Cross-Cutting :**
- Kill switch (FR8) → `internal/firewall/` (nftables Linux / WFP Windows)
- Reconnexion auto (FR2) → `internal/tunnel/reconnect.go` — kill switch firewall reste actif pendant les reconnexions
- Failover multi-relais → `internal/registry/failover.go` + `internal/tunnel/` (kill switch maintenu entre relais)
- Session tokens → `internal/tunnel/client.go` + `internal/relay/verify_handler.go`
- Anti-fuite WebRTC → `internal/leakcheck/` (validation) + `internal/tun/` + `internal/firewall/` (prévention structurelle)
- Démarrage (FR14) → `internal/service/` + `internal/elevation/` (check capabilities Linux / UAC Windows)
- Découverte registre → `internal/registry/` + `internal/relay/` (sert le JSON)
- IPC → `internal/ipc/` + `internal/ipchandler/`

### Data Flow

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
              [Cloudflare CDN ({relay-domain})]
                        ↓ (HTTP/3)
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
              [HTTP/3 stream → Cloudflare → client]
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

## Architecture Validation Results

### Coherence Validation

**Decision Compatibility :** Aucun conflit détecté
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

**Pattern Consistency :** Vérifié
- snake_case cohérent à travers TOML config, registre JSON et health endpoint
- Error wrapping fmt.Errorf("pkg: action: %w", err) — chaîne cohérente
- context.Context partout + reconnexion auto + failover — lifecycle correctement câblé
- IPC — communication UI → service strictement via messages JSON, pas d'import direct
- Ordre strict d'orchestration (elevation → tun → routing → firewall → tunnel) imposé par service + documenté dans patterns

**Structure Alignment :** Vérifié
- Chaque frontière architecturale a un package internal/ dédié
- Registry (avec failover, latency, discoverer, cache, countries) isole la logique multi-relais
- Service centralise l'orchestration de tous les composants
- IPC handler centralise le dispatch des requêtes UI
- Frontend dans frontend/ — séparation nette Go/Web, servi par HTTP local embarqué
- Build tags couvrent Linux + Windows dans tun/, firewall/, routing/, config/, elevation/, ipc/, ui/
- Direction des dépendances empêche les imports circulaires
- Nouveaux packages (tun/firewall/routing) orthogonaux — pas de dépendances croisées entre eux

### Requirements Coverage Validation

**Functional Requirements : 36/36 couverts** (FR37-40 extension retirés)

| FR | Composant architectural |
|---|---|
| FR1-4 (Tunnel) | internal/tunnel/, internal/crypto/ |
| FR5-8 (Capture L3 + DNS + Kill Switch) | internal/tun/, internal/tunnel/pump.go, internal/firewall/, internal/routing/, internal/relay/dns_resolver.go, internal/relay/blocklist.go |
| FR9-13b (Interface Utilisateur) | frontend/, internal/ui/ |
| FR14-16 (Lifecycle) | internal/service/, internal/elevation/, cmd/client/ |
| FR17-19b (Relais Multi-VPS) | internal/relay/, cmd/relay/, deploy/ |
| FR20-22 (Distribution) | .goreleaser.yaml, installer/ (Windows NSIS), packaging/ (Linux nfpm + AUR) |
| FR23-26 (Découverte & Sélection) | internal/registry/ |
| FR27-30 (IP Camouflage) | internal/tun/, internal/tunnel/, internal/relay/tunnel_handler.go, nat_table.go, ip_limiter.go, bandwidth_limiter.go, cfip.go, verify_handler.go |
| FR31-34 (Anti-Fuite) | internal/leakcheck/, internal/stun/ (parsing). Prévention structurelle via tun/ + firewall/ |
| FR35-36 (Mise à jour) | internal/updater/ |
| ~~FR37-40~~ | **SUPPRIMÉ** (capture L3 rend l'extension redondante) |

**Non-Functional Requirements : 20/20 couverts**
- Sécurité (NFR1-9) : TLS 1.3 via quic-go, Ed25519 par relais + registre signé master key, certificate pinning, zero persistence, HTTP/3 camouflage, SSRF protection côté relais, validation source CF (CF-Connecting-IP), code auditable, kill switch firewall kernel-level
- Performance (NFR10-14) : Go natif, capture L3 zéro-copy (lecture TUN vers stream QUIC), failover transparent, bandwidth limiting, RAM < 25MB (hausse acceptable vs 20MB — inclut buffers TUN + NAT table)
- Fiabilité (NFR15-18) : Kill switch firewall persistant (survit au crash service), watchdog TUN 3s, failover multi-relais (firewall maintenu), crash-recovery firewall au restart, circuit breaker (5 échecs → déconnexion + firewall actif)
- Confidentialité (NFR19-20) : Zero log IP client (relais ne loggue que métriques anonymes agrégées), IP hash uniquement dans session tokens, NAT table en RAM volatile avec TTL court

### Implementation Readiness Validation

**Decision Completeness :** Décisions critiques documentées. Migration proxy → TUN/Wintun à implémenter sur la base du snapshot Windows stable (`windows-stable-2026-04-15`)
**Structure Completeness :** ~18 packages internal/ (ajout tun/firewall/routing, retrait httpproxy/blocklist/browser/watchdog), 3 entry points cmd/ (client, ui, relay) + genregistry, frontend HTML/CSS/JS, installeur NSIS, packaging Linux (nfpm + AUR), déploiement systemd relais
**Pattern Completeness :** Naming, formats, erreurs, concurrence, IPC, sélection relais, failover, UI, session tokens, tunnel IP encapsulation, firewall rulesets, ordre strict Connect/Disconnect — tous spécifiés

### Gap Analysis Results

**Gaps critiques : 0**

**Gaps mineurs (acceptables pour le MVP Linux) :**
- Config TOML par défaut (génération à la première exécution) → détail d'implémentation
- Registre dynamique (API de gestion) → Phase 2
- Support ICMP dans le handler /tunnel du relais — peut être abandonné au MVP si trop complexe (ping ne fonctionnerait pas, mais pas bloquant)
- Support IPv6 complet — à valider lors de l'implémentation (wireguard/tun + nftables le supportent nativement)
- Gestion distros Linux sans libayatana-appindicator3 (ex: GNOME pur vanilla) — mitigation : fallback CLI-only ou dépendance stricte

**Écart PRD ↔ Code :**
- Le PRD devra être mis à jour pour refléter :
  - Suppression FR37-40 (extension)
  - Reformulation FR5-8 (proxy DNS local → capture L3 machine-wide + DNS côté relais)
  - Reformulation FR27-30 (proxy CONNECT → tunnel IP)
  - Ajout support Linux (Debian/Ubuntu, Fedora/RHEL, Arch, Alpine)
  - Exigences de plateforme et dépendances runtime par distro

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

**Statut global : ARCHITECTURE RÉVISÉE — IMPLÉMENTATION LINUX + MIGRATION L3 À PLANIFIER**

**Niveau de confiance : Élevé**

**Changements majeurs (2026-04-15) :**
- **Ajout support Linux** — Debian/Ubuntu (.deb), Fedora/RHEL (.rpm), Arch (AUR), Alpine (.apk) via GoReleaser + nfpm
- **Bascule capture L3 unifiée** : TUN (Linux `/dev/net/tun`) + Wintun (Windows DLL signée) via `wireguard/tun`. Capture machine-wide sans configuration applicative
- **Modèle gateway NAT côté relais** : le relais désencapsule les paquets IP, applique NAT, forwarde sur Internet. Client = pas de stack TCP/IP userspace (simplicité)
- **Kill switch firewall OS-level** : nftables (Linux) + Windows Filtering Platform (Windows). Persiste aux crashes du service
- **Suppression extension navigateur** (FR37-40) — redondante avec la capture L3 machine-wide
- **Suppression proxy local HTTP CONNECT et proxy DNS UDP local** (internal/httpproxy/, internal/dns/proxy.go, internal/dns/kill_switch.go)
- **Suppression composants associés** : internal/blocklist/ (client), internal/browser/, internal/watchdog/, internal/stun/interceptor+relayer, tools/crxgen/, extension/
- **Nouveaux packages** : internal/tun/, internal/firewall/, internal/routing/
- **Refactor relais** : handler unifié `/tunnel` (stream IP bidirectionnel + NAT + DNS interne + blocklist). Retrait `/dns-query`, `/connect`, `/stun-relay`
- **Abandon du bypass > 50 Mo** : impossible en L3 sans inspection deep packet. Tout passe par le tunnel
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
- **Capture L3 machine-wide** — aucune configuration applicative requise, tout le trafic IP capté. Navigateurs, mail, jeux, clients BitTorrent : tout bénéficie du tunnel
- **Kill switch robuste** — règles firewall kernel-level, indépendantes du process client, survivent au crash. Impossible à contourner accidentellement
- **Anti-fuite structurelle** — DNS, WebRTC, IPv6 ne peuvent pas fuir : tout passe par la TUN ou est droppé par le firewall
- Architecture 2 processus simplifiée — service privilégié (SCM/systemd) + UI user, communication IPC propre
- Architecture multi-relais résiliente — registre signé Ed25519, failover automatique, kill switch maintenu pendant le failover
- Zero-log par design — aucune donnée à compromettre, architecture stateless côté relais (NAT table RAM avec TTL court)
- Camouflage protocolaire — paquets IP encapsulés dans HTTP/3 via Cloudflare, indiscernables du trafic web
- UI riche via webview/webview — charte plateformeliberte.fr fidèlement reproduite, frontend HTML/CSS/JS réutilisé Linux + Windows
- Packaging Linux natif — apt/dnf/pacman/apk + AUR, intégration systemd standard
- Code cross-platform unifié — même package `wireguard/tun` pour TUN + Wintun, même interface `Firewall` pour nftables + WFP

**Risques identifiés :**
- **Performance tunnel** : latence additionnelle due à l'encapsulation IP dans HTTP/3 (vs proxy L7 direct). À benchmarker post-implémentation
- **Complexité NAT côté relais** : gestion TCP/UDP états + éviction TTL. Pas d'ICMP au MVP envisageable
- **IPv6** : à valider — le support est natif dans wireguard/tun et nftables mais requiert test
- **Distros sans appindicator** : GNOME Wayland pur sans extension nécessite `gnome-shell-extension-appindicator`. Mitigation : documenter dans README ou dépendance dure

**Améliorations futures (post-MVP) :**
- Registre dynamique avec API de gestion (Phase 2)
- CI/CD GitHub Actions complet (build multi-arch + publication AUR)
- Support macOS (utun — `wireguard/tun` le gère déjà)
- Alignement PRD avec la nouvelle architecture
- Support ICMP (ping, traceroute) côté relais via raw sockets — Phase 2
- Support split tunneling par domaine (si demande utilisateur, via routing policy côté client)

## Architecture Decision Records (ADRs)

Décisions structurantes du pivot 2026-04-15 — format condensé.

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
