---
stepsCompleted: ['step-01-init', 'step-02-discovery', 'step-02b-vision', 'step-02c-executive-summary', 'step-03-success', 'step-04-journeys', 'step-05-domain', 'step-06-innovation', 'step-07-project-type', 'step-08-scoping', 'step-09-functional', 'step-10-nonfunctional', 'step-11-polish', 'step-12-complete', 'step-e-01-discovery', 'step-e-02-review', 'step-e-03-edit', 'step-e-01-discovery-2', 'step-e-02-review-2', 'step-e-03-edit-2']
inputDocuments: ['../brainstorming/brainstorming-session-2026-03-08-1530.md', 'architecture.md']
workflowType: 'prd'
documentCounts:
  briefs: 0
  research: 0
  brainstorming: 1
  projectDocs: 0
classification:
  projectType: 'desktop_app + network_server + browser_extension'
  domain: 'cybersecurity_privacy'
  complexity: 'high'
  projectContext: 'greenfield'
lastEdited: '2026-04-08'
editHistory:
  - date: '2026-04-08'
    changes: 'Suppression mode portable, remplacement Wails v2 par webview/webview + fyne.io/systray (binaire UI unique). Architecture 2 processus (service + UI). FR14-16, FR20-22 reformulés. Desktop App Architecture réécrite. Risques et compromis portable supprimés.'
  - date: '2026-04-02'
    changes: 'Alignement PRD avec code réel — multi-processus (service kardianos/service + tray fyne.io/systray + desktop Wails v2), IPC named pipes, mode portable alternatif, installeur NSIS Windows. FR14-16 et FR20-22 reformulés, architecture desktop réécrite.'
  - date: '2026-03-30'
    changes: 'Alignement PRD avec architecture mono-processus portable Wails v3 — suppression service OS/IPC, Wails v2→v3 + systray natif, binaire portable unique (suppression installateur), FR14-16 et FR20-22 reformulés, démarrage auto→raccourci Startup optionnel.'
  - date: '2026-03-16'
    changes: 'Alignement PRD avec architecture révisée — multi-relais, UI Wails v2, registre distribué, IP camouflage, anti-fuite, extension navigateur, auto-update. 22→40 FRs, 16→20 NFRs.'
---

# Product Requirements Document - Le Voile de Vélia

**Author:** Akerimus
**Date:** 2026-03-08
**Dernière révision :** 2026-04-08

## Executive Summary

Le Voile est un VPN desktop qui garantit le zero-log par architecture — les relais VPS sont stateless, il n'y a physiquement rien à enregistrer. Contrairement aux VPN traditionnels qui promettent le zero-log par politique de confidentialité, Le Voile le prouve par le design et le code source ouvert.

Destiné au grand public francophone soucieux de sa vie privée, le produit cible un besoin urgent : la France s'apprête à bloquer les VPN traditionnels. Le Voile y survivra grâce à un trafic indiscernable du trafic web normal (QUIC/HTTPS via Cloudflare).

Le client desktop 2 processus (service background kardianos/service + UI unique combinant fyne.io/systray et webview/webview) reproduisant la charte plateformeliberte.fr et les relais VPS stateless multi-pays forment un ensemble indissociable. Le Voile route les requêtes DNS via DoH et tunnelise le trafic web via un proxy HTTP CONNECT local, masquant l'IP réelle de l'utilisateur. Une extension navigateur gère le routage intelligent — bypass automatique pour les téléchargements volumineux. Un installeur NSIS assure le déploiement Windows (service SCM, UI autostart, shortcuts, extension).

Gratuit, financé par donations, distribué via plateformeliberte.fr.

### Ce qui rend Le Voile unique

