---
stepsCompleted: ['step-01-validate-prerequisites', 'step-02-design-epics', 'step-03-create-stories', 'step-04-final-validation']
inputDocuments: ['prd.md', 'architecture.md', 'ux-design-specification.md', 'implementation-readiness-report-2026-04-15.md', 'prd-validation-report-2026-04-15.md', 'prototype-le-voile.html', 'ux-design-directions.html']
lastEdited: '2026-04-15'
editHistory:
  - date: '2026-04-15'
    changes: 'Régénération complète post-révision Linux + capture L3 (TUN/Wintun) + suppression extension navigateur. Inventaire exigences à jour : 36 FR (FR37-40 retirés) + 38 NFR (ajouts NFR9b-j, NFR22a-i, NFR25-26).'
---

# bmad_vpn_le_voile_de_velia - Epic Breakdown

## Overview

Décomposition des exigences PRD + Architecture + UX en epics et stories implémentables. Reflète l'architecture cible 2026-04-15 (capture L3 unifiée TUN Linux / Wintun Windows + kill switch firewall nftables/WFP + support Linux .deb/.rpm/.apk/AUR + suppression extension navigateur).

## Requirements Inventory

### Functional Requirements

**Tunnel & Connexion Réseau (FR1-4)**

- FR1: Le client peut établir un tunnel QUIC/HTTP3 vers le relais sélectionné via Cloudflare au démarrage
- FR2: Le client peut se reconnecter automatiquement au relais après une perte de connexion (kill switch firewall maintenu pendant toute la durée)
- FR3: Le client peut authentifier chaque relais via sa clé publique Ed25519 unique (certificate pinning)
- FR4: Les relais peuvent accepter et relayer les connexions QUIC/HTTP3 entrantes des clients

**Capture Trafic L3 & Kill Switch (FR5-8d)**

- FR5: Le client peut créer une interface virtuelle TUN (Linux) / Wintun (Windows) nommée `levoile0` avec MTU 1420 pour capturer tout le trafic IP de la machine
- FR5b: Le client peut détecter la disparition ou l'altération externe de l'interface TUN/Wintun (watchdog 3s) et déclencher une reconnexion complète avec kill switch firewall maintenu
- FR5c: Le client peut détecter au démarrage la présence d'autres interfaces VPN actives. Si détecté, refus de connexion avec message explicite : "VPN concurrent détecté ({nom_interface}). Déconnectez-le pour utiliser Le Voile."
- FR6: Le client peut configurer le routage système pour router le trafic par défaut via `levoile0`, avec route spécifique vers l'IP du relais via la gateway originale
- FR7: Le client peut détruire l'interface TUN/Wintun et restaurer les routes d'origine à la désactivation ou au shutdown propre
- FR7b: Le client peut flusher le cache DNS système au disconnect (`ipconfig /flushdns` Windows ; `resolvectl flush-caches` ou équivalent Linux selon resolver détecté)
- FR8: Le client peut activer un kill switch firewall OS-level (nftables Linux / WFP Windows) droppant tout trafic sauf (a) sur l'interface TUN, (b) sortant vers l'IP du relais sur port 443. Persiste même si le service crashe
- FR8b: Le relais peut filtrer les requêtes DNS via une blocklist de domaines malveillants (StevenBlack/hosts), téléchargée périodiquement et stockée en mémoire côté relais
- FR8c: Le client peut détecter un captive portal Wi-Fi au démarrage (probe HTTP RFC 7710). Mode "captive portal" : lockdown firewall relaxé autorisant uniquement gateway local. Bandeau UI + transition automatique vers kill switch plein dès succès du probe
- FR8d: IPv6 non tunnelisé au MVP. Par défaut entièrement bloqué. Option avancée `[ ] Autoriser IPv6 hors tunnel` (décochée, avec avertissement). Setting persisté en config TOML

**Interface Utilisateur (FR9-13c)**

- FR9: L'utilisateur peut voir l'état de protection via une fenêtre desktop (connecté/en cours/déconnecté) avec indicateur visuel
- FR10: L'utilisateur peut voir le pays sélectionné, le relais actif et l'IP visible dans la fenêtre
- FR11: L'utilisateur peut sélectionner un pays parmi les relais disponibles via un sélecteur avec drapeaux et nombre de relais
- FR12: L'utilisateur peut connecter/déconnecter Le Voile via la fenêtre ou le menu tray
- FR13: L'utilisateur peut accéder rapidement à la fenêtre via l'icône system tray (clic gauche = toggle fenêtre)
- FR13b: L'utilisateur peut quitter Le Voile via le menu clic droit du tray
- FR13c: Si l'UI ne peut pas joindre l'IPC du service, écran fixe avec titre "Service Le Voile non démarré" et commande shell selon OS détecté (systemctl/sc). Retry IPC toutes les 5s

**Démarrage & Lifecycle (FR14-16b)**

- FR14: Le service démarre automatiquement au boot (SCM Windows / systemd Linux), l'UI démarre via autostart (HKCU Windows / XDG autostart Linux)
- FR15: L'icône system tray persiste en arrière-plan après fermeture de la fenêtre webview (seule la fenêtre est détruite, le tray et le service continuent). Réouverture via menu tray "Ouvrir"
- FR15b: Le processus UI est supervisé par un gestionnaire de redémarrage automatique (systemd user `Restart=on-failure` Linux / job object Windows). Couvre les crashes du desktop environment
- FR16: L'utilisateur peut quitter l'UI via le menu tray. Le service continue (contrôlé par systemd/SCM). Pour arrêt complet : UI ou `systemctl stop` / `sc stop`
- FR16b: L'utilisateur peut désactiver temporairement le kill switch firewall via menu tray "Mode dégradé" ou `levoile-ctl killswitch off`. Restauration automatique à la prochaine connexion tunnel réussie. Indicateur visuel permanent (tray rouge + bandeau) tant que mode dégradé actif

**Relais Multi-VPS (FR17-19b)**

- FR17: Les relais peuvent recevoir et relayer les paquets IP bruts via un stream HTTP/3 `/tunnel`, appliquer NAT, résoudre le DNS en interne, et forwarder le trafic vers les destinations
- FR18: Les relais peuvent fonctionner sans aucune persistence de données (stateless — NAT table en RAM avec TTL court)
- FR19: Les relais peuvent être déployés comme binaires autonomes sur des VPS Linux (systemd)
- FR19b: Les relais peuvent être organisés par pays. Chaque pays dispose d'au moins 1 relais. Pays prioritaires (DE, ES, GB, US) ciblés à 2 relais minimum pour failover intra-pays

**Distribution & Installation (FR20-22)**

- FR20: L'utilisateur peut installer Le Voile via :
  - Windows : installeur NSIS (service SCM, UI autostart, shortcuts, Wintun DLL)
  - Linux : paquets natifs `apt install levoile` / `dnf install levoile` / `yay -S levoile` / `apk add levoile`. Post-install configure capabilities et active le service systemd
- FR20b: Tous les paquets de distribution (.exe NSIS, .deb, .rpm, .apk) sont signés Ed25519 par la master key. PKGBUILD AUR : checksum SHA256 + signatures GPG des commits. Refus d'installation sans signature valide
- FR21: Le service persiste via SCM Windows / systemd Linux. Les règles firewall et l'interface TUN persistent tant que le service tourne. Au shutdown propre, tout est nettoyé
- FR22: Configuration utilisateur stockée en TOML dans `%AppData%/LeVoile/` (Windows) ou `~/.config/levoile/` (Linux). Config service Linux : `/etc/levoile/config.toml`. Cache registre relais en JSON séparé

**Découverte & Sélection de Relais (FR23-26)**

- FR23: Chaque relais peut servir le registre complet via `/.well-known/relay-registry.json`, signé Ed25519 par la master key
- FR23b: Le client peut se connecter à un relais bootstrap hardcodé au premier lancement pour obtenir le registre initial
- FR24: Le client peut sélectionner un relais par pays choisi par l'utilisateur
- FR25: Le client peut distribuer les connexions entre les relais d'un même pays via round-robin
- FR26: Le client peut basculer automatiquement vers un autre relais du même pays en cas d'échec (timeout 3s, erreur 503, perte connexion). Kill switch firewall maintenu pendant le failover

**IP Camouflage & Tunnel IP (FR27-30b)**

- FR27: Le client peut encapsuler les paquets IP bruts capturés par TUN/Wintun dans un stream HTTP/3 `/tunnel` vers le relais (framing : 2 octets longueur + payload)
- FR28: Le relais peut désencapsuler les paquets IP, appliquer NAT (source IP = relais + port NAT alloué), résoudre le DNS en interne avec blocklist, et forwarder le trafic via sockets système
- FR29: Le relais peut authentifier les connexions /tunnel via des session tokens Ed25519 signés (TTL 4h, IP hash SHA256 dans le payload)
- FR30: Le relais peut limiter le nombre de tunnels par IP source (max 200 simultanés) et appliquer un bandwidth quota journalier (10 GiB/jour + 1 GiB/heure)
- FR30b: Le relais peut limiter le nombre total de tunnels simultanés (max 150), rejetant les connexions excédentaires avec HTTP 503

**Protection Anti-Fuite (FR31-34)**

- FR31: Le client peut émettre des requêtes STUN Binding (RFC 5389) via la TUN pour valider que le trafic UDP passe bien par le tunnel
- FR32: La capture L3 garantit structurellement qu'aucun paquet (DNS, WebRTC, IPv6, ICMP) ne peut sortir hors du tunnel. Le firewall kill switch drop tout le reste
- FR33: Le client peut comparer l'IP détectée via STUN avec l'IP du tunnel attendue pour vérifier l'absence de fuite
- FR34: En cas d'anomalie détectée (TUN bypass, IP inattendue), le client peut : déclencher reconnexion complète, alerte UI (icône orange + bandeau), log Event Log/journald, maintenir kill switch actif

**Mise à Jour Automatique (FR35-36)**

- FR35: Le client peut vérifier périodiquement la disponibilité de nouvelles versions via les releases GitHub
- FR36: Le client peut télécharger, vérifier la signature Ed25519 et appliquer les mises à jour au prochain démarrage (SCM restart / `systemctl restart levoile.service`), avec rollback automatique si tunnel pas établi dans les 30s. Si remplacement atomique échoue : continue sur ancienne version, log + notification UI, retry à la prochaine occasion

> **Note** : FR37-FR40 (extension navigateur) **SUPPRIMÉS** lors de la révision 2026-04-15 — la capture L3 machine-wide rend l'extension redondante. Le bypass > 50 Mo est abandonné.

### NonFunctional Requirements

**Security (NFR1-9j)**

