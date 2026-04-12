---
stepsCompleted: ['step-01-validate-prerequisites', 'step-02-design-epics', 'step-03-create-stories', 'step-04-final-validation']
inputDocuments: ['prd.md', 'architecture.md', 'ux-design-specification.md']
---

# bmad_vpn_le_voile_de_velia - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for bmad_vpn_le_voile_de_velia, decomposing the requirements from the PRD, UX Design if it exists, and Architecture requirements into implementable stories.

## Requirements Inventory

### Functional Requirements

**Tunnel & Connexion Réseau (FR1-4)**
- FR1: Le client peut établir un tunnel QUIC/HTTPS vers le relais sélectionné via Cloudflare au démarrage
- FR2: Le client peut se reconnecter automatiquement au relais après une perte de connexion
- FR3: Le client peut authentifier chaque relais via sa clé publique Ed25519 unique
- FR4: Les relais peuvent accepter et relayer les connexions QUIC/HTTPS entrantes des clients

**Protection DNS (FR5-8)**
- FR5: Le client peut rediriger toutes les requêtes DNS du système vers le tunnel (DNS-over-HTTPS)
- FR6: Le client peut modifier le resolver DNS système à l'activation
- FR7: Le client peut restaurer le resolver DNS système à la désactivation ou en cas de crash (watchdog)
- FR8: Le client peut bloquer toutes les requêtes DNS sortantes lorsque le tunnel est coupé (kill switch)

**Interface Utilisateur (FR9-13b)**
- FR9: L'utilisateur peut voir l'état de protection via une fenêtre desktop (connecté/en cours/déconnecté) avec indicateur visuel
- FR10: L'utilisateur peut voir le pays sélectionné, le relais actif et l'IP visible dans la fenêtre
- FR11: L'utilisateur peut sélectionner un pays parmi les relais disponibles via un sélecteur avec drapeaux et nombre de relais
- FR12: L'utilisateur peut connecter/déconnecter Le Voile via la fenêtre ou le menu tray
- FR13: L'utilisateur peut accéder rapidement à la fenêtre via l'icône system tray (clic gauche = toggle fenêtre)
- FR13b: L'utilisateur peut quitter Le Voile via le menu clic droit du tray

**Démarrage & Lifecycle (FR14-16)**
- FR14: Le service Windows SCM démarre automatiquement au boot, l'UI démarre via clé autostart HKCU
- FR15: Le tray (fyne.io/systray) persiste en arrière-plan. Fermer la fenêtre webview la détruit — le tray et le service continuent. Réouverture via le menu tray "Ouvrir"
- FR16: L'utilisateur peut quitter complètement l'application via le menu tray, déclenchant l'arrêt propre (tunnel déconnecté, DNS restauré, proxy système WinINET nettoyé, politiques navigateur restaurées)

**Relais Multi-VPS (FR17-19b)**
- FR17: Les relais peuvent recevoir et relayer des requêtes DNS-over-HTTPS et des connexions HTTP CONNECT vers les destinations
- FR18: Les relais peuvent fonctionner sans aucune persistence de données (stateless)
- FR19: Les relais peuvent être déployés comme binaires autonomes multi-plateforme sur des VPS
- FR19b: Les relais peuvent être organisés par pays, chaque pays disposant d'un ou plusieurs relais

**Distribution & Installation (FR20-22)**
- FR20: L'utilisateur installe via l'installeur NSIS qui enregistre le service SCM, configure l'UI autostart, crée les shortcuts, et déploie l'extension navigateur
- FR21: Le service et les extensions persistent via registre Windows. Les politiques navigateur anti-WebRTC sont maintenues tant que Le Voile est installé
- FR22: Configuration utilisateur (pays préféré, préférences UI, relay domain, clé publique Ed25519) stockée en TOML dans %AppData%/LeVoile/ (Windows). Cache registre relais en JSON séparé