- **Confiance par le design** — Relais stateless, zéro donnée à compromettre, code source ouvert (github.com/velia-the-veil/le_voile)
- **Indétectable** — Trafic QUIC/HTTPS via Cloudflare, mimant du trafic web standard. Résistant au blocage des VPN traditionnels
- **Multi-relais géographiques** — Relais répartis par pays, failover automatique, registre distribué sans point de défaillance unique
- **IP Camouflage** — Proxy HTTP CONNECT local tunnelisant le trafic web via les relais, masquant l'IP réelle pour toute navigation
- **Routage intelligent** — Extension navigateur avec bypass automatique des téléchargements > 50 Mo, cohabitation SysProxy + extension pour couvrir navigateurs et applications
- **Anti-fuite WebRTC** — Détection via STUN + politiques navigateur Chromium/Firefox, sans bloquer les appels vidéo
- **Zero-config** — Le produit fonctionne dès le lancement

## Project Classification

- **Type :** Application desktop (client) + serveurs réseau (relais VPS stateless multi-pays) + extension navigateur (WebExtension Chrome/Firefox)
- **Domaine :** Cybersécurité / Vie privée
- **Complexité :** Élevée — QUIC, HTTPS, DNS-over-HTTPS, HTTP CONNECT proxy, STUN, Ed25519, tunneling, registre distribué, gestion DNS système, politiques navigateur, extension WebExtension
- **Contexte :** Greenfield
- **Distribution :** Installeur NSIS Windows (service + UI) via GoReleaser
- **Ressources :** Développeur unique (Akerimus) + IA

## Success Criteria

### User Success

- L'utilisateur lance Le Voile et est protégé immédiatement — zéro configuration
- L'UI affiche le pays sélectionné, le relais actif, l'IP visible et le statut de protection
- Les requêtes DNS passent par le tunnel chiffré (DoH) sans action utilisateur
- Le trafic web est tunnelisé via le proxy CONNECT, masquant l'IP réelle
- L'utilisateur peut choisir un pays parmi les relais disponibles
- Le failover entre relais d'un même pays est transparent — pas d'interruption perceptible
- Aucun impact perceptible sur la navigation quotidienne

### Business Success

- Le produit fonctionne de bout en bout (client → tunnel → relais multi-pays → internet)
- Des utilisateurs réels le téléchargent et l'utilisent au quotidien
- Projet open-source — le succès se mesure à l'adoption organique

### Measurable Outcomes

- Temps de lancement → protection active : < 30 secondes
- Zéro fuite DNS vérifiable via outils de test standard
- Zéro fuite IP WebRTC vérifiable via browserleaks.com, ipleak.net
- IP web masquée vérifiable via whatismyip.com
- Failover entre relais d'un même pays : < 5 secondes, transparent pour l'utilisateur
- Uptime par relais : ≥ 99.5% mensuel mesuré via endpoint /health

## User Journeys

### Parcours 1 : Camille — Découverte et protection immédiate

**Qui :** Camille, 34 ans, journaliste freelance. Pas technique, cherche une solution simple face au blocage VPN annoncé.

**Scène d'ouverture :** Elle tombe sur plateformeliberte.fr via une recommandation et télécharge le binaire Le Voile.

**Action :** Elle lance l'installeur téléchargé. Installation rapide. L'icône Le Voile apparaît dans le tray — thème sombre aux couleurs de plateformeliberte.fr. Elle ouvre la fenêtre. Statut : "Connecté — Islande (is-01)". IP visible affichée.

**Moment clé :** Test de fuite DNS : zéro fuite. whatismyip.com affiche une IP islandaise. Sans rien faire.

**Résolution :** Le Voile tourne en arrière-plan (icône tray), elle l'oublie. Exactement ce qu'elle voulait.

### Parcours 2 : Akerimus — Opérateur multi-relais

**Qui :** Akerimus, développeur et opérateur unique.

**Scène d'ouverture :** Deux VPS commandés — Islande et France. Déployer les relais, configurer Cloudflare.

**Action :** Binaires déployés sur les VPS. Registre JSON créé avec les deux relais (is-01, fr-01), déployé sur chaque instance. Sous-domaines Cloudflare configurés : is.levoile.dev, fr.levoile.dev.

