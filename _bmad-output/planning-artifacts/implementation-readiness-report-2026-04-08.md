---
stepsCompleted: [1, 2, 3, 4, 5, 6]
inputDocuments: ['prd.md', 'architecture.md', 'epics.md', 'ux-design-specification.md', 'prd-validation-report.md']
workflowType: 'implementation-readiness'
project_name: 'bmad_vpn_le_voile_de_velia'
user_name: 'Akerimus'
date: '2026-04-08'
---

# Implementation Readiness Assessment Report

**Date:** 2026-04-08
**Project:** bmad_vpn_le_voile_de_velia

## PRD Analysis

### Functional Requirements

- FR1: Tunnel QUIC/HTTPS vers relais via Cloudflare
- FR2: Reconnexion automatique après perte de connexion
- FR3: Authentification relais via Ed25519
- FR4: Relais acceptent connexions QUIC/HTTPS
- FR5: Redirection DNS système vers DoH tunnel
- FR6: Modification resolver DNS à l'activation
- FR7: Restauration resolver DNS (watchdog)
- FR8: Kill switch DNS quand tunnel coupé
- FR8b: Blocklist DNS StevenBlack/hosts
- FR9: Fenêtre desktop état de protection
- FR10: Pays, relais actif, IP visible dans fenêtre
- FR11: Sélecteur pays avec drapeaux
- FR12: Connect/disconnect via fenêtre ou tray
- FR13: Toggle fenêtre via tray
- FR13b: Quitter via tray
- FR14: Service SCM auto-boot + UI autostart HKCU
- FR15: Tray persiste, fenêtre webview ouverte/fermée à la demande
- FR16: Arrêt propre via tray
- FR17: Relais DoH + CONNECT
- FR18: Relais stateless
- FR19: Relais binaires autonomes multi-plateforme
- FR19b: Relais organisés par pays
- FR20: Installation NSIS (service SCM, UI autostart, shortcuts, extension)
- FR21: Service + extensions persistent via registre Windows
- FR22: Config TOML %AppData%/LeVoile/
- FR23: Registre distribué /registry
- FR23b: Relais bootstrap hardcodé
- FR24: Sélection relais par pays
- FR25: Round-robin intra-pays
- FR26: Failover automatique
- FR27: Proxy HTTP CONNECT local loopback
- FR28: Relais CONNECT TCP sortant bidirectionnel
- FR29: Session tokens Ed25519 (TTL 4h)
- FR30: Limite CONNECT par IP (200 max)
- FR30b: Limite DoH simultanées (150 max)
- FR31: Détection fuites WebRTC STUN
- FR32: Politiques navigateur anti-WebRTC
- FR33: Comparaison IP STUN vs IP tunnel
- FR34: Callbacks détection/recovery fuites
- FR35: Vérification périodique GitHub releases
- FR36: Auto-update + rollback
- FR37: Extension route trafic via proxy
- FR38: Bypass téléchargements > 50 Mo
- FR39: Installation auto extension via politiques
- FR40: Cohabitation SysProxy + extension

**Total FRs: 40** (FR1-40, incluant FR8b, FR13b, FR19b, FR23b, FR30b)

### Non-Functional Requirements

- NFR1: TLS 1.3 minimum (QUIC/HTTPS)
- NFR2: Ed25519 standard, clé unique par relais
- NFR3: Relais zero persistence
- NFR4: Indiscernabilité DPI (0 pattern-match VPN)
- NFR5: Zéro fuite DNS
- NFR6: Restauration DNS tous scénarios
- NFR7: Code auditable GitHub
- NFR8: Validation source Cloudflare IP
- NFR9: Protection SSRF réseaux privés
- NFR10: Latence DNS < 50ms additionnelle
- NFR11: Tunnel < 3s
- NFR12: Reconnexion < 1s
- NFR13: RAM < 20 MB
- NFR14: CPU < 1%
- NFR15: Kill switch < 100ms
- NFR16: Watchdog DNS < 5s
- NFR17: Crash-recovery < 5s
- NFR18: Uptime relais ≥ 99.5%
- NFR19: Failover < 5s, 0 requête perdue
- NFR20: Aucun log IP client
- NFR21: IP hash uniquement dans session tokens