- NFR1: Communications client-relais chiffrées via QUIC/HTTPS (TLS 1.3 minimum)
- NFR2: Authentification client-relais exclusivement Ed25519 via les bibliothèques standards Go (`crypto/ed25519`) + TLS via quic-go (TLS 1.3 standard Go). Aucune crypto maison. Une paire Ed25519 unique par relais
- NFR3: Les relais ne persistent aucune donnée au-delà de la durée d'une requête. NAT table en RAM uniquement, TTL ≤ 300s TCP / ≤ 120s UDP. Éviction automatique
- NFR4: Trafic tunnel indiscernable du trafic HTTPS standard par analyse DPI — vérifié par capture Wireshark (0 pattern-match VPN sur 100 échantillons)
- NFR5: Aucune fuite DNS pendant le fonctionnement normal, la reconnexion ou le failover — garanti structurellement par la capture L3 + kill switch firewall
- NFR6: Interface TUN/Wintun, routes système et règles firewall restaurées dans tous les scénarios (désactivation, crash, shutdown). Zéro résidu
- NFR7: Code source publiquement auditable sur GitHub
- NFR8: Les relais valident que les requêtes proviennent des plages IP Cloudflare — requêtes hors plage rejetées
- NFR9: Les relais bloquent les paquets IP vers les réseaux privés (loopback, RFC 1918, link-local) — protection SSRF
- NFR9b: Kill switch firewall OS-level (nftables/WFP) survit aux crashes du process service — aucune fuite possible même en cas de défaillance applicative
- NFR9c: Toutes les comparaisons cryptographiques utilisent `crypto/subtle.ConstantTimeCompare` — résistance aux timing attacks
- NFR9d: Le relais vérifie que l'IP hash (SHA256) du session token correspond à l'IP source Cloudflare (CF-Connecting-IP) — rejet immédiat si différent, empêche le replay
- NFR9e: TLS entre Cloudflare et VPS relais configuré en "Full (Strict)" — certificat valide obligatoire côté origine, TLS 1.3 minimum
- NFR9f: Le relais valide les signatures DNSSEC sur les réponses upstream (Cloudflare 1.1.1.1 + Quad9 9.9.9.9) — protection contre DNS poisoning amont
- NFR9g: Le client détecte l'injection de paquets externes sur l'interface TUN par comparaison de checksum et timestamp. Paquets non émis par le pump tunnel ignorés et loggés
- NFR9h: Binaires compilés avec `-ldflags="-s -w"` (strip symbols + DWARF) — freine le reverse engineering basique. Obfuscation avancée différée Phase 2
- NFR9i: Résolution DNS du relais au bootstrap via DoH (Cloudflare DoH ou Quad9 DoH) — protège contre DNS poisoning du resolver système au premier lancement
- NFR9j: Config TOML client stockée avec permissions 0600 (Linux) / ACL user-only (Windows). Modification externe détectée au démarrage (HMAC signé clé machine-local) = refus de démarrer + alerte UI

**Performance (NFR10-14)**

- NFR10: Latence DNS additionnelle via tunnel : < 50ms
- NFR11: Établissement tunnel initial (TUN + firewall + QUIC handshake) : < 3 secondes sur ADSL/fibre résidentielle (RTT < 50ms vers Cloudflare)
- NFR12: Reconnexion automatique : initiation < 1 seconde après perte
- NFR13: Consommation RAM client : < 25 MB en fonctionnement normal (inclut buffers TUN + stack encapsulation)
- NFR14: Impact CPU en état stable : < 2% d'utilisation CPU sur 5 minutes (encapsulation L3)

**Reliability (NFR15-19)**

- NFR15: Kill switch firewall actif dès l'activation du tunnel et maintenu pendant toutes les phases. Activation initiale < 100ms — mesuré par chronométrage applicatif avec assertion sur l'état nftables/WFP
- NFR16: Watchdog TUN : détection interface disparue < 5 secondes, reconnexion automatique avec maintien du kill switch
- NFR17: Crash-recovery : au redémarrage du service après crash, règles firewall et interface TUN orphelines détectées et nettoyées proprement < 5 secondes
- NFR18: Uptime par relais : ≥ 99.5% mensuel mesuré via endpoint /health
- NFR19: Failover entre relais d'un même pays : bascule < 5 secondes, 0 paquet IP perdu au-delà de la fenêtre de bascule, kill switch maintenu

**Privacy (NFR20-21)**

- NFR20: Aucun log IP client sur les relais — ni /tunnel, /verify, /.well-known/relay-registry.json
- NFR21: IP source hashée (SHA256) uniquement dans les session tokens Ed25519 (TTL 4h, non persisté)

**Platform Compatibility (NFR22-24)**

- NFR22: Fonctionnement équivalent sur toutes les plateformes cibles, mesuré par une matrice de tests e2e (tunneling, kill switch, leak check, UI, connect/disconnect, failover, update) dont 100% doivent passer sur Windows 11, Ubuntu 24.04, Fedora 40, Arch rolling et Alpine 3.19 avant release
- NFR23: Dépendances runtime Linux résolues automatiquement par les gestionnaires de paquets natifs (apt/dnf/pacman/apk) — aucune installation manuelle requise pour libwebkit2gtk, libayatana-appindicator3, nftables, iproute2
- NFR24: Installation Linux configure les capabilities via le unit systemd (`AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW`, `User=levoile`) — pas de sudo récurrent, capabilities persistent aux mises à jour binaire

**Logging & Observability — Client (NFR22a-c)**

- NFR22a: Logs client (syslog/Event Log) contiennent uniquement événements opérationnels (état tunnel, erreurs, alertes, updates). **Aucune URL, aucun nom de domaine résolu, aucune IP destination, aucun contenu utilisateur**
- NFR22b: Niveau log par défaut : INFO. DEBUG activable uniquement via flag CLI `--debug`. DEBUG n'active JAMAIS le log de données utilisateur
- NFR22c: Rotation logs automatique (journald ou rotation manuelle Windows) — taille max 10 Mo, conservation 7 jours

**Security Testing & Supply Chain (NFR22d-f)**

- NFR22d: Pipeline CI exécute au minimum : `go vet`, `gosec` (SAST), `govulncheck`, `go test -race ./...`. Build bloqué si l'un échoue avec severity ≥ medium
- NFR22e: Dépendances Go épinglées (go.sum commit), révisées à chaque mise à jour. Renovate bot. Scan hebdomadaire govulncheck sur `main`
- NFR22f: Fuzzing (Go 1.18+ native) sur les parsers critiques : packet IP, STUN, TOML config, registre JSON. Exécution hebdomadaire en CI

**Cryptographic Key Management (NFR22g-i)**

- NFR22g: Master key Ed25519 stockée exclusivement sur machine dédiée isolée (air-gapped ou YubiKey HSM). Sauvegardes chiffrées hors ligne. Rotation tous les 24 mois ou sur incident
- NFR22h: Les clients embarquent une chaîne de confiance permettant la rotation de la master key : clé actuelle + clé de rotation future. Mise à jour de clé publique cliente via release dual-signée (transitoire 6 mois)
- NFR22i: Compte GitHub velia-the-veil protégé : 2FA hardware (YubiKey), pas de Personal Access Token long-terme, signing commits GPG obligatoire, branch protection main

**Runtime Integrity & Startup Safety (NFR25-26)**

- NFR25: Le kill switch firewall est activé dans le même ordre que le reste de la séquence Connect (après TUN + routing + tunnel établi). **Risque accepté** : fenêtre de fuite de quelques secondes au démarrage système avant le tunnel prêt. Acceptable face à la simplicité architecturale (cible grand public)
- NFR26: Le service vérifie l'intégrité de son propre binaire au démarrage — hash SHA256 comparé à une valeur signée Ed25519 embarquée. Échec = refus de démarrer + log + refus d'activer le tunnel. Protège contre remplacement du binaire post-installation

### Additional Requirements

**Architecture — Stack & Bibliothèques**

- Stack : Go 1.26 + quic-go v0.59.0 + webview/webview + fyne.io/systray v1.12.0 + kardianos/service v1.2.4 + golang.zx2c4.com/wireguard/tun + nftables (Linux) / Windows Filtering Platform (Windows) + GoReleaser v2 + nfpm + NSIS
- Microsoft/go-winio v0.6.2 pour named pipes Windows (IPC)
- BurntSushi/toml v1.5.0 pour la configuration
- Cloudflare comme CDN intermédiaire (camouflage protocolaire QUIC/HTTPS)
- Wintun DLL signée Microsoft embarquée via `//go:embed` (Windows uniquement, build tag)

**Architecture — Modèle 2 Processus**

- `levoile-service` : service privilégié cross-platform via kardianos/service (SCM Windows en LocalSystem / systemd Linux user `levoile` + AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW)
- `levoile-ui` : binaire UI unique combinant fyne.io/systray + webview/webview + serveur HTTP local (127.0.0.1:{port}) dans un seul processus. Singleton via mutex nommé Windows / flock Linux par utilisateur
- `levoile-ctl` : CLI opérationnel (killswitch off/on, status, reconnect, logs) — communique avec le service via IPC, authentification token machine-local
- IPC : named pipes Windows (`\\.\pipe\levoile`) / Unix sockets Linux (`/run/levoile/ipc.sock` permissions 0660 user `levoile` groupe `levoile`). Protocole JSON ligne par ligne, max 4 Ko
- Frontend ↔ UI : serveur HTTP local embarqué exposant une API REST JSON (assets servis via `//go:embed`)

**Architecture — Capture L3 & Routage**

- Interface virtuelle `levoile0` créée à l'activation, MTU 1420
- Route par défaut via TUN (`0.0.0.0/0 dev levoile0`) + route spécifique vers IP relais via gateway originale
- Linux : `ip rule` + table 51820. Windows : `winipcfg` + metrics
- Modèle gateway NAT côté relais (pas de netstack userspace client) — simplicité
- Ordre Connect strict : `elevation.Check() → tun.New() → routing.Setup() → firewall.Activate(relayIP, tunName) → tunnel.Connect()`
- Ordre Disconnect strict : `tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()`

**Architecture — Communication & Protocoles**

