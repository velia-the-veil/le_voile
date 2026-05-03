---
validationTarget: '_bmad-output/planning-artifacts/prd.md'
validationDate: '2026-04-30'
inputDocuments:
  - '_bmad-output/planning-artifacts/prd.md'
  - '_bmad-output/brainstorming/brainstorming-session-2026-03-08-1530.md'
  - '_bmad-output/planning-artifacts/architecture.md'
  - '_bmad-output/planning-artifacts/implementation-readiness-report-2026-04-29-android.md'
validationStepsCompleted: ['step-v-01-discovery', 'step-v-02-format-detection', 'step-v-03-density-validation', 'step-v-04-brief-coverage-validation', 'step-v-05-measurability-validation', 'step-v-06-traceability-validation', 'step-v-07-implementation-leakage-validation', 'step-v-08-domain-compliance-validation', 'step-v-09-project-type-validation', 'step-v-10-smart-validation', 'step-v-11-holistic-quality-validation', 'step-v-12-completeness-validation', 'step-v-13-report-complete']
validationStatus: COMPLETE
holisticQualityRating: '5/5 — Excellent'
overallStatus: 'Pass'
validationFocus: 'Post-Android edit (2026-04-30) — alignement PRD ↔ architecture (révision 2026-04-29) sur la couverture Android Phase 2'
---

# PRD Validation Report

**PRD Being Validated:** `_bmad-output/planning-artifacts/prd.md`
**Validation Date:** 2026-04-30
**Validation Focus:** Post-édition Android (2026-04-30). Vérification que les ajouts Phase 2 — Android sont cohérents avec l'architecture (révision 2026-04-29, ADR-08 à ADR-15), traçables, mesurables et conformes aux standards BMAD PRD (information density, SMART, no implementation leakage, traçabilité).

## Input Documents