**Total NFRs: 21** (NFR1-21)

### Additional Requirements

- Architecture: webview/webview + fyne.io/systray (binaire UI unique), kardianos/service, quic-go, IPC named pipes
- UX: Zero-config, charte plateformeliberte.fr, tray-first, messages français non-technique
- Infrastructure: Relais systemd, Cloudflare, GoReleaser 3 cibles

### PRD Completeness Assessment

Le PRD est complet et à jour (révisé 2026-04-08). Les 40 FRs et 21 NFRs couvrent tous les domaines. Le mode portable a été supprimé et l'architecture UI migrée de Wails v2 vers webview/webview + systray.

## Epic Coverage Validation

### Coverage Matrix

| FR | Exigence PRD | Couverture Epic | Statut |
|---|---|---|---|
| FR1 | Tunnel QUIC/HTTPS via Cloudflare | Epic 1 (DONE) | ✓ |
| FR2 | Reconnexion automatique | Epic 2 (DONE) | ✓ |
| FR3 | Authentification Ed25519 relais | Epic 1 (DONE) | ✓ |
| FR4 | Relais accepte QUIC/HTTPS | Epic 1 (DONE) | ✓ |
| FR5 | DNS DoH via tunnel | Epic 2 (DONE) | ✓ |
| FR6 | Modification resolver DNS | Epic 2 (DONE) | ✓ |
| FR7 | Restauration resolver (watchdog) | Epic 2 (DONE) | ✓ |
| FR8 | Kill switch DNS | Epic 2 (DONE) | ✓ |
| FR8b | Blocklist DNS StevenBlack/hosts | Epic 8 (DONE) | ✓ |
| FR9 | Fenêtre desktop état de protection | Epic 10 | ✓ |
| FR10 | Pays, relais, IP dans fenêtre | Epic 10 | ✓ |
| FR11 | Sélecteur pays drapeaux | Epic 10 | ✓ |
| FR12 | Connect/disconnect fenêtre ou tray | Epic 10 | ✓ |
| FR13 | Toggle fenêtre via tray | Epic 10 | ✓ |
| FR13b | Quitter via tray | Epic 12 | ✓ |
| FR14 | Service SCM auto-boot + UI autostart | Epic 3 (DONE) | ⚠️ DÉSALIGNEMENT |
| FR15 | Tray persiste, webview à la demande | Epic 10 | ⚠️ DÉSALIGNEMENT |
| FR16 | Arrêt propre via tray | Epic 12 | ✓ |
| FR17 | Relais DoH + CONNECT | Epic 1 (DONE) | ✓ |
| FR18 | Relais stateless | Epic 1 (DONE) | ✓ |
| FR19 | Relais binaires autonomes | Epic 1 (DONE) | ✓ |
| FR19b | Relais organisés par pays | Epic 9 (DONE) | ✓ |
| FR20 | Installation NSIS | Epic 4 (DONE) | ⚠️ DÉSALIGNEMENT |
| FR21 | Extensions persistent via registre | Epic 4 (DONE) | ❌ DÉSALIGNEMENT MAJEUR |
| FR22 | Config TOML %AppData% | Epic 4 (DONE) | ⚠️ DÉSALIGNEMENT |
| FR23 | Registre distribué /registry | Epic 9 (DONE) | ✓ |
| FR23b | Relais bootstrap hardcodé | **NON TROUVÉ** | ❌ MANQUANT |
| FR24 | Sélection par pays | Epic 9 (DONE) | ✓ |
| FR25 | Round-robin intra-pays | Epic 9 (DONE) | ✓ |
| FR26 | Failover automatique | Epic 9 (DONE) | ✓ |
| FR27 | Proxy HTTP CONNECT local | Tech-spec (DONE) | ✓ |
| FR28 | Relais CONNECT TCP bidirectionnel | Tech-spec (DONE) | ✓ |
| FR29 | Session tokens Ed25519 | Tech-spec (DONE) | ✓ |
| FR30 | Limite CONNECT par IP (200) | Tech-spec (DONE) | ✓ |
| FR30b | Limite DoH simultanées (150) | **NON TROUVÉ** | ❌ MANQUANT |
| FR31 | Détection fuites WebRTC STUN | Epic 5 (DONE) | ✓ |
| FR32 | Politiques navigateur anti-WebRTC | Tech-spec (DONE) | ✓ |
| FR33 | Comparaison IP STUN vs tunnel | Epic 7 (DONE) | ✓ |
| FR34 | Callbacks détection/recovery | Epic 7 (DONE) | ✓ |
| FR35 | Vérification GitHub releases | Epic 6 (DONE) | ✓ |
| FR36 | Auto-update + rollback | Epic 6 (DONE) | ✓ |
| FR37 | Extension route trafic proxy | Epic 11 | ✓ |
| FR38 | Bypass > 50 Mo | Epic 11 | ✓ |
| FR39 | Installation auto extension | Epic 11 | ✓ |
| FR40 | Cohabitation SysProxy + extension | Epic 11 | ✓ |

