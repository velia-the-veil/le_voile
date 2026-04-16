# Story 2.6: Kill switch firewall OS-level via nftables (Linux)

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a **utilisateur final Linux**,
I want **qu'un kill switch firewall kernel-level bloque tout trafic sortant sauf TUN + IP relais:443**,
so that **aucune fuite n'est possible même si le service crashe (SIGKILL, panic, OOM)**.

## Acceptance Criteria

### AC1 — Activation nominale via `nft -f -`

**Given** le binaire `nft` est présent (`/usr/sbin/nft` ou dans `$PATH`) et le module kernel `nf_tables` est chargé (vérifiable via `/proc/modules` ou `nft list tables`)
**When** `firewall.Activate(relayIP net.IP, tunName string)` est appelé sur Linux avec `relayIP=198.51.100.42` et `tunName="levoile0"`
**Then** un ruleset `inet levoile` est généré via un template Go (`ruleset.nft.tmpl`) et appliqué atomiquement via `nft -f -` (stdin pipe, pas de fichier temporaire)
**And** le ruleset flush d'abord toute table `inet levoile` préexistante (idempotent)
**And** les règles autorisent strictement :
- `oifname "levoile0" accept` (tout trafic sortant via la TUN)
- `ip daddr 198.51.100.42 udp dport 443 accept` (QUIC vers relais)
- `ip daddr 198.51.100.42 tcp dport 443 accept` (fallback TCP vers relais, si QUIC refusé)
- `iifname "lo" accept` et `oifname "lo" accept` (loopback)
- `ct state established,related accept` (retours connexions autorisées)
- politique `chain output { type filter hook output priority 0; policy drop; }` (deny-all par défaut)
- chain `input` politique `drop` sauf `iifname "lo"` et `ct state established,related` et `oifname "levoile0"` (réponses TUN)
**And** `nft list ruleset` lancé après Activate contient la table `inet levoile` avec les règles attendues
**And** l'activation complète (génération template + shellout + vérification) est mesurée `< 100ms` par chronométrage applicatif (NFR15)

### AC2 — Persistance après crash service

**Given** le service `levoile.service` a appelé `firewall.Activate()` avec succès
**When** le process est tué brutalement (`kill -9 <pid>` ou panic fatal)
**Then** la table `inet levoile` reste chargée dans le kernel (vérifiable via `nft list ruleset` après SIGKILL)
**And** aucun trafic non-tunnel ne peut sortir (test : `curl --interface eth0 https://1.1.1.1` échoue avec `Network is unreachable` ou timeout)
**And** le ping ICMP vers internet hors TUN est droppé

### AC3 — Nettoyage orphelins au redémarrage

**Given** le service redémarre après un crash et détecte une table `inet levoile` préexistante
**When** `Activate()` est appelé à nouveau (orchestration service Connect)
**Then** la table orpheline est détectée par lecture de `nft list ruleset` (ou `nft list table inet levoile`)
**And** flush + remplacement atomique s'effectue via un seul `nft -f -` (incluant `flush table inet levoile` avant `table inet levoile { ... }` dans le même script)
**And** aucune fenêtre sans règles (pas de `Deactivate` puis `Activate` séparés)
**And** un log WARN est émis : `"orphan nftables ruleset detected, replacing"` (NFR9b, NFR17)

### AC4 — Absence de `nft` ou `nf_tables` → refus de démarrer

**Given** le binaire `nft` est absent du `$PATH` **OR** l'exécution `nft list ruleset` retourne une erreur contenant "Could not process rule" ou "Operation not supported" (module `nf_tables` non chargeable)
**When** `firewall.Activate()` est appelé
**Then** la fonction retourne une erreur typée `ErrNftablesUnavailable` avec message : `"nftables kernel module unavailable, cannot start Le Voile"`
**And** aucune commande `nft -f` n'est tentée (échec early)
**And** le service refuse de démarrer (propagation de l'erreur jusqu'à `service.Start()`)
**And** un log ERROR est émis avec la cause détectée (binary missing vs. module not loaded)

### AC5 — Deactivate idempotent

**Given** la table `inet levoile` est active
**When** `Deactivate()` est appelé
**Then** `nft delete table inet levoile` est exécuté
**And** la fonction retourne `nil` même si la table n'existe pas (idempotent : `nft delete table` renvoie erreur "No such file" → traité comme succès)
**And** après `Deactivate()`, `IsActive()` retourne `false`

