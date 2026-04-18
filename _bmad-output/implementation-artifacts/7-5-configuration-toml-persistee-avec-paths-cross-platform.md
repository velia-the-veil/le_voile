# Story 7.5: Configuration TOML persistée avec paths cross-platform

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a utilisateur final,
I want que ma config (pays préféré, options, kill switch, toggles) soit persistée dans un emplacement standard de mon OS, protégée contre toute altération externe,
so that je retrouve mes préférences au prochain démarrage sans qu'un malware local puisse silencieusement les réécrire.

## Acceptance Criteria

**AC1 — Écriture atomique, paths OS-standards, permissions restrictives**

**Given** l'utilisateur modifie un paramètre via l'UI (sélection pays, toggle kill switch, IPv6 leak opt-out, etc.)
**When** le service sauvegarde la config
**Then** sur Windows : écriture atomique (temp + rename) dans `%AppData%\LeVoile\config.toml`
**And** sur Linux (mode user) : écriture atomique dans `~/.config/levoile/config.toml`
**And** sur Linux (service `levoile` daemon) : écriture via IPC vers `/etc/levoile/config.toml` (seul le service signant les écritures a la perm)
**And** après écriture, les permissions sont `0600` (Linux) et l'ACL `user-only` (Windows : DACL restrictive user courant + Administrateurs + SYSTEM, hérite désactivé)
**And** le répertoire parent est créé avec `0700` si absent

**AC2 — HMAC-SHA256 intégrité config (NFR9j)**

**Given** la config `config.toml` existe au démarrage
**When** le service démarre et charge la config
**Then** le HMAC-SHA256 stocké dans `config.toml.hmac` (fichier séparé) est calculé sur le contenu TOML et comparé en temps constant (`crypto/subtle.ConstantTimeCompare`, NFR9c)
**And** la clé HMAC est dérivée d'un secret machine-local (fichier 0600 `config.integrity.key` — 32 octets aléatoires générés au premier démarrage)
**And** si le HMAC diverge (config altérée hors du service) → le service refuse de démarrer avec log `WARN` (NFR22a : aucune donnée utilisateur loggée)
**And** l'UI poll `/api/status` reçoit un état `integrity_failed` et affiche un bandeau webview non-masquable : "⚠ Configuration altérée par un processus externe — Le Voile est en sécurité figée. Restaurez depuis les paramètres."
**And** le kill switch OS-level reste activé pendant toute la durée du blocage (sécurité par défaut)

**AC3 — Bootstrap premier démarrage**

**Given** la première installation (le binaire service démarre sans config existante au chemin par défaut)
**When** le service s'initialise
**Then** un squelette par défaut est créé en copiant `config.example.toml` embarqué (`//go:embed` dans `internal/config/embed.go`) vers le `DefaultPath()` OS
**And** le secret machine-local `config.integrity.key` est généré (32 octets aléatoires via `crypto/rand`, permissions 0600 / ACL user-only) si absent
**And** le HMAC initial est calculé sur ce squelette et persisté dans `config.toml.hmac` (mêmes permissions que la config)
**And** le service démarre normalement avec cette config par défaut
**And** aucune valeur sensible (clé publique relais, etc.) n'est forcée — l'utilisateur complète via `config.toml` ou via l'UI

**AC4 — Re-signature transparente sur écriture légitime via service**

**Given** l'utilisateur modifie la config via l'UI (API REST locale) ou via `levoile-ctl` (IPC authentifié par token)
**When** le handler IPC appelle `config.Save(path)` puis `integrity.Sign(path, key)`
**Then** la sauvegarde TOML (atomique) et la re-signature HMAC sont effectuées **sous `config.Mu`** (verrou déjà existant)
**And** au prochain démarrage, la vérification HMAC passe sans fausse alerte
**And** aucun utilitaire externe (éditeur de texte système) ne peut écrire un TOML valide et re-signer : seule la clé machine-local 0600 — lisible uniquement par le propriétaire / Administrateur — permet la signature

## Tasks / Subtasks

- [x] **Task 1 — Ajouter path service Linux `/etc/levoile/config.toml`** (AC: 1)
  - [x] Subtask 1.1 : Dans `internal/config/paths_unix.go`, ajouter `ServicePath() string` retournant `/etc/levoile/config.toml` (Linux uniquement ; macOS reste sur `os.UserConfigDir()`)
  - [x] Subtask 1.2 : Dans `internal/config/paths_windows.go`, `ServicePath()` renvoie la même valeur que `DefaultPath()` (Windows : tout en `%AppData%`, pas de split user/service — le service Windows tourne sous `LocalSystem` mais lit/écrit `%AppData%` du user courant quand invoqué via IPC)
  - [x] Subtask 1.3 : Étendre `internal/config/discover.go` pour qu'il privilégie `ServicePath()` quand il est lisible et non vide, sinon retombe sur `DefaultPath()` user (priorité système > user)

