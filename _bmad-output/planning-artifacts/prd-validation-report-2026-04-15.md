---
validationTarget: '_bmad-output/planning-artifacts/prd.md'
validationDate: '2026-04-15'
inputDocuments:
  - '_bmad-output/brainstorming/brainstorming-session-2026-03-08-1530.md'
  - '_bmad-output/planning-artifacts/architecture.md'
validationStepsCompleted:
  - 'step-v-01-discovery'
  - 'step-v-02-format-detection'
  - 'step-v-03-density-validation'
  - 'step-v-04-brief-coverage-validation'
  - 'step-v-05-measurability-validation'
  - 'step-v-06-traceability-validation'
  - 'step-v-07-implementation-leakage-validation'
  - 'step-v-08-domain-compliance-validation'
  - 'step-v-09-project-type-validation'
  - 'step-v-10-smart-validation'
  - 'step-v-11-holistic-quality-validation'
  - 'step-v-12-completeness-validation'
  - 'step-v-13-report-complete'
validationStatus: COMPLETE
holisticQualityRating: '4.8/5 (Good → Excellent après polish)'
overallStatus: PASS
postValidationPolishApplied: true
---

## Post-Validation Polish (2026-04-15)

Les 3 améliorations recommandées (Top 3) ont été appliquées après validation :

**✅ Improvement 1 — Corrections mesurabilité (8 items)** :
- FR13c : "écran d'aide clair" → "écran fixe avec titre 'Service Le Voile non démarré' et bloc-texte de la commande shell selon OS détecté"
- FR15 : retrait `fyne.io/systray` (leakage) → "L'icône système (system tray) persiste..."
- FR19b : "un ou plusieurs relais" → "au moins 1 relais. Pays prioritaires (FR/IS/FI/DE/ES/GB) ciblés à 2+ relais"
- NFR2 : "bibliothèques standard" → "Go standards (`crypto/ed25519`, TLS 1.3 via quic-go)"
- NFR3 : "TTL court" → "≤ 300s TCP, ≤ 120s UDP"
- NFR11 : "connexion standard" → "ADSL/fibre résidentielle RTT < 50ms"
- NFR15 : ajout méthode mesure "coupure tunnel provoquée + assertion état nftables/WFP"
- NFR22 : "fonctionnement identique" → "matrice tests e2e 100% passing sur Win 11, Ubuntu 24.04, Fedora 40, Arch rolling, Alpine 3.19"

**✅ Improvement 2 — TOC + numérotation H2** :
- Table of Contents ajoutée en début de doc
- Sections H2 numérotées 1-10 (Executive Summary → Non-Functional Requirements)
- Ancres automatiques compatibles markdown

**✅ Improvement 3 — 2 User Journeys edge** :
- Parcours 7 — Théo, utilisateur technique activant IPv6 opt-in (couvre FR8d)
- Parcours 8 — Camille, kill switch bloquant en mobilité → mode dégradé (couvre FR16b + FR34)
- Journey→Capabilities Mapping étendu avec 3 nouvelles lignes

**Rating post-polish estimé:** 4.8/5 (Excellent)
- Toutes les violations mineures de mesurabilité corrigées
- Leakage FR15 retirée (Implementation Leakage = 0)
- Couverture traceability complète (IPv6 opt-in + mode dégradé ont maintenant des Journeys explicites)

# PRD Validation Report

**PRD Being Validated:** `_bmad-output/planning-artifacts/prd.md`
**Validation Date:** 2026-04-15
**PRD Last Edited:** 2026-04-15 (révision Linux + TUN/Wintun)

## Input Documents

- PRD: `prd.md` (419 lignes, révisé 2026-04-15) ✓
- Brainstorming: `brainstorming-session-2026-03-08-1530.md` ✓
- Architecture: `architecture.md` (révisé 2026-04-15) ✓

## Validation Findings

### Advanced Elicitation Session (2026-04-15)

**Méthodes appliquées:** Red Team vs Blue Team → Self-Consistency → What If Scenarios → Architecture Decision Records → Security Audit Personas