### AC6 — `IsActive()` reflète l'état réel du kernel

**Given** l'état réel des règles dans le kernel (pas de cache applicatif)
**When** `IsActive()` est appelé
**Then** il retourne `true` si `nft list table inet levoile` réussit (exit 0), `false` sinon
**And** l'erreur réseau/shellout est propagée avec un second retour `error`

### AC7 — Interface cross-platform préservée

**Given** le package `internal/firewall/` doit supporter Linux (cette story) et Windows (Story 2.7)
**When** le package est compilé avec `GOOS=linux`
**Then** seul `firewall_linux.go` est compilé (build tag `//go:build linux`)
**And** l'interface `Firewall` définie dans `firewall.go` (sans build tag) expose : `Activate(relayIP net.IP, tunName string) error`, `Deactivate() error`, `IsActive() (bool, error)`
**And** un constructeur `New() Firewall` existe avec build tags (retourne impl nftables sur Linux)
**And** `firewall_windows.go` reste un stub non-implémenté (`panic("not implemented")` ou `return ErrNotImplemented`) jusqu'à Story 2.7 — **ne pas toucher à l'impl Windows dans cette story**

## Tasks / Subtasks

- [x] **Task 1 : Créer l'interface et le squelette du package** (AC: 7)
  - [x] Créer `internal/firewall/firewall.go` (sans build tag) : interface `Firewall`, erreurs exportées (`ErrNftablesUnavailable`, `ErrNotImplemented`), fonction `New() Firewall` déclarée via build-tag-split
  - [x] Créer `internal/firewall/firewall_linux.go` avec `//go:build linux`
  - [x] Créer `internal/firewall/firewall_windows.go` avec `//go:build windows` (stub retournant `ErrNotImplemented`)
  - [x] Créer `internal/firewall/firewall_test.go` (sans build tag — tests de l'interface + compilation)

- [x] **Task 2 : Détection `nft` et `nf_tables` au démarrage** (AC: 4)
  - [x] Implémenter `detectNft() error` dans `firewall_linux.go` : `exec.LookPath("nft")` puis `nft list ruleset` probe
  - [x] Mapper les erreurs shellout stderr vers `ErrNftablesUnavailable` (binary missing, module unavailable)
  - [x] Appeler `detectNft()` en tête de `Activate()` — échec hard early
  - [x] Test unitaire : mock exec (via injection de `execCommand func(name string, args ...string) *exec.Cmd`) vérifiant binary-missing et module-unavailable

- [x] **Task 3 : Template ruleset nftables** (AC: 1, 3)
  - [x] Créer `internal/firewall/ruleset.nft.tmpl` — template Go `text/template` avec paramètres `{{.RelayIP}}`, `{{.TunName}}`
  - [x] Inclure `flush table inet levoile` puis `table inet levoile { chain input {...} chain output {...} }` dans un seul script (atomicité)
  - [x] Embed via `//go:embed ruleset.nft.tmpl` dans `firewall_linux.go`
  - [x] Fonction `renderRuleset(relayIP net.IP, tunName string) (string, error)` (rejette IP nil, tunName vide, IPv6 → hors scope NFR24 / Story 2.9)
  - [x] Test unitaire : snapshot du ruleset rendu pour un cas nominal + cas edge (IP privée, IP publique)

- [x] **Task 4 : Shellout `nft -f -` atomique** (AC: 1, 3)
  - [x] `applyRuleset(script string) error` : `cmd := exec.Command("nft", "-f", "-")` ; `cmd.Stdin = strings.NewReader(script)` ; capture stderr ; échec si exit != 0
  - [x] Chronométrer l'application (assertion `< 100ms` uniquement dans test e2e, pas en prod — log DEBUG de la durée)
  - [x] Test unitaire avec `execCommand` injecté : vérifier que le script stdin contient bien `flush table inet levoile` et les règles attendues

- [x] **Task 5 : `Activate` complet** (AC: 1, 3)
  - [x] `(f *nftFirewall) Activate(relayIP net.IP, tunName string) error` : `detectNft` → `renderRuleset` → `applyRuleset` → vérification post-apply via `IsActive()`
  - [x] Log INFO : `"firewall activated"` avec `relay_ip`, `tun_name`, `duration_ms`
  - [x] Gestion orphelin : la séquence `flush ... ; table ...` dans le template rend `Activate` idempotent sans code spécifique — ajouter un log WARN si la table était déjà présente (détectable via `IsActive()` pré-appel)
  - [x] Test unitaire : succès nominal, échec shellout, détection orphelin (log WARN)

- [x] **Task 6 : `Deactivate` idempotent + `IsActive`** (AC: 5, 6)
  - [x] `(f *nftFirewall) Deactivate() error` : `nft delete table inet levoile`, ignorer erreur "No such file or directory"
  - [x] `(f *nftFirewall) IsActive() (bool, error)` : `nft list table inet levoile` ; exit 0 → true ; stderr contient "No such file" → false, nil ; autre erreur → false, err
  - [x] Tests unitaires : Deactivate × 2 appels (second = no-op), IsActive true/false/error

- [x] **Task 7 : Test d'intégration (integration tag)** (AC: 1-6)
  - [x] `firewall_linux_test.go` avec build tag `//go:build linux && integration` : teste contre un vrai `nft` (skip si CI sans nftables ou sans root)
  - [x] Scénario : Activate → vérifier `nft list ruleset` contient `inet levoile` → Deactivate → vérifier absence
  - [x] Scénario orphelin : Activate → simuler crash (ne pas Deactivate) → Activate à nouveau → vérifier log WARN + règles correctes
  - [x] Scénario absence `nft` : mock `$PATH` sans nft → Activate retourne `ErrNftablesUnavailable`

- [x] **Task 8 : Documentation package** (AC: 7)
  - [x] `doc.go` avec description du package, de l'interface, et de l'ordre strict Activate/Deactivate (après TUN + routing, avant tunnel.Connect)
  - [x] Mention explicite : **cette story N'intègre PAS firewall dans `internal/service/`** — l'orchestration est à la charge d'une story ultérieure dans Epic 2 (ordre Connect complet quand tun + routing + firewall sont tous prêts)

## Dev Notes

### Architectural Constraints (MUST FOLLOW)

- **Go 1.26**, module `github.com/velia-the-veil/le_voile` [Source: go.mod]
- **Package location** : `internal/firewall/` — NOUVEAU package [Source: architecture.md#Project-Structure L751-759]
- **Fichiers** : `firewall.go` (interface, sans build tag), `firewall_linux.go` (`//go:build linux`), `firewall_windows.go` (`//go:build windows` — stub), `ruleset.nft.tmpl` (template embarqué via `//go:embed`), `firewall_test.go` (cross-platform), `firewall_linux_test.go` (impl + integration tag) [Source: architecture.md#File-Naming L439-440, L754-758]
- **Interface** : `Activate(relayIP net.IP, tunName string) error`, `Deactivate() error`, `IsActive() (bool, error)` [Source: architecture.md#Firewall-Lifecycle L603-609]
- **nftables exclusif** : PAS de fallback iptables. Si `nft` absent → hard-fail avec message clair [Source: architecture.md#ADR-03 L1328-1330, epics.md#Story-2.6 L556-559]
- **Shellout** `nft -f -` via stdin pipe (pas de fichier temp — évite race/cleanup) [Source: architecture.md L607]
- **Atomicité** : flush + apply dans un seul script passé à `nft -f -` [Source: architecture.md L607]
- **Table name** : `inet levoile` (famille `inet` = IPv4+IPv6 dual, nom `levoile`) [Source: epics.md L545]
- **Règles autorisées** : `oifname "levoile0"`, `ip daddr {relayIP}` sur UDP/443 (QUIC) et TCP/443 (fallback), loopback [Source: epics.md L546, architecture.md#Kill-Switch-Firewall L238]
- **Performance** : activation < 100ms (NFR15), mesurée par chronométrage applicatif [Source: epics.md L547, prd.md#NFR15]
- **Persistance kernel** : les règles doivent survivre au SIGKILL du process (nftables est kernel-level — garantie native si pas de Deactivate) [Source: epics.md L550-554, prd.md#NFR9b]
- **Crash-recovery** : détection + reset orphelins < 5s au redémarrage [Source: prd.md#NFR17]

### Ordre Connect/Disconnect (référence — NON implémenté dans cette story)

```
Connect:   elevation.Check() → tun.New() → routing.Setup() → firewall.Activate(relayIP, tunName) → tunnel.Connect()
Disconnect: tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()
```
[Source: architecture.md L613-620] — **cette story livre uniquement le package firewall**, l'orchestration dans `internal/service/` est hors scope.

### Logging

- Utiliser le logger `log/slog` (stdlib) — pattern existant du repo (vérifier `internal/service/service.go` si présent)
- Niveaux : INFO (Activate/Deactivate succès), WARN (orphelin détecté), ERROR (nft absent, shellout failed), DEBUG (durées, contenu script tronqué à 200 chars)

### Capabilities

- Le binaire service tourne avec `CAP_NET_ADMIN` + `CAP_NET_RAW` fournies par `systemd` (`AmbientCapabilities=`) [Source: architecture.md L73]
- Pour les tests locaux : `sudo go test ./internal/firewall/ -tags integration` (Task 7)
- **Ne pas** exiger root dans le code — reposer sur les capabilities du process

### Code Reuse / Anti-Reinvention

- **Existant à NE PAS utiliser** : `internal/dns/kill_switch.go` (ancien kill switch DNS-level — sera SUPPRIMÉ dans le refactor Epic 2, ne pas s'en inspirer) [Source: architecture.md#Supprimés L243]
- **Pattern shellout** : voir `internal/dns/flush_linux.go` (à créer — Story 2.5) ou, si déjà présent, `internal/elevation/elevation_unix.go` pour inspiration sur `exec.Command` + stderr capture
- **Injection `execCommand`** : pour tests, pattern `var execCommand = exec.Command` en package-level var, surchargeable en test (standard Go testing pattern)

### Project Structure Notes

- Alignement total avec `internal/firewall/` tel que spécifié dans architecture.md#Project-Structure L751-759.
- Aucune variance attendue. Le fichier `e2e_test.go` listé dans l'arbo architecturale est reporté à une story d'intégration (ordre Connect complet dans Epic 2) — **pas créé dans cette story**.
- `ruleset.nft.tmpl` : placer dans `internal/firewall/` à côté du `.go`, embedded via `//go:embed`.

### Testing Standards

- **Tests unitaires** : Go `testing` stdlib, nommage `TestKillSwitch_Activate`, `TestKillSwitch_Deactivate`, `TestKillSwitch_Orphan`, etc. [Source: architecture.md#Naming L454]
- **Tests intégration** : build tag `//go:build linux && integration` — skip si `nft` absent ou tests non-root
- **Coverage attendu** : ≥ 80% sur le package firewall (cohérent avec la barre du repo — inspecter les autres packages si besoin)
- **Pas de mocks réseau** nécessaires (pas de TCP/UDP réel dans les unit tests — juste exec + template)
- **Snapshot du ruleset** : stocker l'attendu inline dans le test (pas de fichiers `.golden` — simplifier la review)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-2.6 L535-559] — AC BDD complet
- [Source: _bmad-output/planning-artifacts/architecture.md#Firewall-Linux L70-71, L143-147] — choix nftables exclusif
- [Source: _bmad-output/planning-artifacts/architecture.md#Firewall-Lifecycle L603-609] — interface + atomicité
- [Source: _bmad-output/planning-artifacts/architecture.md#Ordre-Connect L613-621] — orchestration (réf, hors scope)
- [Source: _bmad-output/planning-artifacts/architecture.md#Project-Structure L751-759] — arbo package
- [Source: _bmad-output/planning-artifacts/architecture.md#ADR-03 L1328-1330] — nftables exclusif (pas de fallback iptables)
- [Source: _bmad-output/planning-artifacts/prd.md#NFR9b, NFR15, NFR17] — persistance crash, perf < 100ms, recovery < 5s
- [Source: _bmad-output/planning-artifacts/architecture.md#Capabilities L73] — `CAP_NET_ADMIN` + `CAP_NET_RAW` via systemd

### Latest Tech Notes

- **nftables** : syntaxe stable depuis kernel 3.13+, distros cibles (Debian 11+, Ubuntu 22+, Fedora 38+, Arch, Alpine 3.18+) toutes supportées [Source: architecture.md#ADR-03 L1330]
- **`nft -f -`** : mode standard pour appliquer un ruleset complet depuis stdin — atomicité garantie par netlink transaction interne
- **Go `text/template`** : stdlib, pas de dépendance externe pour le rendu du ruleset
- **`os/exec`** : pattern `cmd.Stdin = strings.NewReader(script)` standard, pas de writer goroutine nécessaire pour des payloads < 4KB (notre ruleset ~500 bytes)

### Anti-Patterns à PROSCRIRE

- ❌ **Ne PAS** utiliser `iptables` même en fallback (ADR-03) [Source: architecture.md L701, L1328]
- ❌ **Ne PAS** écrire le ruleset dans un fichier temp puis `nft -f /tmp/xxx` (race conditions, cleanup foireux)
- ❌ **Ne PAS** appeler `Deactivate` puis `Activate` en séquence (fenêtre de fuite) — utiliser `flush ... ; table ...` atomique
- ❌ **Ne PAS** toucher à `internal/service/` dans cette story (orchestration = story ultérieure)
- ❌ **Ne PAS** implémenter WFP/Windows dans cette story (Story 2.7)
- ❌ **Ne PAS** implémenter la détection captive portal (Story 2.8) ni IPv6 opt-out (Story 2.9) — kill switch pur IPv4-default ici, IPv6 bloqué par défaut via `inet` famille
- ❌ **Ne PAS** ajouter de dépendance externe Go (pas de `google/nftables` — rejeté par ADR) [Source: architecture.md L144]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

- Aucune — implémentation straightforward, pas de debug nécessaire.

### Completion Notes List

- Package `internal/firewall/` créé avec interface cross-platform `Firewall` (Activate/Deactivate/IsActive)
- Implémentation Linux via nftables shellout (`nft -f -` stdin pipe), atomique (flush+apply dans un seul script)
- Template `ruleset.nft.tmpl` embarqué via `//go:embed`, rendu via `text/template` avec validation (nil IP, IPv6 rejeté, tunName vide)
- Détection `nft` binary + `nf_tables` kernel module avec erreur typée `ErrNftablesUnavailable`
- Détection orphelin via `IsActive()` pré-appel avec log WARN
- `Deactivate` idempotent (ignore "No such file or directory")
- `IsActive` interroge le kernel réel (pas de cache applicatif)
- Windows stub retourne `ErrNotImplemented` (Story 2.7)
- Logger optionnel (interface `Logger` — nil = silencieux), aligné sur le pattern watchdog du repo
- Tests unitaires Linux via injection de `commandRunner`/`stdinRunner`/`lookPathFunc`
- Tests d'intégration Linux avec build tag `//go:build linux && integration`
- Pattern `commandRunner` aligné sur `internal/dns/` existant
- Aucune modification de `internal/service/` (hors scope)
- Build complet Windows OK, `GOOS=linux go vet` OK, aucune régression

### File List

- `internal/firewall/doc.go` — NEW — Documentation package
- `internal/firewall/firewall.go` — NEW — Interface Firewall, erreurs sentinelles, Logger interface
- `internal/firewall/firewall_linux.go` — NEW — Implémentation nftFirewall (Activate/Deactivate/IsActive + logging helpers)
- `internal/firewall/firewall_windows.go` — NEW — Stub Windows (ErrNotImplemented)
- `internal/firewall/firewall_test.go` — NEW — Tests cross-platform (New, sentinel errors)
- `internal/firewall/exec_linux.go` — NEW — commandRunner type + defaultRunner
- `internal/firewall/detect_linux.go` — NEW — detectNft (binary + kernel module probe)
- `internal/firewall/detect_linux_test.go` — NEW — Tests detectNft (binary missing, module unavailable, success)
- `internal/firewall/ruleset.nft.tmpl` — NEW — Template nftables (flush + table inet levoile)
- `internal/firewall/ruleset_linux.go` — NEW — renderRuleset (template embed + render + validation)
- `internal/firewall/ruleset_linux_test.go` — NEW — Tests renderRuleset (nominal, private IP, nil, IPv6, empty tun)
- `internal/firewall/apply_linux.go` — NEW — stdinRunner type + applyRuleset (nft -f - stdin pipe)
- `internal/firewall/apply_linux_test.go` — NEW — Tests applyRuleset (success, stdin content, nft error)
- `internal/firewall/activate_linux_test.go` — NEW — Tests Activate (nominal, detectNft fail, orphan, shellout fail)
- `internal/firewall/deactivate_linux_test.go` — NEW — Tests Deactivate (success, idempotent, double-call) + IsActive (true/false/error)
- `internal/firewall/firewall_integration_test.go` — NEW — Tests intégration Linux (activate/deactivate, orphan, nft absent)

## Change Log

- 2026-04-16: Story 2.6 implémentée — package `internal/firewall/` complet avec kill switch nftables Linux, interface cross-platform, tests unitaires et d'intégration
- 2026-04-16: Code review — 7 findings corrigés (2H/3M/2L) : fix template nftables first-run, timing complet Activate, validation tunName anti-injection, init stdinRun dans New(), test intégration via New(), dead code supprimé, doc.go complété