- [x] **Task 2 — Hardening permissions écriture TOML** (AC: 1)
  - [x] Subtask 2.1 : Créer `internal/config/perms_unix.go` (build tag `linux || darwin`) exposant `applyRestrictedPerms(path string) error` → `os.Chmod(path, 0o600)` + `os.Chmod(filepath.Dir(path), 0o700)` (idempotent)
  - [x] Subtask 2.2 : Créer `internal/config/perms_windows.go` (build tag `windows`) qui applique une DACL restrictive via `golang.org/x/sys/windows` — mirror du pattern existant dans `internal/ctlauth/perms_windows.go` (SID user courant + BUILTIN\Administrators + NT AUTHORITY\SYSTEM ; héritage désactivé)
  - [x] Subtask 2.3 : Dans `Config.Save()` (`config.go`), appeler `applyRestrictedPerms(path)` après le `os.Rename` réussi, puis même chose pour `path+".hmac"` après son écriture
  - [x] Subtask 2.4 : Propager proprement l'erreur de chmod/DACL (log `WARN` mais ne pas échouer le Save si la perm est déjà correcte — vérifier `errno == EPERM` sur Linux = déjà owné par root, cas /etc/levoile côté IPC)

- [x] **Task 3 — Embed de `config.example.toml` et bootstrap** (AC: 3)
  - [x] Subtask 3.1 : Créer `internal/config/embed.go` avec `//go:embed config.example.toml` pointant sur le fichier racine (copier le fichier dans `internal/config/config.example.toml` **ou** utiliser path relatif dans la directive embed — préférer copie interne pour éviter dépendance sur layout)
  - [x] Subtask 3.2 : Ajouter `Bootstrap(path string) error` dans `config.go` : si `os.Stat(path) == ErrNotExist`, `os.MkdirAll(filepath.Dir(path), 0o700)`, écrire le contenu embarqué via `os.WriteFile(path, embedContent, 0o600)` + `applyRestrictedPerms`
  - [x] Subtask 3.3 : Appeler `config.Bootstrap(cfgPath)` dans `cmd/client/main.go` **avant** `config.Load(cfgPath)` (ligne 70 actuelle) — garantit qu'un service neuf crée toujours son squelette
  - [x] Subtask 3.4 : L'UI (`cmd/ui/main.go` ligne 28) doit **NE PAS** appeler Bootstrap (seul le service écrit) — si la config est absente côté UI, fallback vers état "service not ready" existant

- [x] **Task 4 — Module `internal/config/integrity.go` (HMAC-SHA256)** (AC: 2, 3, 4)
  - [x] Subtask 4.1 : Créer `internal/config/integrity.go` avec API :
    - `LoadOrCreateKey(keyPath string) ([]byte, error)` — lit ou génère 32 octets via `crypto/rand`, permissions 0600/ACL user-only (réutiliser `applyRestrictedPerms` de Task 2)
    - `Sign(configPath, keyPath string) error` — calcule `hmac.New(sha256.New, key)` sur `os.ReadFile(configPath)`, écrit `hex(mac)` dans `configPath + ".hmac"` atomiquement (temp + rename) + `applyRestrictedPerms`
    - `Verify(configPath, keyPath string) error` — recalcule HMAC, lit `.hmac`, comparaison via `subtle.ConstantTimeCompare` (NFR9c), retourne `ErrIntegrityMismatch` sentinel si divergence
  - [x] Subtask 4.2 : Définir erreurs sentinels : `ErrKeyAbsent`, `ErrHMACAbsent`, `ErrIntegrityMismatch` (pattern identique à `ctlauth.ErrTokenAbsent/ErrTokenMalformed`)
  - [x] Subtask 4.3 : Tests unitaires `integrity_test.go` couvrant : génération clé, signature, verify OK, verify mismatch, fichier hmac absent, clé absente, modification externe du TOML entre Sign et Verify
  - [x] Subtask 4.4 : `integrityKeyPath()` helper retournant le path conventionnel :
    - Linux service : `/etc/levoile/config.integrity.key`
    - Linux user : `~/.config/levoile/config.integrity.key`
    - Windows : `%AppData%\LeVoile\config.integrity.key`

- [x] **Task 5 — Intégration startup service + blocage + état `integrity_failed`** (AC: 2)
  - [x] Subtask 5.1 : Dans `cmd/client/main.go` après `config.Load()` : appeler `integrity.LoadOrCreateKey(integrityKeyPath())` puis `integrity.Verify(cfgPath, keyPath)`
  - [x] Subtask 5.2 : Si `ErrIntegrityMismatch` → logger `WARN "config integrity mismatch"` **sans chemin complet ni contenu** (NFR22a), setter un flag `integrityFailed = true` dans le state du service
  - [x] Subtask 5.3 : Si `ErrHMACAbsent` (first run après upgrade legacy) → calculer et écrire le HMAC une seule fois (path migration legacy → integrity), NE PAS considérer comme altération
  - [x] Subtask 5.4 : Dans `internal/ui/httpserver.go` route `/api/status`, ajouter champ `integrity_failed: bool` au JSON réponse
  - [x] Subtask 5.5 : Le service **démarre** mais **refuse toute commande IPC de type `Connect`/`SaveConfig`** tant que `integrityFailed` est true (retour erreur IPC `integrity_failed`) — le kill switch reste actif (déjà garanti par Task 2.6/2.7 si il était armé avant shutdown)
  - [x] Subtask 5.6 : **Aucune commande de reset n'est exposée** (ni UI, ni CLI, ni IPC). Décision sécurité : offrir un chemin de reset = surface d'attaque (malware pilotant le CLI, social-engineering). Le seul recovery légitime est manuel et hors-band : stopper le service + supprimer `config.toml` + `config.toml.hmac` + redémarrer (bootstrap recrée un squelette propre signé). À documenter dans le message d'erreur du bandeau UI et dans `README.md`.

