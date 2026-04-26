# le_voile

## Repository structure

The repo is split into per-OS / per-tier subtrees so that a change to one
platform never silently affects another. Each subtree is autonomous —
its own `Makefile`, its own `.goreleaser.yaml`, its own release script.

```
/                       OS-agnostic shared infrastructure
├── tools/{genkey,signpkg,verifypkg}/   maintainer crypto utilities
├── internal/{captive,crypto,httpproxy,leakcheck,registry,
│            stun,tunnel,updater,watchdog}/   shared client packages
│
├── relay/              server-side relay tier (linux/amd64)
│   ├── cmd/{relay,genregistry,verify-registry}/
│   ├── relay/          relay HTTP/3 server, blocklist, NAT, DoH, etc.
│   ├── deploy/         install.sh, systemd units, cert hooks
│   ├── scripts/        release-sign.sh
│   ├── Makefile  .goreleaser.yaml
│
├── windows/            Windows desktop tier
│   ├── cmd/{client,ui,ctl}/
│   ├── internal/{anomaly,blocklist,browser,config,ctlauth,dns,
│   │             elevation,firewall,ipc,ipchandler,preflight,
│   │             routing,service,tun,ui,uiwatchdog}/
│   ├── frontend/       Wails webview assets (HTML/JS/CSS)
│   ├── installer/      NSIS installer + build scripts
│   ├── tools/{wfp_repro,ipc_send,gen_icons.go}/   Windows diagnostics
│   ├── scripts/        fetch-wintun.sh, diag-blank-window.ps1, release-sign.sh
│   ├── Makefile  .goreleaser.yaml
│
├── linux/              Linux desktop tier
│   ├── cmd/{client,ui,ctl}/
│   ├── internal/       same package set as windows/internal/, Linux impls
│   ├── frontend/       same source as windows/frontend/, evolves independently
│   ├── packaging/      systemd units, polkit rules, hicolor icons, AUR PKGBUILD
│   ├── scripts/        test-aur-install.sh, test-auto-update-linux.sh, release-sign.sh
│   ├── Makefile  .goreleaser.yaml
│
├── android/            stub (implementation deferred — see android/README.md)
│
├── docs/  config.example.toml  go.mod  go.sum  LICENSE  README.md  SECURITY.md
```

Each release is cut independently with its own Git tag (`windows-vX.Y.Z`,
`linux-vX.Y.Z`, `relay-vX.Y.Z`). To build:

```bash
cd windows && make             # Windows binaries (run on Windows)
cd linux   && make              # Linux binaries (run on Linux)
cd relay   && make              # Relay binary (run on Linux)
```

`internal/blocklist/` is **duplicated** between `windows/` and `linux/` by
design (decision 2026-04-26): drift between copies is acceptable, with a
documented quarterly diff check.

## Installation

### Windows

