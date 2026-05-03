---
stepsCompleted: ['step-01-validate-prerequisites', 'step-02-design-epics', 'step-03-create-stories', 'step-04-final-validation']
inputDocuments: ['prd.md', 'architecture.md', 'ux-design-specification.md']
lastEdited: '2026-05-02'
editHistory:
  - date: '2026-05-02'
    changes: 'Extension Phase 2 Android — préservation des 8 epics existants (Phase 1 Windows + Linux). Ajout au Requirements Inventory : FR-AND-1..10 (10 FRs) + NFR-AND-1..11 (11 NFRs) + Additional Requirements Android (ADR-08..15, journeys J6-J8, composants C13-C17). Sources : PRD 2026-04-30, Architecture 2026-04-29, UX 2026-05-02. Étape 1 (validate-prerequisites) terminée ; étapes 2-4 à régénérer pour ajouter epics Phase 2 Android (Epic 9+) et étendre FR Coverage Map.'
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

**Phase 2 — Android (FR-AND-1 à FR-AND-10)**

> Ajoutés lors de l'extension Phase 2 Android (2026-05-02). Sources : PRD 2026-04-30 §9 « Phase 2 — Android », Architecture 2026-04-29 ADR-08..15, UX 2026-05-02 §J6-J8 + §C13-C17.

