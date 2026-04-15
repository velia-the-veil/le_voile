---
stepsCompleted: ['step-01-document-discovery', 'step-02-prd-analysis', 'step-03-epic-coverage-validation', 'step-04-ux-alignment', 'step-05-epic-quality-review', 'step-06-final-assessment']
filesIncluded: ['prd.md', 'architecture.md', 'epics.md', 'ux-design-specification.md']
---

# Implementation Readiness Assessment Report

**Date:** 2026-04-15
**Project:** bmad_vpn_le_voile_de_velia

## Document Inventory

| Type | File | Taille | Modifié |
|---|---|---|---|
| PRD | `prd.md` | 55 946 o | 2026-04-15 |
| Architecture | `architecture.md` | 101 960 o | 2026-04-15 |
| Epics | `epics.md` | 29 353 o | 2026-04-12 |
| UX | `ux-design-specification.md` | 54 394 o | 2026-04-12 |

- Aucun doublon whole/sharded
- Aucun document requis manquant
- Annexes UX : `ux-design-directions.html`, `prototype-le-voile.html`

## PRD Analysis

### Functional Requirements (40 FR)

Tunnel/Réseau : FR1-FR4
Capture L3 & Kill Switch : FR5, FR5b, FR5c, FR6, FR7, FR7b, FR8, FR8b, FR8c, FR8d
UI : FR9-FR13, FR13b, FR13c
Lifecycle : FR14, FR15, FR15b, FR16, FR16b
Relais Multi-VPS : FR17-FR19, FR19b
Distribution : FR20, FR20b, FR21, FR22
Découverte Relais : FR23, FR23b, FR24-FR26
Tunnel IP : FR27-FR30, FR30b
Anti-Fuite : FR31-FR34
Update : FR35, FR36
**Supprimés 2026-04-15 :** FR37-FR40 (extension navigateur) + bypass > 50 Mo

### Non-Functional Requirements (38 NFR)

Security : NFR1-9, NFR9b-j
Performance : NFR10-14
Reliability : NFR15-19
Privacy : NFR20-21
Platform : NFR22-24
Logging : NFR22a-c
Supply Chain : NFR22d-f
Crypto Keys : NFR22g-i
Startup Safety : NFR25-26

### PRD Completeness Assessment

- PRD globalement complet, bien structuré, traçable
- Numérotation claire avec sous-exigences explicites
- 8 parcours utilisateurs + mapping Journey→Capabilities
- Success criteria mesurables, risques/mitigations complets (17 entrées)
- Conformité RGPD/RGAA/Disclosure traitée
- Warrant canary narratif mais sans FR/NFR dédié (à vérifier en epics)

## Epic Coverage Validation

### 🚨 ALERTE CRITIQUE : Document Epics Obsolète

Le fichier `epics.md` (daté 2026-04-12) est **massivement désynchronisé** avec le PRD révisé (2026-04-15). La révision majeure du PRD introduit un **changement architectural fondamental** (bascule du modèle DNS/proxy CONNECT vers capture L3 TUN/Wintun + kill switch firewall OS-level + support Linux + suppression extension navigateur) qui n'est **pas du tout reflété** dans les epics.

**Preuves de désynchronisation :**
- Epics `Requirements Inventory` reprend les anciens FR5-8 (DNS redirection/DoH/resolver) au lieu des nouveaux FR5-8 (TUN/routing/kill switch firewall)
- Epics contient FR37-40 (extension navigateur) **supprimés** du PRD
- Epics cite `FR17 = DoH + CONNECT` alors que le PRD dit `FR17 = /tunnel paquets IP bruts`
- Aucune mention du support Linux (paquets .deb/.rpm/AUR/.apk, systemd, nftables, capabilities)
- Toutes les nouvelles sous-exigences FR5b/5c/7b/8b/8c/8d/13c/15b/16b/19b/20b/23b absentes ou partiellement traitées
- Tous les nouveaux NFR9c-j, NFR22a-i, NFR23-26 absents de la couverture
- Epic 11 (extension navigateur) entier devenu obsolète
- Epic 8 (Blocklist DNS communautaire côté client) devenu obsolète : blocklist déplacée côté relais (FR8b)

### Matrice de Couverture FR

Légende : ✅ = couvert aligné / 🟡 = couvert mais désaligné avec PRD révisé / ❌ = manquant / 🚫 = epic obsolète