- **PRD** — `_bmad-output/planning-artifacts/prd.md` (édité 2026-04-30, 701 lignes, 10 sections H2 + Phase 2 sub-sections, 36 FRs core + 10 FR-AND-* + 26 NFRs core + 11 NFR-AND-*)
- **Brainstorming** — `_bmad-output/brainstorming/brainstorming-session-2026-03-08-1530.md` (genèse 2026-03-08 — antérieur aux pivots Linux et Android, pertinence limitée pour cette validation)
- **Architecture** — `_bmad-output/planning-artifacts/architecture.md` (révision 2026-04-29, 2423 lignes, ADR-01 à ADR-15, plateformes : Windows + Linux + Android 10+ API 29+)
- **Implementation Readiness Report Android** — `_bmad-output/planning-artifacts/implementation-readiness-report-2026-04-29-android.md` (3 gaps critiques identifiés côté PRD/UX/Epics — gap PRD comblé par l'édition de ce jour, à vérifier)

## Validation Findings

### Step v-02 — Format Detection

**PRD Structure (## Level 2 headers) :**

1. Table of Contents
2. Executive Summary
3. Project Classification
4. Success Criteria
5. User Journeys
6. Domain-Specific Requirements
7. Innovation & Novel Patterns
8. Client App Requirements (renommé depuis "Desktop App Requirements" au cours de l'édition 2026-04-30)
9. Project Scoping & Phased Development
10. Functional Requirements
11. Non-Functional Requirements

**BMAD Core Sections Present :**

- Executive Summary : ✓ Present (§1)
- Success Criteria : ✓ Present (§3)
- Product Scope : ✓ Present — couvert par §8 "Project Scoping & Phased Development" (variante naming acceptée par BMAD : Scope/Phases)
- User Journeys : ✓ Present (§4, 9 parcours)
- Functional Requirements : ✓ Present (§9, 36 FRs core + 10 FR-AND-* Phase 2)
- Non-Functional Requirements : ✓ Present (§10, 26 NFRs core + 11 NFR-AND-* Phase 2)

**Format Classification :** BMAD Standard
**Core Sections Present :** 6/6
**Sections additionnelles BMAD-friendly :** Project Classification (§2), Domain-Specific Requirements (§5), Innovation & Novel Patterns (§6), Client App Requirements (§7) — toutes alignées avec les sections optionnelles BMAD (Domain Requirements, Innovation Analysis, Project-Type Requirements).

**Verdict :** Le PRD est en format BMAD Standard parfaitement structuré. Routage vers les checks de qualité (v-03 à v-12).

### Step v-03 — Information Density Validation

**Anti-Pattern Violations (scan FR + EN, case-insensitive) :**

- **Conversational Filler** (FR : "Il est important de noter que", "Le système permettra", "Afin de", "Dans le cadre de", "En vue de", "Il convient de noter" ; EN : "allow users to", "important to note", "In order to", "For the purpose of", "With regard to") : **0 occurrence**
- **Wordy Phrases** (FR : "En raison du fait que", "Dans l'éventualité", "À ce moment précis", "De manière à", "Au niveau de" ; EN : "Due to the fact", "In the event of", "At this point in time") : **0 occurrence**
- **Redundant Phrases** (FR : "Plans futurs", "Antécédents passés", "Absolument essentiel", "Compléter entièrement" ; EN : "Future plans", "Past history", "Completely finish") : **0 occurrence**

**Total Violations :** 0
**Severity Assessment :** **Pass** (seuil < 5)

**Observation :** Le polish 2026-04-15 et l'édition 2026-04-30 ont préservé la haute densité d'information. Le style direct ("Le client peut...", "Le relais peut...") est cohérent dans les nouveaux FR-AND-* / NFR-AND-* — pas de régression sur la concision lors de l'ajout Android.

**Recommandation :** Le PRD démontre une excellente densité d'information. Aucune action requise sur ce critère.

### Step v-04 — Product Brief Coverage

**Status :** N/A — aucun Product Brief formel BMAD dans les inputDocuments du PRD. La genèse est documentée par une session de brainstorming (`brainstorming-session-2026-03-08-1530.md`) qui sert d'amont équivalent. Le check formel "Product Brief coverage" est sauté ; les éléments de vision/users/problem/features/goals/differentiators sont validés implicitement via les autres checks (notamment v-06 traceability et v-12 completeness).

### Step v-05 — Measurability Validation

**Périmètre analysé :**
- 36 FRs core (FR1 à FR36, après suppressions FR37-40) + 10 FR-AND-* (Phase 2 Android) = **46 FRs**
- 26 NFRs core (NFR1 à NFR26, dont NFR8 retiré) + 11 NFR-AND-* (Phase 2 Android) = **37 NFRs**
- **Total : 83 requirements**

**Functional Requirements :**

- **Format compliance** ("[Actor] can [capability]") : 100% des FRs et FR-AND-* respectent le pattern "Le client/Le relais/L'utilisateur peut..."
- **Subjective adjectives** : 0 violation dans les FRs/NFRs. 2 occurrences de "rapide" (PRD ligne 130 dans Parcours 1 — narratif Journey ; ligne 232 dans Mapping table — descriptif), aucune dans un FR/NFR formel
- **Vague quantifiers** : 0 violation. 1 occurrence de "plusieurs composants" (ligne 415, prose architecturale §7) immédiatement explicitée par les composants nommés (`MainActivity`, `LeVoileVpnService`, notification, noyau Go `.aar`) → non-bloquant
- **Implementation leakage** : APIs nommées (`android.net.VpnService`, `WebViewAssetLoader`, `@JavascriptInterface`, gomobile, nftables, WFP, Wintun, kardianos/service, fyne.io/systray) sont **capability-relevant** au sens BMAD — ces APIs sont la matérialisation de la capacité non-rootée / kernel-level, leur substitution casse l'exigence. Cohérent avec le polish 2026-04-15 (NFR2 cite `crypto/ed25519`, NFR9c cite `crypto/subtle.ConstantTimeCompare`)

**FR Violations Total : 0**

**Non-Functional Requirements :**

Vérification template "criterion + metric + measurement method + context" sur les 11 NFR-AND-* :

| NFR | Critère | Metric | Méthode mesure |
|---|---|---|---|
| NFR-AND-1 | RAM | < 60 MB | `adb shell dumpsys meminfo` ✓ |
| NFR-AND-2 | Démarrage | < 3s | chronométrage applicatif (Trace API) ✓ |
| NFR-AND-3 | Taille APK | < 25 MB | `apkanalyzer apk file-size` ✓ |
| NFR-AND-4 | minSdk/targetSdk | 29 / 34 | configuration Gradle (déclaratif, vérifiable build) ✓ |
| NFR-AND-5 | Signature | v2 + v3 Ed25519 | PackageManager Android au install ✓ |
| NFR-AND-6 | Build reproductible | hash SHA256 identique | `sha256sum` 2 builds successifs ✓ |
| NFR-AND-7 | Permissions | liste explicite (5 permissions) | `apkanalyzer manifest permissions` (assertion CI) ✓ |
| NFR-AND-8 | Zéro télémétrie | absence modules listés | `gradle dependencies` (assertion CI) ✓ |
| NFR-AND-9 | Logs | WARN+ release / INFO+ debug | configuration buildType + revue code ✓ |
| NFR-AND-10 | Tests instrumentés | 100% passing API 29/33/34 | Espresso + AndroidX Test ✓ |
| NFR-AND-11 | Obfuscation | `minifyEnabled true` | configuration Gradle ✓ |

Tous les NFRs core (NFR1-26) ont été polish 2026-04-15 (8 corrections mesurabilité documentées) et restent conformes. Les NFR-AND-* respectent le même standard.

**NFR Violations Total : 0**

### Overall Assessment

**Total Requirements :** 83 (46 FRs + 37 NFRs)
**Total Violations :** 0
**Severity :** **Pass** (seuil < 5)

**Recommandation :** Le PRD démontre une mesurabilité exemplaire. Tous les FRs sont au format capability-based testable, tous les NFRs incluent métrique + méthode de mesure. L'ajout Android Phase 2 a respecté le standard établi par le polish 2026-04-15 — pas de régression.

### Step v-06 — Traceability Validation

**Chain Validation :**

- **Executive Summary → Success Criteria** : ✓ **Intact**. Vision (zero-log architectural, indétectable QUIC/HTTP3, multi-relais, capture L3, kill switch OS-level, Android Phase 2) → mappée à Measurable Outcomes (zéro fuite DNS/WebRTC/IPv6, IP masquée, failover < 5s, uptime 99.5%, RAM/CPU desktop, sous-section Phase 2 Android : démarrage VpnService < 3s, RAM < 60 MB, APK < 25 MB, taux activation "VPN permanent" > 95%, build F-Droid reproductible)
- **Success Criteria → User Journeys** : ✓ **Intact**. Chaque critère est démontré par au moins un parcours :
  - "Lance et protégé immédiatement" → Camille #1
  - "Tout le trafic IP transite par tunnel" → Camille #5
  - "Choisir pays" → Camille #4
  - "Failover transparent" → Camille #3
  - "Démarrage VpnService < 3s + RAM < 60 MB Android" → Léa #9
  - "Taux activation 'VPN permanent' via onboarding" → Léa #9 (onboarding obligatoire au premier lancement)
  - "Mode dégradé optionnel" → Camille #8
  - "Linux paquets natifs" → Mathieu #6
  - "IPv6 opt-in" → Théo #7
- **User Journeys → Functional Requirements** : ✓ **Intact**. Le Journey→Capabilities Mapping (§4) listage explicite couvre les 9 parcours. Léa #9 (Android Phase 2) est mappée à 6 capacités : VpnService, Foreground Service+notification, onboarding "VPN permanent"+deeplink, kill switch délégué OS, MainActivity+WebView, distribution F-Droid+APK
- **Scope → FR Alignment** : ✓ **Intact**. MVP §8 couvre les FRs core (FR1-36 Windows+Linux). Phase 2 §8 couvre explicitement Android (FR-AND-1..10, NFR-AND-1..11) + autres enrichissements (IPv6 e2e, ICMP, registre dynamique, certificate pinning renforcé, CI/CD complet)

**Orphan Elements :**

**Orphan Functional Requirements (analyse exhaustive) :**

Examen des 4 FR-AND-* non listés explicitement dans le mapping §4 :

| FR | Mapping §4 | Traçabilité réelle |
|---|---|---|
| FR-AND-5 (JS Bridge `@JavascriptInterface`) | Pas listé | Découle directement de FR-AND-4 (MainActivity+WebView, listé Léa #9) — c'est l'implémentation fonctionnelle du bridge UI↔native évoqué dans le mapping. Non-orphan |
| FR-AND-6 (détection autre VPN via `VpnService.prepare()`) | Pas listé | Edge case parallèle à FR5c desktop (détection VPN concurrent) qui n'est pas non plus dans le mapping. Trace à Executive Summary "le produit fonctionne dès le lancement" + Risques §5. Non-orphan |
| FR-AND-8 (zéro télémétrie/crash reporter Android) | Pas listé | Traçable directement à la promesse zero-log de l'Executive Summary + ADR-15 architecture. Cohérent avec NFR20 (privacy). Non-orphan |
| FR-AND-10 (config JSON `getFilesDir()`) | Pas listé | Traçable à FR22 (config utilisateur persistée, qui mentionne explicitement Android getFilesDir). Non-orphan |

→ **0 orphan FR critique.** Ces 4 FR-AND-* sont des FRs de support/edge case, suivant le même pattern que les FRs desktop FR5b/c, FR7b, FR8c, FR15b qui ne sont pas non plus dans le mapping mais traçables à des principes Executive Summary ou à d'autres FRs.

**Unsupported Success Criteria :** 0
**User Journeys Without FRs :** 0

### Traceability Matrix (résumé)

| Chaîne | État |
|---|---|
| Vision → Success Criteria | Intact ✓ |
| Success Criteria → User Journeys | Intact ✓ |
| User Journeys → FRs (mapping explicite §4) | Intact ✓ (6 lignes ajoutées pour Léa #9) |
| Scope MVP/Phase 2 → FRs | Intact ✓ (Phase 2 Android explicitement scopée) |
| FR-AND-* → User Journeys ou principes Executive Summary | 6/10 mappés directement, 4/10 traçables comme FRs de support (cohérent pattern desktop) |
| NFR-AND-* → FR-AND-* / Executive Summary / Architecture | Intact ✓ (chaque NFR-AND référence un FR-AND ou un principe — ex. NFR-AND-7 ↔ §5 Permissions, NFR-AND-8 ↔ FR-AND-8) |

**Total Traceability Issues : 0**
**Severity : Pass**

**Recommandation :** La chaîne de traçabilité est intacte. Le PRD étend rigoureusement le pattern existant à Android Phase 2 sans rupture. Suggestion mineure non-bloquante : enrichir le Journey→Capabilities Mapping §4 avec 4 lignes pour les FR-AND-5/6/8/10 (parallélisme avec le pattern Léa #9), mais ce n'est pas requis — la pratique actuelle est cohérente avec le côté desktop.

### Step v-07 — Implementation Leakage Validation

**Contexte :** Ce projet est un VPN où la technologie sous-jacente **est** la capacité (la promesse "non-rooté", "kernel-level", "indiscernable DPI" se matérialise par des APIs spécifiques inviolables). Le polish 2026-04-15 a déjà appliqué la règle (FR15 leakage `fyne` retiré). Vérification que l'ajout Android maintient le standard.

**Scan par catégorie :**

- **Frontend frameworks** : 0 violation (pas de React/Vue/Angular/Svelte). Le frontend est HTML/CSS/JS partagé desktop↔Android, mention capability-relevant dans §6 Innovation et §7 Architecture
- **Backend frameworks** : 0 violation (pas d'Express/Django/Rails)
- **Databases** : 0 violation (zero persistence — promesse architecturale)
- **Cloud platforms** : Cloudflare mentionné dans §5 Risques + Innovation (§6) — **capability-relevant** : Cloudflare est l'élément structurant du modèle d'indistinguibilité DPI ("trafic identique à HTTPS Cloudflare standard"), substituer = perdre la promesse anti-blocage
- **Infrastructure** : Pas de Docker/Kubernetes/Terraform. systemd / SCM / Gradle mentionnés — **capability-relevant** (lifecycle service auditable, NFR mesurable)
- **Libraries Android** :
  - `android.net.VpnService`, `WebViewAssetLoader`, `@JavascriptInterface` (FR-AND-1/2/4/5) — **capability-relevant** : seules APIs non-rootées Android pour capture L3 + UI cross-OS. Substituer = casse la promesse "non-rooté" (cf. ADR-10) ou "frontend partagé" (cf. ADR-14)
  - `gomobile` (.aar), JNI bridge (Executive Summary §1, Classification §2, Innovation §6, Architecture §7) — **capability-relevant** : seul mécanisme stable pour partager le noyau Go avec Android. Substituer (réécriture Kotlin native) = doubler la maintenance, contredit ADR-08 (isolation OS, single-dev sustainable)
  - `R8/ProGuard` (NFR-AND-11) — **capability-relevant** : équivalent fonctionnel du `-ldflags="-s -w"` desktop (NFR9h), assertion mesurable
  - `Espresso` + `AndroidX Test` (NFR-AND-10) — **capability-relevant** : framework de test rendant l'NFR matrice plateforme auditable
  - `apkanalyzer`, `gradle dependencies`, `Settings.Global` (NFR-AND-3/7/8 + FR-AND-3) — **capability-relevant** : outils auditables imposés par la mesurabilité de l'NFR
- **Data Formats** : JSON / TOML mentionnés (FR22, FR-AND-10) — **capability-relevant** : convention écosystème (TOML desktop, JSON Android via `getFilesDir()`) auditable, pas un détail d'implémentation interchangeable car affecte le code de migration de config
- **Modules télémétrie cités en négatif** (NFR-AND-8 : Firebase, Sentry, Bugsnag, Crashlytics, Mixpanel, Adjust, Branch, Amplitude) — **capability-relevant** : cités pour rendre l'assertion "zéro télémétrie" mesurable via assertion CI `gradle dependencies`. Pattern parallèle à NFR22d desktop

**Vérification spécifique des nouveaux FR-AND-* / NFR-AND-* :**

Aucune mention de framework/lib non-justifiée. Toutes les références sont :
- Soit des **APIs OS standards** définissant la capacité (VpnService, Foreground Service, JS Bridge — équivalents fonctionnels desktop nftables/WFP/TUN/Wintun/named pipes déjà acceptés au polish 2026-04-15)
- Soit des **outils auditables** rendant les NFRs mesurables (apkanalyzer, gradle, Espresso — équivalents fonctionnels desktop go vet, gosec, govulncheck déjà acceptés)
- Soit des **canaux de distribution distinctifs** (F-Droid build reproductible — équivalent fonctionnel desktop NSIS / .deb / .rpm / AUR / .apk)

### Summary

**Total Implementation Leakage Violations : 0**
**Severity : Pass**

**Recommandation :** Aucun leakage détecté. Le PRD respecte rigoureusement la doctrine "WHAT not HOW" pour ce projet où certaines APIs OS sont structurellement la capacité. Cohérence forte avec le polish 2026-04-15. L'ajout Android n'a introduit aucune mention de framework non-justifié (pas de Compose / Flutter / RN dans les FRs — uniquement dans §6 Compromis pour expliquer pourquoi ils ont été rejetés, ce qui est documentaire).

### Step v-08 — Domain Compliance Validation

**Domain :** `cybersecurity_privacy` (frontmatter)
**Complexité :** **Élevée** (domaine régulé, exigences renforcées)

**Sections spéciales requises pour ce domaine :**

| Exigence | Présence | Localisation | Adéquation |
|---|---|---|---|
| Privacy / RGPD | ✓ | §5 "Vie Privée & Réglementation" | Adéquat — minimisation, DPA Cloudflare, mentions légales RGPD Art. 12-14, juridiction opérateur, warrant canary advancé MVP |
| Accessibilité (RGAA produit français) | ✓ | §5 "Accessibilité" | Adéquat — niveau AA, NVDA/Orca/TalkBack (TalkBack ajouté Phase 2), contraste, navigation clavier, focus, taille police |
| Disclosure / Incident Response | ✓ | §5 "Disclosure & Incident Response" | Adéquat — SECURITY.md, security.txt RFC 9116, SLA triage 48h, disclosure 90j, bug bounty informel, SLA patch CVE, registre incidents |
| Threat model / Risques & Mitigations | ✓ | §5 "Risques & Mitigations" | Adéquat — ~28 risques structurés (Cloudflare, VPS saisie, clés Ed25519, AV/firewall, fuite reconnect, blocage VPN France, fuite WebRTC, crash, saturation relais, auto-update cassée, runtime deps, IPv6, firewall tiers Windows, master key compromise, GitHub compromise, DDoS, DNS poisoning, malware local, Wintun signature, packet injection, session token + 4 risques Android Phase 2) |
| Cryptographic standards | ✓ | §5 "Contraintes Techniques Domaine" + NFR1-2, NFR9c, NFR9e, NFR9f, NFR9i, NFR22g-i | Adéquat — Ed25519 std lib uniquement, TLS 1.3, constant-time compare, DNSSEC, DoH bootstrap, master key air-gapped + rotation 24 mois |
| Supply chain security | ✓ | NFR22d-f, FR20b, FR-AND-7, NFR-AND-5/6 | Adéquat — `gosec`, `govulncheck`, deps épinglées, fuzzing, signature Ed25519 paquets, signing commits GPG, build reproductible F-Droid (Phase 2), Gradle audit deps (Phase 2) |
| Audit / Observability (sans logs intrusifs) | ✓ | NFR22a-c, NFR-AND-9 | Adéquat — logs ops only, rotation 7j/10MB, niveau filtré, Logcat Android filtré buildType |
| Permissions minimales | ✓ | §5 "Contraintes Techniques Domaine" + §5 "Permissions Android Minimales" + NFR-AND-7 | Adéquat — capabilities Linux CAP_NET_ADMIN/RAW, LocalSystem Windows, permissions Android sans aucune permission dangereuse |
| Runtime integrity | ✓ | NFR25-26 + NFR-AND-5 | Adéquat — vérification intégrité binaire SHA256+Ed25519 desktop, PackageManager check signature APK Android |
| Distribution security | ✓ | FR20b + §5 "Distribution Android" + NFR-AND-6 | Adéquat — paquets signés Ed25519 desktop, APK signé v2/v3 + build reproductible F-Droid Android |

**Conformité des ajouts Android Phase 2 :**

Tous les ajouts respectent les exigences `cybersecurity_privacy` :
- FR-AND-7 (F-Droid build reproductible + APK signé v2/v3) → supply chain
- FR-AND-8 (zéro télémétrie/crash reporter) → privacy
- FR-AND-10 (config scoped storage `getFilesDir()`) → privacy + integrity
- NFR-AND-5 (signature APK v2+v3) → supply chain
- NFR-AND-6 (build reproductible vérifiable) → supply chain (auditable utilisateur)
- NFR-AND-7 (permissions minimales auditables) → privacy
- NFR-AND-8 (zéro modules télémétrie auditables CI) → privacy
- NFR-AND-9 (logs Android filtrés, pas de données utilisateur) → privacy + observability
- NFR-AND-11 (R8/ProGuard release) → reverse engineering deterrent (parallèle NFR9h desktop)

### Summary

**Required Sections Present : 10/10**
**Compliance Gaps : 0**
**Severity : Pass** (adéquation au-delà du standard BMAD habituel)

**Recommandation :** Conformité exemplaire pour un domaine `cybersecurity_privacy`. Le PRD couvre RGPD, RGAA, disclosure, threat model, crypto, supply chain, observability sans logs intrusifs, permissions minimales, runtime integrity, distribution security. Les ajouts Android Phase 2 maintiennent et étendent rigoureusement ces standards (notamment via build reproductible F-Droid, audit dépendances Gradle, permissions Android sans aucune permission dangereuse).

### Step v-09 — Project-Type Compliance Validation

**Project Type :** `desktop_app + mobile_app + network_server` (frontmatter, mis à jour 2026-04-30 pour inclure `mobile_app` Phase 2)

Projet **hybride** combinant 3 types — la validation applique les exigences de chacun.

**Required sections (par type) :**

| Type | Required | Localisation PRD | État |
|---|---|---|---|
| desktop_app | Desktop UX | §7 Desktop sous-section (architecture 2-process, system integration nftables/WFP, auto-update, platform support) | ✓ Présent |
| desktop_app | Platform specifics (Windows/Mac/Linux) | §7 Platform Support (Windows 10/11, 4 distros Linux, macOS Phase 3) | ✓ Présent |
| mobile_app | Mobile UX | §7 Android sous-section (architecture mono-process MainActivity+WebView+Foreground Service, layout responsive mobile boutons 48dp) | ✓ Présent |
| mobile_app | Platform specifics (Android) | §7 Platform Support ligne Android 10+ API 29+ + §5 Permissions Android Minimales + §5 Distribution Android | ✓ Présent |
| mobile_app | Offline mode behavior | Comportement défini : kill switch bloque tout en mode déconnecté (FR8 desktop + FR-AND-3 Android) ; mode dégradé optionnel Camille #8 / FR16b ; pas de "data sync mode" applicable à un VPN | ✓ Couvert (par design VPN) |
| network_server | Endpoint Specs | §9 "Découverte & Sélection de Relais" (FR23 `/.well-known/relay-registry.json`) + §9 "IP Camouflage & Tunnel IP" (FR27-30 `/tunnel`, framing) + §10 NFR18 endpoint `/health` | ✓ Présent |
| network_server | Auth Model | FR3 (auth Ed25519 par relais), FR29 (session tokens Ed25519 TTL 4h), NFR1-2 (TLS 1.3 + Ed25519), NFR9d (IP hash check) | ✓ Présent |
| network_server | Data Schemas | FR23 (registre JSON signé), FR27 (framing 2 octets longueur + payload), FR22 (config TOML / JSON Android) | ✓ Présent |

**Excluded sections check :**

Type hybride — les exclusions classiques (desktop exclut mobile, mobile exclut desktop, network_server exclut UX/UI) **ne s'appliquent pas** car ce PRD couvre légitimement les 3 dimensions ensemble. La présence simultanée de Desktop UX + Mobile UX + Network Server endpoints est cohérente avec la classification hybride.

**Aucune section non-justifiée détectée.**

### Compliance Summary

**Required Sections : 8/8 présentes**
**Excluded Sections Present : 0** (pas d'exclusion bloquante pour un projet hybride)
**Compliance Score : 100%**
**Severity : Pass**

**Recommandation :** Le PRD couvre parfaitement les exigences combinées de desktop_app + mobile_app + network_server. La structure hybride est explicitement assumée via §7 "Client App Requirements" (sous-sections Desktop / Android) et §9 (sections Tunnel/Capture/Relais/Camouflage/Découverte côté serveur + sections UI côté client). Cohérence forte avec l'architecture qui adopte la même structure (cf. ADR-08 isolation OS).

### Step v-10 — SMART Requirements Validation

**Total Functional Requirements analysés : 46** (36 FRs core + 10 FR-AND-* Phase 2)

**Méthodologie :** scoring 1-5 sur Specific / Measurable / Attainable / Relevant / Traceable. Échantillonnage exhaustif des FR-AND-* (nouveaux) + sampling représentatif des FR core (déjà polish 2026-04-15).

#### Scoring détaillé — FR-AND-* (Phase 2 Android — nouveaux ajouts 2026-04-30)

| FR # | Specific | Measurable | Attainable | Relevant | Traceable | Avg | Flag |
|---|---|---|---|---|---|---|---|
| FR-AND-1 (VpnService Builder + tun fd) | 5 | 5 | 5 | 5 | 5 (Léa #9) | 5.0 | — |
| FR-AND-2 (Foreground Service + notification) | 5 | 5 | 5 | 5 | 5 (Léa #9) | 5.0 | — |
| FR-AND-3 (onboarding "VPN permanent" + deeplink) | 5 | 4 (heuristique Settings.Global fragile, documentée) | 5 | 5 | 5 (Léa #9, kill switch ADR-10) | 4.8 | — |
| FR-AND-4 (MainActivity + WebView responsive) | 5 | 4 (responsive testable mais "≥ 48dp" implicite) | 5 | 5 | 5 (Léa #9) | 4.8 | — |
| FR-AND-5 (JS Bridge `@JavascriptInterface`) | 5 | 5 (4 méthodes nommées + max 4 Ko) | 5 | 5 | 4 (découle de FR-AND-4) | 4.8 | — |
| FR-AND-6 (détection autre VPN via prepare()) | 5 | 5 (Intent non-null testable) | 5 | 5 | 4 (edge case, parallèle FR5c desktop) | 4.8 | — |
| FR-AND-7 (F-Droid build reproductible + APK signé v2/v3) | 5 | 5 (hash SHA256 vérifiable) | 5 | 5 | 5 (Léa #9 + supply chain) | 5.0 | — |
| FR-AND-8 (zéro télémétrie audit Gradle) | 5 | 5 (modules listés, audit CI) | 5 | 5 | 5 (privacy + ADR-15) | 5.0 | — |
| FR-AND-9 (auto-update différencié F-Droid / APK) | 5 | 4 (pas de seuil chiffré sur la notification) | 5 | 5 | 4 (Léa #9 résolution mentionne reboot) | 4.6 | — |
| FR-AND-10 (config JSON `getFilesDir()`) | 5 | 5 (chemin précis) | 5 | 5 | 4 (lié FR22) | 4.8 | — |

**Avg FR-AND-* : 4.86 / 5.0** — Aucun flag, qualité excellente.

#### Échantillonnage FR core (déjà polish 2026-04-15)

| FR # | Avg | Note |
|---|---|---|
| FR1 (tunnel QUIC/HTTP3) | 5.0 | Spec, métrique implicite via NFR1, traçable Camille #1 |
| FR5 (TUN/Wintun Builder) | 5.0 | Très spec (MTU 1420, nom levoile0) |
| FR8 (kill switch firewall OS-level) | 5.0 | Très spec (interface TUN + IP relais:443) |
| FR8d (IPv6 opt-in) | 5.0 | Spec exemplaire (TOML setting, warning text, decoché par défaut) |
| FR13c (écran "service non démarré") | 5.0 | Polish 2026-04-15 a cité texte exact |
| FR16b (mode dégradé) | 4.8 | Spec + indicateur visuel persistent |
| FR19b (relais par pays) | 5.0 | Quantification chiffrée (≥ 1 + DE/ES/GB/US ≥ 2) |
| FR23 (registre `/.well-known/...`) | 5.0 | URL endpoint exacte |
| FR27 (encapsulation paquets IP) | 5.0 | Framing précisé (2 octets longueur + payload) |
| FR30 (rate limiting) | 5.0 | Quantifié (200 tunnels/IP, 10 GiB/jour) |
| FR36 (auto-update + rollback) | 5.0 | Conditions explicites (30s, atomique, retry) |

**Avg FR core (sample) : ~4.95 / 5.0**

### Scoring Summary

- **All scores ≥ 3 :** 100% (46/46)
- **All scores ≥ 4 :** 100% (46/46)
- **Overall Average Score : ~4.93 / 5.0**

### Improvement Suggestions

**Aucune amélioration critique requise.** Les rares scores à 4 (jamais en dessous) sont :

1. **FR-AND-3** — Mesurabilité 4 sur la heuristique `Settings.Global` (API non-publique Android, fragile). **Atténuation déjà documentée** dans la description du FR ("fallback 'non vérifiable'"). C'est une honnêteté technique, pas une faiblesse rédactionnelle.
2. **FR-AND-4** — Mesurabilité 4 car "boutons tactiles ≥ 48dp" pourrait être précisé "vérifié via Espresso UI test ou inspection layout". Suggestion mineure : ajouter une référence explicite à la méthode de vérification (cohérent avec le polish 2026-04-15 sur les NFRs).
3. **FR-AND-9** — Mesurabilité 4 car "notification UI" n'a pas de seuil de timing. Suggestion mineure : préciser la périodicité de check (ex. quotidien).
4. **FR-AND-5/6/10** — Traçabilité 4 (non listés explicitement dans Mapping §4). Suggestion mineure déjà notée à v-06 : enrichir le Mapping §4 (non-bloquant, cohérent avec la pratique desktop).

### Overall Assessment

**Severity : Pass** (0% FRs flagged ; 100% all-criteria ≥ 4)

**Recommandation :** Qualité SMART exemplaire. Le polish 2026-04-15 et l'édition Android 2026-04-30 maintiennent un standard très élevé. Les 4 suggestions mineures ci-dessus peuvent être appliquées en cleanup futur si désiré, mais ne bloquent pas la consommation downstream du PRD.

### Step v-11 — Holistic Quality Assessment

#### Document Flow & Coherence

**Assessment : Excellent**

**Strengths :**
- Structure exemplaire : TOC + 10 sections H2 numérotées avec transitions logiques (Vision → Classification → Success Criteria → Journeys → Domain → Innovation → Client App → Scoping → FRs → NFRs)
- Fil rouge Android Phase 2 visible de bout en bout : introduit §1 Exec Summary → classifié §2 → mesurable §3 → illustré Parcours 9 §4 → conforme §5 → innovant §6 → architecturé §7 → scopé §8 → formalisé §9 (FR-AND-*) et §10 (NFR-AND-*)
- Risques & Mitigations §5 dense et structuré (28 lignes incluant 4 risques Android Phase 2)
- editHistory frontmatter chronologique tracé (7 entrées de 2026-03-08 à 2026-04-30) — auditabilité forte
- Distinction Desktop/Android/Phase 2 systématiquement marquée dans les FRs/NFRs (préfixes ou parenthèses explicites)

**Areas for Improvement :**
- Mapping §4 pourrait être enrichi de 4 lignes pour FR-AND-5/6/8/10 (cosmétique, non-bloquant — cf. v-06 et v-10)

#### Dual Audience Effectiveness

**For Humans :**
- Executive-friendly : ✓ §1 Executive Summary clair (vision zéro-log architectural, Android Phase 2 positionnée explicitement), différenciateurs listés
- Developer clarity : ✓ §7 architecture détaillée (composants nommés, APIs nommées, lifecycle décrit), §9/§10 FRs/NFRs précis avec méthodes de mesure
- Designer clarity : ✓ §4 Journeys narratifs avec 5 personas (Camille/Akerimus/Mathieu/Théo/Léa), Charte plateformeliberte.fr référencée, accessibilité RGAA + TalkBack
- Stakeholder decision-making : ✓ §5 Risques + §8 Phasing explicite (MVP / Phase 2 / Phase 3) + measurable outcomes chiffrés

**For LLMs :**
- Machine-readable structure : ✓ H2 numérotés 1-10, FR/NFR IDs stables (FR-AND-1..10 pattern parfaitement extractible parallèle à FR1..36)
- UX readiness : ✓ Journeys structurés avec sections Qui/Scène/Action/Moment clé/Résolution
- Architecture readiness : ✓ §7 architecture explicite, références ADR, mapping FR ↔ composants
- Epic/Story readiness : ✓ FR-AND-* listés en tant que baseline de découpage Phase 2 (mention explicite dans le préambule de la sous-section §9)

**Dual Audience Score : 5/5**

#### BMAD PRD Principles Compliance

| Principe | État | Notes |
|---|---|---|
| Information Density | ✓ Met | 0/0 anti-patterns détectés (v-03) |
| Measurability | ✓ Met | 0/83 violations (v-05) |
| Traceability | ✓ Met | 0 broken chains, 0 critical orphans (v-06) |
| Domain Awareness | ✓ Met | 10/10 sections requises cybersecurity_privacy (v-08) |
| Zero Anti-Patterns | ✓ Met | 0 leakage non-justifiée (v-07) |
| Dual Audience | ✓ Met | Humains + LLMs servis (v-11 ci-dessus) |
| Markdown Format | ✓ Met | TOC + H2 numérotés + tables + YAML frontmatter |

**Principles Met : 7/7**

#### Overall Quality Rating

**Rating : 5/5 — Excellent**

**Justification :** Tous les checks systématiques v-02 à v-10 passent. Aucun gap critique. Document prêt pour consommation downstream (UX, Architecture, Epics). L'édition Android 2026-04-30 a maintenu et étendu le niveau de qualité atteint au polish 2026-04-15, sans introduire aucune régression.

#### Top 3 Improvements (cosmétiques, non-bloquants) — ✅ APPLIQUÉS le 2026-04-30

1. **Enrichir le Journey→Capabilities Mapping §4 avec 4 lignes Phase 2 supplémentaires** — ✅ **Appliqué.** 4 lignes ajoutées au mapping (FR-AND-5 JS Bridge, FR-AND-6 détection autre VPN, FR-AND-8 zéro télémétrie, FR-AND-10 config getFilesDir), toutes mappées à Léa #9. Les 10 FR-AND-* sont désormais tous reflétés dans le mapping.

2. **Préciser la fréquence du check d'auto-update Android dans FR-AND-9** — ✅ **Appliqué.** FR-AND-9 précisé : "vérification au lancement de l'app + vérification périodique en arrière-plan toutes les 24h via `WorkManager` (cohérent FR35 desktop)". Pour F-Droid, vérification embarquée désactivée (le client F-Droid gère).

3. **Ajouter la condition réseau dans NFR-AND-2 (établissement tunnel < 3s)** — ✅ **Appliqué.** NFR-AND-2 enrichi : "sur réseau LTE/4G+ ou Wi-Fi domestique avec RTT < 80ms vers le VPS relais — parallèle fonctionnel à NFR11 desktop (ADSL/fibre, RTT < 50ms)".

#### Summary

**This PRD is :** un document BMAD Standard de qualité exemplaire, qui couvre rigoureusement un projet hybride desktop+mobile+network_server en domaine cybersecurity_privacy, avec une traçabilité complète et une mesurabilité sans faille — l'extension Phase 2 Android s'intègre proprement sans régression sur les standards atteints au polish 2026-04-15.

**To make it great :** 3 ajustements cosmétiques mineurs ci-dessus (~ 30 minutes de travail), aucun bloquant pour démarrer la phase de découpage epics/stories Android.

### Step v-12 — Completeness Validation

#### Template Completeness

**Template Variables Found : 0 véritables violations**

4 occurrences de la notation `{xxx}` dans le PRD, toutes **patterns documentés** (placeholders i18n des messages d'app, **pas** des template variables BMAD non résolus) :

| Ligne | Occurrence | Statut |
|---|---|---|
| 383 | `127.0.0.1:{port}` | ✓ Pattern : port HTTP local dynamique de l'UI (documenté en prose) |
| 439 | `"Mise à jour {version} disponible"` | ✓ Pattern : message UI Android avec version dynamique |
| 518 | `"VPN concurrent détecté ({nom_interface})"` | ✓ Pattern : message FR5c avec nom interface dynamique |
| 611 | `"Mise à jour {version} disponible"` | ✓ Pattern : analogue ligne 439 (FR-AND-9) |

Aucun template variable BMAD (`{{var}}`, `[placeholder]`, `[TODO]`, `[TBD]`) trouvé dans le PRD.

#### Content Completeness by Section

| Section | État | Notes |
|---|---|---|
| §1 Executive Summary | ✓ Complete | Vision, différentiateurs, Android Phase 2 explicitement positionnée |
| §2 Project Classification | ✓ Complete | Type hybride, domaine, complexité, contexte, distribution, ressources |
| §3 Success Criteria | ✓ Complete | User Success + Business Success + Measurable Outcomes + Measurable Outcomes Phase 2 (Android) |
| §4 User Journeys | ✓ Complete | 9 parcours (Camille ×4, Akerimus, Mathieu Linux, Théo IPv6, Camille mode dégradé, **Léa Android Phase 2**) + Mapping enrichi |
| §5 Domain-Specific | ✓ Complete | Vie privée, Disclosure, Accessibilité, **Distribution Android**, **Permissions Android**, Contraintes tech, Risques (28 lignes) |
| §6 Innovation | ✓ Complete | 8 patterns (dont 2 Android), 4 compromis (dont 2 Android), Validation (dont F-Droid + TalkBack) |
| §7 Client App Requirements | ✓ Complete | Sous-sections **Desktop** + **Android** + Platform Support (7 plateformes) |
| §8 Scoping | ✓ Complete | MVP / Phase 2 (avec Android explicite + hors-scope iOS/Wear/TV/Auto) / Phase 3 |
| §9 FRs | ✓ Complete | 36 FRs core (10 sous-sections) + 10 FR-AND-* (Phase 2 Android) |
| §10 NFRs | ✓ Complete | 26 NFRs core (8 sous-sections) + 11 NFR-AND-* (Phase 2 Android) |

#### Section-Specific Completeness

- **Success criteria measurability** : ✓ All — chaque critère a une métrique (chiffrée ou méthode auditable)
- **Journeys coverage** : ✓ Yes — couvre les 5 personas (Camille grand public, Akerimus opérateur, Mathieu Linux, Théo technique, Léa Android)
- **FRs cover MVP scope** : ✓ Yes — MVP §8 list mappable vers FR1-36 ; FR-AND-* explicitement scopés Phase 2
- **NFRs have specific criteria** : ✓ All — chaque NFR a métrique + méthode de mesure (vérifié v-05)

#### Frontmatter Completeness

| Champ | État | Valeur |
|---|---|---|
| stepsCompleted | ✓ Present | 23 steps (workflow create + 3 cycles edit) |
| classification | ✓ Present | `desktop_app + mobile_app + network_server` / `cybersecurity_privacy` / `high` / `greenfield` |
| inputDocuments | ✓ Present | 2 documents (brainstorming + readiness report Android) + ref architecture |
| date / lastEdited | ✓ Present | date `2026-03-08` / lastEdited `2026-04-30` |
| editHistory | ✓ Present | 7 entrées chronologiques |
| workflowType | ✓ Present | `prd` |
| documentCounts | ✓ Present | briefs/research/brainstorming/projectDocs |

**Frontmatter Completeness : 7/4** (au-delà du minimum requis)

#### Completeness Summary

**Overall Completeness : 100%** (10/10 sections complètes, frontmatter complet, 0 template variables)

**Critical Gaps : 0**
**Minor Gaps : 0**
**Severity : Pass**

**Recommandation :** Le PRD est complet à tous les niveaux de granularité. Aucune action requise. Document prêt pour distribution downstream.

---

## Final Summary

**Overall Status : Pass** ✓

### Quick Results

| Check | Sévérité | Note |
|---|---|---|
| Format Detection (v-02) | BMAD Standard | 6/6 sections core |
| Information Density (v-03) | Pass | 0/0 anti-patterns |
| Product Brief Coverage (v-04) | N/A | Pas de Product Brief formel |
| Measurability (v-05) | Pass | 0/83 violations |
| Traceability (v-06) | Pass | 0 broken chains, 0 critical orphans |
| Implementation Leakage (v-07) | Pass | 0 leakage non-justifiée |
| Domain Compliance (v-08) | Pass | 10/10 sections requises cybersecurity_privacy |
| Project-Type Compliance (v-09) | Pass | 100% (projet hybride desktop+mobile+server) |
| SMART Quality (v-10) | Pass | 100% all-criteria ≥ 4 ; avg 4.93/5 |
| Holistic Quality (v-11) | Excellent | 5/5 |
| Completeness (v-12) | Pass | 100% |

**Critical Issues : 0**
**Warnings : 0**

### Strengths

- Structure BMAD Standard exemplaire (TOC, H2 numérotés, frontmatter riche)
- Mesurabilité quasi-parfaite (chaque NFR a métrique + méthode de mesure auditable)
- Conformité cybersecurity_privacy au-delà du standard BMAD habituel (RGPD + RGAA + disclosure + supply chain + audit + crypto + permissions minimales + runtime integrity + distribution security)
- Traçabilité visible et explicite via Journey→Capabilities Mapping §4
- Architecture hybride desktop+mobile+network_server proprement scopée
- L'extension Phase 2 Android (édition 2026-04-30) maintient et étend rigoureusement le standard atteint au polish 2026-04-15 — pas de régression
- Cohérence forte avec l'architecture (révision 2026-04-29, ADR-08 à ADR-15) — les 3 gaps critiques côté PRD identifiés dans le readiness report 2026-04-29 sont **comblés**

### Holistic Quality Rating

**5/5 — Excellent** : Document exemplaire, prêt pour production downstream (UX update, création epics Android, sprint planning Phase 2).

### Top 3 Improvements — ✅ APPLIQUÉS le 2026-04-30

1. ✅ **Mapping §4 enrichi** : 4 lignes ajoutées pour FR-AND-5 / FR-AND-6 / FR-AND-8 / FR-AND-10 (toutes mappées Léa #9)
2. ✅ **FR-AND-9 précisé** : périodicité auto-update au lancement + WorkManager 24h (cohérence FR35 desktop)
3. ✅ **NFR-AND-2 enrichi** : condition réseau LTE/4G+ ou Wi-Fi domestique, RTT < 80ms (parallèle NFR11 desktop)

### Recommandation Finale

PRD validé en l'état. Les gaps critiques côté PRD identifiés par le rapport readiness Android 2026-04-29 sont comblés. Les 2 autres gaps critiques (UX en contradiction "pas de version mobile" + absence d'epics Android) restent à traiter dans les artefacts respectifs (UX et epics) avant de pouvoir produire un verdict de readiness Android complet.

**Suivants :**
1. Mise à jour `ux-design-specification.md` (retirer la phrase "pas de version mobile", ajouter wireframes mobile + onboarding kill switch + responsive guidelines)
2. Création des epics Android dans `epics.md` (cohérence avec la séquence d'implémentation 16 étapes du Decision Impact Analysis architecture)
3. Re-run du check `bmad-bmm-check-implementation-readiness` une fois UX et epics traités












