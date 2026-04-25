---
stepsCompleted: [1, 2, 3, 4]
inputDocuments: ['idées.txt']
session_topic: 'Le Voile — nouvelles fonctionnalités (protection WebRTC, etc.)'
session_goals: 'Générer des idées de fonctionnalités innovantes fidèles à la philosophie minimaliste et zero-config'
selected_approach: 'ai-recommended'
techniques_used: ['SCAMPER Method', 'Cross-Pollination', 'Chaos Engineering']
ideas_generated: 16
session_active: false
workflow_completed: true
context_file: ''
---

# Brainstorming Session Results

**Facilitateur:** Akerimus
**Date:** 2026-03-08

## Session Overview

**Sujet :** Le Voile — explorer de nouvelles fonctionnalités, notamment la protection WebRTC contre les fuites IP
**Objectifs :** Générer des idées de fonctionnalités innovantes fidèles à la philosophie minimaliste et zero-config

### Contexte

Projet existant avec vision produit définie : client desktop Go minimaliste, relais VPS stateless (Islande), tunnel QUIC/HTTPS via Cloudflare, DNS-over-HTTPS, blocklist communautaire, zero-log, zero-config, gratuit avec donations.

### Setup de session

- Approche sélectionnée : Techniques recommandées par l'IA
- Hors scope : croissance, monétisation, défis techniques déjà documentés

## Technique Selection

**Approche :** Techniques recommandées par l'IA
**Contexte d'analyse :** Le Voile — nouvelles fonctionnalités, philosophie minimaliste et zero-config

**Techniques recommandées :**

- **SCAMPER Method :** Exploration systématique des modifications/extensions possibles au produit existant
- **Cross-Pollination :** Emprunter des solutions d'autres domaines (ad-blockers, Tor, anti-fingerprinting, etc.)
- **Chaos Engineering :** Stress-tester mentalement Le Voile pour révéler des fonctionnalités défensives