| FR | Texte PRD (résumé) | Epic(s) | Statut |
|---|---|---|---|
| FR1 | Tunnel QUIC/HTTP3 vers relais via Cloudflare | Epic 1 (DONE) | ✅ |
| FR2 | Reconnexion auto (kill switch firewall maintenu) | Epic 2 (DONE) | 🟡 implémenté mais description epic mentionne "kill switch DNS" |
| FR3 | Auth Ed25519 (certificate pinning) | Epic 1 (DONE) | ✅ |
| FR4 | Relais acceptent connexions QUIC/HTTP3 | Epic 1 (DONE) | ✅ |
| FR5 | Interface TUN/Wintun `levoile0` MTU 1420 | **aucun** | ❌ epic décrit DNS redirection (ancien modèle) |
| FR5b | Watchdog TUN 3s + reconnexion | **aucun** | ❌ |
| FR5c | Détection VPN concurrents | **aucun** | ❌ |
| FR6 | Routage système via levoile0 + route spécifique relais | **aucun** | ❌ epic décrit modification resolver DNS |
| FR7 | Destruction TUN + restauration routes | **aucun** | ❌ epic décrit restauration resolver DNS |
| FR7b | Flush cache DNS système au disconnect | **aucun** | ❌ |
| FR8 | Kill switch firewall nftables/WFP | **aucun** | ❌ epic décrit kill switch DNS (ancien modèle) |
| FR8b | Blocklist DNS côté relais (StevenBlack) | Epic 8 (DONE côté client) | 🟡 implémenté côté client, à déplacer côté relais |
| FR8c | Captive portal detection + mode lockdown relaxé | **aucun** | ❌ |
| FR8d | IPv6 hors tunnel (option) + avertissement | **aucun** | ❌ |
| FR9 | Fenêtre état protection | Epic 10 Story 10.1 | ✅ |
| FR10 | Pays/relais/IP visible | Epic 10 Story 10.2 | ✅ |
| FR11 | Sélecteur pays drapeaux | Epic 10 Story 10.2 | ✅ |
| FR12 | Connect/disconnect UI/tray | Epic 10 Story 10.3 | ✅ |
| FR13 | Toggle fenêtre via tray | Epic 10 Story 10.3 | ✅ |
| FR13b | Quitter via menu tray | Epic 12 Story 12.1 | ✅ |
| FR13c | Écran "Service non démarré" + cmd shell | **aucun** | ❌ |
| FR14 | Service démarre au boot SCM/systemd | Epic 3 (DONE Windows) | 🟡 Linux systemd absent |
| FR15 | Tray persiste après fermeture webview | Epic 10 Story 10.1 | ✅ |
| FR15b | UI supervisée par restart manager | **aucun** | ❌ |
| FR16 | Quitter UI, service continue | Epic 12 Story 12.1 | ✅ |
| FR16b | Mode dégradé (kill switch off temporaire) | **aucun** | ❌ |
| FR17 | Relais /tunnel paquets IP bruts + NAT + DNS | Epic 1 (DONE DoH+CONNECT) | 🟡 modèle différent à refondre |
| FR18 | Relais stateless | Epic 1 (DONE) | ✅ |
| FR19 | Relais déployés VPS Linux systemd | Epic 1 (DONE) | ✅ |
| FR19b | Relais par pays ≥ 1/pays, prioritaires ≥ 2 | Epic 9 (DONE) | ✅ |
| FR20 | Installation NSIS Windows + paquets Linux (.deb/.rpm/AUR/.apk) | Epic 4 (DONE Windows) | 🟡 Linux absent |
| FR20b | Paquets signés Ed25519, rejet sans signature | **aucun** | ❌ |
| FR21 | Persistance service SCM/systemd, firewall/TUN | Epic 4 (DONE Windows) | 🟡 Linux absent + mention WebRTC policies obsolète |
| FR22 | Config TOML + cache registre | Epic 4 (DONE) | 🟡 chemins Linux `~/.config/levoile/` absents |
| FR23 | Registre via /.well-known/relay-registry.json signé | Epic 9 (DONE via /registry) | 🟡 endpoint à vérifier |
| FR23b | Relais bootstrap hardcodé au 1er lancement | Epic 9 (DONE) | ✅ |
| FR24 | Sélection relais par pays | Epic 9 (DONE) | ✅ |
| FR25 | Round-robin intra-pays | Epic 9 (DONE) | ✅ |
| FR26 | Failover auto intra-pays | Epic 9 (DONE) | ✅ |
| FR27 | Client encapsule paquets IP bruts dans /tunnel HTTP/3 | Tech Spec IP Camouflage (DONE pour CONNECT) | 🟡 modèle CONNECT à remplacer par /tunnel paquets IP |
| FR28 | Relais désencapsule, NAT, DNS, forward | Tech Spec IP Camouflage (DONE pour CONNECT) | 🟡 modèle à refondre |
| FR29 | Session tokens Ed25519 TTL 4h IP hash | Tech Spec IP Camouflage (DONE) | ✅ |
| FR30 | Relais 200 tunnels/IP + 10 GiB/jour | Tech Spec Rate Limiting (DONE CONNECT) | 🟡 à adapter pour tunnels |
| FR30b | Max 150 tunnels simultanés, 503 | Tech Spec Rate Limiting (DONE DoH) | 🟡 à adapter |
| FR31 | STUN Binding via TUN | Epic 5 (DONE) | 🟡 validé via TUN à confirmer |
| FR32 | Capture L3 garantit aucun paquet hors tunnel | Tech Spec WebRTC Policies (DONE politiques navigateur) | 🚫 modèle politiques navigateur obsolète, remplacé par couverture structurelle L3 |
| FR33 | Comparaison IP STUN vs tunnel | Epic 7 (DONE) | ✅ |
| FR34 | Callbacks détection/recovery + kill switch | Epic 7 (DONE) | 🟡 à adapter pour firewall kill switch |
| FR35 | Vérification GitHub releases | Epic 6 (DONE) | ✅ |
| FR36 | Download + signature + rollback + fallback atomique | Epic 6 (DONE) | 🟡 fallback échec remplacement atomique à vérifier |
| FR37-40 | **SUPPRIMÉS** du PRD | Epic 11 (planifié) | 🚫 Epic 11 à supprimer complètement |