**Découverte & Sélection de Relais (FR23-26)**
- FR23: Chaque relais peut servir le registre complet de tous les relais via un endpoint dédié (/registry)
- FR23b: Le client peut se connecter à un relais bootstrap hardcodé au premier lancement pour obtenir le registre initial
- FR24: Le client peut sélectionner un relais par pays choisi par l'utilisateur
- FR25: Le client peut distribuer les connexions entre les relais d'un même pays via round-robin
- FR26: Le client peut basculer automatiquement vers un autre relais du même pays en cas d'échec (timeout 3s, erreur 503, perte connexion)

**IP Camouflage (FR27-30)**
- FR27: Le client peut exposer un proxy HTTP CONNECT local sur l'interface loopback pour tunneliser le trafic web via le relais sélectionné
- FR28: Le relais peut établir des connexions TCP sortantes vers les destinations et relayer le trafic bidirectionnellement
- FR29: Le relais peut authentifier les requêtes CONNECT via des session tokens Ed25519 signés (TTL 4h)
- FR30: Le relais peut limiter le nombre de connexions CONNECT par IP source (max 200 simultanées)
- FR30b: Le relais peut limiter le nombre de connexions DoH simultanées (max 150), rejetant les connexions excédentaires avec HTTP 503

**Protection Anti-Fuite (FR31-34)**
- FR31: Le client peut détecter les fuites IP WebRTC via des requêtes STUN Binding (RFC 5389)
- FR32: Le client peut appliquer des politiques navigateur anti-WebRTC sur Chromium (registre Windows) et Firefox (policies.json)
- FR33: Le client peut comparer l'IP détectée via STUN avec l'IP du tunnel pour vérifier l'absence de fuite
- FR34: Le client peut déclencher des callbacks de détection et de recovery en cas de fuite détectée

**Mise à Jour Automatique (FR35-36)**
- FR35: Le client peut vérifier périodiquement la disponibilité de nouvelles versions via les releases GitHub
- FR36: Le client peut télécharger, vérifier la signature Ed25519 et appliquer les mises à jour au prochain démarrage, avec rollback automatique en cas d'échec

**Extension Navigateur (FR37-40)**
- FR37: L'extension peut router par défaut tout le trafic navigateur via le proxy Le Voile (127.0.0.1:50113)
- FR38: L'extension peut détecter les téléchargements volumineux (Content-Length > 50 Mo) et les basculer automatiquement en connexion directe
- FR39: Le client peut installer automatiquement l'extension via les politiques navigateur (Chromium registry, Firefox policies.json)
- FR40: L'extension peut cohabiter avec le SysProxy — l'extension gère le routage navigateur, le SysProxy gère les applications hors navigateur

### NonFunctional Requirements

**Security (NFR1-9)**
- NFR1: Communications client-relais chiffrées via QUIC/HTTPS (TLS 1.3 minimum)
- NFR2: Authentification client-relais exclusivement Ed25519 via bibliothèques cryptographiques standard — une paire de clés unique par relais
- NFR3: Les relais ne persistent aucune donnée au-delà de la durée d'une requête
- NFR4: Trafic tunnel indiscernable du trafic HTTPS standard par analyse DPI — vérifié par capture Wireshark (0 pattern-match VPN sur 100 échantillons)
- NFR5: Aucune fuite DNS pendant le fonctionnement normal ou la reconnexion — vérifié par dnsleaktest.com
- NFR6: Resolver DNS système restauré dans tous les scénarios (désactivation, crash, désinstallation)
- NFR7: Code source publiquement auditable sur GitHub
- NFR8: Les relais valident que les requêtes proviennent des plages IP Cloudflare — requêtes hors plage rejetées
- NFR9: Les relais bloquent les connexions CONNECT vers les réseaux privés (loopback, RFC 1918, link-local) — protection SSRF

**Performance (NFR10-14)**
- NFR10: Résolution DNS via tunnel : < 50ms de latence additionnelle
- NFR11: Établissement tunnel initial : < 3 secondes sur connexion standard
- NFR12: Reconnexion automatique : initiation < 1 seconde après perte
- NFR13: Consommation RAM client : < 20 MB en fonctionnement normal
- NFR14: Impact CPU en état stable : < 1% d'utilisation CPU

