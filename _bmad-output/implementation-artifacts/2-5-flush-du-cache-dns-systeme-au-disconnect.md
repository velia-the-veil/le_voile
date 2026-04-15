# Story 2.5: Flush du cache DNS système au disconnect

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur final,
I want que le cache DNS système soit purgé à la déconnexion,
so that aucune entrée résolue via le tunnel ne subsiste après désactivation du VPN.

## Acceptance Criteria

1. **AC1 — Flush Windows** : Given le tunnel est en cours de désactivation, When `dns.Flush(ctx)` est appelé sur Windows, Then `ipconfig /flushdns` est exécuté en sous-processus avec console cachée (`HideWindow: true`) et le retour est journalisé mais non fatal.

2. **AC2 — Flush Linux systemd-resolved** : Given `systemd-resolved.service` est actif (détection via `systemctl is-active systemd-resolved` exit 0), When `dns.Flush(ctx)` est appelé sur Linux, Then `resolvectl flush-caches` est exécuté et son retour est journalisé mais non fatal.

3. **AC3 — Flush Linux nscd** : Given `nscd` est détecté (binaire `nscd` dans `PATH` OU process `nscd` actif), When `dns.Flush(ctx)` est appelé, Then `nscd -i hosts` est exécuté.

4. **AC4 — Flush Linux dnsmasq** : Given un PID `dnsmasq` est détecté (lecture `/var/run/dnsmasq/dnsmasq.pid` ou scan `/proc/*/comm == "dnsmasq"`), When `dns.Flush(ctx)` est appelé, Then `SIGHUP` est envoyé au PID dnsmasq via `syscall.Kill(pid, SIGHUP)`.

5. **AC5 — No-op silencieux** : Given Linux sans resolver détecté (aucun des trois ci-dessus), When `dns.Flush(ctx)` est appelé, Then aucune erreur n'est retournée et un log `debug` indique "no DNS resolver detected, skipping flush".

6. **AC6 — Cumul multi-resolver Linux** : Given deux resolvers sont simultanément détectés sur Linux (ex. systemd-resolved + dnsmasq), When `dns.Flush(ctx)` est appelé, Then TOUS les resolvers détectés reçoivent leur flush (pas de retour anticipé).

