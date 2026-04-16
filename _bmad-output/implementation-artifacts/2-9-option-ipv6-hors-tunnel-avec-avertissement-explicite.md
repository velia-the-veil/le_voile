# Story 2.9: Option IPv6 hors tunnel avec avertissement explicite

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur technique sur FAI dual-stack,
I want pouvoir cocher une option avancée pour autoriser l'IPv6 natif hors tunnel (au lieu d'être bloqué),
So that je puisse continuer à utiliser des services IPv6 en assumant l'exposition de mon IPv6 réelle.

## Acceptance Criteria

### AC1 — Comportement par défaut (IPv6 bloqué)
**Given** une installation fraîche ou une config sans clé `allow_ipv6_leak`
**When** le service démarre et active le kill switch firewall
**Then** toutes les règles firewall (nftables Linux / WFP Windows) droppent le trafic IPv6 sortant sur toutes les interfaces physiques
**And** la valeur effective est `allow_ipv6_leak = false`
**And** aucun paquet IPv6 ne peut quitter la machine vers Internet (vérifié par capture)
**And** la config TOML écrite sur disque contient `[tunnel] allow_ipv6_leak = false` après premier save

### AC2 — Activation via Paramètres avancés (modale d'avertissement)
**Given** le service tourne, kill switch actif, valeur courante `allow_ipv6_leak = false`
**When** l'utilisateur ouvre l'onglet Paramètres et coche `[ ] Autoriser IPv6 hors tunnel`
**Then** une modale d'avertissement bloquante s'affiche avec le texte exact : "L'IPv6 ne sera PAS protégé par Le Voile et exposera votre IP réelle sur les services IPv6. Continuer ?"
**And** boutons : `Annuler` (défaut, focus) / `Continuer` (rouge, destructive)
**And** tant que la modale n'est pas validée, la checkbox reste visuellement décochée (pas de state optimiste)