**Reliability (NFR15-19)**
- NFR15: Kill switch DNS : activation < 100ms après détection perte tunnel
- NFR16: Watchdog DNS : restauration resolver < 5 secondes après crash service
- NFR17: Service : redémarrage automatique < 10 secondes après crash
- NFR18: Uptime par relais : ≥ 99.5% mensuel mesuré via endpoint /health
- NFR19: Failover entre relais d'un même pays : bascule < 5 secondes, 0 requête HTTP échouée côté client pendant la bascule

**Privacy (NFR20-21)**
- NFR20: Aucun log IP client sur les relais — ni requêtes DNS, ni connexions CONNECT, ni accès /registry
- NFR21: IP source hashée (SHA256) uniquement dans les session tokens Ed25519 (TTL 4h, non persisté)

### Additional Requirements

**Architecture — Starter Template & Structure**
- Monorepo Go : `cmd/client/` + `cmd/ui/` + `cmd/relay/` + `internal/` (~18 packages) + `frontend/`
- GoReleaser v2 pour build & distribution (3 cibles : service Win, UI Win, relay Linux)

**Architecture — Bibliothèques & Dépendances**
- quic-go v0.59.0 pour le tunnel QUIC/HTTPS (HTTP/3 client + serveur)
- webview/webview pour la fenêtre desktop (WebView2 sur Windows)
- fyne.io/systray pour le system tray (même processus que webview)
- kardianos/service pour la gestion service Windows SCM
- Go standard `testing` pour les tests

**Architecture — Communication & Protocoles**
- Client ↔ Relais (DoH) : HTTP/3 via quic-go, requêtes POST HTTPS DoH (RFC 8484)
- Client ↔ Relais (CONNECT) : POST `https://{country}.levoile.dev/connect` avec session token Authorization
- IPC Service ↔ UI : Named pipe Windows (`\\.\pipe\levoile`) + messages JSON ligne par ligne
- UI interne : Serveur HTTP local embarqué (127.0.0.1:{port}) — API REST JSON pour le webview, sert les assets frontend
- Proxy local sur `127.0.0.1:50113` (loopback uniquement)

**Architecture — Infrastructure & Déploiement**
- Relais déployés via scp + systemd sur VPS
- Cloudflare : un sous-domaine par pays (ex: is.levoile.dev, fr.levoile.dev), orange cloud
- Endpoint `/health` anonyme sur chaque relais pour le monitoring
- Config client : TOML dans %AppData%/LeVoile/ (pays préféré, cache registre)
- Cache registre local pour cold start résilient
- Installeur NSIS Windows (service SCM, UI autostart, shortcuts, extension deployment)

**Architecture — Sécurité**
- Session tokens : Ed25519 signés, TTL 4h, IP hash SHA256 dans payload, refresh auto avec backoff exponentiel
- Rate limiting : 200 connexions CONNECT max par IP via sync.Map + atomics (lock-free)
- Validation source Cloudflare (plages IP) sur chaque relais
- Protection SSRF : blocage connexions CONNECT vers réseaux privés
- Limite 150 connexions DoH simultanées par relais (503 au-delà)

**Architecture — Séquence d'implémentation recommandée**
1. Structure projet + go.mod + dépendances
2. Module crypto (Ed25519)
3. Module registre (modèle de données, chargement JSON, cache)
4. Relais stateless (HTTP/3 server, DoH, /connect, /health, /registry)
5. Module tunnel client (HTTP/3 client, QUIC, reconnexion, session tokens)
6. Module sélecteur (pays, round-robin, failover)
7. Module proxy HTTP CONNECT local
8. Module DNS (interface + Windows, kill switch, watchdog)
9. Module anti-fuite (leakcheck STUN, politiques navigateur)
10. Module IPC + handler
11. UI unique (fyne.io/systray + webview/webview + serveur HTTP local, charte plateformeliberte.fr)
12. Module elevation (UAC)
13. Module updater (GitHub releases, signature, rollback)
14. Service OS (kardianos/service)
15. Intégration bout en bout + distribution