7. **AC7 — Intégration orchestrateur** : `dns.Flush(ctx)` est invoqué par `service.Disconnect()` APRÈS `tun.Close()` (voir ordre d'orchestration architecture §TUN/Firewall/Routing Patterns). Un échec de flush N'EMPÊCHE PAS la complétion du disconnect — seulement journalisé `stderr`.

8. **AC8 — Timeout** : `dns.Flush(ctx)` respecte le `context.Context` passé en argument (timeout global disconnect = 5s). Chaque sous-commande est wrappée par `exec.CommandContext`.

## Tasks / Subtasks

- [ ] **Task 1 — Créer module `internal/dns/flush.go`** (AC: 1, 2, 3, 4, 5, 6, 7, 8)
  - [ ] Déclarer signature portable : `func Flush(ctx context.Context) error`
  - [ ] Dispatch via build tags vers `flush_linux.go` / `flush_windows.go`
  - [ ] Déclarer helper `type flushResult struct { name string; err error }` pour cumuler résultats (Linux)
  - [ ] Journaliser chaque tentative via `log.Printf` / `fmt.Fprintln(os.Stderr, ...)` préfixé `dns: flush:`

- [ ] **Task 2 — Implémenter `flush_windows.go`** (AC: 1, 8)
  - [ ] `//go:build windows`
  - [ ] Réutiliser le pattern `hiddenRunner(ctx, "ipconfig", "/flushdns")` déjà exporté dans [`internal/dns/cmd_windows.go`](internal/dns/cmd_windows.go) (exporter en package-local `defaultRunner`)
  - [ ] Logger la sortie combinée (ipconfig affiche "Successfully flushed the DNS Resolver Cache")
  - [ ] Retourner `nil` même si non-admin (ipconfig /flushdns fonctionne sans élévation sur Windows 10/11)

- [ ] **Task 3 — Implémenter `flush_linux.go`** (AC: 2, 3, 4, 5, 6, 8)
  - [ ] `//go:build linux`
  - [ ] Fonction `detectResolvers(ctx) []string` qui retourne la liste des resolvers actifs (`"systemd-resolved"`, `"nscd"`, `"dnsmasq"`)
    - [ ] `systemd-resolved` : `exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "systemd-resolved").Run()` exit 0
    - [ ] `nscd` : `exec.LookPath("nscd")` OU fichier `/var/run/nscd/nscd.pid` lisible
    - [ ] `dnsmasq` : PID lisible dans `/var/run/dnsmasq/dnsmasq.pid` OU `/run/dnsmasq/dnsmasq.pid`
  - [ ] Pour chaque resolver détecté, appeler sa fonction de flush (ne pas court-circuiter en cas d'erreur)
  - [ ] `flushSystemd(ctx)` : `exec.CommandContext(ctx, "resolvectl", "flush-caches").Run()`
  - [ ] `flushNscd(ctx)` : `exec.CommandContext(ctx, "nscd", "-i", "hosts").Run()`
  - [ ] `flushDnsmasq(ctx)` : lire PID depuis pidfile, `syscall.Kill(pid, syscall.SIGHUP)` ; en cas d'échec (permission, PID disparu) logger warning et retourner nil
  - [ ] Aucun resolver détecté → log debug `"no DNS resolver detected"`, retourner `nil`

- [ ] **Task 4 — Implémenter stub cross-platform `flush_other.go`** (AC: 5)
  - [ ] `//go:build !linux && !windows`
  - [ ] `func Flush(ctx context.Context) error { return nil }`

- [ ] **Task 5 — Tests unitaires `flush_test.go`** (AC: 1, 5, 6)
  - [ ] Mock du runner (`defaultRunner`) avec variable injectable pour tests
  - [ ] Table-driven tests : Windows path produit bien l'invocation `ipconfig /flushdns`
  - [ ] Linux path : stubber les fonctions de détection, vérifier cumul multi-resolver
  - [ ] Cas "aucun resolver" retourne nil sans erreur
  - [ ] Cas context annulé propage via sous-commandes (test avec `context.WithCancel` immédiatement annulé)
  - [ ] Cas dnsmasq pidfile absent → warning mais nil

- [ ] **Task 6 — Intégration dans `internal/service/service.go`** (AC: 7)
  - [ ] Dans la fonction `Disconnect()` (ou équivalent orchestrateur disconnect), APRÈS `tun.Close()`, invoquer :
    ```go
    flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := dns.Flush(flushCtx); err != nil {
        fmt.Fprintf(serviceStderr, "service: dns flush: %v\n", err)
    }
    ```
  - [ ] Même pattern dans la branche emergency/defer si le service crash pendant disconnect
  - [ ] Vérifier que `dns.RestartDnscache()` (Windows, existant ligne 668/916/1094) reste appelé avant le flush (ordre : restart service Dnscache → flush cache)

- [ ] **Task 7 — Test E2E opt-in** (AC: 1, 2)
  - [ ] Ajouter test `//go:build e2e && windows` dans `internal/dns/e2e_flush_windows_test.go` qui invoque `Flush(ctx)` et vérifie exit 0 (skip si non-admin non requis)
  - [ ] Ajouter test `//go:build e2e && linux` dans `internal/dns/e2e_flush_linux_test.go` qui vérifie `Flush(ctx)` ne panic pas sur une machine sans aucun resolver

## Dev Notes

### Developer Context

Cette story ferme une **fuite de confidentialité résiduelle** : sans flush au disconnect, les entrées DNS résolues pendant la session VPN (via le résolveur interne du relais, story 3.5) restent dans le cache OS et peuvent être observées par un analyste local après coup. Le scope est strict — PAS de flush au connect, PAS de flush périodique — uniquement au disconnect propre ET au disconnect d'urgence (crash recovery defer).

### Technical Requirements

- **Go 1.23+**, standard library uniquement (pas de nouvelle dépendance)
- **Build tags** : séparation stricte `flush_linux.go` / `flush_windows.go` / `flush_other.go` — un seul fichier compilé par OS
- **Context-awareness** : toutes les commandes externes via `exec.CommandContext` (AC8)
- **Erreurs non fatales** : le flush est "best-effort", un échec N'EMPÊCHE PAS le disconnect (AC7)
- **Windows console hiding** : conforme au pattern projet `HideWindow: true` déjà appliqué dans [`cmd_windows.go`](internal/dns/cmd_windows.go) (cf. commit `a1adf3f` "hide console windows for netsh/net commands")
- **Timeouts** : contexte de 5s global au disconnect, suffisant pour `ipconfig /flushdns` (~500ms) ou `resolvectl flush-caches` (~50ms)

### Architecture Compliance

Source : [_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md)

- **Structure cible** : `internal/dns/flush.go`, `flush_linux.go`, `flush_windows.go`, `flush_test.go` (§Project Structure lines 814–822)
- **Ordre d'orchestration Disconnect** (§TUN/Firewall/Routing Patterns, ligne 618–620) :
  ```
  tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()
  ```
  Ajouter `dns.Flush()` APRÈS `tun.Close()` (dernière étape).
- **Naming patterns** : fonction exportée PascalCase `Flush`, fonctions internes camelCase `flushSystemd`, `detectResolvers` (§Naming Patterns ligne 431)
- **Error handling** : pattern `fmt.Errorf("dns: flush: %w", err)` pour wrap, log `stderr` (§Error Handling Patterns ligne 576)
- **Concurrency** : pas de goroutine — Flush est synchrone, appelé depuis la goroutine disconnect de service.go (§Concurrency Patterns ligne 651)

### Library & Framework Requirements

- **Standard library only** : `context`, `os/exec`, `syscall` (pour `Kill(pid, SIGHUP)`), `strconv`, `io/ioutil`→`os.ReadFile`
- **Pas de `github.com/shirou/gopsutil`** — la détection process/PID doit rester dependency-free (lire `/proc` ou pidfiles directement)
- **Pas de nouvelle dépendance** dans `go.mod`

### File Structure Requirements

Fichiers à créer :
- [internal/dns/flush.go](internal/dns/flush.go) — API publique + logique commune
- [internal/dns/flush_linux.go](internal/dns/flush_linux.go) — implémentation Linux
- [internal/dns/flush_windows.go](internal/dns/flush_windows.go) — implémentation Windows
- [internal/dns/flush_other.go](internal/dns/flush_other.go) — stub cross-platform
- [internal/dns/flush_test.go](internal/dns/flush_test.go) — tests unitaires (portable, compile sur tous OS)

Fichiers à modifier :
- [internal/service/service.go](internal/service/service.go) — ajout appel `dns.Flush(ctx)` dans `Disconnect()` (après `tun.Close()`) et dans le defer emergency restore (après ligne 666 `dns.RestartDnscache()`)

### Testing Requirements

- **Unit tests** : couverture ≥ 80 % sur `flush.go` et la logique de détection Linux via mocks
- **Table-driven tests** conformes conventions Go
- **E2E tests** : opt-in via build tag `e2e`, skip par défaut
- **Pas de test qui modifie le cache DNS global de la machine de CI** — les tests unitaires stubbent le runner
- Exécution CI : `go test ./internal/dns/... -race` doit rester vert sur Windows et Linux

### Previous Story Intelligence

**⚠️ Note importante** : La sprint-status indique que les anciens epics (12 epics) sont **obsolètes** depuis la restructuration 2026-04-15. Les fichiers `2-1-client-tunnel-quic-https-avec-connexion-au-relais.md`, `2-2-gestion-dns-systeme-et-redirection-doh.md`, `2-3-kill-switch-dns-watchdog-et-reconnexion-automatique.md` dans `_bmad-output/implementation-artifacts/` appartiennent à l'ancien Epic 2 (tunnel QUIC/DoH). **Ils NE sont PAS les stories précédentes du nouvel Epic 2 (Capture L3 / Kill Switch Firewall).**

Les nouvelles stories 2-1 à 2-4 du nouvel Epic 2 n'ont pas encore été créées comme fichiers de contexte — elles restent à créer. Cependant, le code existant couvre déjà une partie de l'ancien domaine DNS (voir ci-dessous), qu'il convient de **conserver ou réutiliser** plutôt que de réécrire.

### Git Intelligence Summary

Commits récents pertinents :
- `a1adf3f` — "hide console windows for netsh/net commands, reduce shutdown delay" → **pattern à réutiliser** : `HideWindow: true` via `syscall.SysProcAttr`
- `8c9938d` — "fast registry polling" → non lié, ignorer
- `66469e7` — "minimize-to-tray, no disconnect" → le flush ne doit PAS se déclencher sur minimize, uniquement sur disconnect réel
- `c1d7c3a` — "ES/GB countries" → non lié

Code existant à réutiliser / cohabiter :
- [`internal/dns/cmd_windows.go`](internal/dns/cmd_windows.go) : helper `hiddenRunner(ctx, name, args...) ([]byte, error)` et `hiddenCommand(name, args...) *exec.Cmd` — **exporter ou réutiliser pour `ipconfig /flushdns`**
- [`internal/dns/dnscache_windows.go`](internal/dns/dnscache_windows.go) : pattern de gestion services Windows avec mutex — bon template pour cumul d'erreurs
- [`internal/service/service.go:668,916,1094`](internal/service/service.go) : points d'injection existants `dns.RestartDnscache()` — le flush doit être ajouté **à côté**, pas à la place

### Latest Technical Information

- **`ipconfig /flushdns`** (Windows 10 22H2 / 11 24H2) : unchanged, fonctionne sans élévation, output anglais-localisé — ne pas parser le texte, se fier uniquement à l'exit code.
- **`resolvectl flush-caches`** (systemd ≥ 239) : standard sur Ubuntu 22.04+, Fedora 38+, Arch — accepté comme canonique.
- **`nscd -i hosts`** : deprecated dans glibc 2.39 (mars 2024), mais encore présent sur RHEL 8/9. Tolérer exit 1 silencieusement.
- **dnsmasq** (≥ 2.85) : SIGHUP recharge les fichiers hosts ET flushe le cache. Confirmé via manpage 2026.

### Project Structure Notes

- **Alignement** : la structure cible `internal/dns/flush*.go` est exactement celle prévue par l'architecture (§Project Structure ligne 814–822).
- **Conflit potentiel** : le fichier `internal/dns/cmd_windows.go` définit actuellement `hiddenRunner` en package-private. Pour le réutiliser depuis `flush_windows.go` (même package `dns`), aucune modification n'est requise — l'accès package-local suffit. **Ne PAS exporter inutilement**.
- **Pas de conflit** avec la simplification DNS prévue par l'architecture (§938 "Fichiers `internal/dns/proxy.go`, `kill_switch.go`, `dnscache_*.go`, `reuseaddr_*.go` supprimés") : ces suppressions relèvent d'une autre story (Epic 1 ou 3 refactor) et ne bloquent PAS `flush.go`. Si au moment de l'implémentation ces fichiers sont encore présents, **laisser l'intégration existante intacte**.

### References

- Epic 2 / Story 2.5 : [_bmad-output/planning-artifacts/epics.md:519-533](_bmad-output/planning-artifacts/epics.md#L519-L533)
- FR7b (flush DNS au disconnect) : [_bmad-output/planning-artifacts/epics.md:34](_bmad-output/planning-artifacts/epics.md#L34)
- Architecture — structure `internal/dns/flush*` : [_bmad-output/planning-artifacts/architecture.md:814-822](_bmad-output/planning-artifacts/architecture.md#L814-L822)
- Architecture — ordre orchestration Disconnect : [_bmad-output/planning-artifacts/architecture.md:613-621](_bmad-output/planning-artifacts/architecture.md#L613-L621)
- Pattern `HideWindow` : [internal/dns/cmd_windows.go:14-25](internal/dns/cmd_windows.go#L14-L25)
- Points d'injection service : [internal/service/service.go:668](internal/service/service.go#L668)

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

### Completion Notes List

- Ultimate context engine analysis completed — comprehensive developer guide created
- Architecture compliance verified : placement `internal/dns/flush*.go`, ordre orchestration post-`tun.Close()`
- Previous-story caveat flagged : les anciens fichiers `2-*.md` appartiennent à l'Epic 2 obsolète (pre-2026-04-15) — ne PAS les utiliser comme source

### File List