### Matrice de Couverture NFR (résumé)

| NFR | Statut |
|---|---|
| NFR1 TLS 1.3 | ✅ epic inventory |
| NFR2 Ed25519 via crypto/ed25519 | ✅ |
| NFR3 Stateless RAM TTL | ✅ |
| NFR4 Indiscernabilité DPI | ✅ |
| NFR5 Aucune fuite DNS | 🟡 couverture structurelle L3 à réécrire |
| NFR6 Restauration TUN/routes/firewall | ❌ epic décrit restauration resolver DNS uniquement |
| NFR7-8-9 | ✅ |
| NFR9b Kill switch kernel survit aux crashes | ❌ |
| NFR9c ConstantTimeCompare | ❌ |
| NFR9d IP hash vs CF-Connecting-IP | ✅ tech spec |
| NFR9e TLS Full (Strict) | ❌ |
| NFR9f Validation DNSSEC upstream | ❌ |
| NFR9g Détection injection paquets TUN | ❌ |
| NFR9h Strip symbols `-ldflags -s -w` | ❌ |
| NFR9i DNS bootstrap via DoH | ❌ |
| NFR9j Config TOML HMAC + permissions 0600 | ❌ |
| NFR10-14 Performance | 🟡 seuils à ajuster (RAM 20→25 MB, CPU 1→2%) |
| NFR15 Kill switch firewall < 100ms | ❌ epic décrit kill switch DNS < 100ms |
| NFR16 Watchdog TUN < 5s | ❌ epic décrit watchdog DNS |
| NFR17 Crash-recovery < 5s nettoyage firewall/TUN | ❌ |
| NFR18 Uptime /health | ✅ |
| NFR19 Failover < 5s | ✅ |
| NFR20-21 Privacy | ✅ |
| NFR22 Matrice tests e2e Win11/Ubuntu24/Fedora40/Arch/Alpine | ❌ Linux/multi-distro absent |
| NFR23 Deps runtime Linux | ❌ |
| NFR24 Capabilities systemd | ❌ |
| NFR22a-c Logging | ❌ |
| NFR22d-f Security testing (gosec, govulncheck, fuzzing) | ❌ |
| NFR22g-i Master key management (air-gap, rotation, GitHub 2FA) | ❌ |
| NFR25 Kill switch ordre séquence | ❌ |
| NFR26 Intégrité binaire SHA256 + Ed25519 | ❌ |