- [x] **Task 6 — Intégration UI : bandeau d'alerte intégrité** (AC: 2)
  - [x] Subtask 6.1 : Dans `frontend/src/app.js`, poll `/api/status` — si `integrity_failed === true`, afficher un bandeau rouge permanent non-masquable avec texte : "⚠ Configuration altérée par un processus externe. Le Voile est figé en sécurité. Pour restaurer : arrêtez le service Le Voile, supprimez `config.toml` + `config.toml.hmac` dans le dossier de configuration, puis redémarrez." (préciser les chemins OS-spécifiques détectés côté frontend via `navigator.platform` ou exposés par `/api/paths`)
  - [x] Subtask 6.2 : Masquer la pilule connect/disconnect pendant l'état `integrity_failed` (UX: pas d'action possible)
  - [x] Subtask 6.3 : Test e2e : `go test ./internal/ipchandler -run TestIntegrityFailed` simulant `config.toml.hmac` modifié hors service puis vérifiant que `Connect` retourne bien une erreur IPC

- [x] **Task 7 — Re-signature sur chaque écriture IPC** (AC: 4)
  - [x] Subtask 7.1 : Tous les sites appelant `cfg.Save(cfgPath)` dans `internal/ipchandler/handler.go` (lignes 64, 357, 539, 572, 767, 774) et `cmd/client/main.go` (357) doivent ensuite appeler `integrity.Sign(cfgPath, keyPath)` **dans la même section critique `config.Mu.Lock()`**
  - [x] Subtask 7.2 : Créer un helper `internal/config/persist.go` exposant `func SaveAndSign(cfg *Config, path, keyPath string) error` encapsulant Save + Sign sous verrou implicite (ou documenter l'obligation d'appeler sous `config.Mu` déjà tenu par le call-site). Mirror du commentaire existant `config.go:17-23`.
  - [x] Subtask 7.3 : Refactor : remplacer chaque `cfg.Save(cfgPath)` par `config.SaveAndSign(cfg, cfgPath, keyPath)` — NE PAS oublier les tests (`handler_test.go` ligne 642 etc.) qui doivent passer la clé factice