### Exigences Manquantes dans les Epics

**FR23b** — Relais bootstrap hardcodé au premier lancement
- Impact : Faible — le code l'implémente déjà via config.toml relay domain par défaut
- Recommandation : Ajouter à Epic 9 ou documenter comme couvert implicitement

**FR30b** — Limite DoH simultanées (150 max, HTTP 503)
- Impact : Faible — le code l'implémente déjà via middleware relais
- Recommandation : Ajouter à la couverture des tech-specs existantes

### Désalignements Critiques (Epics ↔ PRD)

**❌ CRITIQUE : Les epics ne reflètent pas la nouvelle architecture (2026-04-08)**

Les sections suivantes des epics sont désalignées avec le PRD révisé :

1. **FR14 dans epics** : dit "Le service peut démarrer automatiquement" — OK mais le texte Epic 3 manque de précision sur l'architecture 2 processus
2. **FR15 dans epics** : dit "Le tray UI peut démarrer automatiquement et se connecter au service" — ne mentionne pas webview à la demande
3. **FR20 dans epics** : dit "installateur Windows avec élévation UAC unique" — OK mais ne précise pas 2 binaires (service + UI)
4. **FR21 dans epics** : dit "Version portable (avec élévation manuelle)" — **LE MODE PORTABLE N'EXISTE PLUS** dans le PRD. Le PRD dit maintenant "Le service et les extensions persistent via registre Windows"
5. **FR22 dans epics** : ne mentionne pas la suppression du path portable

**❌ CRITIQUE : Epic 10 titre et stories réfèrent "Wails v2"**
- Titre : "Interface Desktop Wails v2" → devrait être "Interface Desktop webview/webview"
- Story 10.1 : "Wails v2 est initialisé", "bindings Wails", "frontend/embed.go" → devrait être webview + API REST locale
- Story 10.2 : OK (pas de ref Wails spécifique)
- Story 10.3 : "fenêtre Wails", "toggle Wails" → devrait être "fenêtre webview"

**❌ CRITIQUE : Epic 12 stories réfèrent "Wails"**
- Story 12.1 : "fenêtre Wails", "arrêt propre Wails" → devrait être "fenêtre webview"
- Story 12.2 : "fenêtre Wails" × multiples occurrences

**⚠️ Section "Additional Requirements" dans epics.md** :
- Référence "Wails v2" comme framework UI
- Référence "6 cibles" GoReleaser (devrait être 3)
- Référence "cmd/tray/", "cmd/desktop/", "internal/desktop/", "internal/tray/" (devraient être cmd/ui/, internal/ui/)
- Séquence d'implémentation référence "Init Wails", "Module tray" séparé

### Coverage Statistics

- Total PRD FRs : 40
- FRs couverts dans les epics : 38
- FRs manquants : 2 (FR23b, FR30b) — impact faible, déjà implémentés dans le code
- FRs désalignés : 5 (FR14, FR15, FR20, FR21, FR22) — texte obsolète dans les epics
- Epics avec références Wails v2 obsolètes : 3 (Epic 10, 12, Additional Requirements)
- Couverture : 95% (38/40)
- **Alignement architectural : NON — le fichier epics.md n'a pas été mis à jour après la révision architecture du 2026-04-08**

## UX Alignment Assessment