**Moment clé :** Premier test bout en bout — le client télécharge le registre, affiche les deux pays. Connexion à l'Islande, puis bascule vers la France. Failover testé en coupant is-01 — bascule automatique vers is-02.

**Résolution :** Ajout d'un relais = générer une clé Ed25519, ajouter au registre JSON, déployer. Pas de données à gérer.

### Parcours 3 : Camille — Tunnel coupé, failover transparent

**Qui :** Camille, quelques jours plus tard. Le Wi-Fi du café provoque une perte de connexion au relais.

**Action :** Le sélecteur bascule automatiquement vers un autre relais du même pays. Kill switch DNS actif pendant la bascule — aucune fuite.

**Moment clé :** Camille ne remarque rien. La fenêtre Le Voile affiche brièvement "Reconnexion..." puis "Connecté — Islande (is-02)".

**Résolution :** Changement de réseau. Tunnel reconnecté automatiquement. Rien fait manuellement.

### Parcours 4 : Camille — Choix du pays

**Qui :** Camille, veut apparaître depuis la France pour un site géo-restreint.

**Action :** Elle ouvre la fenêtre Le Voile depuis l'icône tray. Le sélecteur de pays affiche les drapeaux avec le nombre de relais disponibles. Elle clique sur "France (2 relais)".

**Moment clé :** Reconnexion automatique via un relais français. L'IP visible change. Le site géo-restreint fonctionne.

**Résolution :** Le pays préféré est sauvegardé — au prochain démarrage, Le Voile se connecte directement à la France.

### Parcours 5 : Camille — Navigation protégée et téléchargement volumineux

**Qui :** Camille, navigue avec Le Voile actif et l'extension navigateur installée.

**Action :** Toute sa navigation passe par le proxy CONNECT local → relais → internet. Son IP réelle est masquée partout. Elle lance le téléchargement d'un fichier de 200 Mo.

**Moment clé :** L'extension détecte le Content-Length > 50 Mo et bascule automatiquement le téléchargement en connexion directe — pas de surcharge sur le relais, téléchargement rapide.

**Résolution :** Navigation protégée + téléchargements rapides. Aucune action manuelle.

### Journey → Capabilities Mapping

| Capacité requise | Parcours source |
|---|---|
| Installeur NSIS Windows | Camille #1 |
| UI webview/webview : fenêtre desktop, charte plateformeliberte.fr | Camille #1, #4 |
| Sélecteur de pays (drapeaux, nombre relais) | Camille #4 |
| System tray fyne.io/systray : icône d'état + accès rapide fenêtre (même processus que webview) | Camille #1, #3 |
| Tunnel QUIC/HTTPS auto-connecté | Camille #1 |
| DNS-over-HTTPS via tunnel | Camille #1 |
| Kill switch DNS | Camille #3 |
| Reconnexion automatique + failover multi-relais | Camille #3 |
| Proxy HTTP CONNECT local (IP camouflage) | Camille #5 |
| Extension navigateur : routage intelligent + bypass gros fichiers | Camille #5 |
| Détection fuite WebRTC + politiques navigateur | Camille #1 |
| Indicateur visuel d'état (connecté/en cours/déconnecté) | Camille #3, #4 |
| Relais multi-pays déployables sur VPS | Akerimus #2 |
| Registre distribué (chaque relais sert /registry) | Akerimus #2 |
| Relais stateless (zéro persistence) | Akerimus #2 |

## Domain-Specific Requirements

### Vie Privée & Réglementation

- **Zero-log architectural** — Relais stateless, aucune donnée persistée. Rien à fournir en cas de réquisition
- **Juridiction favorable** — Hébergement multi-pays, favorisant les juridictions respectueuses de la vie privée
- **RGPD** — Conformité simplifiée : aucune donnée personnelle collectée
- **Code source ouvert** — Auditable publiquement dès le MVP
- **Confidentialité renforcée** — Aucun log IP client sur les relais ni le proxy. Hash IP uniquement dans les session tokens (TTL 4h)