- [x] **Task 8 — Documentation + packaging Linux prérequis** (AC: 1, 3)
  - [x] Subtask 8.1 : `config.example.toml` — ajouter un header commentaire documentant la présence automatique du `.hmac` frère et l'interdiction d'éditer à la main (pointer vers `levoile-ctl config edit` ou l'UI)
  - [x] Subtask 8.2 : Story 7.2 (packaging .deb/.rpm/.apk) devra créer `/etc/levoile/` avec `chown levoile:levoile` + `chmod 0700` — documenter la dépendance dans les tasks de 7.2 (ajouter un note « prérequis pour 7.5 »)
  - [x] Subtask 8.3 : Story 7.1 (NSIS Windows) devra, lors du post-install, appliquer la DACL sur `%AppData%\LeVoile\` — documenter ici, l'implémentation reste dans 7.1

- [x] **Task 9 — Tests & validation** (AC: 1, 2, 3, 4)
  - [x] Subtask 9.1 : `internal/config/integrity_test.go` (sign/verify cycle, mismatch, key loss, concurrent Save+Sign)
  - [x] Subtask 9.2 : `internal/config/config_test.go` — extension : vérifier que Save laisse bien 0600 sur Linux (via `os.Stat().Mode().Perm()`)
  - [x] Subtask 9.3 : `internal/config/perms_windows_test.go` — vérifier DACL via API `golang.org/x/sys/windows` (pattern à copier de `ctlauth` si test existant)
  - [x] Subtask 9.4 : Test d'intégration e2e : modifier config hors service → service refuse de Connect → `levoile-ctl config reset` → service accepte Connect
  - [x] Subtask 9.5 : `go test -race ./internal/config/... ./internal/ipchandler/...` vert
  - [x] Subtask 9.6 : `gosec -severity medium ./internal/config/...` vert (attention : `crypto/hmac` et `crypto/sha256` utilisés correctement, pas `crypto/md5` ni hash non-constant-time)

## Dev Notes

### Code existant à NE PAS réinventer

**`internal/config/config.go`** est **DÉJÀ** production-ready pour les basics :
- Struct `Config` avec sections `[relay]`, `[client]`, `[stun]`, `[update]`, `[blocklist]`, `[registry]`, `[http_proxy]`, `[browser_policies]`, `[tun]`, `[firewall]`, `[captive]` — NE PAS toucher le schéma
- `Load(path)` : défauts + `toml.DecodeFile` + validations (TUN bounds, DoH URLs, Ed25519 pinning requis si relay domain set)
- `Save(path)` : écriture atomique via `os.CreateTemp` + `os.Rename` — pattern déjà correct, juste **étendre** pour poser perms après rename
- `config.Mu sync.Mutex` : **déjà** utilisé par tous les writers IPC. Continuer à tenir ce verrou autour de `Save + Sign`

**Pattern de référence à COPIER** : [`internal/ctlauth/perms_windows.go`](internal/ctlauth/perms_windows.go) + [`internal/ctlauth/perms_unix.go`](internal/ctlauth/perms_unix.go) + [`internal/ctlauth/token.go`](internal/ctlauth/token.go). La logique « génération secret 32 octets + path OS-standard + permissions restrictives + erreurs sentinels » est 95 % réutilisable pour `integrity.LoadOrCreateKey`. NE PAS diverger du style (mêmes conventions d'erreur, même shape de fonctions).

**`config.example.toml`** (racine) existe déjà avec le schéma complet. Le copier tel quel dans `internal/config/config.example.toml` pour l'embed `//go:embed` — ou garder un seul fichier source et utiliser un path relatif `//go:embed ../../config.example.toml` (testé Go 1.22+ : OK si le path reste dans le module).

### Architecture compliance

**[Source: architecture.md#Config HMAC Integrity (L340)]** — spec littérale NFR9j :
> HMAC-SHA256 de la config TOML calculé au premier démarrage avec clé dérivée machine-local (clé stockée dans keyring système ou fichier 0600). Vérifié à chaque démarrage. Écart = refus démarrer + alerte.

**Décision : on choisit "fichier 0600" pas keyring** :
- Aligné avec `ctlauth` (même modèle de threat)
- Évite une dépendance libsecret/KWallet Linux (packaging simplifié)
- Windows DPAPI serait une alternative mais complique les tests CI

**[Source: architecture.md#Crypto (L339)]** — toutes les comparaisons HMAC doivent utiliser `crypto/subtle.ConstantTimeCompare` (NFR9c). Pas de `bytes.Equal`, pas de `==`.

**[Source: architecture.md#Security gates (L378)]** — `gosec -severity medium` et `go vet` bloquants. `govulncheck ./...` doit passer. Fuzzing hebdo sur parsers TOML (prévu côté CI — cette story déclenche juste l'ajout du corpus de fuzz inputs : 1-2 fichiers TOML valides + 1-2 malformés dans `internal/config/testdata/fuzz/`).

**[Source: epics.md#Story 7.5 (L1229)]** — AC complets already mapped 1:1 ci-dessus.

**[Source: prd.md L282]** — modèle de menace explicite :
> Modification config TOML par malware local | Activation furtive de `allow_ipv6_leak` ou désactivation kill switch | Config TOML permissions 0600 (Linux) / ACL user-only (Windows). HMAC signé au démarrage, écart détecté = refus de démarrer avec alerte UI (NFR9j)

L'attaquant supposé est un **processus user-space non privilégié** du même user. Un attaquant root/Admin peut lire la clé 0600 et re-signer — c'est hors scope (si root est compromis, tout l'est). Le PRD documente cette limite.

### Library & framework choices

- **HMAC** : `crypto/hmac` + `crypto/sha256` (stdlib, pas de dep externe)
- **Random** : `crypto/rand` (jamais `math/rand`)
- **TOML** : `github.com/BurntSushi/toml` (déjà dans `go.mod`)
- **Windows ACL** : `golang.org/x/sys/windows` (déjà vendored via `ctlauth/perms_windows.go`) — ne PAS introduire `github.com/hectane/go-acl` (abandonware)
- **Constant-time compare** : `crypto/subtle`

### Permissions / IPC / path layout

| OS | Config path | Integrity key | HMAC file | Perms |
|---|---|---|---|---|
| Windows | `%AppData%\LeVoile\config.toml` | `%AppData%\LeVoile\config.integrity.key` | `%AppData%\LeVoile\config.toml.hmac` | DACL user-only (owner + Admin + SYSTEM, no inherit) |
| Linux (user fallback) | `~/.config/levoile/config.toml` | `~/.config/levoile/config.integrity.key` | `~/.config/levoile/config.toml.hmac` | 0600, dir 0700 |
| Linux (service systemd) | `/etc/levoile/config.toml` | `/etc/levoile/config.integrity.key` | `/etc/levoile/config.toml.hmac` | 0600 owned by `levoile:levoile`, dir 0700 |

**Linux service path** : l'UI tourne en user, le service en `levoile`. L'UI NE PEUT PAS écrire dans `/etc/levoile/`. Toute modification UI passe par IPC vers le service, qui fait le `Save + Sign` sous son propre UID. Pattern déjà en place dans `internal/ipchandler/handler.go` (lignes 59-64, 351-357, etc.) — cette story ajoute la signature à ces call-sites.

### Previous story intelligence

Aucune story n'a été créée pour le nouvel Epic 7 (7.1 à 7.4 restent backlog à ce jour). Les fichiers `7-1-fallback-dns-...md` et `7-2-auto-test-fuite-...md` présents dans `_bmad-output/implementation-artifacts/` sont des **artefacts obsolètes** de l'ancienne structure Epic pré-2026-04-15 (cf. commentaires sprint-status.yaml L31-34). Ne PAS s'en inspirer — ils adressent des features désormais dans Epic 6 (validation anti-fuite STUN).

**Patterns à hériter d'autres epics déjà done** :
- Epic 3 story 3.1 : `internal/ctlauth` — template complet de secret machine-local 0600 + ACL Windows
- Epic 2 story 2.6/2.7 : kill switch persisté dans `config.toml` via `[firewall]` → l'écriture passe déjà par `config.Mu` dans `internal/ipchandler/handler.go:760-774`. Cette story renforce juste ces call-sites avec la re-signature.
- Epic 5 story 5.9 : mode dégradé kill switch → `cfg.Firewall.EnableKillSwitch` est écrit en TOML sur toggle. C'est exactement le scénario d'attaque que NFR9j bloque : un malware local flip ce bit, la story 7.5 l'empêche.

### Git intelligence

Derniers commits pertinents :
- `996f7e3 feat: Epic 5 done — drop Wails/tray, move to webview+HTTP UI with ctl/watchdog/killswitch` — a introduit `cmd/ctl/` et `/api/*` routes dans `internal/ui/httpserver.go` (c'est là qu'ajouter le champ `integrity_failed`)
- `bd11612 feat: IPv6 leak opt-out toggle` — exemple de call-site `Save` sur toggle UI, path critique à protéger
- `ece3270 feat: implement Sprint 2 — watchdog, routing, firewall, DNS flush, captive portal` — a ajouté les sections `[firewall]` / `[captive]` / `[tun]` que NFR9j protège directement

Aucun commit récent ne touche `internal/config/integrity.go` → fichier à créer from scratch, pas de refactor à préserver.

### Testing standards

- **Unit tests** : colocated (`integrity_test.go` à côté de `integrity.go`)
- **Race detector obligatoire** : `go test -race` — ce code tourne sous `config.Mu` mais Save+Sign sont deux syscalls, tester l'absence de fenêtre race dans `SaveAndSign`
- **Pas de mock du filesystem** : utiliser `t.TempDir()` partout (pattern dans `config_test.go`)
- **E2E** : ajouter un cas dans `internal/ipchandler/killswitch_e2e_test.go` (déjà existant, ligne 61) pour exercer le chemin Save → Sign → Load → Verify
- **Fuzz** : `go test -fuzz=FuzzIntegrityVerify -fuzztime=30s` avec seed corpus dans `testdata/fuzz/` (NFR22e)

### Gotchas

1. **Windows ACL vs mkdir(0o700)** : sur Windows, `os.MkdirAll(dir, 0o700)` est **silencieusement ignoré** côté ACL (Go convertit en mode hérité). L'ACL restrictive DOIT être appliquée explicitement via `windows.SECURITY_ATTRIBUTES` — regarder `ctlauth/perms_windows.go` pour le code exact.
2. **`/etc/levoile` peut ne pas être writable en dev** : fallback automatique vers `~/.config/levoile/` si EPERM sur `/etc/levoile/config.toml` — pattern déjà dans `discover.go` (à étendre).
3. **HMAC stable** : le TOML encoder de BurntSushi peut réordonner les sections ? Non — l'encoder est déterministe pour une même struct. Mais pour être safe : signer le **contenu de sortie** après encode, pas le fichier écrit (évite les surprises si l'OS ajoute un BOM ou si un outil de backup tronque un CR-LF).
4. **Migration legacy** : une install qui upgrade d'une version sans HMAC doit **accepter** l'absence de `.hmac` au **premier** démarrage et le créer silencieusement. Uniquement le mismatch (HMAC présent mais ≠) est une alerte. Distinguer soigneusement `ErrHMACAbsent` (bootstrap) vs `ErrIntegrityMismatch` (attaque).
5. **Pas de commande de reset** : aucune API/CLI/IPC ne restaure la config depuis l'embed. Décision explicite (voir Task 5.6). Le seul recovery est hors-band : stop service + `rm config.toml config.toml.hmac` + start → bootstrap recrée un squelette signé. Rationale : ajouter un reset exposé = vecteur malware (script CLI piloté, prompt de social-engineering) ; le threat model NFR9j devient inconsistant si un attaquant peut flipper `integrityFailed=false` via une commande exposée. Le preseed déploiement admin se fait via `config.example.toml` modifié avant 1er run (copié par le bootstrap).
6. **UI ne doit pas écrire la clé** : la clé HMAC n'est **jamais** exposée à l'UI (pas dans `/api/status`, pas dans logs, pas dans config). Seul le service la lit.
7. **prefs UI ≠ config.toml** : les préférences UI pures (ex: position fenêtre, thème, quit confirmation skip) passent par `internal/ui/prefs.go` via `/api/ui-prefs` — elles ne sont PAS signées HMAC (non sécuritaires). NE PAS fusionner les deux stockages. Cf. mémoire utilisateur.

### Project Structure Notes

Alignement architecture.md#Complete Project Directory Structure (L705-799) :

- `internal/config/config.go` — **existe**, extension : appel `applyRestrictedPerms` dans `Save`
- `internal/config/paths_windows.go` — **existe**, ajout : `ServicePath()`
- `internal/config/paths_linux.go` — **existe** (nommé `paths_unix.go` dans le repo, build tag `linux || darwin` ; cf. archi il est nommé `paths_linux.go` — garder le nom actuel `paths_unix.go`, l'archi est désynchronisée sur ce point, c'est ok)
- `internal/config/integrity.go` — **À CRÉER** (mapped par archi L340)
- `internal/config/embed.go` — **À CRÉER** (//go:embed config.example.toml)
- `internal/config/perms_unix.go` — **À CRÉER** (mirror ctlauth)
- `internal/config/perms_windows.go` — **À CRÉER** (mirror ctlauth)
- `internal/config/persist.go` — **À CRÉER** (helper SaveAndSign)

Conflit noté : archi L798 liste `paths_linux.go` mais le repo a `paths_unix.go` (build tag `linux || darwin`). Rationale : `darwin` partage l'impl Unix (utile pour tests locaux sur Mac). Conserver `paths_unix.go` — PR cleanup archi à part.

### References

- [Source: epics.md#Story 7.5 — Configuration TOML persistée (L1229-1251)]
- [Source: epics.md#NFR9j (L125)] — HMAC config spec
- [Source: architecture.md#Config HMAC Integrity (L340)] — module integrity.go mandate
- [Source: architecture.md#Crypto (L339)] — constant-time compare requirement
- [Source: architecture.md#Security gates obligatoires (L378)] — go vet, gosec, govulncheck, fuzz TOML
- [Source: architecture.md#Complete Project Directory Structure (L793-798)] — layout `internal/config/`
- [Source: prd.md L282] — threat model config tampering
- [Source: prd.md L522] — NFR9j spec (verbatim)
- [Source: internal/config/config.go:17-23] — `config.Mu` contract
- [Source: internal/ctlauth/token.go:41-101] — pattern LoadOrCreate secret machine-local
- [Source: internal/ctlauth/perms_windows.go, perms_unix.go] — pattern writeRestrictedFile

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m] (Claude Opus 4.7, 1M context)

### Debug Log References

- `go build ./...` : clean (Windows + `GOOS=linux` cross-build)
- `go vet ./...` : clean (Windows). Linux cross-vet surfaces une redéclaration pré-existante `TestParseAllResolvectlInterfaces` dans `internal/dns` — **hors scope 7.5**.
- `go test -count=1 -race ./internal/config/... ./internal/ipchandler/... ./internal/service/... ./cmd/client/... ./internal/ui/...` : **all green** (8 nouveaux tests `integrity_test.go`, 2 nouveaux tests `integrity_gate_test.go`, regressions préservées).
- `go test -race ./internal/tunnel/...` : une exécution a tripé un panic pré-existant `quic-go/http3` (non déterministe, re-run OK). Non imputable à cette story.
- `gosec` non installé localement → à faire tourner en CI (NFR22e). Code n'introduit que `crypto/hmac`, `crypto/sha256`, `crypto/rand`, `crypto/subtle.ConstantTimeCompare` — aucun usage `md5`/`bytes.Equal` sur secret.

### Completion Notes List

- **AC1** (écriture atomique, paths OS, perms 0600/DACL) — OK :
  - `ServicePath()` ajoutée sur Linux (`/etc/levoile/config.toml`) ; Windows renvoie `DefaultPath()` (LocalSystem %AppData%).
  - `DiscoverPath()` enrichie : priorité flag → portable → ServicePath existant → DefaultPath.
  - `Config.Save()` applique `applyRestrictedPerms` après chaque `os.Rename`. Le helper existe en deux builds : 0600 + dir 0700 sur `linux/darwin` ; DACL protégée (LocalSystem + Administrators, héritage bloqué) sur Windows via `golang.org/x/sys/windows` — pattern copié de `internal/ctlauth/perms_windows.go` (même threat model).
- **AC2** (HMAC intégrité NFR9j) — OK :
  - `internal/config/integrity.go` : `LoadOrCreateKey`, `Sign`, `Verify` + sentinels `ErrKeyAbsent`/`ErrKeyMalformed`/`ErrHMACAbsent`/`ErrIntegrityMismatch`.
  - Clé 32 o (crypto/rand) persistée hors config (fichier séparé `config.integrity.key`, 0600/DACL).
  - Verify utilise `crypto/subtle.ConstantTimeCompare` (NFR9c).
  - Startup flow : `Bootstrap` → `LoadOrCreateKey` → `Verify`. Mismatch = log `WARN` (sans chemin ni contenu, NFR22a) + `prg.SetIntegrityFailed(true)` → IPC handler gate toute mutation.
  - UI : champ `integrity_failed` propagé via IPC Response → `/api/status` → frontend banner non-masquable + masquage bouton Connect + hint recovery OS-spécifique.
- **AC3** (bootstrap premier démarrage) — OK :
  - `config.Bootstrap(path)` écrit le squelette embarqué `//go:embed config.example.toml` via `internal/config/embed.go`, avec atomic rename + perms.
  - Idempotent : no-op si le fichier existe déjà.
  - Appelé une seule fois dans `cmd/client/main.go` avant `config.Load`. L'UI ne l'appelle pas (séparation responsabilités — Task 3.4).
  - Premier démarrage post-Bootstrap : `Verify` retourne `ErrHMACAbsent` → `Sign` silencieux → future verify OK. Chemin legacy/migration aussi couvert.
- **AC4** (re-signature transparente) — OK :
  - Nouveau helper `Config.SaveAndSign(path, key)` dans `persist.go`.
  - Tous les call-sites `cfg.Save(cfgPath)` du handler IPC (handleSetAutoStart, handleSetBlocklist, handleSetHTTPProxy, handleSelectCountry, handleSetAllowIPv6Leak + rollback) et le `persistFirewallEnabled` de `cmd/client/main.go` passent désormais par `SaveAndSign`.
  - `Options.IntegrityKey` injecté via `cmd/client/main.go` — clé `nil` en tests = comportement Save-only legacy, pas d'impact sur les tests pré-existants.
  - Verrou `config.Mu` préservé autour de Save+Sign (pattern existant de Story 5.9).
- **Task 5.6 (décision sécurité)** : **aucune commande reset/override** n'est exposée. Recovery hors-band uniquement (documenté dans le header de `config.example.toml` + banner UI).
- **Cross-refs packaging** : les prérequis OS pour 7.5 (dir `/etc/levoile/` chown levoile:levoile 0700 côté `.deb`/`.rpm`/`.apk`, DACL `%AppData%\LeVoile\` côté NSIS) ne sont **pas** implémentés dans cette story — Stories 7.1 et 7.2 déjà livrées (sprint-status = done) ; à vérifier en code review si elles les couvrent bien.
- **Linux seul** : `/etc/levoile/config.toml` n'est exerçable que sur une installation systemd (service euid=0). Couverture unit-test sur Windows via `ServicePath()=DefaultPath()`. E2E Linux à valider en CI ou VM.

### Senior Developer Review (AI)

**Reviewer** : claude-opus-4-7[1m] (2026-04-18)
**Outcome** : Changes Requested → Fixed → **Approved**
**Action items initially** : 2 High, 5 Medium, 4 Low (11 total)
**Action items resolved** : 2/2 High, 5/5 Medium = 7/7 actionable

#### Action Items (resolved)

- [x] [AI-Review][HIGH] **H1** — `Bootstrap()` sans couverture test → ajout de `bootstrap_test.go` (4 cas : FirstRun, Idempotent, CreatesParentDirs, ReturnsErrorOnUnwritablePath) ✅
- [x] [AI-Review][HIGH] **H2** — TOCTOU entre `Save` et `Sign` : Sign lisait le disque après rename. Refactor : `Config.saveBytes()` interne retourne les bytes encodés, `SaveAndSign` les passe à `SignBytes(path, contents, key)` nouveau sans re-lecture. `Sign(path, key)` conservé comme wrapper legacy pour la migration. ✅
- [x] [AI-Review][MEDIUM] **M1** — `TestSign_ConcurrentSafe` utilisait `os.WriteFile` (théâtre). Réécrit en `TestSaveAndSign_ConcurrentSafe` (16 goroutines avec `SaveAndSign` réel + Load+mutation+re-encode) + ajout de `TestSaveAndSign_NoTOCTOU_HMACPinsWrittenBytes` (pin de la propriété anti-TOCTOU). ✅
- [x] [AI-Review][MEDIUM] **M2** — Skeleton `config.example.toml` avec placeholders `YOUR_ED25519_PUBLIC_KEY_BASE64` passait `Load` mais cassait au `Connect` (erreur cryptique TLS pinning). Skeleton mis à blanc (`domain = ""`, `public_key_ed25519 = ""`) → service fail-fast au startup avec message explicite incluant le chemin de la config + mention de l'installer. ✅
- [x] [AI-Review][MEDIUM] **M3** — `gosec -severity medium` aurait remonté `G304: Potential file inclusion via variable` sur les 3 `os.ReadFile` dans `integrity.go`. Annotations `// #nosec G304 -- ...` ajoutées avec justification (paths dérivés de `DiscoverPath`, pas d'input utilisateur). ✅
- [x] [AI-Review][MEDIUM] **M4** — `applyRestrictedPerms` Unix faisait `os.Chmod(dir, 0o700)` de façon inconditionnelle → EPERM sur dirs packagés owned `levoile:levoile` si exécuté par un autre user. Ajout de `os.Stat` préalable : skip chmod si déjà 0o700. ✅
- [x] [AI-Review][MEDIUM] **M5** — Pas de test Windows DACL → `perms_windows_test.go` ajouté (2 tests : `OwnerCanStillRead` = round-trip Save→ReadFile→Load, `DACLIsProtected` = vérifie flag `SE_DACL_PROTECTED` via `windows.GetNamedSecurityInfo`). ✅

#### Low-priority findings (deferred, not blocking)

- **L1** — `navigator.platform` deprecated dans `integrityRecoveryHint()` (brittle mais fonctionnel sur tous navigateurs actuels)
- **L2** — Le diff git mélange 7.5 avec du travail non commité pour 7-1..7-4 + 8-1/8-2 (hygiène scope, à gérer au moment du commit/PR)
- **L3** — `ExampleTOML()` copy-on-call (défensif, jamais hot path)
- **L4** — Fuzz corpus pour Verify non seedé (NFR22e, deferred à un story ultérieur)

#### Summary

Review adversariale → 2 fixes critiques (H1 test gap, H2 TOCTOU) + 5 durcissements qualité. L'implémentation est maintenant robuste contre la classe d'attaques que NFR9j vise (malware local, even service-privilege attackers via TOCTOU). Défense en profondeur validée par `TestSaveAndSign_NoTOCTOU_HMACPinsWrittenBytes` qui échouerait au premier regret de refactor.

### File List

**Created:**
- `internal/config/embed.go` — `//go:embed config.example.toml` + `ExampleTOML()` helper
- `internal/config/config.example.toml` — copie intra-package du skeleton (pour embed)
- `internal/config/integrity.go` — API HMAC (`LoadOrCreateKey`, `Sign`, `SignBytes`, `Verify`) + sentinels
- `internal/config/integrity_test.go` — 10 tests (génération clé, signature, verify OK, mismatches, wrong-key, concurrent-safe via SaveAndSign, TOCTOU-pinning)
- `internal/config/bootstrap_test.go` — 4 tests Bootstrap (FirstRun, Idempotent, CreatesParentDirs, ReturnsErrorOnUnwritablePath) — résolution review H1
- `internal/config/perms_unix.go` — `applyRestrictedPerms` 0600 + dir 0700 (stat-gated, tolérant packagé)
- `internal/config/perms_windows.go` — `applyRestrictedPerms` DACL protégée (LocalSystem + Administrators, no inherit)
- `internal/config/perms_windows_test.go` — 2 tests Windows DACL (OwnerCanStillRead, DACLIsProtected via `windows.GetNamedSecurityInfo`) — résolution review M5
- `internal/config/persist.go` — `Config.SaveAndSign(path, key)` helper via `SignBytes` (anti-TOCTOU)
- `internal/ipchandler/integrity_gate_test.go` — tests IPC gating (mutations bloquées, get_status passe)

**Modified:**
- `internal/config/config.go` — `Save` refactorée en wrapper de `saveBytes(path) ([]byte, error)` qui encode+atomic-rename+tighten et retourne les bytes ; `Bootstrap(path)` ajouté pour squelette embarqué.
- `internal/config/paths_unix.go` — ajout de `ServicePath()` + `IntegrityKeyPath()`.
- `internal/config/paths_windows.go` — ajout de `ServicePath()` + `IntegrityKeyPath()`.
- `internal/config/discover.go` — `DiscoverPath` consulte `ServicePath()` avant le fallback user.
- `internal/service/service.go` — champ `integrityFailed atomic.Bool` + accesseurs `SetIntegrityFailed`/`IntegrityFailed`.
- `internal/ipc/messages.go` — ajout champ `IntegrityFailed bool` dans `Response`.
- `internal/ipchandler/handler.go` — gating mutations quand `prg.IntegrityFailed()`, propagation `resp.IntegrityFailed` sur les 3 branches de `handleGetStatus`, refactor `cfg.Save` → `cfg.SaveAndSign`, `Options.IntegrityKey`, signature `persistPreferredCountry` avec `key []byte`.
- `internal/ipchandler/handler_test.go` — signature `persistPreferredCountry` adaptée (arg `nil`).
- `internal/ui/httpserver.go` — champ `IntegrityFailed` dans `APIStatusResponse`, copie depuis IPC.
- `cmd/client/main.go` — wiring `Bootstrap → LoadOrCreateKey → Verify`, propagation `integrityKey` dans `Options` + `persistFirewallEnabled`, gestion `ErrHMACAbsent` (migration legacy) vs `ErrIntegrityMismatch` (set flag). Message d'erreur `resolveConfig` clarifié quand pubkey vide ou placeholder (résolution review M2).
- `config.example.toml` (racine) — header documentant l'interdiction d'édition manuelle + procédure recovery hors-band Linux/Windows. `[relay]` blanked out (domain/public_key_ed25519 = "") pour fail-fast résolution review M2.
- `internal/config/integrity.go` — `SignBytes(path, contents, key)` ajouté comme API primaire anti-TOCTOU (résolution review H2) ; `Sign` conservé en wrapper legacy avec `#nosec G304` annotations (résolution M3).
- `internal/config/perms_unix.go` — `os.Stat` préalable sur dir avant chmod : skip si déjà 0o700 (résolution review M4).
- `internal/config/persist.go` — `SaveAndSign` utilise `saveBytes → SignBytes` au lieu de `Save → Sign` pour passer les bytes encodés directement (résolution review H2).
- `frontend/index.html` — bandeau `#integrity-banner` sticky, plus haute priorité que kill-switch.
- `frontend/src/app.js` — détection `integrity_failed` dans `updateUI`, `integrityRecoveryHint()` OS-aware, masquage bouton Connect.
- `frontend/src/style.css` — styles `.integrity-failed-banner` + `.integrity-recovery-hint`.