- FR-AND-1: Le client Android peut établir un tunnel via `android.net.VpnService` (Builder pattern : `mtu(1420)`, `addRoute("0.0.0.0", 0)`, `addAddress(tun_ip, 24)`). L'interface TUN est créée par `establish()` et le file descriptor utilisé pour lire/écrire les paquets IP bruts via `FileInputStream`/`FileOutputStream`
- FR-AND-2: Le client Android peut héberger le tunnel dans un Foreground Service (`LeVoileVpnService` héritant de `VpnService`) avec notification persistante ongoing (channel `levoile_vpn_status`, importance LOW, non-dismissable, action « Déconnecter » via PendingIntent `FLAG_IMMUTABLE`). La notification affiche statut + pays + IP visible
- FR-AND-3: Le client Android peut afficher au premier lancement un onboarding obligatoire incitant l'utilisateur à activer « VPN permanent + bloquer connexions sans VPN » dans Settings via deeplink `Settings.ACTION_VPN_SETTINGS`. L'app détecte au lancement si le toggle est actif via heuristique `Settings.Global` et affiche un warning persistant UI (bandeau rouge non-dismissable) tant que non-activé
- FR-AND-4: Le client Android peut héberger l'UI dans une `MainActivity` avec `WebView` plein écran chargeant les mêmes assets HTML/CSS/JS que desktop via `WebViewAssetLoader` (synchronisés au build via `android/scripts/sync-frontend.sh`). Layout responsive mobile (media queries : sélecteur pays vertical, pas de sidebar, boutons tactiles ≥ 48dp)
- FR-AND-5: Le client Android peut exposer les commandes natives au frontend JS via `@JavascriptInterface` (méthodes `connect()`, `disconnect()`, `getStatus()`, `selectCountry(iso)`), sérialisation JSON ligne par ligne, max 4 Ko par message. Pas de serveur HTTP local (limitation Android non-rooté + posture sécurité)
- FR-AND-6: Le client Android peut détecter au lancement la présence d'une autre app VPN active via `VpnService.prepare()` retournant un `Intent` non-null. Si détecté, refus de démarrer le tunnel avec message UI explicite : « Une autre application VPN est active sur cet appareil. Désactivez-la pour utiliser Le Voile. »
- FR-AND-7: Le client Android peut être distribué via F-Droid (catalogue officiel, métadonnées XML versionnées dans le repo) avec build reproductible obligatoire (vérifié par fingerprinting : 2 builds successifs depuis le même tag git produisent un APK avec hash SHA256 identique) et APK direct via GitHub releases signé v2 (APK Signature Scheme v2) + v3 (key rotation) par la master key Ed25519
- FR-AND-8: Le client Android n'embarque aucune télémétrie, aucun crash reporter (pas de Firebase Crashlytics, pas de Sentry, pas de Bugsnag, pas de Mixpanel/Adjust/Branch), aucune analytics maison. Audit dépendances Gradle vérifie l'absence de ces modules. Bug reports utilisateur via export texte manuel local (sans IP, sans identifiant, sans données utilisateur)
- FR-AND-9: Le client Android peut notifier l'utilisateur de la disponibilité d'une nouvelle version. Vérification au lancement de l'app + vérification périodique en arrière-plan toutes les 24h via `WorkManager` (cohérent FR35 desktop). Pour APK direct : notification UI « Mise à jour {version} disponible » + lien GitHub releases (pas d'auto-update embarqué — limitation Android non-rooté + posture sécurité). Pour F-Droid : la vérification embarquée est désactivée (la mise à jour est gérée nativement par le client F-Droid de l'utilisateur)
- FR-AND-10: La configuration utilisateur Android (pays préféré, préférences UI, registre relais cache, dernière clé publique Ed25519 vérifiée) est stockée en JSON dans `getFilesDir()` (scoped storage natif AndroidX, accessible uniquement à l'UID de l'app — équivalent sécurité aux permissions 0600 desktop). Pas de TOML sur Android (convention écosystème AndroidX)

### NonFunctional Requirements

**Security (NFR1-9j)**

- NFR1: Communications client-relais chiffrées via QUIC/HTTPS (TLS 1.3 minimum)
- NFR2: Authentification client-relais exclusivement Ed25519 via les bibliothèques standards Go (`crypto/ed25519`) + TLS via quic-go (TLS 1.3 standard Go). Aucune crypto maison. Une paire Ed25519 unique par relais
- NFR3: Les relais ne persistent aucune donnée au-delà de la durée d'une requête. NAT table en RAM uniquement, TTL ≤ 300s TCP / ≤ 120s UDP. Éviction automatique
- NFR4: Trafic tunnel indiscernable du trafic HTTPS standard par analyse DPI — vérifié par capture Wireshark (0 pattern-match VPN sur 100 échantillons)
- NFR5: Aucune fuite DNS pendant le fonctionnement normal, la reconnexion ou le failover — garanti structurellement par la capture L3 + kill switch firewall
- NFR6: Interface TUN/Wintun, routes système et règles firewall restaurées dans tous les scénarios (désactivation, crash, shutdown). Zéro résidu
- NFR7: Code source publiquement auditable sur GitHub
- NFR8: **Retiré** (pivot 2026-04-19) — l'ancienne validation des plages IP Cloudflare est obsolète depuis la suppression du CDN intermédiaire. Protection anti-abus assurée par rate-limit + bandwidth quota par IP client (FR30, FR30b)
- NFR9: Les relais bloquent les paquets IP vers les réseaux privés (loopback, RFC 1918, link-local) — protection SSRF
- NFR9b: Kill switch firewall OS-level (nftables/WFP) survit aux crashes du process service — aucune fuite possible même en cas de défaillance applicative
- NFR9c: Toutes les comparaisons cryptographiques utilisent `crypto/subtle.ConstantTimeCompare` — résistance aux timing attacks
- NFR9d: Le relais vérifie que l'IP hash (SHA256) du session token correspond à l'IP source du socket (`r.RemoteAddr`) — rejet immédiat si différent, empêche le replay depuis une autre IP
- NFR9e: TLS direct entre le client et le VPS relais, TLS 1.3 minimum, certificat Let's Encrypt servi depuis l'origin (pas de terminaison CDN intermédiaire)
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

**Phase 2 — Android (NFR-AND-1 à NFR-AND-11)**

> Ajoutés lors de l'extension Phase 2 Android (2026-05-02). Sources : PRD 2026-04-30 §10 « Phase 2 — Android », Architecture 2026-04-29 ADR-08..15.

- NFR-AND-1: Consommation RAM client Android < 60 MB en fonctionnement normal (mesuré via `adb shell dumpsys meminfo fr.plateformeliberte.levoile`, agrégé Java heap + native heap + system)
- NFR-AND-2: Établissement tunnel Android (consent VpnService + Builder + `establish()` + handshake QUIC) < 3 secondes sur Pixel 6+ ou équivalent (Snapdragon 7-gen 1+) sur réseau LTE/4G+ ou Wi-Fi domestique avec RTT < 80ms vers le VPS relais — mesuré par chronométrage applicatif (Trace API Android). Parallèle fonctionnel à NFR11 desktop (ADSL/fibre, RTT < 50ms)
- NFR-AND-3: Taille APK signé < 25 MB (mesuré via `apkanalyzer apk file-size`). Inclut le `.aar` gomobile + assets HTML/CSS/JS partagés desktop
- NFR-AND-4: `minSdk = 29` (Android 10+), `targetSdk = 34` (Android 14+). Couvre ~80% du parc actif fin 2026. Refus explicite Android 8/9 (corner cases sécu/VPN ne valant pas l'effort)
- NFR-AND-5: APK signé v2 (APK Signature Scheme v2) + v3 (key rotation) par la master key Ed25519 (cohérent NFR22g). Vérification automatique au install par PackageManager Android — refus d'install si signature invalide ou altérée
- NFR-AND-6: Build F-Droid reproductible — 2 builds successifs depuis le même tag git produisent un APK avec hash SHA256 identique. Vérifiable manuellement par tout utilisateur via `sha256sum apk-fdroid.apk apk-github.apk` (procédure documentée dans le repo)
- NFR-AND-7: Aucune permission Android dangereuse. Permissions déclarées dans `AndroidManifest.xml` : `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC` (API 34+), `POST_NOTIFICATIONS` (API 33+), `BIND_VPN_SERVICE`. Auditable via `apkanalyzer manifest permissions` (assertion CI). **Permission acceptée hors-liste (auto-injectée par AGP 8+) :** `<applicationId>.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` (`fr.plateformeliberte.levoile.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` en release / `fr.plateformeliberte.levoile.debug.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` en debug). Custom (préfixée par notre `applicationId`, surface d'attaque nulle), invisible utilisateur (pas dans la liste UI à l'install), bénigne (sécurité dynamique `BroadcastReceiver` Android 13+ recommandée Google). L'assertion CI doit l'autoriser explicitement
- NFR-AND-8: Aucune télémétrie / analytics / crash reporter (cohérent ADR-15 et NFR20). Audit dépendances Gradle (`gradle dependencies`) bloqué en CI si présence de modules : Firebase, Sentry, Bugsnag, Crashlytics, Mixpanel, Adjust, Branch, Amplitude
- NFR-AND-9: Logs Android via `android.util.Log` filtrés par buildType — release : niveau WARN+ uniquement, debug : INFO+. Aucune URL, aucun nom de domaine, aucune destination IP, aucun contenu utilisateur loggué (cohérent NFR22a). Logcat sortant filtré côté code (pas de log par défaut « user-facing data »)
- NFR-AND-10: Tests instrumentés Android via Espresso + AndroidX Test sur émulateur API 29 + 33 + 34 — matrice e2e (consent VpnService, démarrage VpnService, kill switch via heuristique « VPN permanent », UI flow, Connect/Disconnect, failover, notification persistante) à 100% de passing avant release (cohérent NFR22)
- NFR-AND-11: Code Android obfuscation release : R8/ProGuard activé (`minifyEnabled true` + `proguard-android-optimize.txt`) — équivalent fonctionnel du strip `-ldflags="-s -w"` côté Go natif desktop (cohérent NFR9h). Configuration ProGuard préservant les classes JNI exposées par gomobile (`.aar`)

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
- **Android 10+ (API 29+, Phase 2)** — minSdk 29, targetSdk 34. Cible Pixel 6+ recommandée. Hors scope : iOS, Wear OS, Android TV, Android Auto
- macOS différé Phase 3

---

#### Phase 2 — Android : Exigences additionnelles

> Ajoutées lors de l'extension Phase 2 Android (2026-05-02). Sources : Architecture 2026-04-29 ADR-08..15, UX 2026-05-02 §J6-J8 + §C13-C17.

**Architecture — Stack & Bibliothèques Android**

- gomobile (`golang.org/x/mobile/cmd/gomobile bind -target=android`) : noyau Go partagé compilé en `.aar` consommable par Gradle
- 5 shims gomobile sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` (cf. erratum architecture ADR-09 Story 9.2 — `protocol`/`auth` canoniques, les 3 autres consommant en lecture les packages racine `internal/{crypto,registry,leakcheck,tunnel}`)
- AndroidX : `androidx.webkit` (WebViewAssetLoader), `androidx.work` (WorkManager), `androidx.core` (NotificationCompat)
- Build système : Gradle + Android Gradle Plugin. Script de build dédié `android/scripts/build-aar.sh` (séparé de Gradle pour garder la frontière noyau/UI claire)

**Architecture — Modèle Mono-Processus Android (vs 2 processus desktop)**

- Architecture mono-processus (cohérent ADR-08 isolation OS) : `MainActivity` (UI WebView) + `LeVoileVpnService` (Foreground Service + VpnService) dans le même process, communication via JS Bridge `@JavascriptInterface`
- Singleton implicite : Android impose 1 seule app VPN active via `VpnService.prepare()` (pas besoin de mutex applicatif comme desktop)
- Pas de service runner kardianos, pas de tray fyne.io/systray, pas de webview/webview, pas d'IPC named pipes/Unix sockets — tout est remplacé par les primitives Android natives

**Architecture — Capture L3 & Kill Switch Android**

- Capture L3 : `android.net.VpnService.Builder` (seule API non-rootée) — MTU 1420, route `0.0.0.0/0`, addAddress local
- Kill switch : **délégué à l'OS** via réglage utilisateur Android « VPN permanent + bloquer connexions sans VPN » (`Settings.ACTION_VPN_SETTINGS`) — kernel-level, inviolable côté app, persiste aux crashes/redémarrages
- Détection état kill switch via heuristique non-publique (`Settings.Global`) — fragile, fallback « non vérifiable » documenté
- Pas de simulation kill switch app-level (reconnect-loop, drop sockets) — refusée par ADR-10
- Pas de filtres iptables/eBPF côté app (Android non-rooté ne permet pas)

**Architecture — Lifecycle Android**

- Activity destruction (swipe close) **n'arrête pas** le Foreground Service — la notification persistante reste visible, le tunnel reste actif
- Pas d'autostart au boot (limitation Android 10+ sur les `BootCompletedReceiver` pour apps en background) — l'utilisateur lance l'app manuellement après reboot
- Atténuation reboot : le réglage OS « VPN permanent » reconnecte le dernier VPN actif au boot avant que l'utilisateur n'ouvre l'app
- Crash service : `START_REDELIVER_INTENT` du Foreground Service relance automatiquement
- Battery save agressif (OEM Xiaomi/Huawei/Oppo) : Foreground Service exempt sur Android 8+. Documentation : whitelist battery conseillée pour OEM agressifs

**Architecture — Distribution Android**

- F-Droid : catalogue officiel logiciel libre, métadonnées XML versionnées dans le repo, build reproductible obligatoire (vérifié par fingerprinting hash APK)
- APK direct : GitHub releases signé v2 (APK Signature Scheme v2) + v3 (key rotation) par master key Ed25519
- Pas de Google Play en MVP/Phase 2 (ToS Play Store sur les VPN apps trop restrictives, contradiction posture OSS — Phase 3+ si traction)
- Mises à jour : F-Droid → géré par client F-Droid utilisateur. APK direct → notification UI + lien GitHub (pas d'auto-update embarqué)

**Architecture — Permissions Android Minimales**

- `INTERNET` — accès réseau pour le tunnel
- `FOREGROUND_SERVICE` + `FOREGROUND_SERVICE_DATA_SYNC` (API 34+) — Foreground Service obligatoire pour héberger VpnService long-lived
- `POST_NOTIFICATIONS` (API 33+) — notification persistante du Foreground Service (demandée juste-à-temps au démarrage du Service, pas au boot de l'app)
- `BIND_VPN_SERVICE` — déclaration du service VpnService (consent utilisateur via popup système Android natif)
- **Aucune permission dangereuse** (pas de `READ_PHONE_STATE`, pas de `ACCESS_FINE_LOCATION`, etc.)

**Architecture — Modules Android à créer**

- **Nouveau module Gradle Android** (arbre dédié `android/`) : `MainActivity`, `OnboardingActivity`, `LeVoileVpnService`, JS Bridge `@JavascriptInterface`, AndroidManifest.xml, ressources (strings, drawables vector)
- **Scripts de build** : `android/scripts/build-aar.sh` (gomobile bind), `android/scripts/sync-frontend.sh` (copie assets HTML/CSS/JS desktop)
- **Métadonnées F-Droid** : `metadata/fr.plateformeliberte.levoile.yml` versionné dans le repo
- **CI Android** : workflows GitHub Actions séparés (gradle lint, unit tests, instrumented tests émulateur API 29 + 33 + 34, audit dépendances Gradle, scan ProGuard, vérif reproductibilité APK)

**UX Design — Exigences additionnelles Android**

- Persona principal Phase 2 : **Léa** (29 ans, communicante, Pixel 7 Android 14, grand public — cherche un VPN simple sur mobile)
- 3 nouveaux journeys :
  - **J6** : Premier lancement Android avec onboarding obligatoire « VPN permanent » (3 écrans : bienvenue + consent VpnService + activation kill switch via deeplink Settings)
  - **J7** : Swipe-close de l'app (Activity détruite, Foreground Service continue, notification persistante reste, tunnel actif)
  - **J8** : Mise à jour Android (F-Droid auto vs APK direct manuel via notification UI)
- 5 nouveaux composants Android (en plus des C1-C12 desktop) :
  - **C13** AppBar Android Material 56dp (titre « LE VOILE » + burger + cloche + overflow)
  - **C14** Country Selector Bottom-Sheet (slide-up 250ms, drag handle, 4 pays verticaux)
  - **C15** Onboarding Kill Switch Screen (CTA « OUVRIR LES PARAMÈTRES » + lien discret « Continuer sans (déconseillé) »)
  - **C16** Foreground Service Notification (channel `levoile_vpn_status` LOW, ongoing non-dismissable, action « DÉCONNECTER »)
  - **C17** Bandeau alerte kill switch persistant rouge (visible si « VPN permanent » non activé)
- **Pas de mutualisation CSS desktop ↔ Android** : composants desktop désactivés via `body.platform-android .desktop-only { display: none }` ; markup Android dédié `.android-*` (cohérent ADR-08)
- Layout responsive mobile : sélecteur pays vertical, pas de sidebar, cibles tactiles ≥ 48dp partout
- Pas de bouton « Quitter » UI (pattern Android : back/home = background, action « Déconnecter » via notification ou Réglages → Apps)
- Strings utilisateurs : `R.string.*` côté Kotlin natif (notification, onboarding, AppBar) ; dictionnaire JS chargé selon `navigator.language` côté HTML

**UX — Accessibilité Android (RGAA AA + TalkBack)**

- TalkBack obligatoire : navigation séquentielle WebView sans piège, focus order cohérent
- Cibles tactiles ≥ 48dp (Material guidelines)
- `setContentDescription` complet pour notification Foreground Service
- `aria-live="assertive"` sur bandeau alerte kill switch (annoncé immédiatement à l'apparition)
- `aria-live="polite"` sur statut tunnel
- Test TalkBack avant release sur émulateur API 29 + 33 + 34

**Tests & Qualité Android**

- Tests instrumentés Espresso + AndroidX Test sur émulateur API 29 + 33 + 34 (matrice e2e à 100% passing avant release)
- Tests battery : Foreground Service survit en battery save (Android natif + simulation OEM agressif Xiaomi/Huawei via flags)
- Vérif reproductibilité F-Droid : 2 builds successifs depuis le même tag git → APK hash SHA256 identique
- Audit permissions : `apkanalyzer manifest permissions` (assertion CI : aucune permission dangereuse)
- Audit dépendances Gradle : assertion CI absence des modules Firebase, Sentry, Bugsnag, Crashlytics, Mixpanel, Adjust, Branch, Amplitude
- Lifecycle Foreground Service : `startForeground()` appelé < 5s après `onStartCommand()` sans exception (sinon ANR système)

**Risques Phase 2 Android**

- Utilisateur n'active pas « VPN permanent » Android → kill switch absent (mitigation : onboarding obligatoire + warning persistant + détection heuristique)
- Autre app VPN active (Tailscale, Wireguard, OpenVPN) prend le slot → Le Voile ne peut pas établir le tunnel (mitigation : détection au lancement via `VpnService.prepare()`, message UI explicite)
- OS Android tue le Foreground Service en battery save agressif (OEM Xiaomi/Huawei/Oppo) → tunnel coupé sans avertissement (mitigation : Foreground Service exempt depuis Android 8+, doc whitelist battery)
- F-Droid maintainer compromise → distribution APK F-Droid altérée (mitigation : build reproductible permet à tout utilisateur de vérifier `sha256(APK F-Droid) == sha256(APK GitHub)`)
- gomobile cadence release lente (maintenu par Google) → dépendance externe critique pour le bridge Go ↔ Java (mitigation : version épinglée, fallback réécriture Kotlin documenté Phase 3+)

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

**Phase 2 — Android (FR-AND-1 à FR-AND-10)**

- FR-AND-1: Epic 9 — Tunnel via VpnService Builder + establish() + FileInputStream/FileOutputStream
- FR-AND-2: Epic 9 — Foreground Service LeVoileVpnService + notification persistante MVP (statut texte minimal — voir EBR-01)
- FR-AND-3: Epic 11 — Onboarding obligatoire « VPN permanent » + deeplink Settings (migré depuis Epic 10 — voir EBR-02)
- FR-AND-4: Epic 11 — MainActivity + WebView plein écran + WebViewAssetLoader (assets desktop sync)
- FR-AND-5: Epic 11 — JS Bridge `@JavascriptInterface` (connect/disconnect/getStatus/selectCountry)
- FR-AND-6: Epic 10 — Détection autre app VPN via VpnService.prepare() (Intent non-null)
- FR-AND-7: Epic 12 — Distribution F-Droid (build reproductible) + APK direct GitHub (signé v2/v3)
- FR-AND-8: Epic 10 — Zéro télémétrie/crash reporter/analytics (audit Gradle CI bloquant)
- FR-AND-9: Epic 12 — Notification mise à jour + WorkManager 24h (APK direct uniquement)
- FR-AND-10: Epic 11 — Config JSON dans `getFilesDir()` (scoped storage AndroidX UID-only)

**Notes de couverture :**
- Composant C16 (notification persistante) : Epic 9 livre version MVP (statut texte), Epic 11 enrichit dynamiquement (pays + IP + action)
- Heuristique `Settings.Global.always_on_vpn_app` (détection état kill switch) + bandeau C17 : Epic 10
- Onboarding C15 + persistence `onboarding_completed` SharedPreferences : Epic 11

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

---

### Phase 2 — Android (Epics 9-12)

> Ajoutés lors de l'extension Phase 2 Android (2026-05-02). Décomposition en 4 epics organisés autour de la valeur utilisateur Léa (PRD §9 Parcours 9 + UX §J6-J8). Frontières formalisées via mini-ADRs « Epic-Boundary » (EBR-01, EBR-02, EBR-03 — voir notes de session). Isolation OS maximale (ADR-08) — arbre `android/` autonome, aucun chevauchement avec Epics 1-8 desktop.

### Epic 9: Noyau Android — VpnService + Foreground Service + Bridge gomobile
Léa lance Le Voile pour la première fois, le popup système Android « autorisation VPN » apparaît, elle accepte, le tunnel s'établit en moins de 3 secondes vers le pays par défaut (Allemagne). Elle voit son IP allemande dans l'app et dans la notification persistante de la barre de statut (statut texte minimal : « Le Voile · Connecté »). Elle ferme l'app par swipe → la notification reste, le tunnel reste actif (Foreground Service). Elle utilise Chrome, WhatsApp, Insta — protégée en arrière-plan.
**FRs covered:** FR-AND-1, FR-AND-2 (notification MVP — voir EBR-01)
**NFRs validés:** NFR-AND-1 (RAM < 60 MB), NFR-AND-3 (APK < 25 MB), NFR-AND-4 (minSdk 29 / targetSdk 34), NFR-AND-11 (R8/ProGuard)

### Epic 10: Kill Switch OS-Délégué + Conflit VPN + Zéro Télémétrie
Léa est protégée même au reconnect ou au boot grâce au réglage Android « VPN permanent » détecté par heuristique `Settings.Global` ; un bandeau rouge persistant l'alerte si le toggle n'est pas actif. Si une autre app VPN est active (Tailscale, Wireguard, OpenVPN), elle voit un message explicite avant toute connexion. L'app n'envoie aucune télémétrie ni aucun crash report — auditable par toute personne via `apkanalyzer manifest permissions` et `gradle dependencies` (assertion CI bloquante). Les logs `android.util.Log` ne contiennent aucune URL, IP, ni domaine.
**FRs covered:** FR-AND-6, FR-AND-8
**NFRs validés:** NFR-AND-7 (permissions minimales), NFR-AND-8 (audit dépendances Gradle bloquant), NFR-AND-9 (logs filtrés)

### Epic 11: UI Mobile + Onboarding « VPN permanent » (Activity + WebView + Composants Material)
Léa interagit avec l'app via une UI plein écran reprenant la charte plateformeliberte.fr adaptée mobile : AppBar Material 56dp, sélecteur pays bottom-sheet slide-up, statut central, IP visible. Au premier lancement, elle traverse un onboarding obligatoire en 3 écrans (bienvenue + consent VpnService + activation « VPN permanent » via deeplink Settings) qui la guide jusqu'à activer le kill switch OS. Toutes les interactions (sélectionner pays, connect/disconnect, voir statut) passent par un JS Bridge `@JavascriptInterface` — pas de serveur HTTP local. Sa configuration (pays favori, préférences UI, `onboarding_completed`) est persistée dans `getFilesDir()` (scoped storage AndroidX, UID-only).
**FRs covered:** FR-AND-3, FR-AND-4, FR-AND-5, FR-AND-10 (onboarding migré depuis Epic 10 — voir EBR-02)
**Composants UX:** C13 (AppBar), C14 (Bottom-Sheet), C15 (Onboarding Kill Switch), C16 enrichi (notification dynamique pays + IP + action), C17 (Bandeau alerte)

### Epic 12: Distribution F-Droid + APK Direct + Auto-Update + Tests Instrumentés
Léa découvre Le Voile via plateformeliberte.fr → page Android : F-Droid (recommandé, build reproductible vérifiable) ou APK direct GitHub releases (signé v2 + v3, vérifié par PackageManager). Lors d'une nouvelle release : son client F-Droid lui propose la mise à jour ; sur APK direct, l'app affiche une notification UI « Mise à jour {version} disponible » avec lien GitHub (pas d'auto-update embarqué — limitation Android non-rooté + posture sécurité). Le pipeline CI valide chaque release par tests Espresso sur émulateur API 29 + 33 + 34, vérification reproductibilité APK (hash SHA256 stable), et établissement tunnel < 3s sur Pixel 6+.
**FRs covered:** FR-AND-7, FR-AND-9
**NFRs validés:** NFR-AND-2 (tunnel < 3s sur Pixel 6+ LTE/4G+ Wi-Fi RTT < 80ms), NFR-AND-5 (signature v2/v3 + key rotation), NFR-AND-6 (build reproductible), NFR-AND-10 (tests Espresso API 29/33/34 100% passing)

### Notes de planning sprint (issu de l'élicitation comparative)

Tailles relatives anticipées (à valider à l'étape 3) :
- **Lourds** (≥ 7 stories) : Epic 2 (capture L3 + firewall + 2 OS), Epic 3 (relais + NAT + auth + DNS), Epic 5 (UI complète), **Epic 9** (noyau Android : aar + module Gradle + Activity squelette + VpnService + FG Service + JS Bridge minimal + lifecycle + intégration noyau Go), **Epic 11** (UI Mobile + Onboarding 3 écrans + 5 composants Material + JS Bridge complet + config JSON — re-sizing post-EBR-02)
- **Moyens** (4-6 stories) : Epic 1, Epic 4, Epic 7, **Epic 10** (heuristique kill switch + bandeau + VPN concurrent + audit CI + filtrage logs), **Epic 12** (metadata F-Droid + signature v2/v3 + reproductibilité + WorkManager update + notif UI + matrice Espresso)
- **Légers** (2-3 stories) : Epic 6, Epic 8

Le refactor majeur (Windows-stable → Linux + TUN) est isolé dans Epic 2 et Epic 3. La Phase 2 Android (Epics 9-12) est livrable indépendamment des Epics 1-8 — arbre de code séparé, dépendance unique au noyau Go partagé via `.aar` gomobile.

**Séquence de livraison Phase 2 recommandée :** Epic 9 (foundation, livre un VPN qui marche en mode minimal) → Epic 11 (UI riche + onboarding obligatoire — la séquence Léa #9 devient navigable) → Epic 10 (durcissement sécurité OS-délégué + audits) → Epic 12 (release pipeline + canaux distribution).

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

---

## Epic 9: Noyau Android — VpnService + Foreground Service + Bridge gomobile

Léa lance Le Voile pour la première fois, le popup système Android « autorisation VPN » apparaît, elle accepte, le tunnel s'établit en moins de 3 secondes vers le pays par défaut. Elle voit son IP allemande dans la notification persistante. Swipe-close → tunnel reste actif (Foreground Service).

### Story 9.1: Module Gradle Android `android/` + structure projet

As a développeur,
I want un module Gradle Android autonome dans le sous-dossier `android/` du repo,
So that je puisse compiler un APK Le Voile signable et installable, isolé des arbres Windows/Linux (cohérent ADR-08 isolation OS).

**Acceptance Criteria:**

**Given** le repo contient les arbres `windows/`, `linux/`, `internal/`, `cmd/`
**When** un développeur initialise le module Android
**Then** un nouveau sous-dossier `android/` apparaît à la racine du repo avec `settings.gradle.kts`, `build.gradle.kts` (top-level), `app/build.gradle.kts` (module app), `app/src/main/AndroidManifest.xml`
**And** `minSdk = 29`, `targetSdk = 34`, `compileSdk = 34`, `applicationId = "fr.plateformeliberte.levoile"`, `namespace = "fr.plateformeliberte.levoile"` sont déclarés (NFR-AND-4)
**And** `gradle build` produit un APK debug installable sur émulateur API 29 + 33 + 34

**Given** le module Android est en place
**When** le projet est buildé en release
**Then** `minifyEnabled true` est activé (NFR-AND-11)
**And** `proguard-android-optimize.txt` + `proguard-rules.pro` (rules JNI gomobile préservées) sont référencés
**And** la taille APK release < 25 MB mesurée via `apkanalyzer apk file-size` (NFR-AND-3)

**Given** AndroidManifest.xml minimal
**When** les permissions sont déclarées
**Then** seules les permissions suivantes sont présentes : `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `POST_NOTIFICATIONS`, `BIND_VPN_SERVICE` (NFR-AND-7)
**And** aucune permission dangereuse (READ_PHONE_STATE, ACCESS_FINE_LOCATION, etc.) n'est déclarée
**And** `apkanalyzer manifest permissions` ne révèle aucune permission au-delà de cette liste

### Story 9.2: Script `build-aar.sh` — gomobile bind du noyau Go partagé

As a développeur,
I want un script reproductible qui compile le noyau Go partagé en `.aar` consommable Gradle,
So que la logique protocole/crypto/registre/session du desktop soit réutilisée 100% côté Android (ADR-09).

**Acceptance Criteria:**

**Given** les 5 shims gomobile-compatibles existent (`android/shims/{protocol,auth,crypto,registry,leakcheck}/` — cf. erratum architecture ADR-09 : `protocol`/`auth` canoniques, les 3 autres consommant en lecture `internal/{crypto,registry,leakcheck,tunnel}` racine)
**When** le développeur exécute `bash android/scripts/build-aar.sh` (Linux/macOS) ou `pwsh android/scripts/build-aar.ps1` (Windows)
**Then** le script vérifie que `gomobile` est installé (`go install golang.org/x/mobile/cmd/gomobile@latest` documenté dans `android/README-android.md`)
**And** invoque `gomobile bind -target=android -androidapi=29 -javapkg=fr.plateformeliberte.levoile.core -o android/levoile-core/libs/levoile-core.aar ./android/shims/protocol ./android/shims/auth ./android/shims/crypto ./android/shims/registry ./android/shims/leakcheck`
**And** le `.aar` produit est consommable par `levoile-core/build.gradle.kts` via `api(files("libs/levoile-core.aar"))` et propagé transitivement à `:app`

**Given** le `.aar` est généré
**When** le module Android est buildé
**Then** les classes Java générées par gomobile sont accessibles depuis Kotlin (`fr.plateformeliberte.levoile.core.Protocol`, `Registry`, `Auth`, `Crypto`, `Leakcheck`)
**And** un test unitaire smoke instancie un objet du noyau Go depuis Kotlin et vérifie qu'aucun `UnsatisfiedLinkError` ne survient

**Given** une nouvelle version du noyau Go est livrée
**When** `build-aar.sh` est ré-exécuté avec le même tag git et le même environnement (Docker pinned, Go pinned, gomobile pinned)
**Then** le hash SHA256 du `.aar` produit est stable (préparation reproductibilité Story 12.4)

### Story 9.3: `MainActivity` squelette + WebView placeholder

As a développeur,
I want une `MainActivity` minimaliste avec un WebView affichant un statut texte placeholder,
So que l'app soit lançable au premier installtest, avant que l'UI complète soit livrée Epic 11.

**Acceptance Criteria:**

**Given** l'APK est installé sur un émulateur API 34
**When** l'utilisateur tape l'icône Le Voile
**Then** `MainActivity.onCreate()` est appelée
**And** un `WebView` plein écran charge une page HTML placeholder embarquée (« Le Voile · Démarrage… » + statut tunnel via polling JS Bridge `getStatus()`)
**And** `body.platform-android` est ajouté au DOM via `WebView.evaluateJavascript("document.body.classList.add('platform-android')")` au `onPageFinished`

**Given** la `MainActivity` est lancée
**When** l'utilisateur swipe pour fermer l'app dans la vue récents
**Then** `MainActivity.onDestroy()` est appelée
**And** `LeVoileVpnService` continue de tourner indépendamment (Foreground Service — voir Story 9.5)
**And** la notification persistante reste visible

### Story 9.4: `LeVoileVpnService` — création TUN + pump paquets IP

As a utilisateur Android,
I want que l'app établisse un tunnel VPN via l'API officielle Android non-rootée,
So que tout mon trafic IP traverse Le Voile (FR-AND-1).

**Acceptance Criteria:**

**Given** le service `LeVoileVpnService` hérite de `android.net.VpnService`
**When** l'utilisateur démarre la connexion
**Then** un popup système Android natif « Le Voile demande l'autorisation de configurer un VPN » apparaît (consent VpnService)
**And** si l'utilisateur accepte, `VpnService.Builder` est instancié avec `mtu(1420)`, `addRoute("0.0.0.0", 0)`, `addAddress(tunIp, 24)`
**And** `establish()` retourne un `ParcelFileDescriptor` non-null

**Given** le file descriptor est obtenu
**When** le pump paquets démarre
**Then** un thread lit les paquets IP bruts via `FileInputStream(fd.fileDescriptor)` en boucle (sentinel + interrupt pour arrêt propre)
**And** un autre thread écrit les paquets reçus du tunnel vers `FileOutputStream(fd.fileDescriptor)`
**And** les paquets sont relayés via le noyau Go partagé (Story 9.7) sous forme de byte arrays

**Given** le tunnel est actif et l'utilisateur déconnecte
**When** `LeVoileVpnService.ACTION_DISCONNECT` est reçu
**Then** les threads de pump s'arrêtent proprement
**And** `fd.close()` est appelé, libérant l'interface TUN
**And** aucun résidu d'interface ne subsiste (Android garbage-collecte automatiquement)

### Story 9.5: Foreground Service lifecycle + action « Déconnecter »

As a utilisateur Android,
I want que le tunnel reste actif même après fermeture de l'app par swipe (Foreground Service),
So que je sois protégé en arrière-plan pendant l'usage de Chrome, WhatsApp, Insta (J7).

**Acceptance Criteria:**

**Given** `LeVoileVpnService.onStartCommand()` est appelé
**When** le service démarre
**Then** `startForeground(NOTIF_ID, builder.build())` est appelé en moins de 5 secondes après `onStartCommand()` (sinon ANR système Android)
**And** le service est visible dans `Settings → Apps → Le Voile → Notifications` comme actif
**And** `START_REDELIVER_INTENT` est retourné pour relance automatique en cas de crash service

**Given** le service tourne en Foreground
**When** l'utilisateur tape l'action « DÉCONNECTER » de la notification
**Then** un `PendingIntent.getService(... FLAG_IMMUTABLE)` est invoqué avec `LeVoileVpnService.ACTION_DISCONNECT`
**And** le tunnel se ferme proprement (Story 9.4)
**And** `stopForeground(STOP_FOREGROUND_REMOVE)` est appelé après 5s d'inactivité
**And** `stopSelf()` termine le service

**Given** un OEM agressif (Xiaomi/Huawei/Oppo) tente de tuer le service en battery save
**When** le device entre en doze mode
**Then** le Foreground Service reste actif (exempt depuis Android 8+)
**And** la documentation utilisateur recommande explicitement la whitelist battery pour ces OEM

### Story 9.6: Notification persistante MVP (statut texte minimal)

As a utilisateur Android,
I want une notification persistante dans la barre de statut indiquant que Le Voile est actif,
So que je sache à tout moment si je suis protégé même quand l'app est fermée (FR-AND-2, EBR-01).

**Acceptance Criteria:**

**Given** le service Foreground démarre
**When** la notification est créée
**Then** un `NotificationChannel` `levoile_vpn_status` est créé avec `IMPORTANCE_LOW` (silencieux, pas de heads-up)
**And** la notification utilise `NotificationCompat.Builder` avec `setOngoing(true)` (non-dismissable par swipe), `setSilent(true)`, `setSmallIcon(R.drawable.ic_levoile_status)` (mono-couleur vector drawable)
**And** le titre est « Le Voile · {État} » où {État} ∈ {« Connecté », « Reconnexion… », « Déconnecté », « Erreur »}
**And** le texte secondaire est vide (l'enrichissement pays + IP arrive Story 11.7 — EBR-01)

**Given** la notification est affichée
**When** l'utilisateur tape sur le corps de la notification
**Then** un `PendingIntent.getActivity()` ouvre `MainActivity`
**And** l'app passe en premier plan

**Given** la notification a une action « Déconnecter »
**When** l'action est ajoutée via `NotificationCompat.Action.Builder`
**Then** un `PendingIntent.getService(... FLAG_IMMUTABLE)` invoque `LeVoileVpnService.ACTION_DISCONNECT`
**And** `setContentDescription` est complet pour TalkBack (RGAA AA)

### Story 9.7: Intégration noyau Go via `.aar` — handshake QUIC/HTTP3 + pump IP ↔ HTTP/3

As a utilisateur Android,
I want que le tunnel utilise le même protocole QUIC/HTTP3 que la version desktop, avec session token Ed25519 et certificate pinning,
So que la promesse zéro-log et indiscernabilité DPI soit identique sur Android (ADR-09 réutilisation 100%).

**Acceptance Criteria:**

**Given** le `.aar` gomobile expose `Protocol.Connect(relayDomain, pinnedKey)` depuis Kotlin
**When** `LeVoileVpnService` démarre le tunnel après `establish()`
**Then** une session QUIC/HTTP3 est établie via TLS 1.3 vers `https://{relay-domain}/`
**And** le certificat serveur est validé contre la clé Ed25519 pinnée via `crypto/subtle.ConstantTimeCompare` (NFR9c, code partagé desktop)
**And** la connexion est rejetée si le pinning échoue

**Given** la session QUIC est ouverte
**When** un paquet IP est lu depuis le file descriptor TUN (Story 9.4)
**Then** il est encapsulé dans un stream HTTP/3 `/tunnel` avec framing 2 octets longueur + payload (FR27 desktop, code partagé)
**And** envoyé au relais via le noyau Go
**And** la réponse paquet IP est désencapsulée et écrite vers `FileOutputStream` du fd TUN

**Given** le tunnel est actif sur Pixel 6+ ou équivalent (Snapdragon 7-gen 1+) sur réseau LTE/4G+ ou Wi-Fi domestique RTT < 80ms vers le VPS relais
**When** le chronométrage applicatif (Trace API Android) mesure de `VpnService.prepare()` à premier paquet IP désencapsulé
**Then** le délai est < 3 secondes (NFR-AND-2)
**And** la consommation RAM totale (Java heap + native heap + system) reste < 60 MB en fonctionnement normal mesurée via `adb shell dumpsys meminfo fr.plateformeliberte.levoile` (NFR-AND-1)

---

## Epic 10: Kill Switch OS-Délégué + Conflit VPN + Zéro Télémétrie

L'utilisatrice est protégée même au reconnect ou au boot grâce au réglage Android « VPN permanent » détecté par heuristique ; bandeau rouge persistant si non activé. Si autre app VPN active, refus + message UI. Aucune télémétrie, audit Gradle CI bloquant. Logs filtrés sans data utilisateur.

### Story 10.1: Heuristique détection kill switch via `Settings.Global.always_on_vpn_app`

As a utilisateur Android,
I want que l'app détecte automatiquement si « VPN permanent + bloquer connexions sans VPN » est activé pour Le Voile,
So que je voie un bandeau d'alerte tant que ma protection complète n'est pas en place (cohérent ADR-10).

**Acceptance Criteria:**

**Given** l'app est lancée
**When** la classe `KillSwitchDetector` est instanciée
**Then** elle lit `Settings.Global.getString(contentResolver, "always_on_vpn_app")` (heuristique non-publique)
**And** elle compare la valeur retournée avec `BuildConfig.APPLICATION_ID` (« fr.plateformeliberte.levoile »)
**And** elle expose un `LiveData<KillSwitchStatus>` avec valeurs : `Active`, `Inactive`, `Unverifiable`

**Given** l'utilisateur active « VPN permanent » dans Settings et revient à l'app
**When** `MainActivity.onResume()` est appelée
**Then** `KillSwitchDetector` re-vérifie l'heuristique
**And** si `always_on_vpn_app == "fr.plateformeliberte.levoile"`, status devient `Active`
**And** le `LiveData` notifie tous les observers (bandeau C17 disparaît, etc.)

**Given** l'OS retourne `null` ou une exception sur `Settings.Global.getString` (manufacteur ROM custom, restriction futurs Android)
**When** l'heuristique échoue
**Then** status devient `Unverifiable` (pas `Inactive`)
**And** un log INFO `KillSwitchDetector: heuristique non disponible sur ce device` est émis (sans data utilisateur — NFR-AND-9)
**And** la documentation utilisateur explique le fallback (procédure de vérification manuelle dans Settings → Réseau → VPN)

### Story 10.2: Composant C17 — Bandeau alerte kill switch persistant

As a utilisateur Android,
I want un bandeau rouge persistant si « VPN permanent » n'est pas activé,
So que je sache à tout moment que ma protection est partielle et puisse y remédier en un tap.

**Acceptance Criteria:**

**Given** `KillSwitchDetector` retourne `Inactive` ou `Unverifiable`
**When** `MainActivity` se compose
**Then** le composant C17 (Bandeau alerte) est rendu en haut du panel principal sous l'AppBar
**And** il occupe toute la largeur, hauteur 40dp, fond rouge atténué (`#d42b2b` à 90% opacité), texte blanc Rajdhani 14sp 600
**And** le contenu affiche « ⚠️ Kill switch inactif — Activer › »

**Given** le bandeau est affiché et Story 11.6 est livrée (Epic 11 disponible)
**When** l'utilisateur tape dessus
**Then** une intent ouvre directement l'écran C15 d'onboarding kill switch (sans rejouer écrans 1-2 — voir Story 11.6)
**And** au retour de Settings, `MainActivity.onResume()` re-vérifie l'heuristique (Story 10.1)
**And** si l'heuristique repasse en `Active`, le bandeau disparaît avec animation fade-out 200ms

**Given** le bandeau est affiché et Story 11.6 n'est PAS encore livrée (Epic 10 livré seul, sans Epic 11)
**When** l'utilisateur tape dessus
**Then** **fallback** : l'intent ouvre directement `Intent(Settings.ACTION_VPN_SETTINGS)` (le panneau VPN natif Android)
**And** au retour, `MainActivity.onResume()` re-vérifie l'heuristique (Story 10.1)
**And** un toast in-app explique « Activez "VPN permanent" dans les paramètres Android pour bénéficier de la protection complète »
**And** cette branche garantit qu'Epic 10 reste autonomement déployable

**Given** le bandeau est affiché
**When** TalkBack lit l'écran
**Then** `aria-live="assertive"` annonce le bandeau immédiatement à l'apparition (RGAA AA)
**And** le bandeau est focusable et accessible au focus séquentiel
**And** il n'est pas dismissable par geste swipe (seule action : tap → flow C15)

### Story 10.3: Détection autre app VPN active via `VpnService.prepare()`

As a utilisateur Android,
I want que l'app refuse de démarrer le tunnel si une autre app VPN est déjà active,
So que je ne sois pas connecté à l'aveugle dans un état incohérent (FR-AND-6).

**Acceptance Criteria:**

**Given** l'utilisateur tape « Connecter »
**When** `Connect.handle()` est appelé
**Then** `VpnService.prepare(context)` est invoqué
**And** si l'Intent retourné est non-null, cela signifie qu'une autre app VPN détient le slot OU que le consent n'a jamais été donné

**Given** un autre VPN (Tailscale, Wireguard, OpenVPN) est actif et `prepare()` retourne un Intent non-null
**When** `Settings.Global.always_on_vpn_app` est lu (heuristique)
**Then** si la valeur n'est pas null et différente de `fr.plateformeliberte.levoile`, l'app affiche un message UI explicite : « Une autre application VPN est active sur cet appareil. Désactivez-la pour utiliser Le Voile. »
**And** le tunnel ne démarre pas
**And** un bouton « Ouvrir les paramètres VPN » lance `Intent(Settings.ACTION_VPN_SETTINGS)`

**Given** `prepare()` retourne un Intent non-null mais `Settings.Global.always_on_vpn_app` est null (cas premier lancement, pas de conflit)
**When** la distinction est faite
**Then** l'app lance l'Intent retourné par `prepare()` (popup système Android natif de consent VpnService)
**And** au retour (`onActivityResult` avec `RESULT_OK`), le tunnel démarre normalement (Story 9.4)

### Story 10.4: Audit Gradle CI bloquant — assertion absence dépendances télémétrie

As a auditeur,
I want que le pipeline CI bloque tout build si une dépendance Gradle de télémétrie/analytics est introduite,
So que la promesse zéro-tracking soit vérifiable mécaniquement, pas seulement déclarative (FR-AND-8, NFR-AND-8, ADR-15).

**Acceptance Criteria:**

**Given** le workflow GitHub Actions Android tourne sur chaque PR + push main
**When** la step « Audit dépendances Gradle » s'exécute
**Then** elle invoque `./gradlew app:dependencies --configuration releaseRuntimeClasspath > deps.txt`
**And** elle grep-fail sur la liste : `firebase`, `crashlytics`, `sentry`, `bugsnag`, `mixpanel`, `adjust.io`, `branch.io`, `amplitude`
**And** la présence d'au moins un match fait échouer le job avec exit code 1

**Given** un développeur tente d'ajouter `implementation("com.google.firebase:firebase-analytics:21.0.0")` dans `app/build.gradle.kts`
**When** la PR est soumise
**Then** le job CI « Audit dépendances Gradle » échoue
**And** un commentaire automatique sur la PR explique : « Dépendance télémétrie détectée : firebase-analytics. Cohérent ADR-15 : aucune télémétrie côté Android. PR bloquée. »
**And** la branche ne peut pas être mergée tant que la dépendance n'est pas retirée

**Given** la liste de modules interdits évolue
**When** la liste est mise à jour dans `.github/workflows/android-audit.yml`
**Then** le commit qui modifie cette liste est signé GPG par le mainteneur (NFR22i)
**And** un test unitaire `AuditCITest` vérifie que la liste contient au minimum les 8 modules canoniques

### Story 10.5: Filtrage logs `android.util.Log` par buildType

As a auditeur de la posture confidentialité,
I want que les logs Android ne contiennent jamais d'URL, de domaine, d'IP destination ou de contenu utilisateur,
So que la posture zéro-log soit identique sur Android et desktop (NFR-AND-9, NFR22a).

**Acceptance Criteria:**

**Given** le buildType est `release`
**When** un développeur appelle `Log.i("Tag", "user data")`
**Then** la classe wrapper `LeVoileLog.i()` filtre les niveaux : seuls WARN+ passent en release (NFR-AND-9)
**And** ProGuard (Story 9.1) strippe les appels `Log.d` et `Log.v` du bytecode release via rule `-assumenosideeffects class android.util.Log { public static int d(...); public static int v(...); }`

**Given** le buildType est `debug`
**When** un développeur appelle `Log.i()` ou plus
**Then** les logs apparaissent dans Logcat (filtre INFO+)
**And** un test unitaire `LogFilteringTest` vérifie qu'aucun template de log dans le code source ne contient `${url}`, `${domain}`, `${dest_ip}`, `${user_content}` (assertion CI scan code)

**Given** le scan CI vérifie tous les `.kt` du module Android
**When** la regex `Log\.[diwev]\([^,]+,\s*"[^"]*\$\{(url|domain|destIp|userContent|requestBody|responseBody)\}` matche
**Then** le job CI échoue avec un message explicite : « Pattern log interdit détecté à {file}:{line}. Cohérent NFR-AND-9. »
**And** la PR ne peut pas être mergée tant que le pattern n'est pas retiré

---

## Epic 11: UI Mobile + Onboarding « VPN permanent »

Léa interagit avec l'app via une UI plein écran reprenant la charte plateformeliberte.fr adaptée mobile : AppBar Material 56dp, bottom-sheet pays slide-up, statut central, IP visible. Onboarding obligatoire 3 écrans la guide jusqu'à activer « VPN permanent ». JS Bridge `@JavascriptInterface` (pas de serveur HTTP local). Config JSON dans `getFilesDir()`.

### Story 11.1: Script `sync-frontend.sh` + WebViewAssetLoader + responsive `body.platform-android`

As a utilisateur Android,
I want une UI plein écran reprenant la charte plateformeliberte.fr identique au desktop, mais adaptée mobile,
So que ma confiance visuelle soit immédiate et l'expérience tactile soit native (FR-AND-4, ADR-14).

**Acceptance Criteria:**

**Given** le frontend desktop existe (`internal/ui/web/`)
**When** `bash android/scripts/sync-frontend.sh` s'exécute au build
**Then** les assets HTML/CSS/JS desktop sont copiés vers `android/app/src/main/assets/web/`
**And** le script est idempotent (re-exécution sans diff produit un état identique)
**And** un `.gitignore` empêche le check-in des assets copiés (source de vérité = desktop)

**Given** la `MainActivity` charge l'UI
**When** le `WebViewAssetLoader` est configuré
**Then** il charge `https://appassets.androidplatform.net/assets/web/index.html` (mode sécurisé, pas `file://`)
**And** dès le `onPageFinished`, `evaluateJavascript("document.body.classList.add('platform-android')")` est invoqué
**And** les media queries CSS désactivent les classes desktop : `body.platform-android .desktop-only { display: none }` (C1 Titlebar, C2 Sidebar, C3 Country Item, C4 Star Favorite, C8 Quit Modal)

**Given** le device a une largeur < 600dp (téléphone portrait)
**When** la page se rend
**Then** le layout vertical s'active : sélecteur pays vertical, pas de sidebar, statut central pleine largeur
**And** toutes les cibles tactiles font ≥ 48dp (Material guidelines, RGAA AA)
**And** le contraste texte ≥ 4.5:1 (RGAA AA)

### Story 11.2: JS Bridge `@JavascriptInterface` complet

As a utilisateur Android,
I want que les actions UI (connecter, déconnecter, sélectionner pays) déclenchent immédiatement le service natif,
So que l'expérience soit réactive sans serveur HTTP local (FR-AND-5).

**Acceptance Criteria:**

**Given** la `MainActivity` configure le WebView
**When** `webView.addJavascriptInterface(LeVoileBridge(this), "LeVoile")` est appelé avant `loadUrl`
**Then** le frontend JS peut invoquer `window.LeVoile.connect()`, `window.LeVoile.disconnect()`, `window.LeVoile.getStatus()`, `window.LeVoile.selectCountry(iso)`
**And** chaque méthode est annotée `@JavascriptInterface` côté Kotlin
**And** la classe `LeVoileBridge` est dans son propre fichier (audit sécurité simplifié)

**Given** le frontend appelle `window.LeVoile.getStatus()`
**When** la méthode Kotlin retourne
**Then** le retour est une string JSON ligne par ligne max 4 Ko (FR-AND-5) avec champs `{state, country, ip, killSwitchStatus}`
**And** un test instrumenté vérifie que des inputs malformés (chaîne géante > 4 Ko, JSON invalide) sont rejetés sans crash

**Given** le frontend appelle `window.LeVoile.selectCountry("DE")`
**When** la méthode Kotlin valide l'input
**Then** seuls les ISO 3166-1 alpha-2 dans la whitelist `[DE, ES, GB, US]` sont acceptés
**And** un input invalide (`"FR"`, `"; DROP TABLE"`, `null`) retourne `{ "error": "invalid_country_code" }` sans appeler le service
**And** le service `LeVoileVpnService` n'est jamais invoqué avec un input non-validé (protection injection)

### Story 11.3: Composant C13 — AppBar Material 56dp

As a utilisateur Android,
I want une AppBar Material standard en haut de l'app,
So que je puisse accéder aux paramètres, notifications et infos via les patterns Android natifs.

**Acceptance Criteria:**

**Given** la `MainActivity` se rend
**When** la AppBar (composant C13) se compose
**Then** elle est rendue en haut, hauteur 56dp (standard Material), fond navy `#0b1526`, titre blanc « LE VOILE » Bebas Neue 20sp
**And** elle contient : burger menu (☰, 24dp) à gauche, titre centré, espace flexible, cloche notifications (ⓘ) + menu overflow (⋮) à droite
**And** toutes les zones tappables font ≥ 48dp (RGAA AA)

**Given** l'AppBar est rendue
**When** l'utilisateur tape sur le burger
**Then** un drawer latéral s'ouvre depuis la gauche avec : Paramètres, À propos, Info légale, Paramètres système Android (deeplink Settings → Apps → Le Voile)
**And** l'animation slide-in est 250ms ease-out

**Given** l'AppBar a `role="banner"`
**When** TalkBack lit l'écran
**Then** elle est focusable au focus séquentiel
**And** le titre « LE VOILE » est annoncé
**And** chaque action (burger, cloche, overflow) a un `contentDescription` non-vide en français

### Story 11.4: Composant C14 — Country Selector Bottom-Sheet

As a utilisateur Android,
I want sélectionner un pays via un bottom-sheet qui slide depuis le bas,
So que l'interaction soit native mobile (pas une dropdown desktop) et tactile-friendly.

**Acceptance Criteria:**

**Given** l'utilisateur tape la pill « CHANGER DE PAYS ▼ » dans le panel principal
**When** le bottom-sheet C14 s'ouvre
**Then** il occupe 60% de la hauteur écran, drag handle au top (24dp×4dp), titre « PAYS »
**And** la liste verticale affiche les 4 pays MVP : drapeau emoji 40dp + nom français + indicateur favori (étoile) + check vert si actif
**And** l'animation slide-up est 250ms ease-out, slide-down 200ms ease-in au dismiss

**Given** le bottom-sheet est ouvert
**When** l'utilisateur tape sur un pays inactif
**Then** le bottom-sheet se ferme avec animation slide-down
**And** le panel principal affiche le drapeau du nouveau pays + bouton « CONNECTER »
**And** `window.LeVoile.selectCountry(iso)` est invoqué (Story 11.2)

**Given** le bottom-sheet est ouvert
**When** l'utilisateur fait un drag-down ou tape le fond extérieur ou appuie back Android
**Then** le bottom-sheet se ferme (dismiss sans action)
**And** le focus retourne sur la pill d'origine
**And** TalkBack annonce « Sélection de pays fermée »

**Given** le bottom-sheet a `role="dialog"`
**When** il est ouvert
**Then** il a un focus trap (focus séquentiel reste à l'intérieur)
**And** TalkBack annonce « Sélection de pays, 4 pays disponibles » à l'ouverture (RGAA AA)

### Story 11.5: `OnboardingActivity` 3 écrans + persistence `onboarding_completed`

As a utilisatrice Android première fois,
I want un onboarding clair en 3 écrans qui me guide jusqu'à activer la protection complète,
So que je n'aie pas à comprendre seule comment configurer un VPN sécurisé (FR-AND-3, J6).

**Acceptance Criteria:**

**Given** l'app est lancée pour la première fois
**When** `MainActivity.onCreate()` s'exécute
**Then** elle lit `SharedPreferences.getBoolean("onboarding_completed", false)`
**And** si false, elle redirige vers `OnboardingActivity` avant de monter le WebView principal
**And** `OnboardingActivity` interdit le back Android (back désactivé jusqu'à fin du flow)

**Given** `OnboardingActivity` est lancée
**When** l'utilisatrice traverse les 3 écrans
**Then** Écran 1 (« Bienvenue ») : présentation Le Voile, bouton « Continuer »
**And** Écran 2 (« Autorisation VPN ») : explique le popup système Android natif qui va apparaître, bouton « Continuer » → `VpnService.prepare()` (Story 10.3)
**And** Écran 3 (« VPN permanent ») : **placeholder minimal livré par cette story** — texte « Activez le VPN permanent dans les paramètres Android » + bouton « Continuer » + bouton « Ouvrir les paramètres » (`Intent(Settings.ACTION_VPN_SETTINGS)`). **Story 11.6 enrichit cet écran** en composant C15 complet (icône warning, hiérarchie typo, lien « Continuer sans », fallback « non vérifiable »). Cette séparation garantit que Story 11.5 est implémentable en isolation

**Given** l'utilisatrice complète les 3 écrans
**When** elle valide le dernier (`SharedPreferences.edit().putBoolean("onboarding_completed", true).apply()`)
**Then** `OnboardingActivity.finish()` est appelée
**And** `MainActivity` se lance avec le WebView principal
**And** au prochain lancement, l'onboarding ne se rejoue pas

**Given** l'utilisatrice veut re-déclencher l'onboarding
**When** elle utilise Réglages Android → Apps → Le Voile → Effacer données
**Then** SharedPreferences est purgée
**And** au prochain lancement, l'onboarding rejoue depuis l'Écran 1

### Story 11.6: Composant C15 — Onboarding Kill Switch Screen + deeplink Settings

As a utilisatrice Android première fois,
I want un écran clair qui me guide à activer « VPN permanent » dans Settings via un seul tap,
So que ma protection complète soit active dès le premier usage (FR-AND-3, ADR-10).

**Acceptance Criteria:**

**Given** Écran 3 d'onboarding est rendu (composant C15)
**When** la composition se fait
**Then** l'écran affiche : icône warning ⚠️ 64dp orange, titre « Une dernière étape » Bebas Neue 28sp, texte explicatif 3 lignes max Inter 16sp max-width 320dp, sous-texte conséquence 2 lignes Inter 14sp opacity 0.7
**And** un bouton primaire pleine largeur (Rajdhani 600 16sp, hauteur 48dp) : « OUVRIR LES PARAMÈTRES »
**And** un lien discret (Inter 13sp, opacity 0.5, underline) : « Continuer sans (déconseillé) »

**Given** l'utilisatrice tape « OUVRIR LES PARAMÈTRES »
**When** l'intent est lancée
**Then** `startActivity(Intent(Settings.ACTION_VPN_SETTINGS))` ouvre le panneau VPN d'Android
**And** au retour (`onResume`), `KillSwitchDetector` re-vérifie l'heuristique (Story 10.1)
**And** un écran transitoire « Vérification… » (1s) puis : si `Active`, fin onboarding ; si `Inactive` ou `Unverifiable`, options « Réessayer » et « J'ai vérifié manuellement »

**Given** l'utilisatrice tape le lien discret « Continuer sans (déconseillé) »
**When** un bottom-sheet de confirmation apparaît
**Then** il affiche « Continuer sans le kill switch ? Vous pourrez l'activer plus tard. » + boutons « Annuler » et « Continuer sans »
**And** si elle confirme, `onboarding_completed = true` est persisté
**And** un bandeau rouge persistant (composant C17 d'Epic 10) reste visible dans `MainActivity` tant que kill switch non activé

**Given** Écran 3 est rendu
**When** TalkBack lit l'écran
**Then** `aria-live="polite"` est sur le statut, focus initial sur le bouton primaire
**And** le contraste warning est ≥ 4.5:1 (RGAA AA)

### Story 11.7: Enrichissement notification C16 — pays + IP + action dynamiques

As a utilisateur Android,
I want que la notification persistante affiche le pays et l'IP visible dynamiquement,
So que je voie ma protection effective d'un coup d'œil sans ouvrir l'app.

**Acceptance Criteria:**

**Given** la notification MVP existe (Story 9.6) avec titre « Le Voile · {État} » et texte vide
**When** le tunnel est `Connecté`
**Then** le titre devient « Le Voile · Connecté »
**And** le texte devient « {Drapeau} {Pays français} · {IP visible} » (ex. « 🇩🇪 Allemagne · 5.x.x.x »)
**And** la mise à jour utilise `notify(NOTIF_ID, builder.build())` à chaque transition d'état (Connecté/Reconnexion/Déconnecté/Erreur)

**Given** le tunnel est en `Reconnexion`
**When** la notification se met à jour
**Then** le titre devient « Le Voile · Reconnexion… »
**And** une animation icône légère (alternance opacity 1.0 ↔ 0.6 toutes les 750ms via remplacement périodique) est rendue

**Given** `KillSwitchDetector` retourne `Inactive` (Story 10.1)
**When** le tunnel est `Connecté` mais kill switch absent
**Then** le texte devient « ⚠️ Kill switch inactif · Activer »
**And** un tap sur la notification ouvre `MainActivity` qui affiche le bandeau C17 + flow C15 (déjà couvert Stories 10.2 + 11.6)

**Given** la notification a un contenu enrichi
**When** TalkBack lit la notification
**Then** `setContentDescription` complet annonce « Le Voile, état {état}, pays {pays}, IP {IP_lu_chiffres_un_par_un} »
**And** l'action « DÉCONNECTER » reste accessible au focus séquentiel

### Story 11.7-bis: Wiring Go Backend — Relay Registry + currentIp + bascule NoOpPacketRelay

> **Origine** : ajoutée post-code-review Epic 11 (2026-05-03). Consolide 3 dettes
> techniques héritées de Stories 9.7 + 11.7 (cf. `epic-11-retrospective-notes.md`).
> Story file : `_bmad-output/implementation-artifacts/11-7bis-wiring-go-backend-relay-registry-currentip.md`.

As a utilisateur Android Le Voile,
I want que mon trafic soit RÉELLEMENT chiffré vers les relais européens et que la notification persistante affiche mon IP visible (pas seulement le pays),
So que la promesse de protection soit effective et que je puisse vérifier d'un coup d'œil que ma connexion sort bien par le pays attendu (FR-AND-1, FR-AND-2).

**Given** un device Android avec `LeVoileVpnService` actif
**When** l'utilisateur appuie sur « Connecter » avec pays DE sélectionné
**Then** `LeVoileVpnService.provideRelay()` charge le `relay-registry.json` (cache ConfigStore ou fetch online via shim Go étendu) puis instancie `GoBackedPacketRelay(domainDE, pinnedKeyDE, sink, onStateChanged)`
**And** le tunnel QUIC/HTTP3 s'établit réellement (vs `NoOpPacketRelay` qui dropait tout)
**And** le `StatusCallback` enrichi pousse `visibleIp` au callback Kotlin → `currentIp` mis à jour → notification affiche « 🇩🇪 Allemagne · 5.45.6.7 »

**Given** le shim Go `android/shims/registry/` est étendu pour exposer Parse + Verify gomobile-bindable
**When** l'app démarre et qu'aucun cache registry n'existe
**Then** un fetch online est déclenché vers le bootstrap relay (hardcoded dans `res/raw/`)
**And** la signature Ed25519 est vérifiée avec la master pubkey bundled dans l'APK
**And** le résultat est persisté dans `ConfigStore.registryCache` (placeholder Story 11.8)

**Given** le facade Go `internal/tunnel/gomobile_facade.go` est étendu
**When** la session passe à l'état `connected`
**Then** le `StatusCallback` reçoit `visibleIp` (récupéré via Leakcheck STUN ou /verify enrichi côté relais)
**And** le callback Kotlin met à jour `LeVoileVpnService.currentIp` + `currentCountry`
**And** la notification est repostée avec le contenu enrichi

### Story 11.8: Config JSON `getFilesDir()/config.json`

As a utilisateur Android,
I want que mes préférences (pays favori, registre relais cache, dernière clé Ed25519) soient persistées localement et privées,
So que mon usage soit confortable d'une session à l'autre, sans que mes préférences ne fuient (FR-AND-10, NFR-AND-7).

**Acceptance Criteria:**

**Given** l'app stocke une préférence
**When** `ConfigStore.save(config)` est appelé
**Then** le contenu est sérialisé en JSON (pas TOML — convention écosystème AndroidX) dans `getFilesDir()/config.json`
**And** le fichier est créé avec les permissions par défaut Android (UID-only, équivalent 0600 desktop — NFR-AND-7)
**And** un test instrumenté vérifie qu'aucune autre app ne peut lire le fichier (`adb shell run-as com.autre.app cat /data/data/fr.plateformeliberte.levoile/files/config.json` échoue)

**Given** l'app démarre
**When** `ConfigStore.load()` est invoqué
**Then** si `config.json` existe, le contenu est désérialisé en `ConfigData(preferredCountry, registryCache, lastVerifiedEd25519Key)`
**And** si le fichier est absent (premier lancement), une config par défaut est retournée (`preferredCountry = "DE"`)
**And** si le fichier est corrompu (JSON invalide), un fallback config par défaut + log WARN « Config corrompue, fallback défaut » (sans data utilisateur — NFR-AND-9)

**Given** une mise à jour APK Android arrive
**When** la migration de schema config est nécessaire
**Then** `ConfigStore.migrate(oldVersion, newVersion)` est appelé
**And** la migration préserve les préférences utilisateur (preferredCountry, etc.)
**And** un test unitaire `ConfigMigrationTest` vérifie chaque migration de schema

---

## Epic 12: Distribution F-Droid + APK Direct + Auto-Update + Tests Instrumentés

Distribution via F-Droid (build reproductible) + APK direct GitHub releases (signé v2/v3 par master key Ed25519). Pipeline CI Android valide chaque release : tests Espresso émulateur API 29/33/34, vérif reproductibilité APK (hash SHA256 stable), audit dépendances, signature. Notification UI mise à jour pour APK direct (désactivée pour F-Droid).

### Story 12.1: Métadonnées F-Droid `metadata/fr.plateformeliberte.levoile.yml` versionnées

As a auditeur F-Droid,
I want des métadonnées F-Droid complètes versionnées dans le repo,
So que l'inclusion au catalogue F-Droid soit possible avec build reproductible (FR-AND-7, ADR-11).

**Acceptance Criteria:**

**Given** le repo contient un dossier `metadata/`
**When** `metadata/fr.plateformeliberte.levoile.yml` est créé
**Then** il contient les champs minimum F-Droid : `Categories`, `License: GPL-3.0-or-later`, `WebSite: https://plateformeliberte.fr`, `SourceCode: https://github.com/velia-the-veil/le_voile`, `IssueTracker`, `Description`, `Summary` (max 80 chars)
**And** la description F-Droid est en français (cohérent target audience Léa) avec mention claire « zéro tracking, zéro télémétrie »
**And** les screenshots `metadata/en-US/images/phoneScreenshots/*.png` sont versionnés (au moins 4 captures représentatives)

**Given** une nouvelle release est taggée
**When** `metadata/fr.plateformeliberte.levoile.yml` est mis à jour
**Then** un bloc `Builds:` est ajouté avec : `versionName`, `versionCode`, `commit: <git_tag>`, `subdir: android/app`, `gradle: yes`, build recipe complète (incluant `prebuild: bash android/scripts/build-aar.sh && bash android/scripts/sync-frontend.sh`)
**And** un linter F-Droid local (`fdroid lint`) ne signale aucun warning bloquant

**Given** le mainteneur F-Droid pull le repo Le Voile
**When** il invoque `fdroid build fr.plateformeliberte.levoile`
**Then** la build se complète sans erreur en environnement F-Droid (Docker `srvz/fdroidserver`)
**And** l'APK produit est signé par F-Droid (signature distincte du release GitHub APK direct — c'est intentionnel)

### Story 12.2: Pipeline GitHub Actions Android — lint + tests + audits

As a mainteneur,
I want un pipeline CI Android qui exécute tous les contrôles qualité avant tout merge,
So que la qualité release soit garantie mécaniquement (NFR-AND-8, NFR22d).

**Acceptance Criteria:**

**Given** un push ou PR sur `main`
**When** le workflow `.github/workflows/android-ci.yml` se déclenche
**Then** il exécute en séquence : `gradle lint`, `gradle testDebugUnitTest`, `gradle assembleDebug`, scan ProGuard rules (`proguard-rules.pro` syntaxe valide), `apkanalyzer manifest permissions` (assertion liste = NFR-AND-7, **avec autorisation explicite de la permission custom AGP-injectée `<applicationId>.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION`**), audit dépendances Gradle (Story 10.4 réutilisée)
**And** chaque step échoue le job avec exit code non-zéro si une assertion CI casse
**And** les résultats sont reportés sur la PR (commentaire automatique)

**Given** une PR introduit une régression de lint Android
**When** `gradle lint` détecte un issue de severity ≥ Error
**Then** le job CI échoue avec le rapport HTML `app/build/reports/lint-results-debug.html` archivé en artifact GitHub Actions
**And** la PR ne peut pas être mergée tant que le lint n'est pas vert

**Given** la matrice de tests instrumentés (Story 12.6) tourne
**When** elle est intégrée au workflow android-ci.yml
**Then** elle s'exécute uniquement sur push `main` ou tag (pas sur chaque PR — coûteux en émulateurs)
**And** un check de gating distinct empêche le tag release si la matrice échoue

### Story 12.3: Signature APK v2 + v3 (key rotation) par master key Ed25519

As a utilisateur Android,
I want que l'APK installé soit signé par la master key Le Voile et vérifié automatiquement par PackageManager,
So que je sois protégée contre une APK altérée par un MITM ou un upload malveillant (FR-AND-7, NFR-AND-5).

**Acceptance Criteria:**

**Given** le pipeline release Android s'exécute
**When** la step « Signature APK » se déclenche
**Then** l'APK est signé en v2 (APK Signature Scheme v2) ET v3 (key rotation) via `apksigner sign --v2-signing-enabled true --v3-signing-enabled true ...`
**And** la clé utilisée est la master key Ed25519 stockée HSM/YubiKey (NFR22g, jamais sur runner GitHub)
**And** le passage de la clé au runner se fait via secret encrypted GitHub Actions, scope minimum (release workflow uniquement)

**Given** un utilisateur tente d'installer l'APK signé
**When** PackageManager Android valide la signature
**Then** la signature v2 + v3 est cryptographiquement vérifiée
**And** si la signature est altérée ou invalide, l'installation est refusée avec « Erreur d'analyse de package »
**And** un test instrumenté `SignatureValidationTest` simule une APK altérée (1 byte flippé) et vérifie le refus d'install

**Given** une rotation de master key arrive (NFR22g, tous les 24 mois)
**When** la nouvelle clé est utilisée
**Then** la signature v3 (key rotation) permet à PackageManager de valider la transition de clé
**And** les utilisateurs existants ne sont pas forcés de désinstaller — l'update de release transitionne en douceur
**And** la documentation publique explique la rotation (post-mortem warrant canary mensuel)

### Story 12.4: Vérification reproductibilité APK CI

As a auditeur indépendant,
I want pouvoir vérifier que l'APK F-Droid et l'APK GitHub releases sont issus du même tag git et identiques au byte près,
So que la chain-of-trust ne dépende pas du seul mainteneur (NFR-AND-6, ADR-11).

**Acceptance Criteria:**

**Given** un tag git `vX.Y.Z` est poussé
**When** le job CI « reproducibility-check » se déclenche
**Then** il invoque deux fois `bash android/scripts/build-apk-release.sh` avec un environnement identique (Docker pinned, JDK pinned, Gradle pinned, Android SDK pinned, gomobile pinned)
**And** il calcule `sha256sum` des deux APK produits (avant signature, ou via apk-content-archive neutre)
**And** il fail si les hashes diffèrent

**Given** l'APK F-Droid est publié au catalogue
**When** un auditeur indépendant veut vérifier la reproductibilité
**Then** la documentation utilisateur explique : `git checkout v1.0.0 && bash android/scripts/build-apk-release.sh && sha256sum build/outputs/apk/release/app-release.apk` doit produire le même hash que le `sha256sum apk-fdroid.apk` annoncé sur la page release GitHub
**And** la procédure est testable sans accès à la master key (le hash compare le `unsignedApk` ou la `apk-content-archive` neutre)

**Given** une dérive de reproductibilité est introduite (timestamps embarqués, ordre fichiers ZIP, etc.)
**When** le job CI échoue
**Then** un rapport diff (via `diffoscope` ou `apkdiff`) identifie la source de dérive (nom fichier, métadonnée, timestamp)
**And** le commit fautif est bloqué tant que la dérive n'est pas corrigée

### Story 12.5: WorkManager 24h + check version + notification UI mise à jour (APK direct)

As a utilisateur Android sur APK direct,
I want être notifié quand une nouvelle version est disponible via une notification UI,
So que je puisse manuellement télécharger et installer la mise à jour (FR-AND-9, ADR-11).

**Acceptance Criteria:**

**Given** le buildType est `apkDirect` (pas `fdroid`)
**When** `WorkManager` schedule un check périodique
**Then** une `PeriodicWorkRequest` est créée avec interval = 24h, contraintes = `NetworkType.CONNECTED`, BackoffPolicy = `EXPONENTIAL`
**And** le worker `UpdateCheckWorker` est exécuté au lancement de l'app + tous les 24h en arrière-plan
**And** le worker invoque `https://api.github.com/repos/velia-the-veil/le_voile/releases/latest` avec User-Agent identifiant Le Voile

**Given** le worker reçoit la version GitHub
**When** il compare avec `BuildConfig.VERSION_NAME` (semver)
**Then** si une version supérieure existe, une notification UI (channel `levoile_update`, importance DEFAULT) est postée
**And** le contenu est « Mise à jour {version} disponible · Le Voile » + action « Voir sur GitHub » (PendingIntent ouvre `https://github.com/velia-the-veil/le_voile/releases/tag/v{version}` dans le navigateur)
**And** la notification est dismissable (pas ongoing comme la C16)

**Given** le buildType est `fdroid`
**When** `UpdateCheckWorker` est instancié
**Then** la classe court-circuite immédiatement avec un log INFO « Auto-update désactivé — F-Droid gère les mises à jour »
**And** aucune `PeriodicWorkRequest` n'est planifiée (cohérent FR-AND-9 : « Pour F-Droid : la vérification embarquée est désactivée »)
**And** un test unitaire `UpdateCheckBuildTypeTest` vérifie le comportement différencié

### Story 12.6: Tests instrumentés Espresso + AndroidX Test sur émulateur API 29 + 33 + 34

As a mainteneur,
I want une matrice de tests instrumentés couvrant les 3 versions API supportées,
So que la régression sur une version Android soit détectée avant release (NFR-AND-10, NFR22).

**Acceptance Criteria:**

**Given** le pipeline release tourne sur push `main` ou tag
**When** la step « Instrumented Tests » se déclenche
**Then** elle utilise GitHub Actions matrix avec 3 émulateurs : API 29 (Android 10), API 33 (Android 13), API 34 (Android 14)
**And** chaque émulateur exécute la suite Espresso + AndroidX Test complète
**And** la durée totale est < 30 minutes (parallélisation 3 jobs)

**Given** les tests instrumentés couvrent les flows critiques
**When** la suite s'exécute
**Then** les scénarios couverts incluent : (a) consent VpnService au premier lancement, (b) démarrage VpnService + tunnel établi, (c) heuristique kill switch active/inactive/unverifiable, (d) UI flow J6 (onboarding 3 écrans), (e) Connect/Disconnect via JS Bridge, (f) failover entre relais d'un même pays, (g) notification persistante affiche pays + IP corrects, (h) action « Déconnecter » de la notification ferme le tunnel
**And** chaque scénario est dans son propre fichier `androidTest/.../{Scenario}Test.kt`

**Given** la matrice instrumentée
**When** un test échoue sur un seul émulateur
**Then** le job global échoue avec exit code 1
**And** les logs Logcat de l'émulateur fautif sont uploadés en artifact GitHub Actions
**And** la release ne peut pas être taggée tant que la matrice n'est pas 100% verte (NFR-AND-10)

**Given** un test simule un autre VPN actif (`MockVpnServicePrepareReturnsIntent`)
**When** le test « VpnConflictDetectionTest » s'exécute
**Then** il vérifie que l'app affiche le message UI explicite (Story 10.3)
**And** il vérifie que `LeVoileVpnService` n'est jamais démarré
**And** il vérifie que le bouton « Ouvrir les paramètres VPN » lance bien l'Intent attendue