**UX Design — Exigences additionnelles**
- Zero-config absolu : aucun écran de configuration à l'installation
- UI reproduisant exactement la charte plateformeliberte.fr (thème sombre #0b1526, bleus #1a6fc4/#2a8dff, rouge #d42b2b, fonts Bebas Neue/Rajdhani/Inter)
- Tray-first UX : le system tray est l'interface principale, la fenêtre webview est secondaire (ouverte/fermée à la demande)
- Circuit de confiance : lien direct vers plateformeliberte.fr/test-protection.html depuis l'UI
- Messages en français non-technique ("Reconnexion en cours..." jamais de jargon)
- Sélecteur de pays : 4 pays (Islande, Allemagne, Finlande, US) avec drapeaux emoji et nombre de relais
- Indicateur de statut coloré : vert (connecté), orange (en cours), rouge (déconnecté)
- IP visible affichée en permanence dans la fenêtre
- Notification de mise à jour non-intrusive dans le tray
- Design tokens CSS (variables CSS) extraits de plateformeliberte.fr
- Frontend vanilla HTML/CSS/JS — pas de framework, pas de bundler, servi par HTTP local embarqué

### FR Coverage Map

**Epics 1-9 (DONE — implémentés) :**
- FR1: Epic 1 (ancien) — Tunnel QUIC/HTTPS établi via Cloudflare
- FR2: Epic 2 (ancien) — Reconnexion automatique
- FR3: Epic 1 (ancien) — Authentification Ed25519 relais
- FR4: Epic 1 (ancien) — Relais accepte connexions QUIC/HTTPS
- FR5: Epic 2 (ancien) — Redirection DNS vers DoH tunnel
- FR6: Epic 2 (ancien) — Modification resolver DNS système
- FR7: Epic 2 (ancien) — Restauration resolver (watchdog)
- FR8: Epic 2 (ancien) — Kill switch DNS
- FR14: Epic 3 (ancien) — Démarrage automatique service
- FR17: Epic 1 (ancien) — Relais DoH + CONNECT
- FR18: Epic 1 (ancien) — Relais stateless
- FR19: Epic 1 (ancien) — Binaires autonomes multi-plateforme
- FR19b: Epic 9 (ancien) — Relais organisés par pays
- FR20: Epic 4 (ancien) — Installation NSIS (service SCM, UI autostart, shortcuts, extension)
- FR21: Epic 4 (ancien) — Persistence service + extensions via registre Windows
- FR22: Epic 4 (ancien) — Config TOML %AppData%/LeVoile/
- FR23: Epic 9 (ancien) — Registre distribué /registry
- FR23b: Epic 9 (ancien) — Relais bootstrap hardcodé (config relay domain par défaut)
- FR24: Epic 9 (ancien) — Sélection relais par pays
- FR25: Epic 9 (ancien) — Round-robin intra-pays
- FR26: Epic 9 (ancien) — Failover automatique
- FR27: Epic IP Camouflage (tech-spec) — Proxy HTTP CONNECT local
- FR28: Epic IP Camouflage (tech-spec) — Connexions TCP sortantes relais
- FR29: Epic IP Camouflage (tech-spec) — Session tokens Ed25519
- FR30: Epic Rate Limiting (tech-spec) — Limite connexions CONNECT par IP
- FR30b: Relais (tech-spec) — Limite DoH simultanées (150 max, middleware rejection)
- FR31: Epic 5 (ancien) — Détection fuites WebRTC STUN
- FR32: Epic WebRTC Policies (tech-spec) — Politiques navigateur anti-WebRTC
- FR33: Epic 7 (ancien) — Comparaison IP STUN vs tunnel
- FR34: Epic 7 (ancien) — Callbacks détection/recovery fuites
- FR35: Epic 6 (ancien) — Vérification périodique GitHub releases
- FR36: Epic 6 (ancien) — Téléchargement, signature, rollback auto

**Epics 10-12 (NOUVEAUX — à implémenter) :**
- FR9: Epic 10 — Fenêtre desktop état de protection
- FR10: Epic 10 — Pays sélectionné, relais actif, IP visible dans fenêtre
- FR11: Epic 10 — Sélecteur pays avec drapeaux et nombre de relais
- FR12: Epic 10 — Connect/disconnect via fenêtre ou menu tray
- FR13: Epic 10 — Toggle fenêtre via clic gauche tray
- FR13b: Epic 12 — Quitter via tray (arrêt propre)
- FR15: Epic 10 — Tray persiste, fenêtre webview à la demande
- FR16: Epic 12 — Arrêt propre via tray
- FR37: Epic 11 — Extension route trafic via proxy Le Voile
- FR38: Epic 11 — Bypass téléchargements > 50 Mo
- FR39: Epic 11 — Installation auto extension via politiques navigateur
- FR40: Epic 11 — Cohabitation SysProxy + extension

## Epics Précédents (1-9) — TERMINÉS

### Epic 1 : Relais VPS Stateless Déployé et Opérationnel — DONE
L'opérateur peut déployer un relais stateless fonctionnel acceptant les connexions QUIC/HTTPS et requêtes DoH.
**FRs couverts :** FR1, FR3, FR4, FR17, FR18, FR19
**Stories :** 1-1, 1-2, 1-3

### Epic 2 : Tunnel Chiffré et Protection DNS — DONE
L'utilisateur est protégé : tunnel chiffré, DNS via DoH, kill switch, watchdog, reconnexion auto.
**FRs couverts :** FR2, FR5, FR6, FR7, FR8
**Stories :** 2-1, 2-2, 2-3

### Epic 3 : Expérience Desktop — Service, Tray et Lifecycle — DONE
L'utilisateur interagit via le system tray, le service tourne en arrière-plan avec IPC.
**FRs couverts :** FR14
**Stories :** 3-1, 3-2, 3-3

### Epic 4 : Distribution et Installation Windows — DONE
L'utilisateur peut installer Le Voile via l'installeur NSIS Windows (service SCM, UI autostart, shortcuts, extension).
**FRs couverts :** FR20, FR21, FR22
**Stories :** 4-1, 4-2

### Epic 5 : Proxy STUN Transparent — Protection WebRTC — DONE
L'utilisateur est protégé contre les fuites WebRTC via interception et relais STUN.
**FRs couverts :** FR31
**Stories :** 5-1, 5-2, 5-3

### Epic 6 : Auto-Update et Rollback — DONE
Le client se met à jour automatiquement avec vérification signature et rollback.
**FRs couverts :** FR35, FR36
**Stories :** 6-1, 6-2, 6-3

### Epic 7 : Résilience DNS et Auto-Diagnostic — DONE
Fallback DNS multi-résolveur, cert pinning, auto-test de fuite périodique.
**FRs couverts :** FR33, FR34
**Stories :** 7-1, 7-2

### Epic 8 : Blocklist DNS Communautaire — DONE
Filtrage DNS local via StevenBlack/hosts avec contrôle utilisateur.
**Stories :** 8-1, 8-2

### Epic 9 : Découverte Dynamique des Relais — DONE
Registre distribué, sélection par latence, failover automatique multi-pays.
**FRs couverts :** FR19b, FR23, FR24, FR25, FR26
**Stories :** 9-1, 9-2

### Tech Specs Implémentés (hors epics) — DONE
IP Camouflage proxy CONNECT, rate limiting relais, politiques navigateur WebRTC.
**FRs couverts :** FR27, FR28, FR29, FR30, FR32

---

## Nouveaux Epics (10-12)

## Epic 10 : Interface Desktop webview/webview + systray

L'utilisateur accède à une fenêtre desktop complète reproduisant la charte plateformeliberte.fr — sélecteur de pays avec drapeaux, état de connexion visuel, relais actif, IP visible, bouton connect/disconnect, lien vers test-protection.html. Le tray ouvre la fenêtre webview (clic gauche). L'UI combine fyne.io/systray + webview/webview dans un seul processus (`levoile-ui.exe`), communiquant avec le service via IPC.

**FRs couverts :** FR9, FR10, FR11, FR12, FR13, FR15

### Story 10.1 : Binaire UI unique (systray + webview + serveur HTTP local) avec Statut de Connexion

En tant qu'utilisateur,
je veux voir une fenêtre desktop affichant l'état de ma protection (connecté/en cours/déconnecté) avec un indicateur visuel coloré,
afin de savoir immédiatement si je suis protégé.

**Acceptance Criteria:**

**Given** le service Le Voile est démarré
**When** l'utilisateur ouvre la fenêtre via le menu tray "Ouvrir"
**Then** une fenêtre webview (420×540px) s'ouvre et navigue vers `http://127.0.0.1:{port}/`
**And** l'indicateur de statut affiche l'état actuel (vert connecté, orange en cours, rouge déconnecté)
**And** la fenêtre reproduit la charte plateformeliberte.fr (fond sombre #0b1526, bleus #1a6fc4/#2a8dff, fonts Bebas Neue/Rajdhani/Inter)

**Given** le tunnel change d'état (connexion, perte, reconnexion)
**When** le frontend poll `fetch('/api/status')` toutes les 2 secondes
**Then** l'indicateur visuel change de couleur en temps réel
**And** un message en français non-technique est affiché ("Connecté — Islande", "Reconnexion en cours...", "Déconnecté")

**Given** le projet existant (monorepo Go, cmd/, internal/)
**When** le binaire UI est créé (`cmd/ui/main.go`)
**Then** fyne.io/systray démarre sur le main thread (bloquant)
**And** un serveur HTTP local embarqué (net/http, 127.0.0.1:{port}) sert les assets frontend (`frontend/`) via `go:embed`
**And** le serveur expose une API REST JSON (/api/status, /api/connect, /api/disconnect, /api/country, /api/registry, etc.) qui proxie vers le service via IPC client
**And** les design tokens CSS sont extraits de plateformeliberte.fr
**And** le frontend utilise `fetch()` pour communiquer avec le serveur HTTP local (pas de bindings Go↔JS directs)

### Story 10.2 : Sélecteur de Pays et Affichage IP Visible

En tant qu'utilisateur,
je veux sélectionner un pays parmi les relais disponibles via des drapeaux et voir mon IP visible,
afin de choisir depuis quel pays je souhaite apparaître et vérifier que mon IP est masquée.

**Acceptance Criteria:**

**Given** la fenêtre desktop est ouverte et le registre des relais est chargé
**When** le sélecteur de pays est affiché
**Then** chaque pays disponible est affiché avec son drapeau emoji et le nombre de relais actifs
**And** le pays actuellement sélectionné est mis en évidence

**Given** l'utilisateur clique sur un drapeau de pays différent
**When** la sélection change
**Then** le tunnel se reconnecte via un relais du nouveau pays (< 5s)
**And** l'IP visible affichée dans la fenêtre se met à jour
**And** le pays préféré est sauvegardé dans la config TOML

**Given** le tunnel est connecté
**When** la fenêtre est affichée
**Then** l'IP visible actuelle, le pays sélectionné et l'identifiant du relais actif sont affichés
**And** un lien "Tester ma protection" pointe vers plateformeliberte.fr/test-protection.html

### Story 10.3 : Bouton Connect/Disconnect et Intégration Tray ↔ Webview

En tant qu'utilisateur,
je veux connecter/déconnecter Le Voile depuis la fenêtre ou le tray, et ouvrir la fenêtre depuis le tray,
afin de contrôler ma protection depuis n'importe quel point d'accès.

**Acceptance Criteria:**

**Given** la fenêtre webview est ouverte
**When** l'utilisateur clique sur le bouton Connect/Disconnect
**Then** le frontend appelle `fetch('/api/connect')` ou `fetch('/api/disconnect')`
**And** le serveur HTTP local proxie la commande vers le service via IPC
**And** l'indicateur de statut et l'icône tray se mettent à jour simultanément

**Given** le tray icon est visible dans la barre des tâches
**When** l'utilisateur sélectionne "Ouvrir" dans le menu tray
**Then** une fenêtre webview est créée (ou montrée si elle existe déjà)

**Given** la fenêtre webview est ouverte
**When** l'utilisateur ferme la fenêtre (croix)
**Then** la fenêtre webview est détruite
**And** le tray et le service continuent de fonctionner
**And** la protection reste active
**And** une réouverture depuis le tray montre la fenêtre masquée (pas de recréation)

**Given** le menu clic droit du tray contient "Ouvrir la fenêtre" et "Quitter"
**When** l'utilisateur sélectionne "Quitter"
**Then** le tunnel se déconnecte, le proxy WinINET est restauré, le service s'arrête, le tray disparaît

---

## Epic 11 : Extension Navigateur & Routage Intelligent

L'extension navigateur route automatiquement tout le trafic via Le Voile, avec bypass intelligent des téléchargements > 50 Mo pour préserver la bande passante des relais. Installation automatique via politiques navigateur. Cohabitation SysProxy + extension.

**FRs couverts :** FR37, FR38, FR39, FR40

### Story 11.1 : Extension WebExtension — Routage par Défaut via Proxy Le Voile

En tant qu'utilisateur,
je veux que tout mon trafic navigateur soit automatiquement routé via Le Voile sans aucune action de ma part,
afin que mon IP soit masquée pour toute navigation.

**Acceptance Criteria:**

**Given** l'extension est installée dans Chrome (MV3) ou Firefox
**When** l'utilisateur navigue sur n'importe quel site
**Then** le trafic est routé via le proxy Le Voile (127.0.0.1:50113) via proxy.onRequest
**And** aucune UI/popup n'est affichée dans l'extension

**Given** le proxy Le Voile est actif (service connecté)
**When** l'extension est chargée
**Then** l'extension configure automatiquement le routage via proxy.onRequest
**And** tout le trafic HTTP/HTTPS passe par le proxy local

**Given** le proxy Le Voile est inactif (service déconnecté)
**When** l'extension détecte l'absence du proxy
**Then** le trafic passe en connexion directe (fallback gracieux)
**And** aucune erreur n'est affichée à l'utilisateur

### Story 11.2 : Bypass Intelligent des Téléchargements Volumineux

En tant qu'utilisateur,
je veux que les téléchargements volumineux (> 50 Mo) passent automatiquement en connexion directe,
afin de préserver la bande passante des relais et télécharger rapidement.

**Acceptance Criteria:**

**Given** l'extension route le trafic via le proxy Le Voile
**When** une réponse HTTP contient un header Content-Length > 50 Mo
**Then** l'extension détecte la taille via webRequest.onHeadersReceived
**And** le téléchargement est basculé automatiquement en connexion directe

**Given** un téléchargement < 50 Mo est en cours
**When** la réponse HTTP est reçue
**Then** le téléchargement passe par le proxy normalement
**And** l'IP reste masquée

**Given** une réponse HTTP sans header Content-Length (streaming, chunked)
**When** l'extension ne peut pas déterminer la taille
**Then** le trafic continue via le proxy par défaut (comportement sûr)

### Story 11.3 : Installation Automatique et Cohabitation SysProxy

En tant qu'utilisateur,
je veux que l'extension s'installe automatiquement dans mes navigateurs et cohabite avec le SysProxy existant,
afin d'être protégé sans aucune action manuelle, dans les navigateurs comme dans les autres applications.

**Acceptance Criteria:**

**Given** Le Voile est installé et le service démarre
**When** les politiques navigateur sont appliquées (via `internal/browser/`)
**Then** l'extension est installée automatiquement dans Chromium (registre Windows) et Firefox (policies.json)
**And** aucune action utilisateur n'est requise

**Given** l'extension est active dans le navigateur ET le SysProxy est configuré
**When** l'utilisateur navigue dans le navigateur
**Then** l'extension gère le routage (proxy.onRequest prend la priorité)
**And** le SysProxy reste actif pour les applications hors navigateur (Electron, Windows apps)

**Given** l'extension est installée dans Chrome MV3
**When** l'extension est chargée
**Then** elle respecte les contraintes Manifest V3 (service worker, pas de background page persistante)

**Given** l'extension est installée dans Firefox
**When** l'extension est chargée
**Then** elle utilise la même API proxy (proxy.onRequest) compatible Firefox

---

## Epic 12 : Intégration Bout en Bout & Polish MVP Révisé

Le produit fonctionne de bout en bout avec l'architecture 2 processus : service + UI unique (tray + webview) + extension navigateur. Quitter via le tray arrête proprement la fenêtre webview et envoie Quit au service. Le service maintient la protection même si la fenêtre webview est fermée (le tray persiste).

**FRs couverts :** FR13b, FR16

### Story 12.1 : Shutdown Propre et Indépendance Service/UI

En tant qu'utilisateur,
je veux que quitter Le Voile via le tray arrête proprement tous les composants, et que la protection reste active tant que le service tourne même si la fenêtre webview est fermée,
afin de ne jamais avoir de fuite accidentelle ni de processus orphelin.

**Acceptance Criteria:**

**Given** la fenêtre webview est ouverte et le tray est actif
**When** l'utilisateur sélectionne "Quitter" via le menu clic droit du tray
**Then** la fenêtre webview est détruite
**And** le serveur HTTP local est arrêté
**And** l'UI envoie Quit au service via IPC (tunnel arrêté, DNS restauré, configs navigateur restaurées)
**And** le SysProxy est désactivé
**And** le processus UI se termine
**And** aucun processus orphelin ne subsiste

**Given** le service Le Voile est actif et le tunnel connecté
**When** l'utilisateur ferme la fenêtre webview (croix)
**Then** le tray continue de fonctionner (même processus)
**And** le service continue de fonctionner
**And** le tunnel reste connecté
**And** la protection DNS et IP reste active
**And** le kill switch DNS reste opérationnel

**Given** le service Le Voile est actif et la fenêtre webview est fermée
**When** l'utilisateur sélectionne "Ouvrir" dans le menu tray
**Then** une nouvelle fenêtre webview est créée
**And** l'état actuel (pays, relais, IP, statut) est affiché correctement via l'API REST locale

### Story 12.2 : Validation Bout en Bout et Tests d'Intégration

En tant qu'utilisateur,
je veux que l'ensemble des composants (service + UI tray/webview + extension + proxy + tunnel) fonctionne de manière cohérente et sans fuite,
afin d'avoir confiance que ma protection est réelle et complète.

**Acceptance Criteria:**

**Given** Le Voile est installé avec tous les composants (service, UI tray+webview, extension navigateur)
**When** l'utilisateur lance Le Voile
**Then** le service démarre (auto-boot SCM), l'UI démarre (autostart HKCU), le tray apparaît
**And** le tunnel se connecte, le DNS est redirigé, le proxy CONNECT écoute, l'extension route le trafic
**And** la fenêtre webview (si ouverte) affiche l'état correct via le polling API
**And** le tray affiche l'icône correcte
**And** zéro fuite DNS (vérifiable via dnsleaktest.com)
**And** zéro fuite IP (vérifiable via whatismyip.com)
**And** zéro fuite WebRTC (vérifiable via browserleaks.com)

**Given** Le Voile est connecté via un pays sélectionné
**When** le relais actif tombe
**Then** le failover bascule vers un autre relais du même pays (< 5s)
**And** le kill switch DNS protège pendant la bascule
**And** la fenêtre webview (si ouverte) affiche "Reconnexion..." puis le nouveau relais
**And** le tray met à jour l'icône pendant la transition

**Given** l'utilisateur change de pays via la fenêtre webview
**When** la reconnexion au nouveau pays est complète
**Then** l'IP affichée dans la fenêtre correspond au nouveau pays
**And** l'extension navigateur continue de router via le proxy
**And** le tray reflète le nouveau pays