### Contraintes Techniques Domaine

- **Résistance DPI** — Trafic indiscernable du trafic web normal via QUIC/HTTPS Cloudflare
- **Cryptographie standard** — Ed25519 via bibliothèques cryptographiques standard uniquement. Pas de crypto maison. Une paire de clés par relais
- **Gestion DNS système** — Modification propre à l'activation, restauration complète à la désactivation. Zéro résidu
- **Protection SSRF** — Le relais bloque les connexions CONNECT vers les réseaux privés (loopback, RFC 1918, link-local)
- **Validation source** — Le relais vérifie que les requêtes proviennent des plages IP Cloudflare

### Risques & Mitigations

| Risque | Impact | Mitigation |
|---|---|---|
| Cloudflare bloque le domaine | Tunnel inaccessible | Phase 3 : connexion directe bypass Cloudflare |
| Saisie d'un VPS | Service interrompu sur ce pays | Stateless — rien à trouver. Failover automatique vers les relais restants |
| Compromission clé Ed25519 d'un relais | Usurpation d'identité du relais | Clé unique par relais — révocation granulaire, les autres relais non affectés |
| Antivirus/firewall bloque le client | Lancement échoué | Avertissement page de téléchargement. Instructions de mise en liste blanche |
| Fuite DNS pendant reconnexion | IP exposée | Kill switch DNS dès le MVP + failover rapide |
| Blocage VPN par la France | Produit inutile | Architecture anti-détection : QUIC/HTTPS standard, pas de signature VPN |
| Fuite WebRTC | IP réelle exposée via navigateur | Détection STUN + politiques navigateur Chromium/Firefox |
| Crash processus → DNS bloqué (kill switch) | Plus d'internet jusqu'au relancement | Service SCM redémarre automatiquement. Crash-recovery DNS/proxy au redémarrage |
| Le Voile fermé → navigation sans protection | L'utilisateur navigue sans s'en rendre compte | Service persiste au boot + extension persistante. Le tray reste actif même si la fenêtre est fermée |
| Relais saturés par adoption organique | HTTP 503, failover épuise le pool, déconnexion en boucle | Monitoring /health (NFR18), seuil d'alerte opérationnel à 80% de capacité → ajouter un relais |
| Auto-update appliquée mais fonctionnellement cassée | Crash en boucle + DNS bloqué (kill switch) pour tous les utilisateurs | Critère de santé post-update : tunnel établi dans les 30s, sinon rollback automatique (FR36) |
| WebView2 Runtime absent | Le binaire crash ou affiche un message cryptique | Détection d'absence au lancement + message clair + lien de téléchargement WebView2 |

## Innovation & Novel Patterns

### Innovation Areas

- **Registre distribué pair-à-pair** — Chaque relais sert l'intégralité du registre via `/registry`. Pas de serveur de coordination central, pas de point de défaillance unique. Le client cache le registre localement pour un fonctionnement résilient au démarrage (cold start)
- **Architecture flux légers + IP camouflage** — DNS via DoH tunnel + trafic web via proxy HTTP CONNECT local. Le proxy tunnelise via les relais sans inspecter le contenu. Performance préservée pour le trafic léger, camouflage pour tout le trafic web
- **Camouflage protocolaire** — QUIC/HTTPS via Cloudflare CDN. Pour un observateur réseau, Le Voile ressemble à un site web ordinaire
- **Extension navigateur routage intelligent** — Tout le trafic navigateur passe par le proxy Le Voile par défaut. Les téléchargements > 50 Mo sont automatiquement basculés en connexion directe via détection Content-Length. Cohabitation avec SysProxy pour couvrir les applications hors navigateur
- **Anti-fuite WebRTC sans bloquer les appels** — Détection via STUN Binding Requests + politiques navigateur (Chromium registry, Firefox policies.json), sans désactiver WebRTC entièrement

### Compromis Documentés

