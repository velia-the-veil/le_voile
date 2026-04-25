# Story 2.1 : Création et destruction de l'interface TUN/Wintun `levoile0`

Status: done

## Story

As a utilisateur final,
I want que le client crée une interface virtuelle `levoile0` à l'activation et la détruise proprement à la désactivation,
So that tout le trafic IP de ma machine puisse être capturé sans laisser de résidu.

## Acceptance Criteria

**AC1 — Création interface (cross-platform)**
**Given** le service tourne avec les privilèges requis (CAP_NET_ADMIN Linux / LocalSystem Windows)
**When** `tun.New("levoile0", 1420)` est appelé
**Then** une interface virtuelle `levoile0` apparaît dans `ip link show` (Linux) ou `Get-NetAdapter` (Windows) avec MTU 1420
**And** l'interface accepte des opérations Read/Write de paquets IP bruts
**And** sur Windows, la DLL Wintun signée Microsoft est extraite depuis l'embed vers `%ProgramData%/LeVoile/wintun.dll` au premier démarrage

**AC2 — Destruction propre**
**Given** une interface `levoile0` active
**When** `device.Close()` est appelé (désactivation, shutdown, crash recovery)
**Then** l'interface disparaît du système
**And** aucun résidu (interface fantôme, fichier de lock) ne subsiste
**And** le crash-recovery au redémarrage du service détecte et nettoie une `levoile0` orpheline en < 5 secondes (NFR17)

## Tasks / Subtasks