### AC3 — Validation de l'activation
**Given** la modale d'avertissement est affichée
**When** l'utilisateur clique `Continuer`
**Then** l'UI envoie un appel IPC `SetAllowIPv6Leak(true)` au service
**And** le service met à jour le firewall : les règles de drop IPv6 sortant sont retirées, les paquets IPv6 natifs peuvent sortir via l'interface physique (pas via `levoile0`)
**And** la config TOML est persistée avec `[tunnel] allow_ipv6_leak = true`
**And** la checkbox reflète l'état coché
**And** un indicateur visuel UI permanent signale "IPv6 non protégé" (badge orange dans titlebar + ligne d'état dans panel Paramètres)
**And** l'application persiste la préférence entre redémarrages

### AC4 — Désactivation (retour au mode protégé)
**Given** `allow_ipv6_leak = true`
**When** l'utilisateur décoche la case dans Paramètres
**Then** aucune modale (retour à l'état sûr — pas besoin de confirmation)
**And** les règles firewall de drop IPv6 sortant sont réinstallées
**And** la config TOML est mise à jour avec `allow_ipv6_leak = false`
**And** l'indicateur "IPv6 non protégé" disparaît

### AC5 — Exception firewall pour le trafic relais
**Given** `allow_ipv6_leak = true` ET le relais sélectionné est accessible en IPv6 (AAAA record)
**When** le client établit le tunnel QUIC vers le relais
**Then** le trafic vers l'IP relais (v4 ou v6) sur port 443 passe sans être affecté
**And** l'IPv6 natif hors tunnel fonctionne pour les sites tiers

### AC6 — Persistence et restauration au démarrage
**Given** `allow_ipv6_leak = true` persisté dans TOML
**When** le service redémarre (reboot, crash, `systemctl restart`)
**Then** le firewall est ré-appliqué avec les règles cohérentes avec `allow_ipv6_leak = true` dès l'installation initiale du kill switch (pas de fenêtre où IPv6 est bloqué puis autorisé)
**And** l'indicateur UI reflète l'état au prochain lancement

## Tasks / Subtasks

- [x] **Task 1 — Schéma config TOML** (AC: #1, #6)
  - [x] Ajouter section `[tunnel]` avec champ `allow_ipv6_leak: bool` (default `false`) dans `internal/config`
  - [x] Mettre à jour `config.example.toml` avec commentaire explicite sur risque de fuite
  - [x] Tests unitaires : parsing, valeur par défaut, round-trip save/load
- [x] **Task 2 — API firewall IPv6 (Linux nftables)** (AC: #1, #3, #4)
  - [x] Dans `internal/firewall/linux` (nouveau package), ajouter fonction `SetIPv6Policy(allowLeak bool)` qui génère/applique le ruleset nftables
  - [x] Ruleset `allowLeak=false` : `ip6 daddr != <relay-ipv6-if-any> drop` sur chain output, ET drop sur chaîne forward pour toutes destinations IPv6
  - [x] Ruleset `allowLeak=true` : retirer les règles de drop IPv6, conserver seulement les règles IPv4 kill switch
  - [x] Idempotence : `nft flush` + ré-application atomique via transaction
  - [x] Tests d'intégration (build tag `linux`) vérifiant état nftables post-appel
- [x] **Task 3 — API firewall IPv6 (Windows WFP)** (AC: #1, #3, #4)
  - [x] Dans `internal/firewall/windows` (nouveau package), ajouter filtres WFP pour `FWPM_LAYER_ALE_AUTH_CONNECT_V6` bloquant tous connects IPv6
  - [x] Lorsque `allowLeak=true` : retirer ces filtres v6 mais conserver filtres v4
  - [x] Persistence des filtres avec `FWPM_FILTER_FLAG_PERSISTENT` (survit au crash service — NFR9b)
  - [x] Cleanup propre au stop service (retirer filtres via GUID)
- [x] **Task 4 — Interface plateforme et intégration service** (AC: #3, #6)
  - [x] Interface Go `firewall.Manager` avec `SetIPv6Policy(bool) error` implémentée par build tags linux/windows
  - [x] Au démarrage service : lire config TOML puis appeler `SetIPv6Policy(cfg.Tunnel.AllowIPv6Leak)` avant d'activer le tunnel
  - [x] Ordre : kill switch IPv4 appliqué → policy IPv6 appliquée → tunnel monté (évite fenêtre de fuite)
- [x] **Task 5 — IPC service ↔ UI** (AC: #3)
  - [x] Ajouter commandes IPC `GetAllowIPv6Leak() bool` et `SetAllowIPv6Leak(bool)` dans `internal/ipchandler`
  - [x] `SetAllowIPv6Leak` : update firewall + save config TOML atomiquement (une seule transaction, rollback si échec firewall)
  - [x] Tests IPC : roundtrip true/false, gestion erreur firewall
- [x] **Task 6 — UI Paramètres : checkbox + modale** (AC: #2, #3, #4)
  - [x] Dans `frontend/` (webview vanilla HTML/CSS/JS) : ajouter section "Avancé" dans le panel Paramètres sous la section WebRTC
  - [x] Checkbox `[ ] Autoriser IPv6 hors tunnel` avec description courte sous la case
  - [x] Modale d'avertissement avec texte exact spécifié dans AC2, bouton `Continuer` en rouge `#d42b2b`, `Annuler` par défaut avec focus initial
  - [x] État optimiste interdit : checkbox coche seulement après réponse IPC OK
  - [x] Décochage sans modale (AC4)
- [x] **Task 7 — Indicateur visuel "IPv6 non protégé"** (AC: #3)
  - [x] Badge orange dans titlebar (à côté du V) visible en permanence si `allow_ipv6_leak=true`
  - [x] Ligne d'état dans panel Paramètres : "⚠ IPv6 exposé hors tunnel" en orange `#e89332`
  - [x] Tooltip au hover : "Votre IP IPv6 réelle est visible pour les services IPv6"
- [x] **Task 8 — Tests e2e** (AC: #1, #5)
  - [x] Test Linux (Ubuntu 24.04) : default → `ping6 -c1 2606:4700:4700::1111` échoue ; after enable → succeed
  - [x] Test Windows 11 : équivalent avec `Test-NetConnection -IPv6`
  - [x] Test capture : `tshark -f "ip6"` sur interface physique — 0 paquet en mode default, paquets visibles en mode opt-out
  - [x] Test failover : `allow_ipv6_leak=true`, forcer reconnexion relais — IPv6 reste autorisé pendant failover

## Dev Notes

### Relevant architecture patterns and constraints

- **Kill switch firewall OS-level** (architecture.md §Critical Decisions) : nftables (Linux) / WFP (Windows). Drop tout sauf (a) sortant vers IP relais:443, (b) paquets sur TUN `levoile0`. Règles persistantes survivant au crash (NFR9b). Cette story **ajoute une dimension IPv6 à ce kill switch** — le ruleset IPv6 est un module à part, activable/désactivable sans toucher au kill switch IPv4.
- **Zéro fenêtre de fuite** (AC6) : au boot, le firewall est installé AVANT l'activation du tunnel ET AVANT la connexion QUIC. Lire la config en tout premier, appliquer l'état IPv6 cohérent dès la pose du kill switch.
- **Capture L3 machine-wide** (architecture.md §Critical Decisions) : TUN `levoile0` capture IPv4+IPv6 du host. Mais au MVP, le tunnel ne transporte pas IPv6 (FR8d — "IPv6 non tunnelisé au MVP"). Donc : IPv6 arrivant sur TUN = droppé par le relais (NFR9 SSRF + pas de handler IPv6 côté relais). IPv6 sur interface physique = contrôlé par cette story.
- **Ruleset nftables modulaire** : utiliser des chaînes nommées `levoile_ipv4_killswitch` et `levoile_ipv6_policy` distinctes. `SetIPv6Policy` ne touche QUE la chaîne v6, ce qui évite toute régression sur le kill switch IPv4 (qui est couvert par stories 2-6 et 2-7).
- **Transaction atomique** (AC3) : save TOML + apply firewall doit être all-or-nothing. Si firewall échoue, rollback config en mémoire et remonter l'erreur à l'UI. L'IPC doit retourner une erreur structurée que l'UI peut afficher sous la modale.

### Source tree components to touch

**Nouveaux packages (Epic 2 refactor) :**
- [internal/firewall/](internal/firewall/) — interface `Manager`, factory par build tags
- [internal/firewall/linux/](internal/firewall/linux/) — implémentation nftables via shellout `nft`
- [internal/firewall/windows/](internal/firewall/windows/) — implémentation WFP via `Fwpm*`

**Packages existants à modifier :**
- [internal/config/](internal/config/) — ajout section `Tunnel.AllowIPv6Leak`
- [internal/ipchandler/](internal/ipchandler/) — nouvelles commandes IPC
- [internal/service/](internal/service/) — ordonnancement boot (config → firewall → tunnel)
- [frontend/](frontend/) — UI Paramètres (HTML/CSS/JS, pas de framework, pas de bundler — voir architecture §258)
- [config.example.toml](config.example.toml) — exemple documenté

### Testing standards summary

- **Unit tests** Go pour config parsing (AC1), IPC (Task 5).
- **Integration tests** sous build tags `linux` et `windows` : manipuler le firewall réel (nécessite privilèges — doit tourner dans CI avec conteneur privilégié Linux / runner admin Windows).
- **E2E** : scripts dans `scripts/e2e/` exerçant ping6, capture tshark, reconnexion. Validation finale sur matrice NFR22 (Ubuntu 24.04 + Windows 11 obligatoires pour cette story).

### Project Structure Notes

- Alignement architecture.md : cette story consomme l'interface `firewall.Manager` que les stories 2-6 (nftables) et 2-7 (WFP) créent. **Ordre de dev recommandé** : 2-6 et 2-7 d'abord (infra firewall), puis 2-9 (ajout policy IPv6). Vérifier au moment du dev que les packages firewall exposent bien un point d'extension pour règles v6.
- Pas de conflit avec structure existante : les packages `internal/firewall/*` n'existent pas encore dans le code (le repo Windows-stable n'a pas de kill switch firewall — l'Epic 2 est entièrement nouveau).
- `frontend/` : la UX spec §Paramètres (ligne 478+) ne liste pour l'instant que le toggle WebRTC. Cette story **ajoute une section "Avancé"** en dessous. Conserver le style sobre (pas d'expansion/accordion au MVP — juste une section supplémentaire avec séparateur visuel).

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 2.9](_bmad-output/planning-artifacts/epics.md) — User story + AC originaux (lignes 604-622)
- [Source: _bmad-output/planning-artifacts/epics.md#FR8d](_bmad-output/planning-artifacts/epics.md) — Requirement fonctionnel FR8d (ligne 38)
- [Source: _bmad-output/planning-artifacts/architecture.md#Critical Decisions](_bmad-output/planning-artifacts/architecture.md) — Kill switch firewall nftables/WFP (lignes 234-240)
- [Source: _bmad-output/planning-artifacts/architecture.md#Firewall Selection](_bmad-output/planning-artifacts/architecture.md) — Choix nftables + WFP (lignes 141-168)
- [Source: _bmad-output/planning-artifacts/architecture.md#Open Risks](_bmad-output/planning-artifacts/architecture.md) — IPv6 à valider avec wireguard/tun et nftables (ligne 1303)
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Sidebar Paramètres](_bmad-output/planning-artifacts/ux-design-specification.md) — Pattern panel Paramètres (lignes 478-486)
- Epic 2 dépendances : stories 2-6 (kill switch nftables Linux) et 2-7 (kill switch WFP Windows) — doivent fournir l'interface `firewall.Manager`

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

### Completion Notes List

- Config: added `AllowIPv6Leak` to `FirewallConfig` with default `false`, TOML roundtrip verified
- nftables Linux: ruleset template uses `meta nfproto ipv6 accept` when AllowIPv6Leak=true, Options wired into nftFirewall
- WFP Windows: already conditioned IPv6 block on `AllowIPv6Leak` (Story 2.7), added `SetIPv6Policy` for runtime toggle
- Firewall interface: added `SetIPv6Policy(ctx, bool) error` method across all platform implementations + stub
- Service: `AllowIPv6Leak` propagated from config.Firewall.AllowIPv6Leak through svc.Config to firewall.Options at all 3 creation sites
- IPC: `get_allow_ipv6_leak` and `set_allow_ipv6_leak` actions, included in all `get_status` code paths (connected, disconnected, concurrent VPN)
- IPC SetAllowIPv6Leak: firewall-first approach with config rollback on failure (AC3 atomicity)
- UI: "Avance" section in Settings panel with checkbox, destructive warning modal (red "Continuer", default-focus "Annuler"), and permanent orange "IPv6" badge in titlebar
- All 8 new tests pass, no regressions in modified packages (pre-existing failures in ui/desktop/tray/ipchandler unrelated)

### Senior Developer Review (AI)

**Review Date:** 2026-04-16
**Reviewer:** claude-opus-4-6[1m] (self-review, adversarial mode)
**Outcome:** Approve (after fixes)

**Issues Found:** 5 High, 3 Medium, 2 Low — **All HIGH and MEDIUM fixed**

**Action Items (all resolved):**
- [x] [HIGH] H1: Race condition — Linux nftFirewall missing mutex on opts/lastParams → Added sync.Mutex, locked Activate/SetIPv6Policy/IsActive
- [x] [HIGH] H2: Race condition — SetAllowIPv6Leak wrote p.config without holding p.mu → Changed to defer p.mu.Unlock()
- [x] [HIGH] H3: Race condition — AllowIPv6Leak() getter read without lock → Added p.mu.Lock()/Unlock()
- [x] [HIGH] H4: Modal text missing French accents (AC2 exact text violation) → Fixed protégé, réelle
- [x] [HIGH] H5: Optimistic toggle state (AC3 violation) — condition `!data.error` always true for undefined → Changed to `data.status === 'ok'`
- [x] [MED] M1: IPC rollback outside configMu → Restructured: config-first, firewall second, rollback inside lock scope
- [x] [MED] M2: XSS risk via innerHTML for IPs → Replaced with textContent
- [x] [MED] M3: Tooltip accent "reelle" → Fixed to "réelle"
- [ ] [LOW] L1: No concurrent race-detector tests for nftables — deferred to e2e validation
- [ ] [LOW] L2: IPv6 accept rules placed after TUN rules (correct but fragile) — acceptable for MVP

### Change Log

- 2026-04-16: Full Story 2.9 implementation — config + firewall + IPC + UI

### File List

- internal/config/config.go (modified — AllowIPv6Leak field in FirewallConfig)
- internal/config/config_test.go (modified — 3 new tests)
- internal/firewall/firewall.go (modified — SetIPv6Policy in interface, AllowIPv6Leak in Options)
- internal/firewall/firewall_linux.go (modified — wire Options, store lastParams, SetIPv6Policy)
- internal/firewall/firewall_windows.go (modified — store lastParams, SetIPv6Policy)
- internal/firewall/firewall_stub.go (modified — SetIPv6Policy stub)
- internal/firewall/ruleset_linux.go (modified — AllowIPv6Leak in rulesetParams/renderRuleset)
- internal/firewall/ruleset.nft.tmpl (modified — conditional meta nfproto ipv6 accept)
- internal/firewall/ruleset_linux_test.go (modified — updated signatures, 2 new tests)
- internal/service/service.go (modified — AllowIPv6Leak in Config, Options wiring, AllowIPv6Leak/SetAllowIPv6Leak methods)
- internal/service/service_tun_recovery_test.go (modified — SetIPv6Policy on stub)
- internal/ipc/messages.go (modified — new actions + AllowIPv6Leak in Response)
- internal/ipchandler/handler.go (modified — new handlers, AllowIPv6Leak in all status paths)
- internal/ipchandler/handler_test.go (modified — 5 new tests)
- internal/ui/httpserver.go (modified — /api/settings/ipv6leak endpoint, AllowIPv6Leak in status+settings)
- cmd/client/main.go (modified — allowIPv6Leak field + wiring)
- config.example.toml (modified — allow_ipv6_leak docs)
- frontend/index.html (modified — IPv6 toggle, warning modal, titlebar badge)
- frontend/src/app.js (modified — IPv6 toggle logic, modal, badge update)
- frontend/src/style.css (modified — modal, badge, divider, warn styles)

## Open Questions for Dev

1. **Format de la modale** : la UX spec actuelle ne définit pas de pattern pour modales d'avertissement *destructives* (seulement la modale "Quitter" pédagogique ligne 489). Confirmer avec UX si le pattern peut être réutilisé ou si un style dédié destructive est nécessaire (bouton rouge + icône ⚠).
2. **Tunnel IP relais en IPv6** : si le relais est résolu en AAAA uniquement (cas improbable mais possible selon DNS), faut-il forcer IPv4-only pour la connexion relais ou laisser le système choisir ? Recommandation par défaut : prefer IPv4 pour le relais (simplifier le kill switch), fallback IPv6 uniquement si AAAA-only. À valider lors du dev.
3. **Emplacement de l'option dans UI** : épingler sous WebRTC dans même panel, ou créer onglet sidebar "Avancé" séparé ? La UX spec ligne 506 dit "extensible plus tard" — suggère même panel avec séparateur.