### UX Document Status

**Trouvé :** ux-design-specification.md (complété 2026-03-16, 14 steps)

### Alignement UX ↔ PRD

**Désalignements majeurs :**

1. **Architecture UI** — L'UX spec dit "Binaire portable mono-processus Wails v3 (fenêtre + systray natif)" (ligne 64). Le PRD dit "Architecture 2 processus : service SCM + UI unique webview/webview + fyne.io/systray". L'UX spec n'a jamais été mise à jour au-delà de l'architecture Wails v3 du 2026-03-30, ni vers Wails v2 (2026-04-02), ni vers webview/webview (2026-04-08)

2. **Mode portable supprimé** — L'UX spec mentionne "binaire portable" (lignes 64, 65, 521, 544), "UAC à chaque lancement" (lignes 64, 72, 327), "pas d'installation" (ligne 544). Le PRD a supprimé le mode portable — l'utilisateur installe via NSIS

3. **Communication Go ↔ JS** — L'UX spec dit "Bindings Wails v3 directs (appels Go depuis JS, pas d'IPC inter-processus)" (ligne 234). Le PRD dit "API REST locale + IPC named pipes vers service"

4. **Platform Support** — L'UX spec dit "Linux / macOS — Day one via cross-compilation Go + Wails v3" (ligne 65). Le PRD dit "Windows uniquement au MVP"

### Alignement UX ↔ Architecture

**Désalignements :**

1. **Moteur de rendu** — UX dit "Wails v3 + WebView2" (ligne 232), Architecture dit "webview/webview + WebView2"
2. **Structure frontend** — UX dit "wailsjs/" bindings, Architecture dit "fetch('/api/*') vers serveur HTTP local"
3. **Fenêtre lifecycle** — UX dit "mono-processus, fenêtre ouverte au lancement", Architecture dit "fenêtre webview créée/détruite à la demande depuis le tray"
4. **Fonts embarqués** — UX dit "bundlées dans le binaire Wails" (ligne 404), Architecture dit "embarqués via go:embed, servis par HTTP local"

### Éléments UX toujours valides

Malgré les désalignements techniques, la majorité du contenu UX reste pertinent :
- ✓ Vision produit et persona Camille
- ✓ Zero-config absolu
- ✓ Charte visuelle plateformeliberte.fr (couleurs, fonts, design tokens)
- ✓ Tray-first UX
- ✓ Layout fenêtre 420×540px (sidebar + main panel)
- ✓ Sélecteur de pays avec drapeaux
- ✓ Indicateurs de statut colorés
- ✓ Messages français non-technique
- ✓ Circuit de confiance (test-protection.html)
- ✓ Interactions sans effort

### Recommandation

L'UX spec nécessite une mise à jour ciblée des sections techniques :
- Section "Platform Strategy" (ligne 63-68) — remplacer Wails v3 par webview/webview + systray, supprimer portable
- Section "Design System Approach" (ligne 220+) — remplacer Wails v3 par webview/webview
- Section "Technical Implementation" (ligne 232+) — remplacer bindings Wails par API REST locale
- Section user flows référençant portable/UAC/Wails
- L'impact UX réel est minimal : le changement est technique (comment le frontend communique avec le backend), pas fonctionnel (ce que l'utilisateur voit reste identique)

## Epic Quality Review

### Epic Structure Validation

#### User Value Focus

| Epic | Titre | User Value | Verdict |
|---|---|---|---|
| Epic 1 (DONE) | Relais VPS Stateless Déployé | Opérateur peut déployer un relais | ✓ |
| Epic 2 (DONE) | Tunnel Chiffré et Protection DNS | Utilisateur protégé DNS + tunnel | ✓ |
| Epic 3 (DONE) | Expérience Desktop — Service, Tray et Lifecycle | Utilisateur interagit via tray | ✓ |
| Epic 4 (DONE) | Distribution et Installation Windows | Utilisateur peut installer Le Voile | ✓ |
| Epic 5 (DONE) | Proxy STUN — Protection WebRTC | Utilisateur protégé WebRTC | ✓ |
| Epic 6 (DONE) | Auto-Update et Rollback | Client se met à jour automatiquement | ✓ |
| Epic 7 (DONE) | Résilience DNS et Auto-Diagnostic | Fallback DNS, auto-test fuites | ✓ |
| Epic 8 (DONE) | Blocklist DNS Communautaire | Filtrage DNS StevenBlack/hosts | ✓ |
| Epic 9 (DONE) | Découverte Dynamique des Relais | Registre distribué, failover | ✓ |
| Epic 10 | Interface Desktop ~~Wails v2~~ webview | Fenêtre desktop complète | ✓ (titre obsolète) |
| Epic 11 | Extension Navigateur & Routage Intelligent | Extension route trafic auto | ✓ |
| Epic 12 | Intégration Bout en Bout & Polish MVP | Produit fonctionne bout en bout | ✓ |