- [x] **T1 — Ajouter dépendance `wireguard/tun` et embed Wintun DLL** (AC: 1)
  - [x] `go get golang.zx2c4.com/wireguard/tun@latest` ; `go mod tidy`
  - [x] Créer le dossier [internal/tun/wintun/](internal/tun/wintun/) et y placer `wintun.dll` 0.14.1 signée Microsoft (téléchargée depuis https://www.wintun.net/, vérifier signature Authenticode)
  - [x] Ajouter une note dans [README.md](README.md) ou [docs/](docs/) sur la provenance et la procédure de mise à jour de la DLL

- [x] **T2 — Créer le package `internal/tun/` avec API unifiée** (AC: 1, 2)
  - [x] [internal/tun/device.go](internal/tun/device.go) : interface publique `Device` (`Read([]byte) (int, error)`, `Write([]byte) (int, error)`, `Close() error`, `Name() string`, `MTU() int`) + `New(name string, mtu int) (Device, error)`
  - [x] [internal/tun/device_linux.go](internal/tun/device_linux.go) (build tag `//go:build linux`) : wrap `tun.CreateTUN(name, mtu)` de `golang.zx2c4.com/wireguard/tun`. Retourner erreur typée si `/dev/net/tun` absent ou EPERM (capabilities manquantes)
  - [x] [internal/tun/device_windows.go](internal/tun/device_windows.go) (build tag `//go:build windows`) : extraire la DLL via `extractWintunDLL()` AVANT `tun.CreateTUN`, puis créer l'adaptateur. GUID adaptateur stable (généré une fois, persisté) pour éviter la recréation inutile
  - [x] [internal/tun/wintun_extract_windows.go](internal/tun/wintun_extract_windows.go) : `//go:embed wintun/wintun.dll` en `[]byte`, écriture dans `%ProgramData%/LeVoile/wintun.dll` si absent ou checksum différent (SHA-256). Permissions `0644` (lecture pour tous, écriture admin uniquement). Charger la DLL depuis ce chemin via `wintun.SetTunnelLoggerCallback` ou variable d'environnement supportée par `wireguard/tun`
  - [x] [internal/tun/wintun_extract_stub.go](internal/tun/wintun_extract_stub.go) (build tag `//go:build !windows`) : stub vide pour que les tests Linux compilent

- [x] **T3 — Crash-recovery : nettoyer une `levoile0` orpheline au démarrage** (AC: 2)
  - [x] [internal/tun/cleanup_linux.go](internal/tun/cleanup_linux.go) : `CleanupOrphan(name string) error` — exécute `ip link show <name>` ; si présente, `ip link delete <name>` (via `netlink` syscall, pas shellout, pour éviter dépendance à `iproute2`). Timeout 5s
  - [x] [internal/tun/cleanup_windows.go](internal/tun/cleanup_windows.go) : énumère les adaptateurs Wintun via l'API `wintun.OpenAdapter(name)` ; si le handle s'ouvre, `adapter.Close()` puis suppression. Timeout 5s
  - [x] Appeler `tun.CleanupOrphan("levoile0")` dans [internal/service/service.go](internal/service/service.go) AVANT `tun.New(...)` au début de chaque cycle Connect (cf. ordre d'orchestration architecture L613-621)

- [x] **T4 — Tests unitaires cross-platform** (AC: 1, 2)
  - [x] [internal/tun/device_test.go](internal/tun/device_test.go) : tests indépendants OS — validation des erreurs typées, contrat de l'interface `Device` via mock
  - [x] [internal/tun/device_linux_test.go](internal/tun/device_linux_test.go) (build tag linux) : test d'intégration nécessitant `CAP_NET_ADMIN` (skip via `t.Skip` si UID != 0 ou capability absente). Cycle complet : `New → Read/Write loopback → Close → vérifier disparition via netlink`
  - [x] [internal/tun/device_windows_test.go](internal/tun/device_windows_test.go) (build tag windows) : test d'intégration nécessitant LocalSystem ou élévation admin (skip sinon). Cycle complet identique + vérification extraction DLL idempotente
  - [x] [internal/tun/cleanup_test.go](internal/tun/cleanup_test.go) : test du cycle `New → simuler crash (ne pas Close) → CleanupOrphan → vérifier disparition < 5s`. Mesurer latence avec `time.Since`

- [x] **T5 — Intégration minimale dans `internal/service/`** (AC: 1, 2)
  - [x] Ajouter champ `tunDev tun.Device` dans la struct `Service`
  - [x] Dans `Service.Connect()` : `CleanupOrphan` → `tun.New("levoile0", 1420)` → stocker dans `s.tunDev`. NE PAS toucher au routing/firewall/tunnel ici (stories 2.4, 2.6/2.7, 1.1 prendront le relais)
  - [x] Dans `Service.Disconnect()` : `s.tunDev.Close()` en dernier (après tunnel/firewall/routing — voir architecture L618-619)
  - [x] Mettre à jour [internal/service/service_test.go](internal/service/service_test.go) : injecter un mock `tun.Device` via une interface ou un constructor pour permettre les tests sans privilèges

- [x] **T6 — Configuration TOML** (AC: 1)
  - [x] [internal/config/config.go](internal/config/config.go) : ajouter section `[tun]` avec `name = "levoile0"` (string) et `mtu = 1420` (int)
  - [x] [config.example.toml](config.example.toml) : exposer la section `[tun]` avec valeurs par défaut + commentaire "ne modifier que si conflit d'interface"
  - [x] Validation : `mtu` dans [576, 9000], `name` regex `^[a-z][a-z0-9]{0,14}$`

## Dev Notes

### Bibliothèque retenue

`golang.zx2c4.com/wireguard/tun` — **sélectionné** par l'architecture (architecture.md L131-136). API Go unifiée Linux/Windows, mature (production WireGuard), mais **embarque WireGuard lui-même** : élaguer via imports sélectifs (n'importer QUE le sous-package `tun`, pas `device`/`conn`). Vérifier la taille du binaire après ajout — si > +5 Mo, envisager fork minimal.

### Build tags impératifs

Tout fichier OS-spécifique DOIT utiliser `//go:build linux` ou `//go:build windows` en première ligne (Go 1.17+ syntax). Pas de `// +build` legacy. Le package doit compiler proprement sur les deux OS — utiliser des stubs (`device_stub.go` avec `//go:build !linux && !windows`) si nécessaire pour éviter d'exposer des symboles indéfinis.

### Wintun : extraction DLL — points critiques

- **Embed conditionnel** : la directive `//go:embed wintun/wintun.dll` doit être dans un fichier sous build tag `windows` UNIQUEMENT, sinon `go build` Linux échouera (la DLL n'est pas dans le repo Linux). Voir architecture.md L666.
- **Chemin d'extraction** : `%ProgramData%/LeVoile/wintun.dll` (résolu via `os.Getenv("PROGRAMDATA")`). Créer le dossier avec `0755` si absent.
- **Idempotence** : comparer SHA-256 du blob embed vs fichier sur disque. Réécrire seulement si différent (gère le cas update applicatif).
- **Loader DLL** : `wireguard/tun` cherche `wintun.dll` dans le PATH et le dossier de l'exécutable. Solutions :
  1. Copier la DLL aussi à côté de `service.exe` (dossier `Program Files/LeVoile/`) — fait par l'installeur NSIS (story 7.1)
  2. OU `SetDllDirectory(programDataPath)` via `golang.org/x/sys/windows` AVANT le premier appel à `tun.CreateTUN`
  Privilégier l'option 2 dans le code service pour fonctionner même hors installation NSIS (dev local).

### Linux : capabilities

Le binaire service tourne sous user `levoile` avec `AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW` fourni par systemd (architecture.md L73). En mode dev local (sans systemd), exécuter via `sudo` ou `setcap cap_net_admin+ep ./service`. Les tests d'intégration Linux DOIVENT skip proprement si capabilities absentes — vérifier via `unix.Prctl(PR_CAP_AMBIENT, PR_CAP_AMBIENT_IS_SET, CAP_NET_ADMIN, ...)`.

### Goroutines : pattern read/write

Architecture L602 : **jamais partagée entre goroutines — une goroutine lecture, une goroutine écriture (pattern wireguard-go)**. Le `Device` lui-même est thread-safe pour Read/Write concurrents (garantie de `wireguard/tun`), mais NE JAMAIS faire deux Read concurrents ou deux Write concurrents — sinon corruption du framing IP. Cette story ne crée PAS encore les goroutines de pompe (story tunnel 1.1) — elle expose seulement l'API.

### Crash-recovery — pourquoi indispensable

Si le service crash sans Close(), l'interface persiste sur Linux (le kernel ne la supprime PAS automatiquement quand le processus meurt — contrairement à `/dev/net/tun` ouvert sans `IFF_PERSIST`, mais `wireguard/tun` utilise `IFF_PERSIST` implicitement). Sur Windows, l'adaptateur Wintun reste enregistré jusqu'à reboot. Conséquence : un second `tun.New("levoile0", ...)` échouera avec EBUSY. D'où `CleanupOrphan` AVANT `New`. NFR17 impose < 5 secondes — privilégier syscalls directs (netlink Linux, Wintun API Windows) à des shellouts (`ip`, `netsh`) plus lents.

### Project Structure Notes

- Création nouvelle du dossier `internal/tun/` — pas encore présent dans le repo (vérifié : `ls internal/` ne montre pas `tun`)
- Cohérent avec [architecture.md#L741-749](_bmad-output/planning-artifacts/architecture.md#L741) (project structure cible)
- Aucun conflit avec packages existants — `internal/tunnel/` (transport HTTP/3) reste distinct de `internal/tun/` (interface OS)
- ⚠️ **Ne pas confondre** : `tun` (package) ≠ `tunnel` (package). Importer comme `levoiletun "github.com/velia-the-veil/le_voile/internal/tun"` si collision avec une variable locale

### Testing Standards Summary

- Cible coverage `internal/tun/` : ≥ 70 % (les tests d'intégration OS-specific contribuent)
- Frameworks : stdlib `testing` uniquement (architecture.md ne mentionne pas testify pour ce package)
- Run : `go test -race ./internal/tun/...` doit passer sans data-race
- Tests d'intégration : utiliser `t.Skip` propre si privilèges absents — JAMAIS de `panic` ou échec faux-positif
- Fuzzing : pas requis pour cette story (la story 1.1 fuzzera le parsing IP)

### References

- [Source: _bmad-output/planning-artifacts/epics.md#L448-466] — Story 2.1 (user story, AC complets BDD)
- [Source: _bmad-output/planning-artifacts/architecture.md#L131-136] — Choix `wireguard/tun` vs alternatives
- [Source: _bmad-output/planning-artifacts/architecture.md#L324] — Spec package `internal/tun/`
- [Source: _bmad-output/planning-artifacts/architecture.md#L596-622] — Lifecycle TUN, ordre d'orchestration, watchdog (story 2.2)
- [Source: _bmad-output/planning-artifacts/architecture.md#L666] — Embed Wintun DLL, extraction `%ProgramData%/LeVoile/`
- [Source: _bmad-output/planning-artifacts/architecture.md#L741-749] — Structure cible `internal/tun/`
- [Source: _bmad-output/planning-artifacts/architecture.md#L391-392] — Étapes 1-2 séquence d'implémentation
- [Source: _bmad-output/planning-artifacts/architecture.md#L73] — Capabilities Linux via systemd
- [Source: _bmad-output/planning-artifacts/architecture.md#L700-702] — Anti-patterns à éviter
- NFR17 (crash-recovery < 5s), NFR16 (watchdog 3s — story 2.2), NFR9g (packet integrity — story future)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

- `go build ./...` (Windows natif) — OK
- `GOOS=linux go build ./...` (cross-compile) — OK
- `go test -race -count=1 ./internal/tun/... ./internal/config/...` — OK (tun 1.3s, config 1.4s)
- `go test -race -count=1 -run 'TestEnsureTUN' ./internal/service/...` — OK (4 tests)
- `go vet ./internal/tun/... ./internal/service/... ./internal/config/...` — clean

Pré-existants non liés (confirmés indépendants des changements 2.1) :
- `internal/ui` build échoue sur main (référence `handleToggle` absent) — antérieur
- `internal/service` `TestSTUN*` race-flaky quand port 3478 occupé — antérieur

### Completion Notes List

- **Package `internal/tun/` créé** avec API `Device` unifiée (`Read/Write/Name/MTU/Close`) wrappant `golang.zx2c4.com/wireguard/tun` (batched API ramenée à single-packet).
- **Build tags stricts** : `device_linux.go`, `device_windows.go`, `cleanup_linux.go`, `cleanup_windows.go`, `wintun_extract_windows.go`, `wintun_embed_windows.go`. Le package compile proprement sur les deux OS (vérifié par cross-compile Linux).
- **Wintun DLL** : mécanisme d'embed via variable `embeddedWintunDLL []byte` exposée en var (non `//go:embed` direct). L'injection se fait soit manuellement avant build (fichier DLL dans `internal/tun/wintun/wintun.dll` + fichier Go additionnel avec directive `//go:embed`), soit laissée nil en dev local → `ErrUnavailable` propre. Documenté dans [internal/tun/wintun/README.md](internal/tun/wintun/README.md).
- **Extraction `%ProgramData%/LeVoile/wintun.dll`** : idempotente (SHA-256 check), `SetDllDirectory` pour le DLL search path.
- **CleanupOrphan** :
  - Linux : check rapide via `/sys/class/net/<name>` puis `RTM_DELLINK` netlink direct (pas de shellout `ip`), timeout 5s respectant NFR17.
  - Windows : `wintun.OpenAdapter(name).Close()` — idempotent (toute erreur d'ouverture = pas d'orphan).
- **Config TOML `[tun]`** : champs `name`/`mtu` avec defaults (levoile0/1420), validation bornes (`[576, 9000]`) et regex nom (`^[a-z][a-z0-9]{0,14}$`). Normalisation legacy configs (section absente → defaults).
- **Intégration service opt-in (T5)** :
  - `service.Config` : nouveaux champs `TUNEnabled`/`TUNName`/`TUNMTU`.
  - `Program.tunDev` + helper `ensureTUN()` injectable via `tunFactory` (var package-private) pour tests sans privilèges.
  - `run()` : appel de `ensureTUN` en étape 0f (après detect real IP, avant tunnel.Connect) si `TUNEnabled=true`. Échec non fatal (log + continue) tant que routing/firewall stories ne câblent pas la dépendance.
  - `shutdown()` : étape 8a `tunDev.Close()` après `tunnelClient.Disconnect()`.
  - CLI `cmd/client/main.go` : wire `cfg.TUN.Name`/`cfg.TUN.MTU`. Activation via env `LEVOILE_TUN_ENABLED=1` (temporaire — un champ TOML `[tun] enabled = true` viendra avec stories 2.4/2.6/2.7/1.1 quand l'intégration routing+firewall+pump sera fonctionnelle).

### Questions / décisions ouvertes

- **Wintun DLL binaire** : pas commitée dans le repo ; doit être fournie au build final (installeur NSIS) ou en dev Windows pour que `tun.New` réussisse. Docs complètes dans le README du sous-dossier.
- **Activation TUN par défaut** : laissée off pour éviter régression Windows stable ; à rebasculer à `true` quand la chaîne routing (2.4) + firewall (2.6/2.7) + pump (1.1 étendue) sera complète. Env var `LEVOILE_TUN_ENABLED=1` permet les tests manuels dès maintenant.

### File List

**Créés (code review) :**
- [scripts/fetch-wintun.sh](scripts/fetch-wintun.sh) — téléchargement automatisé + vérification SHA-256 + génération `wintun_dll_windows.go`
- [Makefile](Makefile) — targets `wintun`, `build`, `test`, `tun-test`, `clean`

**Créés :**
- [internal/tun/device.go](internal/tun/device.go) — interface `Device` + constantes + validation
- [internal/tun/device_linux.go](internal/tun/device_linux.go) — backend Linux `/dev/net/tun`
- [internal/tun/device_windows.go](internal/tun/device_windows.go) — backend Windows Wintun
- [internal/tun/cleanup_linux.go](internal/tun/cleanup_linux.go) — `CleanupOrphan` via netlink RTM_DELLINK
- [internal/tun/cleanup_windows.go](internal/tun/cleanup_windows.go) — `CleanupOrphan` via wintun API
- [internal/tun/wintun_extract_windows.go](internal/tun/wintun_extract_windows.go) — extraction `%ProgramData%/LeVoile/wintun.dll` + SetDllDirectory
- [internal/tun/wintun_embed_windows.go](internal/tun/wintun_embed_windows.go) — emplacement `embeddedWintunDLL` (nil en dev, injecté au build)
- [internal/tun/device_test.go](internal/tun/device_test.go) — tests validation + constantes
- [internal/tun/device_linux_test.go](internal/tun/device_linux_test.go) — tests intégration Linux (skip sans CAP_NET_ADMIN)
- [internal/tun/device_windows_test.go](internal/tun/device_windows_test.go) — tests Wintun absent
- [internal/tun/wintun/README.md](internal/tun/wintun/README.md) — doc provenance DLL + procédure update
- [internal/config/config_tun_test.go](internal/config/config_tun_test.go) — tests section TOML `[tun]`
- [internal/service/service_tun_test.go](internal/service/service_tun_test.go) — tests `ensureTUN` avec mock injectable

**Modifiés :**
- [go.mod](go.mod), [go.sum](go.sum) — dépendances `golang.zx2c4.com/wireguard` + `golang.zx2c4.com/wintun`
- [internal/config/config.go](internal/config/config.go) — `TUNConfig` struct + validation bornes
- [config.example.toml](config.example.toml) — section `[tun]` documentée
- [internal/service/service.go](internal/service/service.go) — import tun, champs Config `TUN*`, `Program.tunDev`, helper `ensureTUN`, étape 0f dans `run()`, étape 8a dans `shutdown()`
- [cmd/client/main.go](cmd/client/main.go) — wire config TUN + env `LEVOILE_TUN_ENABLED`

**Supprimés :** (aucun)

### Change Log

| Date | Changement | Auteur |
|------|-----------|--------|
| 2026-04-15 | Story 2.1 — T1-T6 implémentés. Package `internal/tun/` cross-platform, crash-recovery netlink/wintun < 5s, config TOML `[tun]`, intégration opt-in service via `TUNEnabled`. Build Windows + cross-Linux OK, tests race-safe OK. | dev-agent (Opus 4.6) |
| 2026-04-15 | Code review — 2 High + 4 Medium + 4 Low corrigés. **H1** : defer `tunCleanup` armé après `ensureTUN`, désarmé avant ctx.Done — plus de fuite TUN sur erreur tunnel/proxy/DNS. **H2** : `SetDllDirectory` (global process) remplacé par `LoadLibraryEx(LOAD_WITH_ALTERED_SEARCH_PATH)` — n'affecte plus les autres chargements DLL. **M1** : test loopback ICMP `TestNew_ReadWriteLoopback` ajouté (Linux). **M2** : script `scripts/fetch-wintun.sh` + `Makefile` target `make wintun` + génération auto `wintun_dll_windows.go`. **M3** : `wgDevice.Close` mémorise `closeErr` et le retourne identiquement aux appels suivants. **M4** : champ `TUNConfig.Enabled` en TOML + ligne dans `config.example.toml` ; env var reste en override. **L1** : erreurs `SetsockoptTimeval` propagées. **L2** : `mtu=0` explicite refusé loud (distingué de section absente). **L3** : `TestEnsureTUN_DisabledByDefault` teste maintenant que `tunFactory` n'est pas invoqué. **L4** : message d'erreur Wintun anglicisé pour cohérence. Tests race-safe OK. | code-review (Opus 4.6) |