- **Proxy CONNECT → latence additionnelle** — Tout le trafic web transite par le relais. Impact : latence légèrement accrue. Compromis accepté — c'est le prix du masquage IP complet
- **Bypass gros fichiers → exposition IP** — Les téléchargements > 50 Mo passent en direct. L'IP réelle est visible pour ces téléchargements. Compromis accepté — protéger la bande passante des relais, le téléchargement est un acte volontaire
- **SysProxy + Extension → double configuration** — SysProxy pour les apps, extension pour les navigateurs. Complexité accrue mais couverture complète
- **Extension persistante** — L'extension est déployée par l'installeur NSIS via policies registre et persiste. L'extension gère le fail-safe : si le proxy est down, bascule automatique en DIRECT

### Validation

- Test fuite DNS : dnsleaktest.com, ipleak.net
- Test fuite WebRTC : browserleaks.com, ipleak.net
- Test IP masquée : whatismyip.com
- Test DPI : Wireshark — vérifier absence de signature protocolaire VPN (indiscernabilité par 0 pattern-match sur 100 échantillons de trafic)
- Tests terrain avec amis d'Akerimus

## Desktop App Requirements

### Architecture

**Architecture 2 processus (Windows) :**
- **levoile-service.exe** — Service Windows SCM (kardianos/service). Orchestrateur principal : tunnel QUIC, proxy DNS local, kill switch, watchdog, blocklist, HTTP proxy CONNECT, registry discovery, failover, leak check, updater, browser policies. Expose un serveur IPC
- **levoile-ui.exe** — UI unique combinant fyne.io/systray + webview/webview dans un seul processus. Icône tray avec menu contextuel, fenêtre webview 420×540px ouverte/fermée à la demande, serveur HTTP local embarqué servant les assets frontend et exposant une API REST JSON. Polling status via IPC. Gère le proxy système WinINET. Singleton via mutex nommé Windows
- **Communication** — Service ↔ UI : IPC via named pipes (Windows) ou Unix sockets. Protocole JSON ligne par ligne, max 4 Ko. Le service est l'autorité, l'UI est un client IPC. Frontend JS ↔ UI Go : serveur HTTP local embarqué (API REST JSON sur 127.0.0.1:{port})

**Installeur NSIS (Windows) :**
- Installation : service SCM, UI autostart (HKCU), shortcuts desktop/Start menu, extension Chrome/Firefox via policies registre
- Désinstallation propre : restore proxy WinINET, suppression policies, suppression extensions, suppression shortcuts

### System Integration

- **Gestion DNS** — Interface DNSManager avec implémentations OS via build tags (Windows: netsh, Linux: resolv.conf/resolvectl, macOS: networksetup). Modification du resolver à l'activation, restauration à la désactivation ou crash (watchdog)
- **Élévation UAC** — Service SCM avec privilèges, pas d'UAC récurrent pour l'utilisateur
- **Watchdog DNS** — Goroutine surveillant le resolver toutes les 3 secondes, corrige le drift causé par des processus externes
- **Proxy HTTP CONNECT** — Serveur local (127.0.0.1:50113, loopback uniquement) interceptant le trafic navigateurs/apps. Volume tracker par domaine : bypass direct si > 500 Mo (cooldown 24h)
- **Proxy DNS local** — UDP listener 127.0.0.1:53, forward DoH via tunnel, blocklist NXDOMAIN filtering
- **Extension navigateur** — CRX/XPI pré-signés embarqués dans le binaire. Installation automatique via politiques navigateur (Chromium registry, Firefox policies.json). Extension ID dérivé cryptographiquement du CRX public key
- **Singleton** — Mutex nommé Windows empêchant les instances multiples de l'UI
- **Proxy système WinINET** — Configuration proxy HKCU au connect, restauration au disconnect. Recovery orphelin en cas de crash

### Auto-Update Strategy