**Tous les epics délivrent de la valeur utilisateur** — pas de milestones techniques purs.

#### Epic Independence

- Epics 1-9 (DONE) : indépendants séquentiellement ✓
- Epic 10 : dépend de 1-9 (done), indépendant de 11-12 ✓
- Epic 11 : dépend de 1-9 (done), indépendant de 10 et 12 ✓
- Epic 12 : dépend de 10 et 11 — **correct**, c'est l'intégration finale

**Aucune dépendance circulaire ou forward dependency.** ✓

### Story Quality Assessment (Epics 10-12 — non implémentés)

#### Story Sizing

| Story | Taille | Verdict |
|---|---|---|
| 10.1 Init webview + statut | Moyenne | ✓ |
| 10.2 Sélecteur pays + IP | Moyenne | ✓ |
| 10.3 Connect/Disconnect + Tray↔Fenêtre | Moyenne | ✓ |
| 11.1 Extension routage proxy | Moyenne | ✓ |
| 11.2 Bypass gros fichiers | Petite | ✓ |
| 11.3 Installation auto + SysProxy | Moyenne | ✓ |
| 12.1 Shutdown propre + indépendance service | Moyenne | ✓ |
| 12.2 Validation bout en bout | Large | ⚠️ Story de validation, acceptable |

#### Acceptance Criteria

**Stories 10.1-10.3 :** Given/When/Then structurés, testables, couvrent happy path et edge cases ✓
**Stories 11.1-11.3 :** Given/When/Then structurés, couvrent Chrome MV3 + Firefox, fallback, cohabitation ✓
**Stories 12.1-12.2 :** Given/When/Then structurés, couvrent shutdown, failover, validation multi-composants ✓

**Aucun AC vague ou non-mesurable.**

### Violations Détectées

#### 🔴 Violations Critiques

**V1 — Epic 10 titre et stories réfèrent "Wails v2" (obsolète)**
- Titre : "Interface Desktop Wails v2" → devrait être "Interface Desktop webview/webview"
- Story 10.1 : "Wails v2 est initialisé", "dossier frontend/", "bindings Wails connectent le backend Go"
- Story 10.3 : "fenêtre Wails s'ouvre ou se toggle", "fenêtre Wails se masque"
- **Remédiation :** Réécrire les stories 10.1 et 10.3 pour refléter webview/webview + API REST locale

**V2 — Epic 12 stories réfèrent "Wails" (obsolète)**
- Story 12.1 : "fenêtre Wails se ferme proprement", "arrêt propre Wails" × 3 occurrences
- Story 12.2 : "fenêtre Wails affiche l'état correct" × 4 occurrences
- **Remédiation :** Remplacer "Wails" par "webview" dans toutes les stories 12.x

**V3 — FR21 dans les epics dit "Version portable" (supprimé)**
- Epics Requirements Inventory FR21 : "L'utilisateur peut utiliser Le Voile via un binaire portable (avec élévation manuelle)"
- Le PRD dit maintenant : "Le service et les extensions persistent via registre Windows"
- **Remédiation :** Mettre à jour FR21 dans la section Requirements Inventory des epics

#### 🟠 Issues Majeures

**V4 — Section "Additional Requirements" obsolète**
- Référence "Wails v2", "cmd/tray/", "cmd/desktop/", "internal/desktop/", "6 cibles GoReleaser"
- Séquence d'implémentation référence "Init Wails", "Module tray" séparé
- **Remédiation :** Réécrire cette section avec la nouvelle architecture