Installateur NSIS signé (story 7.1) — télécharger `LeVoile-Setup-*.exe` depuis
la page [Releases](https://github.com/velia-the-veil/le_voile/releases) et
exécuter. Le service SCM + l'UI tray + la DLL Wintun sont configurés automatiquement.

### Linux

**Debian / Ubuntu** (`.deb`) :

```bash
curl -fLO https://github.com/velia-the-veil/le_voile/releases/latest/download/levoile_<version>_amd64.deb
sudo apt install ./levoile_<version>_amd64.deb
```

**Fedora / RHEL** (`.rpm`) :

```bash
sudo dnf install https://github.com/velia-the-veil/le_voile/releases/latest/download/levoile-<version>.x86_64.rpm
```

**Alpine** (`.apk`) :

```bash
curl -fLO https://github.com/velia-the-veil/le_voile/releases/latest/download/levoile_<version>_linux_amd64.apk
sudo apk add --allow-untrusted ./levoile_<version>_linux_amd64.apk
```

**Arch Linux** (AUR, story 7.3) :

```bash
yay -S levoile
# ou
paru -S levoile
```

Procédure mainteneur AUR : [docs/aur-release.md](docs/aur-release.md).

## Vérifier l'intégrité d'un téléchargement (Story 7.4)

Chaque artefact publié sur la page Releases est accompagné :
- d'un fichier `<artefact>.sig` — signature Ed25519 détachée (64 octets bruts)
- du `checksums.txt` + `checksums.txt.sig`
- de la clé publique master `levoile-release.pub.pem` (et `.pub` base64)

La master key Ed25519 vit exclusivement sur la machine hors-ligne du mainteneur
(NFR22g). Elle ne transite jamais par GitHub Actions ou un autre tiers.

### Option A — via `levoile-verify` (bundled)

Chaque archive contient `levoile-verify` (Linux/Windows). La clé publique est
embarquée dans le binaire au build — aucune interaction réseau n'est nécessaire :

```bash
# Après avoir téléchargé LeVoile_1.2.3_linux_amd64.tar.gz et .sig
tar xf LeVoile_1.2.3_linux_amd64.tar.gz
cd LeVoile_1.2.3_linux_amd64
./levoile-verify ../LeVoile_1.2.3_linux_amd64.tar.gz ../LeVoile_1.2.3_linux_amd64.tar.gz.sig
# Sortie attendue : ok: ...tar.gz (verified with embedded current)
```

### Option B — via `openssl` (toute distro)

> Nécessite bash (Git Bash, WSL ou Linux/macOS). En PowerShell natif, utiliser Option A ou recopier la commande sans les variables `$VER`.

```bash
VER=1.2.3
BASE="https://github.com/velia-the-veil/le_voile/releases/download/v${VER}"
curl -LO "${BASE}/levoile_${VER}_amd64.deb"
curl -LO "${BASE}/levoile_${VER}_amd64.deb.sig"
curl -LO "${BASE}/levoile-release.pub.pem"

openssl pkeyutl -verify -pubin -inkey levoile-release.pub.pem \
    -rawin -in "levoile_${VER}_amd64.deb" \
    -sigfile "levoile_${VER}_amd64.deb.sig"
# Sortie : "Signature Verified Successfully"
```

### Option C — AUR (automatique)

Le PKGBUILD exécute `verify()` avant l'installation. `yay -S levoile` refuse
d'installer si la signature est invalide — aucune action manuelle requise.

### Si la vérification échoue

**Ne pas installer.** Ouvrir une issue sur le repo avec le hash du fichier et
votre réseau (pays, opérateur) — cela aide à détecter une compromission amont
ou un MITM local.

Documentation complète (génération, rotation, threat model) :
[docs/release-signing.md](docs/release-signing.md).

## Mode dégradé du kill switch (Story 5.9)

Le kill switch firewall (nftables Linux / WFP Windows) bloque tout trafic
sortant sauf le tunnel et l'IP du relais. Sur un Wi-Fi public instable, si le
tunnel ne se rétablit pas, vous pouvez le **désactiver temporairement** pour
récupérer un accès Internet en clair, en assumant le risque.

### Activer le mode dégradé

**Depuis la fenêtre / tray :**

1. Clic droit sur l'icône système → « Mode dégradé ».
2. La fenêtre s'ouvre sur une modale de confirmation destructive avec le
   texte exact : *« Voulez-vous désactiver la protection temporairement ?
   Votre trafic ne sera PAS chiffré. L'icône tray deviendra rouge jusqu'à
   rétablissement du tunnel. »*
3. Cliquez sur **Continuer** (rouge).

L'icône tray devient rouge en permanence et un bandeau rouge s'affiche dans
la fenêtre tant que vous êtes en mode dégradé.

**En CLI (root / Administrateur) :**

```bash
sudo levoile-ctl killswitch off    # désactive
sudo levoile-ctl killswitch on     # réactive immédiatement
sudo levoile-ctl status            # affiche tunnel + killswitch
```

Le binaire `levoile-ctl` lit le token d'authentification machine-local situé
dans :

- Linux : `/etc/levoile/ctl.token` (perms 0600)
- Windows : `%ProgramData%\LeVoile\ctl.token`

Le token est généré automatiquement au premier démarrage du service Le Voile.

### Auto-restauration

Le mode dégradé est **transitoire**. Dès qu'une nouvelle connexion tunnel
réussit (reconnexion automatique, manuelle, ou changement de pays), le kill
switch est automatiquement réactivé, l'icône tray retrouve sa couleur
correspondant à l'état du tunnel et le bandeau rouge disparaît.

### Refus en portail captif

Si un portail Wi-Fi captif est actif, la commande échoue avec
`captive_portal_active`. Authentifiez-vous d'abord sur le portail
(« Activer la protection » dans l'UI), puis le mode dégradé redevient
disponible si nécessaire.

## Validation anti-fuite STUN (Story 6.1)

Le client émet périodiquement (défaut : toutes les 10 minutes) des requêtes
**STUN Binding** (RFC 5389) vers trois serveurs publics (Google x2 +
Cloudflare). Le paquet UDP est émis via `net.DialUDP` — le noyau le route
par la TUN `levoile0` (route par défaut, Story 2.4), il est encapsulé dans
le tunnel HTTP/3, NAT-forwardé par le relais vers le serveur STUN, et la
réponse revient par le même chemin.

C'est un **check de validation**, pas une défense active : la capture L3
(Epic 2) rend les fuites structurellement impossibles. Si la STUN IP
retournée ≠ IP du relais attendue, cela signale une TUN down, une mauvaise
configuration ou un bug — pas une fuite « produit ».

**Failover** : si un serveur STUN ne répond pas, les suivants sont essayés
dans l'ordre (timeout 5 s par serveur). Échec des 3 serveurs → erreur
loggée, prochain check 10 min plus tard.

**Configuration** (section `[stun]` du `config.toml`) :

- `leakcheck_interval = "10m"` — intervalle entre deux checks
- `default_server = "stun.l.google.com:19302"` — override du premier serveur
- `servers = [...]` — override complet de la liste

Aucune IP détectée par STUN n'est loggée (NFR22a). Seules les erreurs
opérationnelles apparaissent dans journald / Event Log.

## Reconnexion automatique sur anomalie (Story 6.3)

Deux déclencheurs relancent une séquence de reconnexion complète
**kill-switch-préservée** :

1. Le scheduler leakcheck détecte `leak_detected` (STUN IP ≠ IP relais
   attendue — la capture L3 est censée rendre ce cas impossible,
   l'observer signale donc TUN down, mauvais routing, ou bug).
2. Le watchdog TUN (Story 2.2) détecte que `levoile0` a disparu ou a été
   altéré.

La séquence elle-même (close TUN → recreate → routing teardown+setup
→ `firewall.Activate` idempotent **sans `Deactivate`** → `tunnel.Connect`)
est celle déjà utilisée pour la recovery watchdog. Le kill switch
`nftables`/`WFP` ne retombe jamais à OFF pendant la procédure : le flush
et le chargement atomiques garantissent qu'aucun paquet ne passe en clair.

Côté utilisateur :

- L'icône tray passe à **orange `IconAlert`** avec le tooltip
  « Anomalie détectée — reconnexion en cours ».
- Un **bandeau orange** apparaît dans la fenêtre webview (`#anomaly-banner`).
  Sur reconnexion réussie il flashe en vert (« Reconnexion réussie »)
  pendant 3 s avant de disparaître.
- Un évènement WARNING est écrit dans le journal système, **sans aucune
  donnée utilisateur** (NFR22a) — seulement une catégorie d'erreur courte
  (`tun_create_failed`, `routing_setup_failed`, `firewall_activate_failed`,
  `tunnel_connect_failed`, `unknown`).

**Consulter les logs** :

- Windows : `Get-WinEvent -LogName Application -Source LeVoile` ou
  Event Viewer → Applications.
- Linux : `journalctl -t levoile` ou `journalctl -u levoile`.

**Trigger manuel (opérationnel)** : `sudo levoile-ctl trigger-recovery`
(ou l'alias `recover`). Authentifié par le token `ctl.token` machine-local,
réponse IPC immédiate ; le suivi se fait via `levoile-ctl status` ou le
journal.

**Concurrence** : un mutex dédié sérialise les reconnexions. Si le
watchdog TUN et le scheduler leakcheck se déclenchent dans la même
fenêtre, une seule séquence s'exécute — la seconde invocation est
silencieusement ignorée.