**Raisonnement IA :** Séquence conçue pour partir du concret (SCAMPER sur l'existant), élargir vers l'inattendu (Cross-Pollination), puis consolider par la résilience (Chaos Engineering)

## Technique Execution Results

### SCAMPER Method

**Substitute (Substituer) :**

- **#1 — Notification tray native (vérification IP)** : Remplacer la page web de vérification IP par une notification système native affichant l'IP visible et le statut de protection directement dans le tray. Zéro navigateur, zéro page web à maintenir.
- **#2 — Proxy STUN via tunnel existant (WebRTC)** : Intercepter uniquement les requêtes STUN/TURN de découverte d'IP (~100 bytes) et les router via le tunnel existant vers le relais. Le relais relaye la requête et renvoie l'IP du VPS. WebRTC fonctionne normalement, IP masquée, zéro infra supplémentaire. Ne route PAS les flux média (audio/vidéo) — uniquement le handshake.

**Adapt (Adapter) :**

- **#3 — Warrant Canary** : Déclaration publique sur plateformeliberte.fr confirmant l'absence de demandes légales, mise à jour mensuellement. Sa disparition = signal d'alerte. Coût zéro, renforce la posture "rien à cacher".

**Modify (Modifier / Amplifier) :**

- **#4 — Découverte dynamique des relais (zero-knowledge du code source)** : Aucune adresse serveur hardcodée dans le binaire. Le client découvre les relais via un mécanisme de résolution (DNS TXT ou endpoint Cloudflare Workers). Quelqu'un qui décompile le binaire ne trouve aucun serveur cible.
- **#5 — Page santé publique auto-générée** : Page sur plateformeliberte.fr affichant en temps réel : uptime relais, version client la plus déployée, nombre de connexions actives (compteur simple, sans IP). Données générées automatiquement par le relais.

**Eliminate (Éliminer) :**

- **#6 — Supprimer la page web de vérification IP** : Le tooltip tray natif (#1) remplace entièrement la page web. Moins de surface d'attaque, plus d'autonomie.
- **#7 — Double distribution (installateur + portable)** : Installateur standard + binaire portable. Message d'avertissement sur la page de téléchargement pour la version portable ("Votre antivirus peut signaler ce fichier"). Pas d'Authenticode au départ.
- **Supprimer le fallback Cloudflare Workers** : Rester sur deux VPS (principal + backup). Moins de complexité, architecture homogène.

**Reverse (Inverser) :**

- **#8 — Blocklist communautaire pull (décentralisée)** : Au lieu de maintenir une blocklist propriétaire, Le Voile tire au démarrage depuis des sources communautaires existantes (hosts files GitHub type StevenBlack/hosts, Energized). Maintenance zéro côté Le Voile.

### Cross-Pollination

- **#9 — Certificate pinning du relais** (inspiré Signal/WireGuard) : Le client embarque le hash de la clé publique du relais et vérifie à chaque connexion. Réutilise l'infrastructure Ed25519 existante.
- **#10 — Sélection automatique du relais optimal** (inspiré CDN) : Quand plusieurs VPS sont disponibles, le client mesure la latence au démarrage et se connecte au plus rapide. Failover automatique.
- **#11 — Rollback automatique de mise à jour** (inspiré Tesla OTA) : Si une nouvelle version cause des échecs de connexion dans les 5 premières minutes, retour automatique à la version précédente. Le binaire précédent est conservé localement.
- **#12 — Auto-test de fuite périodique** (inspiré dispositifs médicaux) : Vérification automatique toutes les 10 minutes que DNS et IP passent bien par le tunnel. Icône orange si fuite détectée, reconnexion automatique. Standard des VPN premium (Mullvad, IVPN, ExpressVPN).

### Chaos Engineering

- **#13 — Fallback DNS multi-résolveur** (scénario : réseau bloque Cloudflare DoH) : Si DoH Cloudflare est inaccessible, basculer automatiquement sur Quad9 (9.9.9.9). Le DNS reste toujours chiffré.
- **#14 — Diversification géographique des hébergeurs** (scénario : saisie VPS) : VPS répartis chez des hébergeurs différents dans des juridictions différentes. Résilience juridictionnelle.
- **#15 — Connexion directe bypass Cloudflare** (scénario : Cloudflare bloque le domaine) : Mode de connexion directe au VPS en dernier recours. Troisième niveau de fallback : QUIC → HTTPS via CF → Direct.
- **#16 — Kill switch DNS** (scénario : tunnel coupé temporairement) : Quand Le Voile est actif mais le tunnel est coupé, bloquer toutes les requêtes DNS sortantes. Aucune fuite pendant la reconnexion.

### Idées retirées (avec justification)

- Injection de bruit DNS : coût/bénéfice moyen, efficacité réelle débattue
- Blocklist DNS + règles STUN unifiées : mieux de séparer, formats et rythmes de MAJ différents
- Blocklist auto-améliorée : contredit le zero-log
- Canal de communication d'urgence : peut faire peur aux utilisateurs
- Blocklist comme outil pédagogique : hors scope
- Mécanisme MAJ comme infrastructure réutilisable : hors scope
- Détection trafic entrant (scans de ports) : hors périmètre VPN, dilue l'identité
- Profils réseau adaptatifs : inutile, Le Voile ne route que les flux légers (DNS), le laisser actif est négligeable
- Double signature Ed25519 : process de build sur machine isolée suffit
- Auto-diagnostic réseau avant/après : complexifie pour peu de valeur
- Sealed Sender : superflu, le relais est déjà stateless et zero-log
- Anti-fingerprinting : hors périmètre VPN, relève du navigateur
- Double vérification DNS : Cloudflare est fiable, alourdit le process
- API locale status : trop niche
- Rotation périodique de relais : risque de déconnexion en session

## Idea Organization and Prioritization

### Organisation thématique

**Thème 1 : Protection réseau avancée**
_Ce que Le Voile bloque et protège activement_

- #2 Proxy STUN via tunnel existant (protection WebRTC)
- #12 Auto-test de fuite périodique (toutes les 10 min)
- #16 Kill switch DNS (blocage DNS quand le tunnel tombe)
- #13 Fallback DNS multi-résolveur (Cloudflare → Quad9)

**Thème 2 : Résilience infrastructure**
_Le Voile ne meurt jamais_

- #4 Découverte dynamique des relais (zéro endpoint hardcodé)
- #10 Sélection automatique du relais optimal (latence)
- #15 Connexion directe bypass Cloudflare (dernier recours)
- #14 Diversification géographique des hébergeurs
- #11 Rollback automatique de mise à jour

**Thème 3 : Expérience utilisateur minimaliste**
_Zéro friction, feedback passif_

- #1 Notification tray native (vérification IP)
- #5 Tooltip tray "santé en un coup d'œil"
- #7 Double distribution (installateur + portable)
- #6 Supprimer page web vérification / Workers

**Thème 4 : Confiance et transparence**
_Prouver ce qu'on promet_

- #3 Warrant Canary
- #5 Page santé publique auto-générée
- #8 Blocklist communautaire pull (sources ouvertes)
- #9 Certificate pinning du relais

### Prioritisation par phase

**Phase MVP — Le Voile fonctionne (validation produit)**

| # | Fonctionnalité | Justification |
|---|---------------|---------------|
| 2 | Proxy STUN (WebRTC) | Différenciateur produit |
| 16 | Kill switch DNS | Sécurité de base — pas de fuite quand le tunnel tombe |
| 1 | Notification tray native | Remplace la page web de vérification, plus simple |
| 6 | Supprimer page web vérification | Conséquence directe du #1 — une chose en moins à construire |
| 7 | Double distribution (installateur + portable) | Distribution jour 1 |

**Phase 2 — Post-validation (le produit marche, on solidifie)**

| # | Fonctionnalité | Priorité |
|---|---------------|----------|
| 13 | Fallback DNS multi-résolveur | Haute — fiabilité |
| 12 | Auto-test de fuite périodique | Haute — confiance utilisateur |
| 9 | Certificate pinning du relais | Haute — sécurité |
| 4 | Découverte dynamique des relais | Haute — résilience + code open source propre |
| 11 | Rollback automatique mise à jour | Moyenne — sécurité des mises à jour |
| 8 | Blocklist communautaire pull | Moyenne — externalise la maintenance |
| 5 | Tooltip tray enrichi | Basse — cosmétique |

**Phase Growth — Le produit a du succès**

| # | Fonctionnalité |
|---|---------------|
| 10 | Sélection automatique relais optimal |
| 15 | Connexion directe bypass Cloudflare |
| 14 | Diversification géographique hébergeurs |
| 3 | Warrant Canary |
| 5 | Page santé publique auto-générée |

## Session Summary

**Résultats :**

- 16 idées retenues sur ~30 explorées
- 15 idées retirées avec justification
- 3 techniques créatives appliquées (SCAMPER, Cross-Pollination, Chaos Engineering)
- 4 thèmes identifiés : protection réseau, résilience, UX, confiance
- Priorisation en 3 phases : MVP → Post-validation → Growth

**Découvertes clés :**

- Le proxy STUN (WebRTC) est le vrai différenciateur — aucun VPN ne fait de substitution transparente d'IP dans les handshakes STUN
- La philosophie "flux légers uniquement" (pas d'audio/vidéo/téléchargement via le tunnel) est un atout architectural qui simplifie tout
- La priorité #1 est un produit fonctionnel pour validation, les fonctionnalités avancées viennent après

**Décisions architecturales prises pendant la session :**

- Le Voile ne route que les flux légers (DNS, STUN handshake) — jamais les flux média
- Pas d'Authenticode au démarrage, avertissement sur la page de téléchargement à la place
- Process de build sur machine isolée plutôt que double signature
- Listes DNS et STUN séparées (pas unifiées)
- Pas de profils réseau adaptatifs — Le Voile actif = toujours protégé, le coût est négligeable