- Vérification périodique des releases GitHub
- Téléchargement en arrière-plan + vérification signature Ed25519
- Installation au prochain démarrage
- Rollback automatique si la nouvelle version échoue
- Notification UI : "Mise à jour prête — appliquée au prochain démarrage"

### Platform Support

| Plateforme | Statut MVP | Notes |
|---|---|---|
| Windows 10/11 | Principal | Installeur NSIS (service SCM + UI webview/systray). WebView2 requis |

## Project Scoping & Phased Development

### MVP (Phase 1) — Valider le socle multi-relais

**Must-Have :**
- Client desktop 2 processus : service Windows SCM (kardianos/service) + UI unique (fyne.io/systray + webview/webview), communication IPC named pipes
- Installeur NSIS Windows (service SCM, UI autostart, shortcuts, extension deployment)
- Tunnel QUIC/HTTPS vers les relais via Cloudflare
- DNS-over-HTTPS à travers le tunnel (proxy DNS local UDP 127.0.0.1:53)
- Kill switch DNS (arrêt proxy DNS si tunnel coupé)
- Blocklist DNS domaines malveillants (StevenBlack/hosts, téléchargement périodique, stockage in-memory, NXDOMAIN filtering)
- Reconnexion automatique du tunnel (backoff exponentiel + circuit breaker)
- Proxy HTTP CONNECT local (IP camouflage trafic web, volume tracker bypass > 500 Mo)
- Multi-relais par pays + registre distribué signé Ed25519 (chaque relais sert /.well-known/relay-registry.json)
- Sélecteur de pays dans l'UI + failover automatique + latency measurement
- Extension navigateur WebExtension (Chrome MV3 + Firefox MV2) : routage intelligent, bypass gros fichiers > 50 Mo, health check proxy, fail-safe DIRECT
- Installation auto extension via politiques navigateur + CRX/XPI pré-signés embarqués
- Détection fuites WebRTC via STUN + politiques navigateur anti-fuite
- Session tokens Ed25519 signés (TTL 4h) pour le proxy CONNECT
- Auto-update via GitHub releases + vérification signature Ed25519 + rollback + rate limiting
- Relais VPS stateless multi-pays — binaires autonomes avec bandwidth limiting par IP
- Distribution Windows : installeur NSIS via GoReleaser (3 cibles : service, UI, relay)

**Hors MVP :**
- Proxy STUN transparent (substitution IP dans handshakes WebRTC) → Phase 2
- Registre dynamique avec API de gestion → Phase 2

### Phase 2 — Enrichissements

- Proxy STUN transparent (protection WebRTC avancée — substitution IP)
- Fallback DNS multi-résolveur (Cloudflare → Quad9)
- Auto-test de fuite périodique (10 min)
- Registre dynamique (API de gestion pour ajouter/retirer des relais sans redéployer)
- Certificate pinning renforcé

### Phase 3 — Expansion