**V5 — FR Coverage Map partiellement obsolète**
- FR21 dit "Version portable" — obsolète
- FR13b dit "arrêt propre Wails" — obsolète
- FR16 dit "fenêtre Wails" — obsolète
- **Remédiation :** Mettre à jour les descriptions dans le FR Coverage Map

#### 🟡 Concerns Mineurs

- Epic 4 description mentionne "version portable" — mais l'epic est DONE, l'impact est documentaire
- Story 10.1 AC mentionne "fenêtre frameless" — webview/webview peut le supporter mais la syntaxe est différente de Wails

### Best Practices Compliance

| Critère | Epic 10 | Epic 11 | Epic 12 |
|---|---|---|---|
| Délivre valeur utilisateur | ✓ | ✓ | ✓ |
| Fonctionne indépendamment | ✓ | ✓ | ✓ (dépend 10+11) |
| Stories correctement dimensionnées | ✓ | ✓ | ✓ |
| Pas de forward dependencies | ✓ | ✓ | ✓ |
| ACs clairs et testables | ✓ | ✓ | ✓ |
| Traçabilité FR maintenue | ✓ | ✓ | ✓ |
| **Contenu à jour avec PRD/Architecture** | **❌** | ✓ | **❌** |

## Summary and Recommendations

### Overall Readiness Status

**NEEDS WORK** — Les artefacts de planification ne sont pas alignés entre eux après la révision architecture du 2026-04-08.

### Diagnostic

Le PRD et l'architecture sont alignés (tous deux mis à jour le 2026-04-08). Cependant :

- **epics.md** — Réfère Wails v2 et le mode portable (supprimé). Les stories des Epics 10 et 12 sont inutilisables en l'état pour un agent de développement
- **ux-design-specification.md** — Réfère Wails v3 et le mode portable (architecture du 2026-03-16, deux révisions de retard)

Les Epics 1-9 (DONE) sont implémentés et fonctionnels. Le désalignement ne les affecte que documentairement.
Les Epics 10-12 (à implémenter) doivent être mis à jour avant de lancer l'implémentation.

### Critical Issues Requiring Immediate Action

1. **🔴 epics.md — Stories 10.1, 10.3, 12.1, 12.2** réfèrent "Wails v2", "bindings Wails", "fenêtre Wails" — un agent suivant ces stories produirait du code Wails v2 au lieu de webview/webview + API REST locale
2. **🔴 epics.md — FR21** dit "Version portable" — cette fonctionnalité n'existe plus
3. **🔴 epics.md — Section "Additional Requirements"** contient l'ancienne architecture (Wails v2, 6 cibles GoReleaser, cmd/tray/, cmd/desktop/)
4. **🟠 ux-design-specification.md** — Sections techniques réfèrent Wails v3 et portable. Impact moindre car l'UX visuelle reste valide

### Recommended Next Steps

1. **Mettre à jour epics.md** — Réécrire les stories 10.1, 10.3, 12.1, 12.2 pour refléter webview/webview + fyne.io/systray + API REST locale. Mettre à jour FR21, FR Coverage Map, et la section Additional Requirements. **Priorité : BLOQUANT pour l'implémentation des Epics 10-12**
2. **Mettre à jour ux-design-specification.md** — Mise à jour ciblée des sections techniques (Platform Strategy, Design System, Technical Implementation). **Priorité : souhaitable mais non bloquant** — l'UX visuelle reste valide
3. **Ajouter FR23b et FR30b aux epics** — FRs manquants dans la couverture (déjà implémentés dans le code, impact documentaire uniquement)

### Final Note

Cette évaluation a identifié **10 issues** réparties en 3 catégories :
- 3 violations critiques (références Wails v2 dans stories à implémenter)
- 5 issues majeures (désalignements architecturaux dans les documents de support)
- 2 concerns mineurs (FRs manquants dans la couverture epics, déjà implémentés)

**Le PRD et l'architecture sont solides et alignés.** Le blocage est exclusivement dans les epics (stories 10, 12) et secondairement dans la spec UX. La résolution est une mise à jour textuelle des stories — pas de re-conception architecturale nécessaire.