- Client ↔ Relais (Tunnel IP) : HTTP/3 POST `/tunnel`, stream bidirectionnel transportant paquets IP bruts (framing 2 octets longueur + payload), header `Authorization: Bearer {session_token}`
- Client ↔ Relais (Verify) : GET `/verify` — challenge-response, émission session token Ed25519 TTL 4h
- Client ↔ Relais (IP) : GET `/ip` — retourne IP visible
- Client → Registre : GET `/.well-known/relay-registry.json` au démarrage (n'importe quel relais)
- DNS résolu côté relais (upstream Cloudflare 1.1.1.1 + Quad9 9.9.9.9 fallback, blocklist StevenBlack appliquée avant NAT-forward)
- Plus de proxy DNS local client, plus de proxy HTTP CONNECT, plus de handler /stun-relay
- Endpoint relais `/health` anonyme : `{status, tunnels, nat_entries, uptime, ram_mb, cpu_pct, country, relay_id}`

**Architecture — Sécurité & Conformité**

- Session tokens Ed25519 : TTL 4h, IP hash SHA256 dans payload, refresh auto avec backoff exponentiel (100ms → 30s) + circuit breaker (5 échecs consécutifs → déconnexion + kill switch maintenu)
- Rate limiting relais : 200 tunnels max/IP source (sync.Map + atomics, lock-free), 150 tunnels totaux/relais, bandwidth 10 GiB/jour + 1 GiB/heure par IP
- Validation source Cloudflare (CF-Connecting-IP) sur chaque relais
- Protection SSRF : blocage paquets vers réseaux privés (loopback, RFC 1918, link-local)
- DNSSEC validation côté relais avant forwarding réponses DNS
- Master key chain-of-trust avec clé de rotation embarquée client (dual-signature transitoire 6 mois)
- Tous les paquets de distribution signés Ed25519 master key
- SECURITY.md + security.txt (RFC 9116) + warrant canary mensuel sur plateformeliberte.fr (advancé de Phase 3 à MVP)
- SLA patch CVE : 7 jours pour CVSS ≥ 9, 30 jours pour CVSS 7-8

**Architecture — Infrastructure & Déploiement**

- Relais déployés via scp + systemd sur VPS (`deploy/install.sh` : user system `levoile`, /opt/levoile, ProtectSystem=strict, NoNewPrivileges=true)
- Cloudflare : un sous-domaine par pays (de.levoile.dev, es.levoile.dev, gb.levoile.dev, us.levoile.dev), Origin Certificate "Full (Strict)"
- Auto-update : check GitHub releases toutes les 6h, signature Ed25519, rollback (tunnel pas établi 30s post-update), rate limit 500 kbps download
- Installeur NSIS Windows : install service SCM + UI autostart HKCU + shortcuts + Wintun DLL `Program Files/LeVoile/wintun.dll` ; uninstall : clear WFP filters + remove TUN adapter + cleanup
- Paquets Linux via GoReleaser + nfpm : `.deb` (Debian 11+/Ubuntu 22.04+), `.rpm` (Fedora 38+/RHEL 9+), `.apk` (Alpine 3.18+), AUR (Arch rolling, PKGBUILD séparé via GitHub Action)
- Dépendances runtime Linux : `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0`, `libayatana-appindicator3-1`, `nftables`, `iproute2` (résolues automatiquement)
- Fichiers Linux : `/usr/bin/levoile-service`, `/usr/bin/levoile-ui`, `/usr/bin/levoile-ctl`, `/usr/lib/systemd/system/levoile.service`, `/etc/levoile/config.toml`, `/usr/share/applications/levoile.desktop`, icônes XDG hicolor (256/128/64/48/32/16)
- Script post-install Linux : `modprobe nf_tables` + test fumée + `systemctl daemon-reload` + `systemctl enable --now levoile.service`
- CI/CD GitHub Actions : build multi-OS (amd64 + arm64), security gates obligatoires (NFR22d/e/f), signatures, release, AUR publish
- Snapshot Windows-stable préservé : git tag `windows-stable-2026-04-15` + branche `backup/windows-stable`

**Architecture — Modules à créer / supprimer**

- **Nouveaux** : `internal/tun/`, `internal/firewall/` (impls nftables Linux + WFP Windows), `internal/routing/` (iproute2 + winipcfg), `internal/integrity/` (binary self-check), `internal/config/integrity.go` (HMAC config), `internal/registry/doh_resolver.go`, `internal/dns/flush.go`, `cmd/ctl/`
- **Refactor** : `internal/tunnel/` (pompe paquets IP ↔ HTTP/3), `cmd/relay/` (handler `/tunnel`, NAT table, DNS interne, blocklist), `internal/leakcheck/` (validation TUN), `internal/service/` (orchestration TUN + firewall + routing avant tunnel), `internal/config/` (sections tun, firewall ; retrait blocklist client + http_proxy + browser_policies)
- **Supprimés** : `internal/httpproxy/`, `internal/browser/`, `extension/`, `tools/crxgen/`, `internal/dns/proxy.go`, `internal/dns/kill_switch.go`, `internal/stun/interceptor.go`, `internal/stun/relayer.go`, `cmd/relay/handlers/dns.go`, `connect.go`, `stun_relay.go`

**UX Design — Exigences additionnelles**

- Zero-config absolu : aucun écran de configuration à l'installation, tunnel auto-connecté au pays par défaut
- UI reproduisant exactement la charte plateformeliberte.fr (fond `#0b1526`/`#0e1e38`, bleus `#1a6fc4`/`#2a8dff`, rouge alerte `#d42b2b`, vert `#4ade80`, orange `#fb923c`, fonts Bebas Neue/Rajdhani/Inter en woff2 embarqués)
- Tray-first : system tray = interface principale, fenêtre webview secondaire (ouverte/fermée à la demande)
- Sélecteur de pays : 4 pays MVP (Allemagne, Espagne, Royaume-Uni, États-Unis) avec drapeaux emoji et nombre de relais
- Indicateur statut coloré : vert (connecté steady), orange (connecting pulse 1.5s), rouge (déconnecté)
- IP visible affichée en permanence dans la fenêtre + lien direct vers `plateformeliberte.fr/test-protection.html`
- Messages utilisateur en français non-technique ("Connecté", "Reconnexion en cours...", "Déconnecté")
- Layout : sidebar 150px (pays) + main panel (statut centré). Fenêtre 420×540px frameless
- Polling frontend : `fetch('/api/status')` toutes les 2s, `fetch('/api/registry')` toutes les 30s
- Modal "Quitter" avec checkbox "Ne plus afficher" persisté
- Notification mise à jour non-intrusive dans le tray
- Frontend vanilla HTML/CSS/JS (pas de framework, pas de bundler), servi par HTTP local embarqué
- Mode dégradé : indicateur visuel permanent (icône tray rouge + bandeau webview rouge) tant qu'actif (FR16b)
- IPv6 hors tunnel : option avancée décochée par défaut + modale d'avertissement explicite (FR8d)
- Captive portal : bandeau UI dédié + transition automatique (FR8c)
- Écran "Service non démarré" avec commande shell selon OS détecté + retry IPC 5s (FR13c)

**UX — Accessibilité (RGAA AA — exigence légale produits français)**

- Contraste AA (4.5:1 texte minimum)
- Navigation clavier complète
- aria-labels sur tous contrôles
- Taille police réglable
- Focus visible
- Test avec lecteur d'écran NVDA (Windows) / Orca (Linux) avant release

**Plateformes Cibles**

- Windows 10/11 (WebView2 Runtime requis, présent par défaut Win 11)
- Debian 11+ / Ubuntu 22.04+
- Fedora 38+ / RHEL 9+
- Arch Linux rolling
- Alpine 3.18+
- macOS différé Phase 3

### FR Coverage Map

- FR1: Epic 1 — Tunnel QUIC/HTTP3 vers relais via Cloudflare
- FR2: Epic 1 — Reconnexion automatique tunnel
- FR3: Epic 1 — Authentification Ed25519 relais (certificate pinning)
- FR4: Epic 1 — Relais accepte connexions QUIC/HTTP3
- FR5: Epic 2 — Création interface TUN/Wintun `levoile0`
- FR5b: Epic 2 — Watchdog TUN (détection altération externe)
- FR5c: Epic 2 — Détection VPN concurrent au démarrage
- FR6: Epic 2 — Routage système (route par défaut via TUN)
- FR7: Epic 2 — Destruction TUN + restauration routes au shutdown
- FR7b: Epic 2 — Flush cache DNS système au disconnect
- FR8: Epic 2 — Kill switch firewall OS-level (nftables/WFP)
- FR8b: Epic 3 — Blocklist DNS côté relais (StevenBlack/hosts)
- FR8c: Epic 2 — Détection captive portal Wi-Fi
- FR8d: Epic 2 — Option IPv6 hors tunnel (avec avertissement)
- FR9: Epic 5 — Affichage état de protection (vert/orange/rouge)
- FR10: Epic 5 — Affichage pays, relais actif, IP visible
- FR11: Epic 5 — Sélecteur de pays (drapeaux + nombre relais)
- FR12: Epic 5 — Connect/disconnect via fenêtre ou tray
- FR13: Epic 5 — Toggle fenêtre via clic gauche tray
- FR13b: Epic 5 — Quitter via menu clic droit du tray
- FR13c: Epic 5 — Écran fallback "Service non démarré" + retry IPC
- FR14: Epic 7 — Démarrage automatique service (SCM/systemd) + UI autostart
- FR15: Epic 5 — Tray persiste, fenêtre webview à la demande
- FR15b: Epic 5 — Supervision UI (Restart=on-failure / job object)
- FR16: Epic 5 — Quitter UI via tray (service continue)
- FR16b: Epic 5 — Mode dégradé kill switch + indicateur visuel
- FR17: Epic 3 — Relais reçoit /tunnel HTTP/3, NAT, DNS, forward
- FR18: Epic 3 — Relais stateless (NAT table en RAM, TTL court)
- FR19: Epic 3 — Relais déployable comme binaire autonome (systemd)
- FR19b: Epic 3 — Relais organisés par pays (DE/ES/GB/US prioritaires ≥2)
- FR20: Epic 7 — Installation NSIS Windows / paquets natifs Linux
- FR20b: Epic 7 — Signature Ed25519 de tous les paquets de distribution
- FR21: Epic 7 — Persistence service via SCM/systemd, cleanup au shutdown
- FR22: Epic 7 — Configuration TOML (%AppData% / ~/.config / /etc)
- FR23: Epic 4 — Registre distribué `/.well-known/relay-registry.json`
- FR23b: Epic 4 — Relais bootstrap hardcodé (premier lancement)
- FR24: Epic 4 — Sélection relais par pays
- FR25: Epic 4 — Round-robin intra-pays
- FR26: Epic 4 — Failover automatique (kill switch maintenu)
- FR27: Epic 3 — Encapsulation paquets IP bruts dans /tunnel HTTP/3
- FR28: Epic 3 — Désencapsulation + NAT + DNS interne + forward
- FR29: Epic 3 — Authentification /tunnel via session tokens Ed25519
- FR30: Epic 3 — Limite tunnels par IP (200 max) + bandwidth quota
- FR30b: Epic 3 — Limite tunnels totaux par relais (150 max, HTTP 503)
- FR31: Epic 6 — Émission STUN Binding via TUN (validation)
- FR32: Epic 6 — Garantie structurelle anti-fuite (capture L3 + firewall)
- FR33: Epic 6 — Comparaison IP STUN vs IP tunnel attendue
- FR34: Epic 6 — Reconnexion + alerte UI + log en cas d'anomalie
- FR35: Epic 8 — Vérification périodique releases GitHub
- FR36: Epic 8 — Téléchargement, signature, rollback automatique

## Epic List

### Epic 1: Tunnel Chiffré et Authentification Relais
L'utilisateur dispose d'un tunnel QUIC/HTTP3 chiffré vers un relais authentifié Ed25519 via Cloudflare, avec reconnexion automatique transparente et certificate pinning.
**FRs covered:** FR1, FR2, FR3, FR4

### Epic 2: Capture L3 Machine-Wide & Kill Switch Firewall
Tout le trafic IP de la machine traverse le tunnel via une interface TUN/Wintun, le kill switch firewall (nftables/WFP) garantit zéro fuite structurelle, IPv6 contrôlé, captive portal géré, VPN concurrent détecté.
**FRs covered:** FR5, FR5b, FR5c, FR6, FR7, FR7b, FR8, FR8c, FR8d

### Epic 3: Relais Stateless Multi-VPS avec Tunnel IP & NAT
L'opérateur déploie des relais stateless qui acceptent /tunnel HTTP/3, désencapsulent les paquets IP, appliquent NAT, résolvent DNS avec blocklist StevenBlack, et appliquent rate limiting + bandwidth quota.
**FRs covered:** FR8b, FR17, FR18, FR19, FR19b, FR27, FR28, FR29, FR30, FR30b

### Epic 4: Découverte & Failover Multi-Pays
Le client découvre les relais via un registre distribué signé Ed25519, sélectionne par pays, distribue via round-robin, et bascule automatiquement vers un autre relais en cas d'échec — kill switch maintenu pendant la bascule.
**FRs covered:** FR23, FR23b, FR24, FR25, FR26

### Epic 5: Interface Desktop Cross-Platform (Tray + Webview)
L'utilisateur voit son état (vert/orange/rouge), sélectionne un pays via drapeaux, voit son IP visible, connecte/déconnecte via fenêtre ou tray, gère le mode dégradé, et garde le tray persistant. Écran fallback "Service non démarré" + supervision UI auto-restart.
**FRs covered:** FR9, FR10, FR11, FR12, FR13, FR13b, FR13c, FR15, FR15b, FR16, FR16b

### Epic 6: Validation Anti-Fuite (STUN + Watchdog TUN)
Le client valide en continu que la TUN capture bien le trafic via STUN Binding (RFC 5389), compare l'IP attendue, alerte et reconnecte en cas d'anomalie. Kill switch maintenu pendant les contrôles.
**FRs covered:** FR31, FR32, FR33, FR34

### Epic 7: Distribution & Installation Cross-Platform Signée
L'utilisateur installe via installeur NSIS Windows ou paquets natifs Linux signés Ed25519 (.deb/.rpm/.apk/AUR), avec démarrage automatique du service+UI au boot, configurations persistées en TOML.
**FRs covered:** FR14, FR20, FR20b, FR21, FR22

### Epic 8: Auto-Update Sécurisé avec Rollback
Le client se met à jour automatiquement via GitHub releases, vérifie la signature Ed25519, applique la mise à jour au prochain démarrage, et rollback automatiquement si le tunnel n'est pas établi en 30s.
**FRs covered:** FR35, FR36

### Notes de planning sprint (issu de l'élicitation comparative)

Tailles relatives anticipées (à valider à l'étape 3) :
- **Lourds** (≥ 7 stories) : Epic 2 (capture L3 + firewall + 2 OS), Epic 3 (relais + NAT + auth + DNS), Epic 5 (UI complète)
- **Moyens** (4-6 stories) : Epic 1, Epic 4, Epic 7
- **Légers** (2-3 stories) : Epic 6, Epic 8

Le refactor majeur (Windows-stable → Linux + TUN) est isolé dans Epic 2 et Epic 3.

---

## Epic 1: Tunnel Chiffré et Authentification Relais

L'utilisateur dispose d'un tunnel QUIC/HTTP3 chiffré vers un relais authentifié Ed25519 via Cloudflare, avec reconnexion automatique transparente et certificate pinning.

### Story 1.1: Établir un tunnel QUIC/HTTP3 vers un relais via Cloudflare avec certificate pinning

As a utilisateur final,
I want que le client établisse un tunnel chiffré QUIC/HTTP3 vers le relais sélectionné via Cloudflare au démarrage,
So that ma communication est chiffrée et le relais est cryptographiquement authentifié.

**Acceptance Criteria:**

**Given** la config TOML contient `relay.domain` et `relay.public_key_ed25519` valides
**When** le service appelle `tunnel.Connect(ctx, relayDomain, pinnedKey)`
**Then** une session QUIC/HTTP3 est établie via TLS 1.3 vers `https://{relay-domain}/`
**And** le certificat serveur est validé contre la clé Ed25519 pinnée via `crypto/subtle.ConstantTimeCompare`
**And** la connexion est rejetée si le pinning échoue (erreur `ErrPinningFailed`)

**Given** un relais expose un endpoint HTTP/3 avec un certificat Cloudflare valide
**When** le client tente la connexion
**Then** le handshake TLS 1.3 réussit en < 3 secondes sur connexion ADSL/fibre (RTT < 50ms vers Cloudflare)
**And** aucune signature DPI VPN n'est observable sur le trafic (vérifiable par capture Wireshark)

### Story 1.2: Reconnexion automatique avec backoff exponentiel et circuit breaker

As a utilisateur final,
I want que le tunnel se reconnecte automatiquement après une perte de connexion,
So that ma protection reprend sans intervention manuelle.

**Acceptance Criteria:**

**Given** un tunnel actif est interrompu (perte réseau, kill connexion QUIC, timeout)
**When** le détecteur d'état change vers `Disconnected`
**Then** la reconnexion est initiée < 1 seconde après la perte
**And** la stratégie de backoff exponentiel s'applique : 100ms → 200ms → 400ms → … → 30s plafond
**And** le kill switch firewall (Epic 2) reste actif pendant toute la durée de la reconnexion

**Given** 5 tentatives consécutives ont échoué
**When** le circuit breaker se déclenche
**Then** le tunnel est déconnecté proprement (state = `Failed`)
**And** le kill switch firewall reste actif (aucune fuite possible)
**And** une alerte est remontée via IPC vers l'UI

### Story 1.3: Relais accepte les connexions QUIC/HTTP3 entrantes

As a opérateur,
I want que le binaire relais accepte les connexions QUIC/HTTP3 sur le port 443,
So that les clients Le Voile peuvent s'y connecter via Cloudflare.

**Acceptance Criteria:**

**Given** le binaire relais est démarré avec `-addr :443 -cert /etc/levoile/cert.pem -key /etc/levoile/key.pem`
**When** un client QUIC initie un handshake HTTP/3 sur le port 443
**Then** le serveur quic-go accepte la connexion
**And** la version TLS négociée est 1.3 minimum
**And** le serveur expose au moins un handler stub (`/health`) répondant 200 OK

**Given** une requête arrive avec une IP source hors plages Cloudflare
**When** le middleware de validation source filtre la requête
**Then** la requête est rejetée avec HTTP 403
**And** aucune entrée de log n'enregistre l'IP client (NFR20)

---

## Epic 2: Capture L3 Machine-Wide & Kill Switch Firewall

Tout le trafic IP de la machine traverse le tunnel via une interface TUN/Wintun, le kill switch firewall (nftables/WFP) garantit zéro fuite structurelle, IPv6 contrôlé, captive portal géré, VPN concurrent détecté.

### Story 2.1: Création et destruction de l'interface TUN/Wintun `levoile0`

As a utilisateur final,
I want que le client crée une interface virtuelle `levoile0` à l'activation et la détruise proprement à la désactivation,
So that tout le trafic IP de ma machine puisse être capturé sans laisser de résidu.

**Acceptance Criteria:**

**Given** le service tourne avec les privilèges requis (CAP_NET_ADMIN Linux / LocalSystem Windows)
**When** `tun.New("levoile0", 1420)` est appelé
**Then** une interface virtuelle `levoile0` apparaît dans `ip link show` (Linux) ou `Get-NetAdapter` (Windows) avec MTU 1420
**And** l'interface accepte des opérations Read/Write de paquets IP bruts
**And** sur Windows, la DLL Wintun signée Microsoft est extraite depuis l'embed vers `%ProgramData%/LeVoile/wintun.dll` au premier démarrage

**Given** une interface `levoile0` active
**When** `device.Close()` est appelé (désactivation, shutdown, crash recovery)
**Then** l'interface disparaît du système
**And** aucun résidu (interface fantôme, fichier de lock) ne subsiste
**And** le crash-recovery au redémarrage du service détecte et nettoie une `levoile0` orpheline en < 5 secondes (NFR17)

### Story 2.2: Watchdog TUN — détection d'altération externe

As a utilisateur final,
I want que le client détecte si l'interface TUN disparaît ou est altérée par un processus externe,
So that ma protection se rétablit automatiquement même si un admin ou malware tente de la contourner.

**Acceptance Criteria:**

**Given** le tunnel est actif et `levoile0` existe
**When** un acteur externe supprime ou altère l'interface (`ip link delete levoile0`, désactivation Wintun)
**Then** le watchdog (poll 3s) détecte la disparition en < 5 secondes (NFR16)
**And** une reconnexion complète est déclenchée : recreate TUN → re-setup routing → re-activate firewall → tunnel reconnect
**And** le kill switch firewall reste actif pendant toute la procédure

### Story 2.3: Détection de VPN concurrent au démarrage

As a utilisateur final,
I want que le client refuse de démarrer si un autre VPN est actif sur ma machine,
So that je ne crée pas de configuration réseau incohérente entre deux tunnels.

**Acceptance Criteria:**

**Given** le service démarre
**When** le scan des interfaces réseau détecte une interface TUN/TAP/utun/wireguard/openvpn/cisco active autre que `levoile0`
**Then** la connexion est refusée
**And** un message explicite est remonté à l'UI : "VPN concurrent détecté ({nom_interface}). Déconnectez-le pour utiliser Le Voile."
**And** aucune interface `levoile0` n'est créée

**Given** aucun VPN concurrent n'est détecté
**When** le scan se termine
**Then** le démarrage continue normalement vers la séquence Connect

### Story 2.4: Routage système — route par défaut via TUN + route spécifique relais

As a utilisateur final,
I want que tout le trafic IP de ma machine soit routé via `levoile0`, sauf le trafic vers l'IP du relais qui passe par la gateway originale,
So that le trafic est tunnelisé sans créer de routing loop.

**Acceptance Criteria:**

**Given** l'interface `levoile0` est active et l'IP du relais est résolue
**When** `routing.Setup(tunName, relayIP, origGateway)` est appelé
**Then** sur Linux : `ip route add 0.0.0.0/0 dev levoile0 table 51820` + `ip rule add from all lookup 51820 priority 100` + `ip route add {relayIP}/32 via {origGateway}` sont posés
**And** sur Windows : `winipcfg` ajoute une route par défaut via TUN avec metric basse + route /32 vers relayIP via gateway originale avec metric haute
**And** les routes originales sont sauvegardées en mémoire pour restauration

**Given** le tunnel est désactivé
**When** `routing.Teardown()` est appelé
**Then** toutes les routes ajoutées par Le Voile sont supprimées
**And** la route par défaut originale est restaurée (NFR6)

### Story 2.5: Flush du cache DNS système au disconnect

As a utilisateur final,
I want que le cache DNS système soit purgé à la déconnexion,
So that aucune entrée résolue via le tunnel ne subsiste après désactivation.

**Acceptance Criteria:**

**Given** le tunnel est en cours de désactivation
**When** `dns.Flush()` est appelé
**Then** sur Windows : `ipconfig /flushdns` est exécuté en sous-processus
**And** sur Linux avec systemd-resolved actif : `resolvectl flush-caches` est exécuté
**And** sur Linux avec nscd : `nscd -i hosts` est exécuté
**And** sur Linux avec dnsmasq : SIGHUP est envoyé au PID dnsmasq
**And** sur Linux sans resolver détecté : no-op silencieux (pas d'erreur)

### Story 2.6: Kill switch firewall OS-level via nftables (Linux)

As a utilisateur final Linux,
I want qu'un kill switch firewall kernel-level bloque tout trafic sortant sauf TUN + IP relais:443,
So that aucune fuite n'est possible même si le service crashe.

**Acceptance Criteria:**

**Given** le binaire `nft` est présent et le module kernel `nf_tables` est chargé
**When** `firewall.Activate(relayIP, "levoile0")` est appelé sur Linux
**Then** le ruleset `inet levoile` est appliqué via `nft -f -` (template Go)
**And** les règles autorisent : `oif levoile0`, `ip daddr {relayIP} udp dport 443`, loopback ; tout le reste est `drop`
**And** l'activation est mesurée < 100ms par chronométrage applicatif (NFR15)
**And** `nft list ruleset` confirme le chargement effectif

**Given** le service crash brutalement (SIGKILL)
**When** le process est tué
**Then** les règles nftables persistent dans le kernel
**And** aucun trafic non-tunnel ne peut sortir
**And** au redémarrage du service, les règles orphelines sont détectées et reset proprement avant nouvelle activation (NFR9b, NFR17)

**Given** `nft` est absent ou `nf_tables` n'est pas chargeable
**When** `firewall.Activate()` est appelé
**Then** le service refuse de démarrer avec message clair : "nftables kernel module unavailable, cannot start Le Voile"
**And** aucune règle n'est posée

### Story 2.7: Kill switch firewall OS-level via WFP (Windows)

As a utilisateur final Windows,
I want qu'un kill switch firewall kernel-level via Windows Filtering Platform bloque tout trafic sortant sauf TUN + IP relais:443,
So that aucune fuite n'est possible même si le service crashe.

**Acceptance Criteria:**

**Given** le service tourne en LocalSystem
**When** `firewall.Activate(relayIP, "levoile0")` est appelé sur Windows
**Then** un provider + sublayer WFP dédiés sont créés via API `Fwpm*`
**And** des filters BLOCK sont posés sur toutes les interfaces sauf la TUN et le flow vers relayIP:443
**And** l'activation est mesurée < 100ms (NFR15)
**And** un test de coupure tunnel provoquée vérifie que toute connexion sortante hors TUN est droppée

**Given** le service crash
**When** le process est tué
**Then** les filters WFP persistent (kernel-level) et continuent à bloquer le trafic
**And** au redémarrage, l'enumeration `WfpEnumFilters` détecte les filters orphelins et les nettoie avant ré-activation

**Given** un firewall tiers (Comodo, ZoneAlarm, Norton) interfère avec WFP
**When** la détection d'altération via `WfpEnumFilters` détecte des règles supprimées
**Then** une alerte UI est remontée : "Règles firewall altérées par tiers — protection compromise"
**And** un log Event Log est écrit

### Story 2.8: Détection captive portal Wi-Fi + lockdown firewall relaxé

As a utilisateur final,
I want que le client détecte les portails captifs Wi-Fi et autorise temporairement l'authentification,
So that je puisse me connecter à un Wi-Fi public puis activer la protection.

**Acceptance Criteria:**

**Given** le client démarre sur un nouveau réseau Wi-Fi
**When** une probe HTTP RFC 7710 ou `http://captive.apple.com/hotspot-detect.html` est émise et reçoit un redirect 30x
**Then** le mode "captive portal" est activé : firewall lockdown relaxé autorisant uniquement le trafic vers la gateway réseau local
**And** un bandeau UI s'affiche : "Authentifiez-vous sur le portail Wi-Fi, puis cliquez 'Activer la protection'"

**Given** l'utilisateur a complété l'authentification Wi-Fi
**When** la probe est ré-émise et retourne 204 No Content (succès)
**Then** la transition vers le kill switch plein + tunnel se fait automatiquement
**And** le bandeau UI disparaît

### Story 2.9: Option IPv6 hors tunnel avec avertissement explicite

As a utilisateur technique sur FAI dual-stack,
I want pouvoir cocher une option avancée pour autoriser l'IPv6 natif hors tunnel (au lieu d'être bloqué),
So that je puisse continuer à utiliser des services IPv6 en assumant l'exposition de mon IPv6 réelle.

**Acceptance Criteria:**

**Given** le mode par défaut
**When** le client se connecte
**Then** tout trafic IPv6 sortant est droppé par le firewall (aucune fuite possible)
**And** la config TOML contient `[tunnel] allow_ipv6_leak = false`

**Given** l'utilisateur ouvre Paramètres avancés et coche `[ ] Autoriser IPv6 hors tunnel`
**When** la case est cochée
**Then** une modale d'avertissement explicite s'affiche : "L'IPv6 ne sera PAS protégé par Le Voile et exposera votre IP réelle sur les services IPv6. Continuer ?"
**And** sur validation, le firewall est mis à jour pour autoriser le trafic IPv6 natif sortant
**And** la config TOML est persistée avec `allow_ipv6_leak = true`
**And** un indicateur visuel UI signale l'état "IPv6 non protégé"

---

## Epic 3: Relais Stateless Multi-VPS avec Tunnel IP & NAT

L'opérateur déploie des relais stateless qui acceptent /tunnel HTTP/3, désencapsulent les paquets IP, appliquent NAT, résolvent DNS avec blocklist, et appliquent rate limiting + bandwidth quota.

### Story 3.1: Binaire relais Go HTTP/3 stateless déployable via systemd

As a opérateur,
I want un binaire Go autonome déployable sur n'importe quel VPS Linux via systemd,
So that je puisse ajouter un nouveau pays en déployant simplement le binaire.

**Acceptance Criteria:**

**Given** un VPS Linux fraîchement provisionné
**When** `deploy/install.sh` est exécuté avec une cert/key TLS valide
**Then** un user système `levoile` est créé
**And** le binaire est installé dans `/opt/levoile/relay`
**And** un unit systemd `levoile-relay.service` est configuré avec `ProtectSystem=strict`, `ProtectHome=true`, `NoNewPrivileges=true`, `Restart=always`, `AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN`
**And** `systemctl enable --now levoile-relay.service` démarre le service

**Given** le service relais tourne
**When** la commande `systemctl restart levoile-relay.service` est exécutée
**Then** aucune donnée persistée ne survit (NAT table reset, pas de fichier d'état) — confirmation NFR3 + FR18
**And** le redémarrage prend < 5 secondes

### Story 3.2: Endpoint /verify avec émission session tokens Ed25519

As a opérateur de relais,
I want que mon relais émette des session tokens Ed25519 signés via /verify,
So that les clients puissent authentifier ensuite leurs requêtes /tunnel.

**Acceptance Criteria:**

**Given** un client émet `GET https://{relay}/verify` avec un challenge dans le body
**When** le handler `/verify` traite la requête
**Then** un token JSON est retourné contenant : `{ip_hash: SHA256(CF-Connecting-IP), issued_at, ttl: 14400, signature: Ed25519(...)}`
**And** la signature utilise la clé privée Ed25519 unique du relais
**And** le TTL est exactement 4h (14400s)
**And** aucune entrée de log ne contient l'IP source (NFR20)

**Given** l'IP source réelle (CF-Connecting-IP) n'est pas dans les plages Cloudflare
**When** la requête arrive
**Then** elle est rejetée avec HTTP 403 sans génération de token

### Story 3.3: Handler /tunnel avec stream bidirectionnel paquets IP

As a opérateur,
I want que mon relais expose un endpoint `/tunnel` HTTP/3 acceptant un stream bidirectionnel de paquets IP bruts,
So that le client puisse encapsuler son trafic IP capturé par TUN/Wintun.

**Acceptance Criteria:**

**Given** un client ouvre un stream `POST https://{relay}/tunnel` avec `Authorization: Bearer {session_token}`
**When** le handler valide le token Ed25519 et l'IP hash via `crypto/subtle.ConstantTimeCompare` (NFR9c, NFR9d)
**Then** le stream bidirectionnel est ouvert
**And** le framing `[2 octets big-endian: length][payload IP packet]` est respecté dans les deux sens
**And** chaque paquet entrant est passé au pipeline de désencapsulation/NAT

**Given** un session token expiré ou un IP hash ≠ SHA256(CF-Connecting-IP)
**When** la validation échoue
**Then** le stream est fermé avec HTTP 401
**And** aucune entrée de log ne contient l'IP source

### Story 3.4: NAT côté relais avec table en RAM et TTL court

As a opérateur,
I want une table NAT en mémoire avec éviction par TTL,
So that le relais reste stateless tout en supportant des flux concurrents.

**Acceptance Criteria:**

**Given** un paquet IP entrant via /tunnel avec 5-tuple (src_ip, src_port, dst_ip, dst_port, proto)
**When** le moteur NAT traite le paquet
**Then** une entrée `(client_session, 5-tuple) → (nat_port, last_seen)` est créée dans `sync.Map` keyée
**And** l'IP source est substituée par l'IP du relais
**And** le port NAT est alloué dans la range 10000-60000
**And** le paquet est forwardé via socket userspace (TCP : dial/accept normal, UDP : `net.ListenUDP`)

**Given** une entrée NAT n'a pas reçu de paquet depuis le TTL (TCP 300s, UDP 120s, NFR3)
**When** le sweeper d'éviction tourne
**Then** l'entrée est supprimée et le port NAT libéré
**And** les sockets associés sont fermés proprement

**Given** le relais est arrêté (`systemctl stop`)
**When** le service s'arrête
**Then** la table NAT en RAM est perdue (aucune persistence — NFR3, FR18)

### Story 3.5: Résolveur DNS interne côté relais avec blocklist StevenBlack

As a opérateur,
I want que mon relais résolve les requêtes DNS en interne avec validation DNSSEC et application d'une blocklist,
So that les clients bénéficient d'une protection malware sans que mon relais ne logge les noms résolus.

**Acceptance Criteria:**

**Given** un paquet UDP/53 ou TCP/53 arrive via /tunnel
**When** le moteur intercepte le paquet DNS
**Then** la résolution est faite via Cloudflare 1.1.1.1 (primaire) avec failover Quad9 9.9.9.9
**And** la signature DNSSEC est validée avant forwarding (NFR9f)
**And** la blocklist StevenBlack/hosts (chargée en mémoire au démarrage, refresh 24h) est appliquée — domaines bloqués retournent NXDOMAIN (FR8b)
**And** aucun nom de domaine résolu n'est écrit dans les logs (NFR22a)

**Given** la résolution upstream échoue ou DNSSEC invalide
**When** la réponse est anormale
**Then** SERVFAIL est retourné au client
**And** un log opérationnel (sans nom de domaine) compte les échecs

### Story 3.6: Rate limiting par IP source + bandwidth quota journalier

As a opérateur,
I want limiter chaque IP source à 200 tunnels simultanés et 10 GiB/jour + 1 GiB/heure,
So that mon relais résiste aux abus et au DDoS via multiplication de tunnels.

**Acceptance Criteria:**

**Given** une IP source atteint 200 tunnels simultanés
**When** elle tente d'ouvrir un 201ème
**Then** la requête est rejetée avec HTTP 429
**And** le compteur est tenu via `sync.Map` lock-free + atomics

**Given** une IP source dépasse 10 GiB transférés sur 24h glissantes
**When** un nouveau paquet arrive
**Then** la requête est rejetée avec HTTP 429
**And** le quota est réinitialisé via fenêtre glissante

**Given** une IP source dépasse 1 GiB transférés sur 1h glissante
**When** un nouveau paquet arrive
**Then** la requête est throttle (rate-limited) plutôt que rejetée
**And** un événement opérationnel (sans IP) est compté

### Story 3.7: Limite globale tunnels par relais (HTTP 503)

As a opérateur,
I want limiter le nombre total de tunnels simultanés à 150 par relais,
So that la capacité du relais soit bornée et que les clients basculent vers un autre relais.

**Acceptance Criteria:**

**Given** le compteur global de tunnels actifs atteint 150
**When** un client tente d'ouvrir un nouveau tunnel via /tunnel
**Then** la requête est rejetée avec HTTP 503
**And** le client est attendu pour basculer via le failover (Epic 4)
**And** l'endpoint `/health` reflète le compteur : `{"tunnels": 150, ...}`

### Story 3.8: Organisation des relais par pays (DE/ES/GB/US ≥ 2 relais)

As a opérateur,
I want déployer plusieurs relais dans les pays prioritaires (DE, ES, GB, US),
So that le failover intra-pays soit possible et que la latence soit minimisée par géolocalisation.

**Acceptance Criteria:**

**Given** un pays prioritaire (DE, ES, GB, US)
**When** le registre est généré via `cmd/genregistry`
**Then** au moins 2 relais sont listés pour ce pays (ex: `de-01`, `de-02`)
**And** chaque relais a son sous-domaine Cloudflare propre (ex: `de.levoile.dev`, `de2.levoile.dev`)

**Given** un pays secondaire
**When** le registre est généré
**Then** au moins 1 relais est listé
**And** le code pays ISO 3166 alpha-2 est extrait du relay ID ou du domaine

---

## Epic 4: Découverte & Failover Multi-Pays

Le client découvre les relais via un registre distribué signé Ed25519, sélectionne par pays, distribue via round-robin, et bascule automatiquement vers un autre relais en cas d'échec — kill switch maintenu pendant la bascule.

### Story 4.1: Endpoint /.well-known/relay-registry.json signé Ed25519

As a opérateur,
I want que chaque relais serve l'intégralité du registre signé Ed25519 par la master key,
So that le client puisse récupérer la liste complète depuis n'importe quel relais (pas de point de défaillance unique).

**Acceptance Criteria:**

**Given** le fichier `/opt/levoile/relay-registry.json` est déployé sur chaque relais
**When** un client appelle `GET /.well-known/relay-registry.json`
**Then** le fichier JSON complet est retourné contenant : `version`, `master_public_key`, `relays[]` avec `id`, `domain`, `public_key`, `signature`, `added`
**And** chaque entry est signée par la master key Ed25519
**And** le client vérifie la signature via `crypto/subtle.ConstantTimeCompare` (NFR9c)

**Given** une signature Ed25519 invalide sur une entrée
**When** le client charge le registre
**Then** l'entrée corrompue est ignorée
**And** un log opérationnel est émis (sans donnée utilisateur)

### Story 4.2: Bootstrap via relais hardcodé au premier lancement

As a utilisateur final,
I want que le client trouve son premier relais sans fichier de registre préexistant,
So that le premier lancement post-installation fonctionne sans configuration manuelle.

**Acceptance Criteria:**

**Given** aucun cache local de registre n'existe (premier lancement)
**When** le service démarre
**Then** le domaine relais hardcodé dans la config par défaut (`relay.levoile.dev`) est utilisé
**And** la résolution DNS du domaine se fait via DoH (Cloudflare DoH ou Quad9 DoH) pour éviter le DNS poisoning du resolver système (NFR9i)
**And** le registre est récupéré depuis ce relais bootstrap puis caché localement

**Given** le cache local existe mais aucun relais ne répond au démarrage
**When** le service tente de se connecter
**Then** le cache est utilisé comme source de vérité (cold start résilient)
**And** le service tente la connexion à chaque relais du cache jusqu'à réussite

### Story 4.3: Sélection relais par pays avec round-robin intra-pays

As a utilisateur final,
I want choisir un pays via l'UI et que le client distribue les connexions entre les relais de ce pays via round-robin,
So that la charge soit répartie sans qu'un relais soit saturé seul.

**Acceptance Criteria:**

**Given** l'utilisateur sélectionne le pays "Allemagne" via l'UI
**When** le service appelle `registry.SelectRelay(country="de")`
**Then** la liste des relais actifs pour `de` est retournée triée par latence /health
**And** le round-robin distribue les nouvelles connexions sur la liste (état tenu en RAM)
**And** le pays préféré est sauvegardé dans la config TOML (`client.preferred_country = "de"`)

**Given** la latence /health est mesurée
**When** un nouveau cycle de mesure tourne
**Then** chaque relais est ping via `GET /health` avec timeout 3s
**And** le RTT médian sur 5 mesures est cache en RAM
**And** la liste est re-triée toutes les 6h ou sur changement de pays

### Story 4.4: Failover automatique avec kill switch maintenu

As a utilisateur final,
I want qu'en cas d'échec d'un relais, le client bascule automatiquement vers un autre relais du même pays sans interruption de protection,
So that je ne perde pas l'accès à internet ni ma protection.

**Acceptance Criteria:**

**Given** un tunnel actif vers `de-01`
**When** le relais répond avec timeout 3s, HTTP 503, ou perte connexion
**Then** le failover bascule vers `de-02` (relais suivant dans le pool)
**And** la bascule complète (détection + reconnect) est < 5 secondes (NFR19)
**And** le kill switch firewall reste actif pendant toute la bascule (aucune fuite)
**And** 0 paquet IP n'est perdu au-delà de la fenêtre de bascule

**Given** tous les relais d'un pays échouent
**When** le pool est épuisé
**Then** le service tente le pool d'un autre pays disponible (failover inter-pays)
**And** une alerte UI est remontée : "Tous les relais {pays} indisponibles, basculement vers {autre_pays}"

---

## Epic 5: Interface Desktop Cross-Platform (Tray + Webview)

L'utilisateur voit son état (vert/orange/rouge), sélectionne un pays via drapeaux, voit son IP visible, connecte/déconnecte via fenêtre ou tray, gère le mode dégradé, et garde le tray persistant. Écran fallback "Service non démarré" + supervision UI auto-restart.

### Story 5.1: Binaire UI unique (systray + webview + serveur HTTP local) avec affichage de statut

As a utilisateur final,
I want voir l'état de protection (connecté/en cours/déconnecté) via une fenêtre desktop avec indicateur visuel coloré,
So that je sache immédiatement si je suis protégé.

**Acceptance Criteria:**

**Given** le service Le Voile est démarré
**When** l'utilisateur ouvre la fenêtre depuis le menu tray "Ouvrir"
**Then** une fenêtre webview 420×540px frameless s'ouvre et navigue vers `http://127.0.0.1:{ui.http_port}/`
**And** l'indicateur de statut affiche la couleur correspondant à l'état (vert connecté steady, orange connecting pulse 1.5s, rouge déconnecté)
**And** la charte plateformeliberte.fr est respectée (fond `#0b1526`, bleus `#1a6fc4`/`#2a8dff`, rouge `#d42b2b`, fonts Bebas Neue/Rajdhani/Inter en woff2 embarqués)

**Given** le tunnel change d'état (connexion, perte, reconnexion)
**When** le frontend poll `fetch('/api/status')` toutes les 2 secondes
**Then** l'indicateur visuel et le message en français se mettent à jour ("Connecté — Allemagne", "Reconnexion en cours...", "Déconnecté")
**And** l'icône system tray reflète l'état (icônes embed via `//go:embed` : connected/connecting/disconnected, .ico Windows, .png Linux)

**Given** le binaire UI démarre
**When** `cmd/ui/main.go` initialise les composants
**Then** fyne.io/systray démarre sur le main thread (bloquant)
**And** un serveur HTTP local (net/http, 127.0.0.1:{port}) sert les assets frontend embarqués via `//go:embed`
**And** une API REST JSON est exposée (/api/status, /api/connect, /api/disconnect, /api/country, /api/registry, /api/leak-status, /api/update-status, /api/quit, /api/settings) qui proxie vers le service via IPC client
**And** un singleton est garanti : mutex nommé (Windows) / flock `~/.local/state/levoile/ui.lock` (Linux par utilisateur)

### Story 5.2: Affichage du pays sélectionné, du relais actif et de l'IP visible

As a utilisateur final,
I want voir le pays sélectionné, l'identifiant du relais actif et mon IP visible dans la fenêtre,
So that je puisse vérifier que ma protection fonctionne et depuis quel pays j'apparais.

**Acceptance Criteria:**

**Given** le tunnel est connecté
**When** la fenêtre webview est affichée
**Then** le pays sélectionné est affiché (drapeau emoji + nom)
**And** l'identifiant du relais actif est affiché (ex: `de-01`)
**And** l'IP visible (récupérée via `/api/status` qui appelle le relais `/ip`) est affichée
**And** un lien "Tester ma protection" pointe vers `https://plateformeliberte.fr/test-protection.html`

### Story 5.3: Sélecteur de pays avec drapeaux et nombre de relais

As a utilisateur final,
I want sélectionner mon pays via un sélecteur affichant les drapeaux et le nombre de relais disponibles,
So that je puisse choisir où apparaître en toute clarté.

**Acceptance Criteria:**

**Given** le registre des relais est chargé via `/api/registry`
**When** le sélecteur de pays est rendu (sidebar 150px)
**Then** chaque pays disponible est affiché avec drapeau emoji + nom + nombre de relais actifs
**And** le pays actuellement sélectionné est mis en évidence visuellement
**And** les 4 pays MVP (Allemagne, Espagne, Royaume-Uni, États-Unis) sont supportés

**Given** l'utilisateur clique sur un drapeau de pays différent
**When** la sélection change
**Then** `POST /api/country` envoie `{code: "is"}` au serveur HTTP local
**And** le serveur proxie via IPC `SelectCountry`
**And** le tunnel se reconnecte via un relais du nouveau pays en < 5s
**And** l'IP visible affichée se met à jour
**And** le pays préféré est sauvegardé dans la config TOML

### Story 5.4: Bouton Connect/Disconnect

As a utilisateur final,
I want pouvoir connecter/déconnecter Le Voile via un bouton dans la fenêtre,
So that je contrôle ma protection sans devoir passer par le tray.

**Acceptance Criteria:**

**Given** la fenêtre webview est ouverte et l'état est `Disconnected`
**When** l'utilisateur clique sur le bouton "Connect"
**Then** le frontend appelle `POST /api/connect`
**And** le serveur HTTP local proxie via IPC `Connect`
**And** l'indicateur de statut passe à orange (connecting), puis vert (connected)
**And** l'icône tray se met à jour simultanément

**Given** l'état est `Connected`
**When** l'utilisateur clique sur le bouton "Disconnect"
**Then** `POST /api/disconnect` est appelé
**And** la séquence Disconnect (tunnel → firewall → routing → tun) est exécutée par le service
**And** l'indicateur passe à rouge

### Story 5.5: Toggle fenêtre via clic gauche tray + Quitter via clic droit

As a utilisateur final,
I want ouvrir/fermer la fenêtre rapidement via un clic gauche sur l'icône tray, et quitter via le menu clic droit,
So that l'accès soit naturel et conforme aux conventions desktop.

**Acceptance Criteria:**

**Given** l'icône tray est visible
**When** l'utilisateur fait un clic gauche dessus
**Then** si la fenêtre webview est fermée, elle est créée et affichée
**And** si la fenêtre est ouverte mais cachée, elle est montrée et amenée au premier plan

**Given** l'utilisateur fait un clic droit sur l'icône tray
**When** le menu contextuel apparaît
**Then** les entrées "Ouvrir la fenêtre" et "Quitter" sont présentes
**And** sélectionner "Ouvrir" équivaut à un clic gauche
**And** sélectionner "Quitter" déclenche l'arrêt propre de l'UI

**Given** la fenêtre est ouverte
**When** l'utilisateur clique sur la croix de fermeture
**Then** la fenêtre webview est détruite (libération mémoire)
**And** le tray et le service continuent
**And** la protection reste active

### Story 5.6: Écran fallback "Service non démarré" + retry IPC

As a utilisateur final,
I want voir un message clair si le service n'est pas démarré, avec la commande shell pour le lancer,
So that je puisse résoudre la situation moi-même sans contacter le support.

**Acceptance Criteria:**

**Given** l'UI démarre mais ne peut pas joindre l'IPC du service (service non démarré, crash, container sans systemd)
**When** la connexion IPC échoue
**Then** un écran fixe s'affiche avec titre "Service Le Voile non démarré"
**And** sur Linux : "Le service Le Voile n'est pas démarré. Ouvrez un terminal et lancez : `sudo systemctl start levoile.service`"
**And** sur Windows : "Le service Le Voile n'est pas démarré. Ouvrez Services.msc et démarrez 'Le Voile Service', ou utilisez `sc start levoile-service` en admin"
**And** un retry de la connexion IPC est tenté toutes les 5 secondes en arrière-plan

**Given** le service est démarré entre deux retries
**When** la connexion IPC réussit
**Then** l'écran fallback disparaît
**And** l'UI normale s'affiche avec l'état actuel

### Story 5.7: Supervision UI avec auto-restart en cas de crash

As a utilisateur final,
I want que l'UI redémarre automatiquement si elle crash (ex: GNOME Shell restart, KDE Plasma crash),
So that le tray reste accessible sans intervention.

**Acceptance Criteria:**

**Given** l'UI est lancée via XDG autostart (Linux)
**When** le processus crash
**Then** un unit systemd user `levoile-ui.service` avec `Restart=on-failure` la relance automatiquement < 10 secondes

**Given** l'UI est lancée via HKCU autostart (Windows)
**When** le processus crash
**Then** un job object / Watchdog du service SCM détecte l'arrêt anormal et relance `levoile-ui.exe`
**And** la nouvelle instance respecte le singleton (mutex nommé) — pas de doublon

### Story 5.8: Quitter UI via tray sans tuer le service

As a utilisateur final,
I want pouvoir quitter l'UI tout en gardant ma protection active en arrière-plan,
So that je libère la RAM de l'UI sans perdre la protection.

**Acceptance Criteria:**

**Given** l'UI est active et le tunnel connecté
**When** l'utilisateur sélectionne "Quitter" dans le menu tray
**Then** l'UI envoie un message IPC vers le service (notification, pas commande Stop)
**And** le serveur HTTP local de l'UI s'arrête
**And** la fenêtre webview est libérée
**And** le processus UI se termine
**And** le service `levoile-service` continue de tourner (contrôlé par systemd/SCM)
**And** le tunnel reste connecté et le kill switch actif

**Given** l'utilisateur veut un arrêt complet
**When** il exécute `sudo systemctl stop levoile.service` (Linux) ou `sc stop levoile-service` (Windows)
**Then** le service termine la séquence Disconnect proprement (tunnel → firewall → routing → tun)
**And** toutes les ressources sont libérées sans résidu

### Story 5.9: Mode dégradé kill switch + indicateur visuel permanent

As a utilisateur final en mobilité (Wi-Fi public instable),
I want pouvoir désactiver temporairement le kill switch firewall pour accéder à internet en clair,
So that je puisse envoyer un email urgent quand le tunnel ne se rétablit pas, en assumant le risque.

**Acceptance Criteria:**

**Given** le tunnel ne parvient pas à se rétablir et le kill switch bloque internet
**When** l'utilisateur sélectionne "Mode dégradé" dans le menu tray
**Then** une modale de confirmation s'affiche : "Voulez-vous désactiver la protection temporairement ? Votre trafic ne sera PAS chiffré. L'icône tray deviendra rouge jusqu'à rétablissement du tunnel."
**And** sur validation, le firewall est désactivé via IPC `SetKillSwitchMode("degraded")`
**And** l'icône tray passe au rouge
**And** un bandeau rouge permanent s'affiche dans la fenêtre webview : "⚠ Mode dégradé — protection désactivée"
**And** l'état est persisté en config

**Given** le mode dégradé est actif et un nouveau tunnel se rétablit
**When** la connexion réussit
**Then** le kill switch est automatiquement réactivé
**And** l'icône tray retrouve sa couleur verte
**And** le bandeau rouge disparaît

**Given** l'utilisateur préfère la CLI
**When** il exécute `levoile-ctl killswitch off` (avec authentification token machine-local)
**Then** le mode dégradé est activé identiquement à la voie UI

---

## Epic 6: Validation Anti-Fuite (STUN + Watchdog TUN)

Le client valide en continu que la TUN capture bien le trafic via STUN Binding (RFC 5389), compare l'IP attendue, alerte et reconnecte en cas d'anomalie. Kill switch maintenu pendant les contrôles.

### Story 6.1: Émission de STUN Binding Requests via la TUN

As a utilisateur final,
I want que le client émette régulièrement des requêtes STUN Binding via la TUN pour valider que le trafic UDP passe bien par le tunnel,
So that je sois alerté si une fuite structurelle apparaît malgré la capture L3.

**Acceptance Criteria:**

**Given** le tunnel est connecté
**When** le scheduler de leakcheck s'active (intervalle configurable, défaut 10 min)
**Then** une requête STUN Binding (RFC 5389) est émise vers stun.l.google.com:19302
**And** la requête transite par l'OS qui la route via la TUN (`levoile0`)
**And** la réponse contient l'IP source vue par le serveur STUN

**Given** 3 serveurs STUN sont configurés (Google ×2, Cloudflare)
**When** un serveur ne répond pas
**Then** le suivant est essayé (failover STUN)
**And** un échec total est loggé sans bloquer l'opération

### Story 6.2: Comparaison IP STUN vs IP tunnel attendue

As a utilisateur final,
I want que le client compare l'IP retournée par STUN avec l'IP du relais attendue,
So that une divergence soit détectée comme une fuite potentielle.

**Acceptance Criteria:**

**Given** l'IP du relais actif est connue (récupérée via `/api/status`)
**When** la réponse STUN retourne l'IP source vue
**Then** la comparaison est faite
**And** si IP STUN == IP relais → état "OK" (capture L3 fonctionne)
**And** si IP STUN ≠ IP relais (ex: IP ISP) → état "LEAK_DETECTED"

**Given** la garantie structurelle (FR32) : capture L3 + kill switch firewall empêche les fuites
**When** un état LEAK_DETECTED apparaît
**Then** cela indique une mauvaise configuration, TUN down, ou un bug — pas une fuite produit normale
**And** le check est utile comme validation, pas comme défense de premier niveau

### Story 6.3: Reconnexion automatique + alerte UI + log en cas d'anomalie

As a utilisateur final,
I want qu'une anomalie détectée déclenche une reconnexion complète avec alerte visuelle,
So that le système se répare tout seul et je suis informé.

**Acceptance Criteria:**

**Given** l'état leakcheck passe à LEAK_DETECTED ou le watchdog TUN détecte une interface disparue
**When** l'anomalie est confirmée
**Then** une reconnexion complète est déclenchée (tunnel close → reset TUN → routing teardown → firewall reactivate avec nouvelle config → tunnel reconnect)
**And** l'icône tray passe à orange (alerte) pendant la procédure
**And** un bandeau webview affiche : "⚠ Anomalie détectée — reconnexion en cours"
**And** un événement est loggé dans Event Log (Windows) / journald (Linux) avec niveau WARNING (sans données utilisateur, NFR22a)
**And** le kill switch firewall reste actif pendant toute la procédure

---

## Epic 7: Distribution & Installation Cross-Platform Signée

L'utilisateur installe via installeur NSIS Windows ou paquets natifs Linux signés Ed25519 (.deb/.rpm/.apk/AUR), avec démarrage automatique du service+UI au boot, configurations persistées en TOML.

### Story 7.1: Installeur NSIS Windows (service SCM + UI autostart + Wintun DLL)

As a utilisateur final Windows,
I want installer Le Voile via un .exe NSIS qui configure tout automatiquement,
So that je n'aie aucune commande shell à exécuter.

**Acceptance Criteria:**

**Given** l'utilisateur lance `LeVoile-Setup-{version}.exe`
**When** UAC est accepté
**Then** les binaires sont copiés dans `C:\Program Files\LeVoile\` (`levoile-service.exe`, `levoile-ui.exe`, `levoile-ctl.exe`)
**And** la DLL Wintun signée Microsoft est copiée dans `C:\Program Files\LeVoile\wintun.dll`
**And** le service `levoile-service` est enregistré dans le SCM (start auto)
**And** l'UI est ajoutée à l'autostart utilisateur (`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`)
**And** des shortcuts sont créés sur le Bureau et dans le menu Démarrer
**And** le service démarre automatiquement et l'UI est lancée

**Given** l'utilisateur lance la désinstallation
**When** le script uninstall NSIS s'exécute
**Then** le service est arrêté et désenregistré
**And** les filters WFP sont supprimés
**And** l'adapter TUN/Wintun est retiré
**And** les shortcuts et fichiers sont supprimés
**And** aucun résidu en `C:\Program Files\LeVoile\` ou `%AppData%\LeVoile\` (sauf si l'utilisateur conserve sa config)

### Story 7.2: Paquets Linux .deb / .rpm / .apk via GoReleaser + nfpm

As a utilisateur final Linux,
I want installer Le Voile via le gestionnaire de paquets natif de ma distribution (apt/dnf/apk),
So that l'installation et les dépendances soient gérées comme tout autre logiciel système.

**Acceptance Criteria:**

**Given** la pipeline GoReleaser + nfpm est configurée
**When** une release est buildée
**Then** des paquets `.deb` (Debian 11+/Ubuntu 22.04+), `.rpm` (Fedora 38+/RHEL 9+), `.apk` (Alpine 3.18+) sont générés pour amd64 et arm64
**And** chaque paquet déclare ses dépendances runtime : `libwebkit2gtk-6.0-0 | libwebkit2gtk-4.1-0`, `libayatana-appindicator3-1`, `nftables`, `iproute2`

**Given** l'utilisateur exécute `sudo apt install ./levoile_{version}_amd64.deb` (ou équivalent dnf/apk)
**When** le post-install s'exécute
**Then** un user système `levoile` est créé
**And** les fichiers sont installés : `/usr/bin/levoile-service`, `/usr/bin/levoile-ui`, `/usr/bin/levoile-ctl`, `/usr/lib/systemd/system/levoile.service`, `/etc/levoile/config.toml`, `/usr/share/applications/levoile.desktop`, icônes XDG hicolor (256/128/64/48/32/16)
**And** `modprobe nf_tables` est tenté + test de fumée (NFR22)
**And** `systemctl daemon-reload` puis `systemctl enable --now levoile.service` activent le service
**And** l'unit systemd contient `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW`, `User=levoile` (NFR24)
**And** l'autostart XDG (`/etc/xdg/autostart/levoile-ui.desktop`) lance l'UI à la prochaine session

**Given** l'utilisateur exécute `sudo apt remove levoile`
**When** le pre-remove s'exécute
**Then** `systemctl disable --now levoile.service` arrête et désactive le service
**And** les fichiers sont retirés
**And** la config `/etc/levoile/config.toml` est conservée si purge n'est pas demandée (convention Debian)

### Story 7.3: PKGBUILD AUR + publication GitHub Action

As a utilisateur final Arch Linux,
I want installer Le Voile via `yay -S levoile`,
So that je suive les conventions Arch sans paquet exotique.

**Acceptance Criteria:**

**Given** une release est publiée sur GitHub
**When** le GitHub Action `aur-publish` se déclenche
**Then** le PKGBUILD `packaging/arch/PKGBUILD` est mis à jour avec la nouvelle version + checksum SHA256 du tarball release
**And** le commit est signé GPG par la clé du mainteneur (NFR22i)
**And** le PKGBUILD est pushé sur le repo AUR `levoile`
**And** les utilisateurs peuvent installer via `yay -S levoile`

**Given** un utilisateur Arch installe via `yay -S levoile`
**When** le PKGBUILD s'exécute
**Then** le tarball est vérifié contre le SHA256 + signature Ed25519
**And** les mêmes fichiers que .deb/.rpm sont installés (chemins identiques)
**And** systemd enable est exécuté

### Story 7.4: Signature Ed25519 de tous les paquets de distribution

As a utilisateur final,
I want que tout paquet d'installation soit signé Ed25519 par la master key Le Voile,
So that je sois certain qu'il vient bien des mainteneurs et n'a pas été altéré.

**Acceptance Criteria:**

**Given** la pipeline GoReleaser produit un artefact (.exe NSIS, .deb, .rpm, .apk)
**When** le hook post-build s'exécute
**Then** une signature détachée Ed25519 (via la master key, machine air-gapped/YubiKey — NFR22g) est générée pour chaque artefact
**And** les checksums SHA256 + signatures sont publiés sur la page release GitHub

**Given** un utilisateur télécharge un paquet
**When** il vérifie la signature avec la clé publique Le Voile
**Then** la vérification réussit
**And** une signature invalide entraîne le refus d'installation par les gestionnaires de paquets configurés (apt/dnf/pacman/apk repos signés)

### Story 7.5: Configuration TOML persistée avec paths cross-platform

As a utilisateur final,
I want que ma config (pays préféré, options) soit persistée dans un emplacement standard de mon OS,
So que je retrouve mes préférences au prochain démarrage.

**Acceptance Criteria:**

**Given** l'utilisateur change un paramètre via l'UI
**When** la config est sauvegardée
**Then** sur Windows : écriture atomique dans `%AppData%\LeVoile\config.toml`
**And** sur Linux user : écriture dans `~/.config/levoile/config.toml`
**And** sur Linux service : `/etc/levoile/config.toml` (via IPC qui passe par le service signant les écritures)
**And** les permissions sont 0600 (Linux) / ACL user-only (Windows) — NFR9j

**Given** la config existe au démarrage
**When** le service charge la config
**Then** le HMAC-SHA256 (clé dérivée machine-local) est vérifié — toute modification externe entraîne refus de démarrer + alerte UI (NFR9j)

**Given** la première installation
**When** le service démarre sans config existante
**Then** un squelette par défaut est créé (`config.example.toml` copié depuis l'embed)
**And** le HMAC initial est calculé et stocké

---

## Epic 8: Auto-Update Sécurisé avec Rollback

Le client se met à jour automatiquement via GitHub releases, vérifie la signature Ed25519, applique la mise à jour au prochain démarrage, et rollback automatiquement si le tunnel n'est pas établi en 30s.

### Story 8.1: Vérification périodique des releases GitHub

As a utilisateur final,
I want que le client vérifie automatiquement la disponibilité de nouvelles versions sans intervention,
So que je sois toujours sur la dernière version sécurisée.

**Acceptance Criteria:**

**Given** la config contient `[update] enabled = true, github_owner = "velia-the-veil", github_repo = "le_voile", check_interval = "6h"`
**When** le scheduler updater tourne
**Then** une requête `GET https://api.github.com/repos/{owner}/{repo}/releases/latest` est émise toutes les 6h
**And** la version retournée est comparée à la version courante (semver)
**And** si une nouvelle version est disponible, l'event est notifié à l'UI via IPC

**Given** une nouvelle version est détectée
**When** l'UI poll `/api/update-status`
**Then** une notification non-intrusive est affichée dans le tray : "Mise à jour disponible : v{nouvelle_version}"
**And** le téléchargement démarre en arrière-plan avec rate limit 500 kbps (`update.rate_limit_kbps`)

### Story 8.2: Téléchargement signé + application + rollback automatique

As a utilisateur final,
I want que la mise à jour soit téléchargée, vérifiée, appliquée au prochain démarrage, et rollback automatique en cas d'échec,
So que je ne me retrouve jamais avec un client cassé bloquant ma protection.

**Acceptance Criteria:**

**Given** une nouvelle release est téléchargée
**When** le binaire est récupéré
**Then** la signature Ed25519 du checksum SHA256 est vérifiée via `crypto/subtle.ConstantTimeCompare` (NFR9c)
**And** une signature invalide rejette la mise à jour avec log syslog/Event Log
**And** le binaire vérifié est stocké dans un emplacement temporaire prêt pour swap

**Given** une mise à jour est prête à être appliquée
**When** le service redémarre (manuel ou planifié)
**Then** le swap atomique remplace l'ancien binaire par le nouveau
**And** sur Linux : `systemctl restart levoile.service` est exécuté
**And** sur Windows : SCM redémarre le service

**Given** la nouvelle version vient d'être appliquée
**When** le service redémarre
**Then** un timer de 30 secondes démarre
**And** si le tunnel n'est pas établi dans les 30s, un rollback automatique restaure l'ancien binaire
**And** le service redémarre sur l'ancienne version
**And** un log syslog/Event Log enregistre l'échec sans données utilisateur

**Given** le swap atomique échoue (disque plein, permissions, écriture bloquée)
**When** l'erreur est détectée
**Then** le service continue sur l'ancienne version sans interruption
**And** l'UI notifie l'utilisateur ("Mise à jour échouée — sera retentée à la prochaine occasion")
**And** un retry est planifié au prochain check_interval