### Coverage Statistics

- **Total PRD FRs :** 36 (FR37-40 supprimés)
- **FRs alignés avec PRD révisé (✅) :** 17
- **FRs couverts mais désalignés (🟡) :** 12 — nécessitent refactoring/mise à jour epic
- **FRs manquants complètement (❌) :** 13 — nouvelles exigences non traitées
- **Taux de couverture aligné :** ~47%
- **Taux de couverture brut (aligné + désaligné) :** ~80%

- **Total PRD NFRs :** 38
- **NFRs alignés :** ~11
- **NFRs désalignés :** ~5
- **NFRs manquants :** ~22
- **Taux de couverture aligné NFR :** ~29%

### Missing Requirements (Critical)

**Nouvelles exigences non couvertes — requièrent de NOUVEAUX epics/stories :**

1. **Bascule modèle DNS → L3 (FR5-8, FR27-28)** — tous les epics tunnel/DNS/proxy à refondre autour de TUN/Wintun + NAT relais sur /tunnel
2. **Support Linux complet (FR14, FR20-22, NFR22-24)** — paquets .deb/.rpm/AUR/.apk, systemd, setcap, XDG autostart, WebKitGTK
3. **Kill switch firewall OS-level (FR8, NFR15, NFR9b)** — nftables Linux + WFP Windows
4. **Captive portal (FR8c)**
5. **IPv6 option hors tunnel (FR8d)**
6. **Mode dégradé (FR16b)**
7. **Écran service non démarré (FR13c)**
8. **Watchdog TUN + UI (FR5b, FR15b)**
9. **Détection VPN concurrent (FR5c)**
10. **Flush cache DNS disconnect (FR7b)**
11. **Signature paquets Ed25519 (FR20b)**
12. **Renforcement sécurité (NFR9c, 9e-j, 22a-i, 25, 26)** — tooling CI, master key management, intégrité binaire
13. **Blocklist DNS côté relais (FR8b)** — déplacement depuis client
14. **Suppression Epic 11 + Epic 8** — obsolètes

### Recommandation

**Le document epics.md DOIT être régénéré/revu en profondeur avant toute implémentation post-2026-04-15.** L'alignement actuel est compromis par la refonte architecturale TUN/L3 + support Linux. Proposer :

- Re-exécuter `bmad-bmm-create-epics-and-stories` avec le PRD et l'architecture à jour
- Ou amender manuellement : mettre à jour Requirements Inventory + FR Coverage Map + ajouter Epics 13-20 pour les nouvelles capacités (TUN/L3, kill switch firewall, Linux packaging, captive portal, IPv6, mode dégradé, renforcement sécurité)
- Marquer Epic 11 comme SUPPRIMÉ et Epic 8 comme RELOCATED_TO_RELAY

## UX Alignment Assessment

### UX Document Status

**Trouvé** : `ux-design-specification.md` (daté 2026-03-16, 1037 lignes) + artefacts HTML (`ux-design-directions.html`, `prototype-le-voile.html`).

### 🚨 Document UX aussi obsolète (même cause que epics.md)

Le document UX date du **2026-03-16**, soit **avant** la bascule architecturale de 2026-03-30 (Wails→webview/webview+systray) ET avant la refonte majeure de 2026-04-15 (DNS→TUN/L3, suppression extension, ajout Linux natif). Il cumule donc deux couches d'obsolescence.

### UX ↔ PRD : Désalignements