**Améliorations intégrées au PRD/architecture:**
- **Red Team** : FR5b watchdog TUN, FR7b flush DNS cache, FR16b mode dégradé, FR20b signature paquets, NFR25 + NFR26 intégrité
- **Self-Consistency** : FR34 reconnexion auto, composants manquants ajoutés à l'architecture (flush DNS, integrity, levoile-ctl, ordre 2-phases) — puis ordre 2-phases rollback sur décision ADR-05
- **What If** : FR5c VPN concurrent, FR8c captive portal, FR8d IPv6 hors tunnel (opt-in), FR13c écran service absent, FR15b UI supervisor, nftables kernel validation, doc multi-user Linux, mitigation firewalls tiers
- **ADRs** : 7 ADRs ajoutés à l'architecture (ADR-01 Gateway NAT, ADR-02 suppression extension, ADR-03 nftables exclusif, ADR-04 systemd AmbientCapabilities, ADR-05 ordre simple, ADR-06 DNS relais, ADR-07 wireguard/tun)
- **Security Audit** : NFR9c-j (constant-time crypto, DNSSEC, TLS Full Strict, TUN integrity, DoH bootstrap, HMAC config), NFR22a-i (logs, CI security, key management), section Conformité RGPD/Disclosure/Accessibilité RGAA, 8 nouveaux risques documentés, warrant canary advancé au MVP

## Format Detection

**PRD Structure (Level 2 headers):**
- Executive Summary
- Project Classification
- Success Criteria
- User Journeys
- Domain-Specific Requirements
- Innovation & Novel Patterns
- Desktop App Requirements
- Project Scoping & Phased Development
- Functional Requirements
- Non-Functional Requirements

**BMAD Core Sections Present:**
- Executive Summary: ✓
- Success Criteria: ✓
- Product Scope: ✓ (via "Project Scoping & Phased Development")
- User Journeys: ✓
- Functional Requirements: ✓
- Non-Functional Requirements: ✓

**Format Classification:** BMAD Standard
**Core Sections Present:** 6/6

## Information Density Validation

**Anti-Pattern Violations:**

- Conversational filler: 0 occurrences
- Wordy phrases: 0 occurrences
- Redundant phrases: 0 occurrences

**Total Violations:** 0

**Severity Assessment:** Pass

**Recommendation:** PRD démontre une excellente densité d'information — aucune formule creuse, style direct et concis, cohérent avec les standards BMAD.

## Product Brief Coverage

**Status:** N/A - Aucun Product Brief fourni en input. Le PRD a été construit à partir d'une session de brainstorming (brainstorming-session-2026-03-08-1530.md) et itéré directement. Traçabilité des 12 idées brainstorming couverte dans `architecture.md` section "Traçabilité brainstorming".

## Measurability Validation

### Functional Requirements

**Total FRs analysés:** 42 (FR1-FR36 + variants b/c/d)