- Connexion directe bypass Cloudflare (dernier recours)
- Diversification géographique des hébergeurs
- Warrant Canary sur plateformeliberte.fr
- Page santé publique auto-générée
- Authentification client (tokens/clés client pour restriction d'accès)

## Functional Requirements

### Tunnel & Connexion Réseau

- FR1: Le client peut établir un tunnel QUIC/HTTPS vers le relais sélectionné via Cloudflare au démarrage
- FR2: Le client peut se reconnecter automatiquement au relais après une perte de connexion
- FR3: Le client peut authentifier chaque relais via sa clé publique Ed25519 unique
- FR4: Les relais peuvent accepter et relayer les connexions QUIC/HTTPS entrantes des clients

### Protection DNS

- FR5: Le client peut rediriger toutes les requêtes DNS du système vers le tunnel (DNS-over-HTTPS)
- FR6: Le client peut modifier le resolver DNS système à l'activation
- FR7: Le client peut restaurer le resolver DNS système à la désactivation ou en cas de crash (watchdog)
- FR8: Le client peut bloquer toutes les requêtes DNS sortantes lorsque le tunnel est coupé (kill switch)
- FR8b: Le client peut filtrer les requêtes DNS via une blocklist de domaines malveillants, téléchargée périodiquement et stockée en mémoire

### Interface Utilisateur

- FR9: L'utilisateur peut voir l'état de protection via une fenêtre desktop (connecté/en cours/déconnecté) avec indicateur visuel
- FR10: L'utilisateur peut voir le pays sélectionné, le relais actif et l'IP visible dans la fenêtre
- FR11: L'utilisateur peut sélectionner un pays parmi les relais disponibles via un sélecteur avec drapeaux et nombre de relais
- FR12: L'utilisateur peut connecter/déconnecter Le Voile via la fenêtre ou le menu tray
- FR13: L'utilisateur peut accéder rapidement à la fenêtre via l'icône system tray (clic gauche = toggle fenêtre)
- FR13b: L'utilisateur peut quitter Le Voile via le menu clic droit du tray

### Démarrage & Lifecycle

- FR14: Le service Windows SCM démarre automatiquement au boot, l'UI démarre via clé autostart HKCU
- FR15: Le tray (fyne.io/systray) persiste en arrière-plan. Fermer la fenêtre webview la détruit — le tray et le service continuent. Réouverture via le menu tray "Ouvrir"
- FR16: L'utilisateur peut quitter complètement l'application via le menu tray, déclenchant l'arrêt propre (tunnel déconnecté, DNS restauré, proxy système WinINET nettoyé, politiques navigateur restaurées)

### Relais Multi-VPS

- FR17: Les relais peuvent recevoir et relayer des requêtes DNS-over-HTTPS et des connexions HTTP CONNECT vers les destinations
- FR18: Les relais peuvent fonctionner sans aucune persistence de données (stateless)
- FR19: Les relais peuvent être déployés comme binaires autonomes multi-plateforme sur des VPS
- FR19b: Les relais peuvent être organisés par pays, chaque pays disposant d'un ou plusieurs relais

### Distribution & Lancement

- FR20: L'utilisateur installe via l'installeur NSIS qui enregistre le service SCM, configure l'UI autostart, crée les shortcuts, et déploie l'extension navigateur
- FR21: Le service et les extensions persistent via registre Windows. Les politiques navigateur anti-WebRTC sont maintenues tant que Le Voile est installé
- FR22: Configuration utilisateur (pays préféré, préférences UI, relay domain, clé publique Ed25519) stockée en TOML dans %AppData%/LeVoile/ (Windows). Cache registre relais en JSON séparé

### Découverte & Sélection de Relais

- FR23: Chaque relais peut servir le registre complet de tous les relais via un endpoint dédié (/registry)
- FR23b: Le client peut se connecter à un relais bootstrap hardcodé au premier lancement pour obtenir le registre initial
- FR24: Le client peut sélectionner un relais par pays choisi par l'utilisateur
- FR25: Le client peut distribuer les connexions entre les relais d'un même pays via round-robin
- FR26: Le client peut basculer automatiquement vers un autre relais du même pays en cas d'échec (timeout 3s, erreur 503, perte connexion)

### IP Camouflage

- FR27: Le client peut exposer un proxy HTTP CONNECT local sur l'interface loopback pour tunneliser le trafic web via le relais sélectionné
- FR28: Le relais peut établir des connexions TCP sortantes vers les destinations et relayer le trafic bidirectionnellement
- FR29: Le relais peut authentifier les requêtes CONNECT via des session tokens Ed25519 signés (TTL 4h)
- FR30: Le relais peut limiter le nombre de connexions CONNECT par IP source (max 200 simultanées)
- FR30b: Le relais peut limiter le nombre de connexions DoH simultanées (max 150), rejetant les connexions excédentaires avec HTTP 503

### Protection Anti-Fuite

- FR31: Le client peut détecter les fuites IP WebRTC via des requêtes STUN Binding (RFC 5389)
- FR32: Le client peut appliquer des politiques navigateur anti-WebRTC sur Chromium (registre Windows) et Firefox (policies.json)
- FR33: Le client peut comparer l'IP détectée via STUN avec l'IP du tunnel pour vérifier l'absence de fuite
- FR34: Le client peut déclencher des callbacks de détection et de recovery en cas de fuite détectée

### Mise à Jour Automatique

- FR35: Le client peut vérifier périodiquement la disponibilité de nouvelles versions via les releases GitHub
- FR36: Le client peut télécharger, vérifier la signature Ed25519 et appliquer les mises à jour au prochain démarrage, avec rollback automatique si le tunnel n'est pas établi dans les 30 secondes après le premier lancement post-update

### Extension Navigateur

- FR37: L'extension peut router par défaut tout le trafic navigateur via le proxy Le Voile (127.0.0.1:50113)
- FR38: L'extension peut détecter les téléchargements volumineux (Content-Length > 50 Mo) et les basculer automatiquement en connexion directe
- FR39: Le client peut installer automatiquement l'extension via les politiques navigateur (Chromium registry, Firefox policies.json)
- FR40: L'extension peut cohabiter avec le SysProxy — l'extension gère le routage navigateur, le SysProxy gère les applications hors navigateur

## Non-Functional Requirements

### Security

- NFR1: Communications client-relais chiffrées via QUIC/HTTPS (TLS 1.3 minimum)
- NFR2: Authentification client-relais exclusivement Ed25519 via bibliothèques cryptographiques standard — une paire de clés unique par relais
- NFR3: Les relais ne persistent aucune donnée au-delà de la durée d'une requête
- NFR4: Trafic tunnel indiscernable du trafic HTTPS standard par analyse DPI — vérifié par capture Wireshark (0 pattern-match VPN sur 100 échantillons de trafic)
- NFR5: Aucune fuite DNS pendant le fonctionnement normal ou la reconnexion — vérifié par dnsleaktest.com
- NFR6: Resolver DNS système restauré dans tous les scénarios (désactivation, crash, fermeture)
- NFR7: Code source publiquement auditable sur GitHub
- NFR8: Les relais valident que les requêtes proviennent des plages IP Cloudflare — requêtes hors plage rejetées
- NFR9: Les relais bloquent les connexions CONNECT vers les réseaux privés (loopback, RFC 1918, link-local) — protection SSRF

### Performance

- NFR10: Résolution DNS via tunnel : < 50ms de latence additionnelle — mesuré par tests automatisés (requêtes DNS avant/après tunnel)
- NFR11: Établissement tunnel initial : < 3 secondes sur connexion standard — mesuré par chronométrage applicatif
- NFR12: Reconnexion automatique : initiation < 1 seconde après perte — mesuré par chronométrage applicatif
- NFR13: Consommation RAM client : < 20 MB en fonctionnement normal — mesuré via Task Manager / profiling mémoire
- NFR14: Impact CPU en état stable : < 1% d'utilisation CPU — mesuré via Task Manager / profiling CPU sur 5 minutes

### Reliability

- NFR15: Kill switch DNS : activation < 100ms après détection perte tunnel — mesuré par chronométrage applicatif
- NFR16: Watchdog DNS : restauration resolver < 5 secondes après crash processus — mesuré par test de crash provoqué
- NFR17: Crash-recovery : au redémarrage, le client détecte et restaure l'état DNS/proxy orphelin du crash précédent < 5 secondes — mesuré par test de crash provoqué
- NFR18: Uptime par relais : ≥ 99.5% mensuel mesuré via endpoint /health
- NFR19: Failover entre relais d'un même pays : bascule < 5 secondes, 0 requête HTTP échouée côté client pendant la bascule — mesuré par test de coupure relais sous charge

### Privacy

- NFR20: Aucun log IP client sur les relais — ni requêtes DNS, ni connexions CONNECT, ni accès /registry
- NFR21: IP source hashée (SHA256) uniquement dans les session tokens Ed25519 (TTL 4h, non persisté)