| Élément UX | PRD révisé (2026-04-15) | Statut |
|---|---|---|
| Moteur UI : **Wails v3** mono-processus, bindings Go↔JS directs | webview/webview + fyne.io/systray dans binaire UI unique + serveur HTTP local 127.0.0.1, API REST JSON (pas de bindings directs) | ❌ obsolète |
| "Élévation UAC à chaque lancement" | Service SCM LocalSystem + UI user-space (pas d'UAC récurrent) | ❌ obsolète |
| Linux/macOS "day one via Wails v3" | Linux MVP via paquets natifs .deb/.rpm/AUR/.apk (WebKitGTK) ; macOS différé Phase 3 | ❌ obsolète |
| Extension navigateur WebExtension (MV3 + Firefox) | **Supprimée** 2026-04-15 | 🚫 obsolète |
| "Kill switch **DNS** instantané", "Watchdog **DNS**" | Kill switch **firewall** OS-level (nftables/WFP), Watchdog **TUN** | ❌ modèle obsolète |
| Sélecteur 4 pays (Islande, Allemagne, Finlande, US) | 6 pays prioritaires (FR, IS, FI, DE, ES, GB) | ❌ désaligné |
| "Restauration DNS" dans diagramme de shutdown | Destruction TUN + restauration routes + retrait règles firewall | ❌ |
| Pas de mode dégradé, pas d'IPv6 opt-in, pas de captive portal, pas d'écran "Service non démarré" | Nouveaux parcours #7, #8 + FR8c, FR8d, FR13c, FR16b | ❌ parcours/écrans manquants |
| Architecture mono-processus "tunnel + tray + proxy + extension" | Architecture 2 processus (service + UI unique combinée tray+webview) | ❌ obsolète |

### UX ↔ Architecture : Désalignements

L'architecture (2026-04-15) décrit webview/webview + fyne.io/systray + IPC named pipes/Unix sockets + serveur HTTP local. Le UX document cite Wails v3 + bindings directs. **Aucun alignement sur le modèle de communication UI↔service.**

### Alignment Issues (résumé)

1. Refonte complète du Platform Strategy nécessaire (Wails→webview, Linux natif, suppression extension)
2. Refonte des diagrammes de flux (shutdown, coupure réseau, failover) pour remplacer DNS par L3+firewall
3. Ajout des nouveaux écrans UX : mode dégradé avec bandeau rouge, modal "Autoriser IPv6 hors tunnel", écran fixe "Service non démarré" avec cmd shell, détection VPN concurrent
4. Révision sélecteur pays (FR/IS/FI/DE/ES/GB au lieu de IS/DE/FI/US)
5. Ajout des 2 personas manquants (Mathieu Linux, Théo IPv6) dans les parcours UX
6. Architecture frontend (embed assets, serveur HTTP local, pas de bundler) alignée mais moteur de rendu à corriger

### Warnings

- ⚠️ Le UX document référence extension navigateur, politiques registre Windows Chromium/Firefox, policies.json — **obsolète** après suppression FR37-40
- ⚠️ Identité visuelle et charte graphique (couleurs, fonts, design tokens CSS extraits de plateformeliberte.fr) : **réutilisables** tels quels, indépendants du stack technique
- ⚠️ Prototype HTML `prototype-le-voile.html` (2026-03-16) également à revalider

## Epic Quality Review

### Portée de la revue

Seuls les **3 nouveaux epics** (Epic 10, 11, 12) ont des stories rédigées dans `epics.md`. Les Epics 1-9 sont marqués DONE sans stories détaillées dans ce document (implémentation faite, pas de plus-value à réauditer). Les tech specs (IP Camouflage, Rate Limiting, WebRTC Policies) sont marqués DONE également.

### Epic 10 : Interface Desktop webview/webview + systray

| Critère | Verdict |
|---|---|
| Titre user-centric | 🟡 "Interface Desktop..." — orienté solution, pas user outcome |
| Goal décrit un résultat utilisateur | ✅ "accède à une fenêtre desktop complète..." |
| Value prop autonome | ✅ oui, utilisable sans Epic 11/12 |
| Indépendance (pas de forward deps) | ✅ dépend des epics DONE (1-9) |
| Stories indépendantes | ✅ 10.1 peut être livrée seule avec fake IPC |
| AC format Given/When/Then | ✅ format BDD respecté |
| AC testables | ✅ seuils mesurables (< 5s, 420×540px, polling 2s) |
| AC complets (happy + error) | 🟡 erreur IPC traitée par FR13c mais AC 10.1 ne couvre pas le cas "service absent" |
| Traçabilité aux FRs | ✅ FR9/10/11/12/13/15 mappés |

**Observations :**
- Story 10.1 mélange 3 préoccupations (fenêtre desktop + serveur HTTP local + design tokens) → risque story > 3 jours, mais cohérent pour bootstrap
- Absence de story explicite pour FR13c (écran fallback "Service non démarré")
- Absence de story pour FR15b (supervision/restart manager UI)

### Epic 11 : Extension Navigateur & Routage Intelligent

🚫 **Epic entièrement OBSOLÈTE** — FR37-FR40 supprimés du PRD 2026-04-15. **Action : SUPPRIMER de la planification.** Aucune revue qualité ne sert.

### Epic 12 : Intégration Bout en Bout & Polish MVP Révisé

| Critère | Verdict |
|---|---|
| Titre user-centric | 🟡 "Intégration Bout en Bout" est un jalon technique, pas user outcome |
| Goal décrit un résultat utilisateur | ✅ shutdown propre + indépendance service/UI |
| Value prop autonome | ❌ nécessite Epic 10 (fenêtre webview) + nouveaux epics Linux/L3 |
| Indépendance | ❌ forward dependencies : l'AC 12.2 teste la capture L3, failover, extension — exigences qui ne sont PAS dans les epics courants |
| AC format Given/When/Then | ✅ |
| AC testables | 🟡 "zéro fuite DNS/IP/WebRTC" dépend de méthodes de test externes (dnsleaktest.com, browserleaks.com) — acceptable mais partiellement manuel |
| AC référencent éléments supprimés | ❌ Story 12.1 AC cite "SysProxy désactivé", "configs navigateur restaurées" — obsolètes |
| Traçabilité aux FRs | 🟡 FR13b, FR16 mappés mais Story 12.2 est un fourre-tout non traçable |

**Observations :**
- Story 12.2 "Validation Bout en Bout" est un **jalon technique** déguisé en user story — violation de best practice
- Story 12.1 mentionne "DNS restauré", "proxy WinINET nettoyé", "SysProxy désactivé" : modèle de shutdown obsolète, à refondre pour modèle TUN+firewall+service
- L'ensemble de l'epic sera à refactorer dans le cadre de la refonte L3

### 🔴 Critical Violations (par rapport aux best practices)

1. **Document epics stale** — Requirements Inventory dans `epics.md` reprend l'ancien PRD (modèle DNS, FR37-40). Incompatible avec toute implémentation future.
2. **Epic 11 obsolète** — Planifié mais les FRs sous-jacents sont supprimés.
3. **Epic 12 Story 12.2** — jalon technique "Validation Bout en Bout" sans valeur utilisateur isolable, doit être décomposée en stories traçables.
4. **Forward dependencies potentielles** — Story 12.2 AC (zéro fuite DNS/IP/WebRTC) exige fonctionnalités non encore planifiées dans de nouveaux epics (TUN L3, kill switch firewall, captive portal, IPv6 opt-in).

### 🟠 Major Issues

5. **Absence d'epics pour ~13 blocs d'exigences nouvelles** (cf. Step 3) — aucune couverture pour : bascule TUN/L3, support Linux, kill switch firewall OS-level, captive portal, IPv6 opt-in, mode dégradé, écran service absent, watchdog TUN, supervision UI, détection VPN concurrent, flush DNS disconnect, signature paquets Ed25519, renforcement sécurité (NFR9c-j, 22a-i, 25-26).
6. **Story 10.1 trop chargée** — fenêtre + serveur HTTP + design tokens ensemble ; acceptable pour bootstrap mais à surveiller en termes de taille.
7. **Traçabilité NFR absente** — les epics listent les NFR dans Requirements Inventory mais ne les mappent pas à des stories/AC. Aucun Non-Functional Requirement Coverage Map.

### 🟡 Minor Concerns

8. Numérotation FR37-40 conservée dans Requirements Inventory malgré suppression PRD.
9. Section "Architecture — Séquence d'implémentation recommandée" (lignes 148-163) obsolète : mentionne WinINET, SysProxy, politiques navigateur, pas de TUN/L3.
10. Section "UX Design — Exigences additionnelles" cite "4 pays : Islande, Allemagne, Finlande, US" au lieu des 6 pays prioritaires du PRD.

### Recommandations de remédiation

1. **Régénérer epics.md** via `bmad-bmm-create-epics-and-stories` avec PRD/architecture à jour
2. Supprimer Epic 11, marquer Epic 8 comme RELOCATED_TO_RELAY
3. Créer Epics 13-N pour les 13 blocs d'exigences manquants, notamment :
   - Epic "Capture L3 + Kill Switch Firewall (refonte tunnel)"
   - Epic "Distribution & Service Linux natif (paquets + systemd + capabilities)"
   - Epic "UX résilience : captive portal, mode dégradé, IPv6 opt-in, écran service absent"
   - Epic "Renforcement sécurité & supply chain (CI gosec/govulncheck/fuzzing, intégrité binaire, master key management)"
4. Décomposer Story 12.2 en stories user-centric traçables
5. Ajouter une NFR Coverage Map alignée (tous les NFR → stories/AC explicites)

## Summary and Recommendations

### Overall Readiness Status

🔴 **NOT READY** — implémentation bloquée tant que les epics et la spec UX ne sont pas ré-alignés sur le PRD 2026-04-15.

### Cause racine

Le PRD a été refondu en profondeur le **2026-04-15** (bascule DNS→TUN/L3, kill switch firewall OS-level, support Linux multi-distro, suppression extension navigateur, renforcement sécurité NFR9c-j/22a-i/25-26). Les documents epics.md (2026-04-12) et ux-design-specification.md (2026-03-16) sont antérieurs à cette refonte et reflètent un modèle architectural obsolète.

### Critical Issues Requiring Immediate Action

1. **`epics.md` obsolète** — Requirements Inventory reprend l'ancien modèle DNS/CONNECT. ~53% des FRs (par statut aligné) non couverts ou désalignés. ~71% des NFRs sans couverture dans les epics.
2. **Epic 11 (Extension Navigateur) à supprimer** — FR37-FR40 retirés du PRD.
3. **Epic 8 (Blocklist DNS client) à relocaliser côté relais** — FR8b.
4. **13 blocs d'exigences sans epic** — TUN/L3, kill switch firewall, Linux natif, captive portal, IPv6 opt-in, mode dégradé, écran service absent, watchdog TUN, supervision UI, VPN concurrent, flush DNS disconnect, signature paquets Ed25519, renforcement sécurité/supply chain.
5. **UX spec obsolète** — Wails v3, extension navigateur, kill switch DNS, 4 pays au lieu de 6. Parcours Mathieu (Linux) et Théo (IPv6) absents.
6. **Story 12.2 = jalon technique** — violation best practice à décomposer.

### Recommended Next Steps

1. **Régénérer `epics.md`** via `bmad-bmm-create-epics-and-stories` en injectant `prd.md` + `architecture.md` à jour (2026-04-15) ; supprimer Epic 11, marquer Epic 8 RELOCATED_TO_RELAY
2. **Créer 4 nouveaux epics** minimum pour combler les trous :
   - Capture L3 + Kill Switch Firewall (refonte tunnel)
   - Distribution & Service Linux (paquets .deb/.rpm/AUR/.apk + systemd + setcap)
   - UX Résilience (captive portal, mode dégradé, IPv6 opt-in, écran service absent, VPN concurrent, watchdog TUN/UI)
   - Sécurité & Supply Chain (CI gosec/govulncheck/fuzzing, intégrité binaire, master key mgmt, GitHub 2FA)
3. **Mettre à jour `ux-design-specification.md`** : Platform Strategy (webview+systray, Linux natif), diagrammes de flux (L3+firewall), écrans nouveaux (mode dégradé, IPv6 modal, service absent), sélecteur 6 pays, personas Mathieu/Théo
4. **Ajouter NFR Coverage Map** explicite : chaque NFR → stories/AC
5. **Décomposer Story 12.2** en stories user-centric traçables
6. **Archiver** les rapports anciens (`implementation-readiness-report-2026-03-12.md`, `-04-01.md`, `-04-08.md`) dans un sous-dossier `archive/`
7. Après régénération, ré-exécuter ce workflow (`check-implementation-readiness`) pour confirmer le READY

### Final Note

Cette évaluation a identifié **plus de 40 issues** réparties en **5 catégories** (couverture FR, couverture NFR, alignement UX, qualité epic, refonte architecturale non propagée). **Statut : NOT READY.** L'implémentation dans son état actuel conduirait à coder l'ancien modèle (DNS/CONNECT/extension), au risque d'invalider tout le travail déjà livré après la refonte.

La bonne nouvelle : le PRD (2026-04-15) et l'architecture (2026-04-15) sont cohérents entre eux, complets et à jour. La régénération des epics + mise à jour UX suffisent à débloquer le READY.

---

**Assessment Date:** 2026-04-15
**Assessor:** Claude (agent PM/SM) — Implementation Readiness Workflow