| Catégorie | Count | Items |
|---|---|---|
| Format "Actor can" violations | 0 | — |
| Quantificateur flou | 1 | FR19b ("un ou plusieurs relais" — pas de min/max) |
| Adjectif subjectif sans métrique | 1 | FR13c ("écran d'aide clair" — non accompagné d'exemple textuel) |
| Fuite d'implémentation | 1 | FR15 (`fyne.io/systray` nommé alors que capacité = persistance tray) |

**FR violations total:** 3

### Non-Functional Requirements

**Total NFRs analysés:** ~45 (NFR1-NFR27 + variants a-j)

| Catégorie | Count | Items |
|---|---|---|
| Métrique chiffrée manquante | 1 | NFR3 ("TTL court" — pas de valeur chiffrée) |
| Condition/contexte vague | 1 | NFR11 ("connexion standard" — RTT/BW non précisés) |
| Méthode de mesure manquante | 1 | NFR15 ("< 100ms" sans méthode explicite) |
| Adjectif subjectif sans critère | 2 | NFR2 ("bibliothèques standard"), NFR22 ("fonctionnement identique") |

**NFR violations total:** 5

### Overall Assessment

**Total Requirements:** ~87
**Total Violations:** 8

**Severity:** Warning (5-10 violations)

**Recommendation:**
Les violations sont mineures et ciblées. Corrections recommandées avant gel :
- FR19b : préciser "1 à N relais par pays, N dimensionné selon charge (≥2 pour failover)"
- FR13c : remplacer "écran d'aide clair" par le texte exact de l'écran (comme FR8c/FR8d le font)
- FR15 : reformuler pour capacité ("Le tray système persiste...") — implémentation déjà couverte par l'architecture
- NFR3 : remplacer "TTL court" par valeur explicite (déjà dans archi : UDP 120s, TCP 300s) → "TTL ≤ 300s TCP, ≤ 120s UDP"
- NFR11 : préciser "connexion standard" → "ADSL/fibre résidentielle RTT < 50ms vers Cloudflare edge"
- NFR15 : ajouter méthode → "mesuré par chronométrage applicatif lors d'un scénario de coupure tunnel provoquée"
- NFR2 : lister les bibliothèques → "Go standard `crypto/ed25519` + TLS via quic-go/TLS standard Go"
- NFR22 : définir "identique" → "matrice de tests e2e 100% passing sur Windows 11, Ubuntu 24.04, Fedora 40, Arch rolling, Alpine 3.19"

## Traceability Validation

### Chain Validation

**Executive Summary → Success Criteria:** ✓ Intact
- Vision (VPN zero-log, anti-DPI, capture L3, multi-OS, kill switch firewall) pleinement reflétée dans les Measurable Outcomes (no DNS leak, no WebRTC leak, failover < 5s, uptime 99.5%, < 30s launch-to-protection)

**Success Criteria → User Journeys:** ✓ Intact
- Zero-config / protection immédiate → Camille #1, #5
- Sélection pays → Camille #4
- Failover transparent → Camille #3
- Support Linux → Mathieu #6
- Operator multi-relais → Akerimus #2

**User Journeys → Functional Requirements:** ✓ Intact
- Toutes les capacités mentionnées dans les Journey → Capabilities Mapping (ligne 157-179) sont couvertes par des FRs explicites
- 6 parcours distincts, tous liés à ≥ 3 FRs chacun

**Scope → FR Alignment:** ✓ Intact
- MVP scope (TUN/Wintun, kill switch firewall, tunnel QUIC, NAT relais, multi-relais, leak check, session tokens, auto-update, paquets Linux + NSIS) tous couverts par FRs correspondants

### Orphan Elements

**Orphan Functional Requirements:** 0 critiques
- Quelques FRs edge-case sont traçables à des business objectives plutôt qu'à des User Journeys explicites (acceptable) :
  - FR8d (IPv6 opt-in) — business objective "utilisateur avancé", pourrait bénéficier d'un journey dédié
  - FR16b (mode dégradé) — business objective "ne pas bloquer l'utilisateur", journey implicite
  - FR20b (signature paquets), NFR22d-i (CI security) — business objective "supply chain security", sans journey user
  - FR5c, FR8c, FR13c, FR15b — edge cases opérationnels, résilience produit

**Unsupported Success Criteria:** 0

**User Journeys Without FRs:** 0 — tous les parcours ont des FRs associés

### Traceability Matrix (synthèse)

| Source | Cible | État |
|---|---|---|
| Vision → Success | Aligné | ✓ |
| Success → Journeys | Aligné (6 parcours couvrent tous critères) | ✓ |
| Journeys → FRs | 100% (via Journey→Capabilities Mapping) | ✓ |
| Scope → FRs | Aligné (MVP complet) | ✓ |
| NFRs → Business Objectives | Implicite mais cohérent | ✓ |

**Total Traceability Issues:** 0 critiques, 4 gaps mineurs (couverture journey perfectible pour edge cases)

**Severity:** Pass

**Recommendation:** Chaîne de traçabilité intacte. Amélioration optionnelle : ajouter 1-2 User Journeys supplémentaires pour couvrir les scénarios edge (utilisateur technique activant IPv6 opt-in, utilisateur confronté au kill switch verrouillé). Non bloquant pour le MVP.

## Implementation Leakage Validation

**Contexte particulier:** Le Voile est un produit de **cybersécurité/réseau** où de nombreux noms techniques sont **la capacité elle-même**, pas des détails d'implémentation :
- QUIC, HTTP/3, TLS 1.3, Ed25519, DNSSEC, DoH, STUN → **protocoles core** définissant la proposition de valeur (anti-DPI, zero-log, résistance au blocage)
- TUN/Wintun → **capacité architecturale** (capture L3 machine-wide)
- nftables, WFP, WFP Filtering Platform → **kill switch kernel-level** est la capacité
- Cloudflare → **infra de camouflage** (pas un choix d'impl, c'est le modèle produit)
- apt/dnf/yay/apk, .deb/.rpm/.apk, NSIS → **distribution cibles utilisateur** (FR20)
- systemd/SCM, CAP_NET_ADMIN → **intégration OS** spécifiée comme capacité

### Leakage réelle détectée

| FR/NFR | Terme | Nature |
|---|---|---|
| FR15 | `fyne.io/systray` | **Leakage confirmée** — la capacité est "tray persiste", pas "utiliser fyne" |
| NFR22f | `go-fuzz / Go 1.18+` | Acceptable borderline — désigne l'outil de fuzzing CI (NFR security testing) |
| NFR22g | `YubiKey` | Acceptable — utilisé comme exemple "type HSM logiciel", pas prescriptif |

### Summary

**Total Implementation Leakage Violations:** 1 (FR15)

**Severity:** Pass (< 2 violations significatives)

**Recommendation:** Aucune fuite d'implémentation majeure. **Action ciblée** : reformuler FR15 pour retirer `fyne.io/systray` (capacité = "le tray système persiste après fermeture de la fenêtre webview", l'implémentation est déjà documentée dans l'architecture). Note : la forte densité de termes techniques dans les FRs/NFRs est cohérente avec le domaine (cybersécurité produit où les protocoles et capacités OS définissent la proposition de valeur).

## Domain Compliance Validation

**Domain:** `cybersecurity_privacy`
**Complexité:** Élevée (produit VPN, traitement de données réseau, cible grand public français/UE)

**Référentiels réglementaires applicables:**
- **RGPD** (Règlement UE 2016/679) — traitement données personnelles
- **Loi française Renseignement** (L. 851-1 CPCE) — opérateur français susceptible de réquisition
- **RGAA 4.1** — accessibilité produits numériques grand public France
- **CNIL** — autorité de contrôle, bonnes pratiques VPN/proxy
- **ANSSI** — recommandations cryptographiques (RGS)
- **Export control** — non applicable (pas d'export hors francophonie, confirmé)

### Required Sections

| Section | État | Notes |
|---|---|---|
| Politique de confidentialité RGPD | ✓ Présent | Section "Vie Privée & Réglementation" mentionne DPA Cloudflare, mentions légales RGPD Art. 12-14 |
| Mention sous-traitant (Cloudflare) | ✓ Présent | DPA signé, rôle documenté |
| Juridiction opérateur | ✓ Présent | Juridiction française documentée, relais hors France, warrant canary advancé MVP |
| Zero-log architectural | ✓ Présent | Core vision + FR18 + NFR20-21 |
| Confidentialité IP client | ✓ Présent | NFR20-21, hash SHA256 dans tokens uniquement |
| Durée de conservation | ✓ Présent | NAT table TTL (UDP 120s, TCP 300s), RAM uniquement |
| Accessibilité RGAA AA | ✓ Présent | Section "Accessibilité" : contraste 4.5:1, navigation clavier, aria-labels, NVDA/Orca |
| Disclosure / Incident Response | ✓ Présent | SECURITY.md, security.txt RFC 9116, PGP key, SLA CVE |
| Cryptographie conforme | ✓ Présent | NFR1-2 (TLS 1.3, Ed25519), NFR9c (constant-time), NFR9f (DNSSEC) |
| Security Testing | ✓ Présent | NFR22d-f (gosec, govulncheck, fuzzing CI) |
| Key Management | ✓ Présent | NFR22g-i (air-gapped master key, rotation, GitHub 2FA hardware) |
| Supply Chain | ✓ Présent | FR20b (signature paquets), GPG commits AUR, Renovate bot |
| Threat Model documenté | ⚠️ Partiel | Risques & Mitigations liste 20+ menaces. Absent : formalisation STRIDE ou LINDDUN |
| Audit externe / Certification | ⚠️ Différé | Code open-source auditable (NFR7). Audit tiers formel non planifié au MVP (acceptable pour v1 gratuit) |
| Data Breach notification procedure | ⚠️ Partiel | Registre d'incidents mentionné (Disclosure). Procédure CNIL 72h formelle non détaillée |

### Compliance Summary

**Required Sections Present:** 12/15 (3 partielles, 0 absente)
**Compliance Gaps:** 3 (tous non bloquants pour MVP)

**Severity:** Pass (avec 3 gaps mineurs à traiter pré-release)

**Recommendation:**
1. **Threat Model formel** : ajouter une section courte "Threat Model" dans la documentation technique (pas le PRD) structurée STRIDE ou LINDDUN. Non bloquant pour le PRD
2. **Procédure CNIL breach** : documenter la procédure interne de notification sous 72h en cas de fuite de données (même si architecture rend ceci très peu probable). Peut figurer dans SECURITY.md
3. **Audit externe** : plan pour un audit de sécurité tiers (pentest) dans la Phase 2 une fois le MVP stabilisé

Le PRD présente une couverture réglementaire **exceptionnelle pour un produit gratuit single-dev** — supérieure à de nombreux VPN commerciaux sur la partie transparence (warrant canary MVP, zero-log by design, open-source, signature chain).

## Project-Type Compliance Validation

**Project Type:** `desktop_app + network_server` (hybride)

### Required Sections (Desktop App)

| Section | État |
|---|---|
| Desktop UX (fenêtre, tray, lifecycle) | ✓ Section "Desktop App Requirements" + FR9-13c, FR15-16b |
| Platform specifics | ✓ Platform Support table (6 plateformes : Win 10/11, Debian/Ubuntu, Fedora/RHEL, Arch, Alpine) |
| Installation/distribution | ✓ FR20 (NSIS + apt/dnf/yay/apk) + section Installeur |
| Service lifecycle | ✓ FR14 (SCM/systemd) + FR15 (tray persistence) + FR15b (UI supervisor) |
| OS integration | ✓ Section System Integration (TUN/Wintun, firewall, routing, capabilities) |

### Required Sections (Network Server)

| Section | État |
|---|---|
| Endpoint specs (relais) | ✓ FR17 (`/tunnel`), FR23 (`/.well-known/relay-registry.json`), FR28-30 (NAT/auth/limits) |
| Auth model | ✓ FR3, FR29, NFR2 (Ed25519 cert pinning + session tokens) |
| Data schemas | ✓ FR22 (TOML config, JSON registre), FR23 (registre signé), session token Ed25519 |
| Monitoring | ✓ NFR18 (uptime relais ≥ 99.5% via /health endpoint) |
| Scaling | ✓ FR19b (multi-relais par pays), FR30 (rate limits par IP), FR30b (limite globale) |
| Stateless / persistence | ✓ FR18 (relais stateless) + NFR3 (NAT table TTL court) |

### Excluded Sections (ne doivent pas être présentes)

| Section | État |
|---|---|
| Mobile UX / iOS / Android | ✓ Absent |
| Touch interactions | ✓ Absent |
| Responsive web design | ✓ Absent |
| Browser compatibility matrix | ✓ Absent (extension supprimée) |
| Push notifications mobiles | ✓ Absent |

### Compliance Summary

**Required Sections (Desktop App):** 5/5 présentes
**Required Sections (Network Server):** 6/6 présentes
**Excluded Sections Present:** 0 (parfait)
**Compliance Score:** 100%

**Severity:** Pass

**Recommendation:** Le PRD couvre exhaustivement les exigences hybrides desktop+network. Aucune section parasite. La nature hybride est bien gérée par les sections distinctes "Desktop App Requirements" (client) et "Relais Multi-VPS" + "IP Camouflage & Tunnel IP" (serveur).

## SMART Requirements Validation

**Total Functional Requirements:** 47 (FR1-FR36 + 11 variants b/c/d)

### Scoring Summary

**All scores ≥ 3:** 100% (47/47)
**All scores ≥ 4:** ~91% (~43/47)
**Overall Average Score:** ~4.6/5.0

### Scoring Distribution (échantillon représentatif)

| FR | Spec | Mes | Att | Rel | Trac | Avg | Notes |
|---|---|---|---|---|---|---|---|
| FR1-FR4 (Tunnel) | 5 | 5 | 5 | 5 | 5 | 5.0 | Référence |
| FR5 (TUN création) | 5 | 5 | 5 | 5 | 5 | 5.0 | Capacité core, métriques (MTU 1420, nom interface) |
| FR5b (watchdog TUN) | 5 | 5 | 5 | 5 | 4 | 4.8 | Edge case, traceability via NFR15-16 |
| FR5c (VPN concurrent) | 4 | 4 | 5 | 5 | 4 | 4.4 | Liste exemples explicites |
| FR8 (kill switch firewall) | 5 | 5 | 5 | 5 | 5 | 5.0 | Spécification précise + persistance documentée |
| FR8c (captive portal) | 5 | 4 | 4 | 5 | 4 | 4.4 | Probe explicite RFC 7710 |
| FR8d (IPv6 opt-in) | 5 | 5 | 5 | 5 | 4 | 4.8 | Setting précis avec valeur défaut |
| FR9-FR13b (UI) | 5 | 5 | 5 | 5 | 5 | 5.0 | Mappés directement aux Journeys |
| FR13c (service absent) | 3 | 3 | 5 | 5 | 4 | 4.0 | "écran d'aide clair" flagged |
| FR14 (autostart) | 5 | 5 | 5 | 5 | 5 | 5.0 | Mécanismes OS précisés |
| FR15 (tray persistence) | 4 | 4 | 5 | 5 | 5 | 4.6 | Leakage `fyne.io/systray` (-1 spec/mes) |
| FR15b (UI supervisor) | 5 | 5 | 5 | 5 | 5 | 5.0 | Spec précise systemd unit + Watchdog SCM |
| FR16b (mode dégradé) | 5 | 5 | 5 | 5 | 4 | 4.8 | Indicateur visuel précisé |
| FR17-FR19b (Relais) | 5 | 5 | 5 | 5 | 5 | 5.0 | — |
| FR19b (un ou plusieurs relais) | 3 | 3 | 5 | 5 | 5 | 4.2 | Quantificateur flou flagged |
| FR20 (install) | 5 | 5 | 5 | 5 | 5 | 5.0 | Cibles concrètes |
| FR20b (signature paquets) | 5 | 5 | 5 | 5 | 4 | 4.8 | — |
| FR21-FR26 (lifecycle/registry) | 5 | 5 | 5 | 5 | 5 | 5.0 | — |
| FR27-FR30b (Tunnel IP & limits) | 5 | 5 | 5 | 5 | 5 | 5.0 | Métriques précises (200/IP, 150 global, 10GiB/jour) |
| FR31-FR34 (Anti-fuite) | 5 | 5 | 5 | 5 | 5 | 5.0 | RFC 5389 explicite |
| FR35-FR36 (Update) | 5 | 5 | 5 | 5 | 5 | 5.0 | Critère santé 30s explicite |

### Improvement Suggestions (FRs flagged)

**FR13c** (Specific/Measurable 3) :
- Remplacer "écran d'aide clair" par le texte exact (déjà fait pour FR8c/FR8d)
- Ex: "Affiche un écran fixe avec titre 'Service Le Voile non démarré' et bloc texte avec la commande shell selon OS détecté"

**FR15** (leakage `fyne.io/systray`) :
- Reformuler : "L'icône système (system tray) persiste en arrière-plan après fermeture de la fenêtre webview. Réouverture via menu tray 'Ouvrir'."

**FR19b** (quantificateur flou) :
- Préciser : "Chaque pays dispose d'au moins 1 relais. Pays prioritaires (FR/IS/FI/DE/ES/GB) ciblés à 2+ relais pour permettre le failover."

### Overall Assessment

**Severity:** Pass (0% flagged < 3 dans toute catégorie)

**Recommendation:** Excellent niveau de qualité SMART. Les 3 FRs mineurs flagged (FR13c, FR15, FR19b) ont des suggestions précises d'amélioration. Aucun FR n'est inutilisable en l'état pour les phases downstream (architecture déjà alignée, implémentation possible).

## Holistic Quality Assessment

### Document Flow & Coherence

**Assessment:** Excellent

**Strengths:**
- Narrative cohérent : vision (anti-DPI VPN zero-log) → différenciateurs → personas → criteria → architecture → FRs/NFRs en flux logique
- Transitions claires entre sections, références croisées (FR mappées dans Journey→Capabilities)
- 4 itérations historiques visibles (editHistory frontmatter) : doc vivant qui suit l'évolution du produit
- Sections "Contraintes Techniques Domaine" + "Risques & Mitigations" + "Compromis Documentés" forment une triade exceptionnelle pour un PRD VPN

**Areas for Improvement:**
- Le PRD est dense (~520 lignes après révision) — un index/TOC en début faciliterait la navigation
- Quelques sections (Disclosure, Accessibilité) ajoutées en fin de section Domain pourraient mériter leur propre H2

### Dual Audience Effectiveness

**For Humans:**
- Executive-friendly: ✓ Executive Summary + "Ce qui rend Le Voile unique" en bullet points punchy
- Developer clarity: ✓ FRs précis avec acteurs et capacités, NFRs avec métriques, architecture séparée
- Designer clarity: ✓ User Journeys narratifs (Camille, Mathieu, Akerimus), Journey→Capabilities Mapping
- Stakeholder decision-making: ✓ Phasing MVP/Phase 2/Phase 3 explicite, Risks documentés

**For LLMs:**
- Machine-readable structure: ✓ ## H2 cohérents, FRs numérotés, tables compliance, frontmatter YAML
- UX readiness: ✓ User Journeys + UI requirements + charte visuelle référencée (architecture)
- Architecture readiness: ✓ Déjà fait — architecture.md aligné et validé
- Epic/Story readiness: ✓ 47 FRs atomiques, traçables, prêts pour décomposition stories

**Dual Audience Score:** 5/5

### BMAD PRD Principles Compliance

| Principle | Status | Notes |
|---|---|---|
| Information Density | Met | 0 anti-pattern détecté (step v-03 Pass) |
| Measurability | Met | 8 violations mineures sur ~87 reqs (step v-05 Warning, corrigeable) |
| Traceability | Met | Chaîne intacte, 0 critique (step v-06 Pass) |
| Domain Awareness | Met | 12/15 sections compliance, RGPD + RGAA + Disclosure (step v-08 Pass) |
| Zero Anti-Patterns | Met | 1 leakage mineure (FR15) (step v-07 Pass) |
| Dual Audience | Met | Voir scoring ci-dessus |
| Markdown Format | Met | Structure ## propre, tables, code blocks, frontmatter YAML |

**Principles Met:** 7/7

### Overall Quality Rating

**Rating:** 4.6/5 — Good (proche Excellent)

**Justification:**
- Tous les 7 principes BMAD respectés
- Couverture exceptionnelle pour un produit single-dev (sécurité, conformité, threat model, accessibilité)
- 11 violations mineures totales (mesurabilité 8 + leakage 1 + traceability gaps 4) — toutes corrigeables en < 1h
- L'écart 4.6 → 5.0 = corrections cosmétiques + ajout TOC + 1-2 journeys edge cases

### Top 3 Improvements

1. **Corrections de mesurabilité ciblées** (FR13c, FR15, FR19b, NFR2, NFR3, NFR11, NFR15, NFR22) — texte exact à substituer fourni dans la section Measurability. Effort : 30 min. Impact : +0.2 sur le rating
2. **Ajout d'un Table of Contents** en début de doc + numérotation H2 explicite — facilite la navigation pour reviewers humains et chunking LLM. Effort : 10 min. Impact : facilite usage downstream
3. **2 User Journeys complémentaires** : "Utilisateur avancé activant IPv6 opt-in" + "Utilisateur confronté au kill switch verrouillé en mobilité" — comble les gaps de couverture journey pour FR8d et FR16b. Effort : 30 min. Impact : traceability complete

### Summary

**This PRD is:** un document de qualité quasi-exemplaire pour un produit VPN open-source, couvrant exhaustivement vision, parcours utilisateurs, exigences fonctionnelles cross-platform (Windows + 4 distros Linux), exigences de sécurité durcies (durcies par session Red Team), conformité réglementaire (RGPD + RGAA + Disclosure), et compromis explicites — supérieur à la médiane des PRDs commerciaux.

**To make it great:** Appliquer les Top 3 Improvements ci-dessus (1h de travail total).

## Completeness Validation

### Template Completeness

**Template Variables Found:** 0 vraies variables (2 occurrences `{...}` mais ce sont des placeholders runtime intentionnels)
- Ligne 281 : `127.0.0.1:{port}` — port configurable documenté dans config TOML, intentionnel
- Ligne 380 : `{nom_interface}` — variable substituée dans message UI utilisateur, intentionnel

**Aucun TODO/FIXME/TBD/XXX trouvé.**

### Content Completeness by Section

| Section | État |
|---|---|
| Executive Summary | ✓ Complete (vision, différenciateurs, distribution, ressources) |
| Project Classification | ✓ Complete (type hybride, domaine, complexité, contexte, ressources) |
| Success Criteria | ✓ Complete (User/Business/Measurable Outcomes avec métriques) |
| User Journeys | ✓ Complete (6 parcours + Journey→Capabilities Mapping) |
| Domain-Specific Requirements | ✓ Complete (Vie Privée/RGPD, Contraintes, Risques, Disclosure, Accessibilité) |
| Innovation & Novel Patterns | ✓ Complete (Innovation Areas + Compromis Documentés + Validation) |
| Desktop App Requirements | ✓ Complete (Architecture, System Integration, Auto-Update, Platform Support) |
| Project Scoping & Phased Development | ✓ Complete (MVP Must-Have, Hors MVP, Phase 2, Phase 3) |
| Functional Requirements | ✓ Complete (47 FRs en 10 sous-sections logiques) |
| Non-Functional Requirements | ✓ Complete (~45 NFRs en 8 sous-sections : Security, Performance, Reliability, Privacy, Platform Compat, Logging, Security Testing, Key Management, Runtime Integrity) |

### Section-Specific Completeness

| Critère | État |
|---|---|
| Success Criteria Measurability | ✓ All measurable (< 30s, no leak, < 5s failover, ≥ 99.5% uptime) |
| User Journeys Coverage | ✓ Complete — couvre user grand public (Camille), opérateur (Akerimus), user technique Linux (Mathieu) |
| FRs Cover MVP Scope | ✓ Yes — 47 FRs couvrent toutes les capacités MVP listées dans Project Scoping |
| NFRs Have Specific Criteria | ✓ All — métriques chiffrées (latences, %, MB, jours), méthodes de mesure documentées (5 violations mineures restantes flaggées étape v-05) |

### Frontmatter Completeness

| Champ | État |
|---|---|
| stepsCompleted | ✓ Présent (20 étapes listées) |
| classification.projectType | ✓ Présent (`desktop_app + network_server`) |
| classification.domain | ✓ Présent (`cybersecurity_privacy`) |
| classification.complexity | ✓ Présent (`high`) |
| classification.projectContext | ✓ Présent (`greenfield`) |
| inputDocuments | ✓ Présent (brainstorming + architecture) |
| lastEdited | ✓ Présent (`2026-04-15`) |
| editHistory | ✓ Présent (5 entrées historiques) |

**Frontmatter Completeness:** 8/8 ✓

### Completeness Summary

**Overall Completeness:** 100% (10/10 sections complètes, 0 template variable non substituée)

**Critical Gaps:** 0
**Minor Gaps:** 0

**Severity:** Pass

**Recommendation:** PRD complet, prêt pour usage downstream (epics, stories, dev). Aucun blocage de complétude.
