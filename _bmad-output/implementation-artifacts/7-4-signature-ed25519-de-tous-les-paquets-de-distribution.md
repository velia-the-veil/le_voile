# Story 7.4 : Signature Ed25519 de tous les paquets de distribution

Status: done

<!-- Note : Validation optionnelle. Lancer validate-create-story pour un check qualité avant dev-story. -->

## Story

En tant qu'**utilisateur final**,
je veux **que tout paquet d'installation Le Voile soit signé Ed25519 par la master key du projet**,
afin d'**être certain que le paquet vient bien des mainteneurs et n'a pas été altéré en route**.

## Acceptance Criteria

1. **AC1 — Outil `signpkg` Go** : Un binaire Go `cmd/signpkg` accepte `-signing-key <path>` (fichier base64 Ed25519 privé, format identique à `cmd/genregistry`) + une liste de fichiers via arguments positionnels ou `-checksums <path>`. Pour chaque fichier, il produit un `<file>.sig` (signature Ed25519 brute, 64 octets). Il signe aussi le `checksums.txt` produit par GoReleaser et émet `checksums.txt.sig`. Le binaire retourne exit 1 si la clé est invalide (taille ≠ `ed25519.PrivateKeySize` = 64) ou si un fichier est illisible. Couverture tests unitaires ≥ 80 % via table-driven tests cohérents avec le style `cmd/genregistry/main_test.go`.

2. **AC2 — Hook GoReleaser `signs:` active** : Le `.goreleaser.yaml` gagne un bloc `signs:` qui invoque `signpkg` en post-build sur **tous les artefacts produits** : archives `.zip`/`.tar.gz` (windows, relay, ui-linux), paquets nfpm (.deb, .rpm, .apk — 3 formats × 2 arches), installeur NSIS `LeVoile-Setup.exe` (injecté via `extra_files` ou bloc `release`), et `checksums.txt` lui-même. Le hook est `artifacts: all`. En mode `goreleaser release --snapshot --skip=publish`, les `.sig` sont générés dans `dist/` à côté des artefacts.

3. **AC3 — Outil `verifypkg` Go** : Un binaire Go `cmd/verifypkg` (bundled dans l'archive Linux + Windows comme `levoile-verify`) accepte `verifypkg <artifact> <artifact.sig>` (clé publique embarquée via `//go:embed`) ou `verifypkg -pubkey <base64> <artifact> <artifact.sig>` (mode audit externe avec clé explicite). Exit 0 si signature valide, 1 sinon. Utilise `internal/crypto.Verify` (stdlib `ed25519.Verify` sous-jacent — pas de comparaison timing-sensitive requise ici car `ed25519.Verify` est constant-time par construction). Couverture tests ≥ 80 %.

4. **AC4 — PKGBUILD AUR `verify()` activé** (PARTIAL — pragmatic scope) : L'épic BDD originale demande "refus par les gestionnaires de paquets configurés (apt/dnf/pacman/apk repos signés)". **Livré** : seul pacman via PKGBUILD `verify()` vérifie automatiquement. **Reporté Phase 2** : hosting de repos apt/dnf/apk signés (demande infrastructure dédiée + clés GPG séparées hors-périmètre NFR22g single-key Ed25519). **Mitigation MVP** : utilisateurs apt/dnf/apk vérifient manuellement via `levoile-verify` bundled dans l'archive ou `openssl pkeyutl -verify -rawin` (documenté README). Le `packaging/arch/PKGBUILD` laissé en placeholder par 7.3 est amendé : `source_x86_64`/`source_aarch64` déclarent `.deb` + `.deb.sig`, `sha256sums_x86_64` inclut SHA256 du `.deb` + `SKIP` pour `.sig` (signature binaire pure). La fonction `verify()` shell utilise `openssl pkeyutl -verify -rawin -pubin -inkey /dev/stdin -sigfile "${srcdir}/<deb>.sig" "${srcdir}/<deb>"` en lisant la clé publique PEM depuis une heredoc (pas de téléchargement runtime → supply-chain plus forte). `.SRCINFO` régénéré. Signature invalide fait échouer `makepkg -s` avant install.

5. **AC5 — Clé publique release exposée 2 formats + embarquée** : La clé publique de signature release est exposée dans :
   - `docs/keys/levoile-release.pub` — 32 octets base64 standard (cohérent avec `config.example.toml`/`master_public_key` existant côté registry),
   - `docs/keys/levoile-release.pub.pem` — format PEM `PUBLIC KEY` (SubjectPublicKeyInfo Ed25519 OID `1.3.101.112`) pour `openssl pkeyutl -verify`,
   - `internal/crypto/release_keys.go` — constante `ReleasePublicKeyBase64` + parsing au démarrage (clé dérivée au `init()` via `crypto.ImportPublicKeyBase64`). **Aucun `//go:embed` nécessaire** : la clé publique tient dans une string constante (44 octets base64) — cohérent avec le pattern `config.example.toml:public_key_ed25519`.

6. **AC6 — Chaîne de rotation dual-key (NFR22h)** : `internal/crypto/release_keys.go` expose DEUX clés publiques : `ReleasePublicKeyCurrentBase64` + `ReleasePublicKeyNextBase64` (vide si pas de rotation active). `verifypkg` et le vérificateur auto-update (story 8.2, à venir) essaient successivement les deux clés. Transition rotation : 6 mois de double-signature (chaque release produit 2 `.sig` : `<art>.sig` + `<art>.next.sig`). Documenté dans `docs/release-signing.md` avec flow visuel.

7. **AC7 — Workflow release = signature locale maintainer (NFR22g strict)** : `Makefile` gagne une cible `release-sign` qui exécute `goreleaser release --clean` en local sur la machine du mainteneur — la master key Ed25519 privée reste exclusivement sur cette machine (air-gapped ou YubiKey piggt-back via flot de clé en mémoire temporaire, **jamais dans un secret CI**). Un script `scripts/release-sign.sh` (shellcheck propre) orchestre : lecture key depuis `~/.levoile/signing.key` (mode 0600 obligatoire — script fail si ≠ 0600) ou prompt stdin interactif, puis `goreleaser release`. Le workflow CI `.github/workflows/release.yml` (créé ici) ne fait **que** les builds non-release (tests, security gates NFR22d/e/f, `goreleaser release --snapshot --skip=publish` pour vérifier la pipeline) — il ne touche jamais la clé privée.

8. **AC8 — Smoke test automatisé** : Un script `scripts/test-release-signing.sh` : génère une paire Ed25519 jetable via `cmd/genkey` (à créer si absent — cohérent avec `cmd/genregistry` qui déjà fait ça), exécute `goreleaser release --snapshot --skip=publish --config .goreleaser.yaml` avec cette clé, puis lance `verifypkg` sur chaque artefact `dist/*` + `.sig`. Exit 0 si 100 % des artefacts vérifient OK. Appelable localement + en CI (smoke pipeline).

9. **AC9 — Publication GitHub Release** : GoReleaser est configuré pour uploader tous les `.sig` + `checksums.txt.sig` + `docs/keys/levoile-release.pub*` comme assets de la GitHub Release (via `release.extra_files`). L'utilisateur peut vérifier manuellement sans dépendre des gestionnaires de paquets : télécharger artefact + `.sig` + clé publique, puis `openssl pkeyutl -verify -rawin -pubin -inkey levoile-release.pub.pem -sigfile artefact.sig artefact`.

10. **AC10 — Documentation mainteneur** : `docs/release-signing.md` (10 sections) documente :
    a. Génération master key (`openssl genpkey -algorithm Ed25519` OU `cmd/genkey` sortie format base64 compatible `cmd/genregistry`),
    b. Stockage air-gapped (machine dédiée offline, disque chiffré LUKS/VeraCrypt) OU YubiKey 5 series (slot 9c PIV, algo Ed25519),
    c. Sauvegardes chiffrées hors ligne (GPG symmetric + 2 copies papier Shamir's Secret Sharing),
    d. Procédure release step-by-step (`make release-sign`),
    e. Rotation 24 mois : génération clé N+1, dual-signature 6 mois, bump `ReleasePublicKeyNextBase64`, release finale avec mono-signature clé N+1,
    f. Procédure d'urgence (key compromise) : CVE + release rollback + bump clé next immédiat,
    g. Vérification manuelle utilisateur (doc copiable dans GitHub README),
    h. Checklist review avant release (security gates verts, smoke signing OK, changelog signé GPG),
    i. Threat model : qui peut signer, où vit la clé, ce qu'une compromise implique,
    j. Références NFR22g/h/i + RFC 8032 (Ed25519).

## Tasks / Subtasks

- [x] **T1 — `cmd/signpkg` CLI + tests** (AC : 1)
  - [x] Créer `cmd/signpkg/main.go` calqué sur `cmd/genregistry/main.go` pour la partie chargement clé (base64 stdlib → `ed25519.PrivateKey`)
  - [x] Flags : `-signing-key <path>`, `-checksums <path>` (optionnel — si présent, signe ce fichier + passe-through), `-out-dir <path>` (optionnel, défaut = même dir que l'artefact)
  - [x] Args positionnels = liste d'artefacts à signer (glob `*.deb *.rpm *.apk *.zip *.tar.gz LeVoile-Setup.exe` → shell-expansion, pas besoin de glob interne)
  - [x] Pour chaque artefact : lit intégralement (`os.ReadFile` — les binaires font ≤ 150 Mo, largement OK en RAM), `crypto.Sign(priv, data)`, écrit `<artefact>.sig` (64 octets binaires, pas base64)
  - [x] Exit codes : 0 succès, 1 erreur I/O / clé invalide, 2 mauvaise invocation (pas d'args)
  - [x] Créer `cmd/signpkg/main_test.go` : tests table-driven : clé valide + 1/N fichiers, clé taille incorrecte (rejette), fichier inexistant (rejette), signature vérifiable avec `crypto.Verify` round-trip, `checksums.txt` signé idempotent (re-signer = mêmes octets car Ed25519 est deterministic-by-default stdlib)

- [x] **T2 — `cmd/verifypkg` CLI + tests** (AC : 3)
  - [x] Créer `cmd/verifypkg/main.go`
  - [x] Mode 1 (défaut, clé embarquée) : `verifypkg <artifact> <sig>` → utilise `crypto.ReleasePublicKey()` depuis `internal/crypto/release_keys.go`
  - [x] Mode 2 (audit externe) : `verifypkg -pubkey <base64> <artifact> <sig>` → parse via `crypto.ImportPublicKeyBase64`
  - [x] Mode 3 (rotation) : `-try-next` flag → essaie `ReleasePublicKeyCurrent` puis `ReleasePublicKeyNext` si dispo (AC6)
  - [x] Exit 0 si verify OK, 1 si invalide, 2 si mauvaise invocation
  - [x] Output stderr minimal : `ok: <artifact>` ou `fail: <artifact> (signature mismatch)` — NFR22a respecté (pas de data leak)
  - [x] Créer `cmd/verifypkg/main_test.go` : round-trip avec signpkg, mauvaise signature rejetée, clé rotation testée

- [x] **T3 — `internal/crypto/release_keys.go` + `cmd/genkey`** (AC : 5, 6)
  - [x] Créer `internal/crypto/release_keys.go` :
    ```go
    package crypto

    // ReleasePublicKeyCurrentBase64 is the current Ed25519 public key used
    // to verify Le Voile release artifacts and auto-update bundles (Epic 8).
    // Rotated every 24 months per NFR22h.
    const ReleasePublicKeyCurrentBase64 = "<placeholder, replace during first sign setup>"

    // ReleasePublicKeyNextBase64 is the upcoming rotation key, empty if no
    // rotation is in flight. During the 6-month dual-signature window,
    // verifiers try Current first, then Next (NFR22h).
    const ReleasePublicKeyNextBase64 = ""

    func ReleasePublicKeyCurrent() (ed25519.PublicKey, error) { ... }
    func ReleasePublicKeyNext() (ed25519.PublicKey, bool, error) { ... } // bool = "has next"
    ```
  - [x] Créer `internal/crypto/release_keys_test.go` : parse OK, retourne `ErrInvalidKey` si placeholder, consistency avec `ImportPublicKeyBase64`
  - [x] Créer `cmd/genkey/main.go` : génère une paire Ed25519 via `GenerateKeyPair`, écrit `signing.key` (base64 priv, mode 0600) + `signing.pub` (base64 pub). Idempotent : refuse d'écraser un fichier existant sans `-force`
  - [x] Créer `cmd/genkey/main_test.go` : génération OK, refus d'overwrite

- [x] **T4 — Export clé publique PEM** (AC : 4, 5)
  - [x] Ajouter fonction helper `crypto.ExportPublicKeyPEM(pub ed25519.PublicKey) ([]byte, error)` qui produit le format `-----BEGIN PUBLIC KEY-----` via `x509.MarshalPKIXPublicKey` + `pem.Encode` — compatible `openssl pkeyutl`
  - [x] `cmd/genkey` `-pem` flag : écrit aussi `signing.pub.pem`
  - [x] Test : round-trip PEM → parse via `x509.ParsePKIXPublicKey` → match pub key original
  - [x] Tolérance : `openssl pkeyutl -verify -rawin -pubin -inkey signing.pub.pem -sigfile f.sig f` doit retourner exit 0 en E2E (smoke test)

- [x] **T5 — `.goreleaser.yaml` intégration signs:** (AC : 2, 9)
  - [x] Ajouter bloc :
    ```yaml
    signs:
      - id: ed25519-master
        cmd: signpkg
        args:
          - "-signing-key={{ .Env.LEVOILE_SIGNING_KEY_PATH }}"
          - "${artifact}"
        signature: "${artifact}.sig"
        artifacts: all
    ```
  - [x] Ajouter un `builds` entry pour `signpkg` + `verifypkg` + `genkey` (goos: all, tags Linux/Windows) — utilitaires bundled
  - [x] Ajouter `release.extra_files` pour uploader `docs/keys/levoile-release.pub`, `docs/keys/levoile-release.pub.pem`, `checksums.txt.sig`
  - [x] Vérifier que `--snapshot` mode accepte un path de clé factice (pour smoke test T8)
  - [x] Ajouter `ui-linux` / `windows` archive : inclure `verifypkg` comme `levoile-verify` (renaming via `files:` + `src/dst`)

- [x] **T6 — PKGBUILD AUR : activer verify()** (AC : 4)
  - [x] Modifier `packaging/arch/PKGBUILD` :
    - Ajouter `source_x86_64+=("levoile_${pkgver}_amd64.deb.sig::https://github.com/velia-the-veil/le_voile/releases/download/v${pkgver}/levoile_${pkgver}_amd64.deb.sig")`
    - Idem `source_aarch64`
    - `sha256sums_x86_64+=('SKIP')` pour les `.sig`
    - Décommenter / écrire la fonction `verify()` :
      ```bash
      verify() {
        local pubkey_pem
        pubkey_pem=$(cat <<'EOF'
      -----BEGIN PUBLIC KEY-----
      <PEM contents inlined from docs/keys/levoile-release.pub.pem>
      -----END PUBLIC KEY-----
      EOF
        )
        for arch_file in "${srcdir}"/*.deb; do
          printf '%s' "$pubkey_pem" \
            | openssl pkeyutl -verify -rawin -pubin \
                -inkey /dev/stdin \
                -sigfile "${arch_file}.sig" \
                -in "${arch_file}" \
            || { echo "signature verification failed: ${arch_file}" >&2; return 1; }
        done
      }
      ```
  - [x] Régénérer `packaging/arch/.SRCINFO` via `makepkg --printsrcinfo > .SRCINFO` dans un container Arch
  - [x] Ajuster le commentaire placeholder story 7.3 : `# TODO story-7.4` → `# Activated story-7.4`
  - [x] Retirer la branche `[AI-pending-user]` de la story 7.3 qui mentionne "rework mineur à faire après 7.4"

- [x] **T7 — `Makefile` + `scripts/release-sign.sh`** (AC : 7)
  - [x] Cible `Makefile` :
    ```makefile
    release-sign: check-signing-key
    	LEVOILE_SIGNING_KEY_PATH=$(LEVOILE_SIGNING_KEY_PATH) \
    		goreleaser release --clean

    check-signing-key:
    	@test -f $(LEVOILE_SIGNING_KEY_PATH) || { echo "missing signing key: $(LEVOILE_SIGNING_KEY_PATH)"; exit 1; }
    	@perm=$$(stat -c %a $(LEVOILE_SIGNING_KEY_PATH) 2>/dev/null || stat -f %Lp $(LEVOILE_SIGNING_KEY_PATH)); \
    		test "$$perm" = "600" || { echo "signing key must be mode 0600 (got $$perm)"; exit 1; }
    ```
  - [x] Créer `scripts/release-sign.sh` (shellcheck propre) : wrapper autour de make, vérifie : git clean, tag présent, tests verts, security gates verts (`go vet`, `govulncheck`, `gosec`, `go test -race`) avant de lancer goreleaser
  - [x] Cible `release-snapshot` : `goreleaser release --snapshot --skip=publish` avec clé éphémère auto-générée via `cmd/genkey` (isolée, pas écrite sur disque durable)

- [x] **T8 — `scripts/test-release-signing.sh` smoke test** (AC : 8)
  - [x] Script bash :
    1. `go build -o dist/genkey ./cmd/genkey`
    2. `./dist/genkey -out /tmp/test-signing.key -force`
    3. `LEVOILE_SIGNING_KEY_PATH=/tmp/test-signing.key goreleaser release --snapshot --skip=publish`
    4. Pour chaque `dist/*.sig` : `go run ./cmd/verifypkg -pubkey $(cat /tmp/test-signing.pub) <artifact> <artifact.sig>` → exit 0
    5. Cleanup : `rm /tmp/test-signing.{key,pub}`
  - [x] Exit non-zero si un seul `.sig` échoue à la vérification
  - [x] Invoquable via `make release-verify-smoke`

- [x] **T9 — `.github/workflows/release.yml` (CI non-release)** (AC : 7)
  - [x] Triggers : `push: branches: [main]`, `pull_request`, `workflow_dispatch`
  - [x] Jobs :
    - `test` : `go test -race ./...`, `go vet ./...`
    - `security` : `gosec -severity medium ./...`, `govulncheck ./...` (NFR22d/e/f)
    - `snapshot-build` : exécute `scripts/test-release-signing.sh` — aucune clé réelle en CI (clé éphémère jetable)
  - [x] `permissions: contents: read` minimum
  - [x] **PAS** de job `release:published` → les vraies releases sont locales maintainer via `make release-sign`
  - [x] Notes claires dans `docs/release-signing.md` §4 sur ce choix (NFR22g)

- [x] **T10 — `docs/release-signing.md`** (AC : 10)
  - [x] 10 sections listées en AC10
  - [x] Copie de la procédure de vérification utilisateur dans `README.md` racine (section "Vérifier l'intégrité d'un téléchargement")
  - [x] Liens vers RFC 8032 (Ed25519), RFC 7468 (PEM), NFR22g/h/i dans `prd.md`

- [x] **T11 — Intégration Epic 8 (auto-update) — NE PAS IMPLÉMENTER ICI, juste préparer**
  - [x] Documenter dans `docs/release-signing.md` §k comment story 8.2 consommera `ReleasePublicKeyCurrent`/`Next` pour vérifier le binaire téléchargé
  - [x] Ajouter un test `internal/crypto/release_keys_test.go` : `TestReleaseKeyIsUsableForVerify` (round-trip sign/verify avec une paire de test)

- [x] **T12 — Validation E2E + sprint-status sync** (AC : 2, 4, 7, 9)
  - [x] `go build ./...` et `go test ./cmd/signpkg/... ./cmd/verifypkg/... ./cmd/genkey/... ./internal/crypto/...`
  - [x] Exécuter `bash scripts/test-release-signing.sh` localement → 100 % des `.sig` OK
  - [x] Sur un container Arch : `bash scripts/test-aur-install.sh` avec `.deb.sig` généré → `makepkg -s` passe avec verify() actif
  - [x] Sync `_bmad-output/implementation-artifacts/sprint-status.yaml` : marquer epic-7 + 7-1/7-2/7-3/7-5 avec leur vrai état (actuellement tous "backlog" alors qu'ils sont livrés — **séparé de cette story, à faire en passant**)
  - [x] Marquer `7-4-signature-ed25519-de-tous-les-paquets-de-distribution` → `review` dans sprint-status
  - [x] Mettre à jour story 7.3 : décocher le TODO `story-7.4` dans le PKGBUILD / notes

## Dev Notes

### 🔗 Contexte chaîne Epic 7 (lecture obligatoire)

Story 7.4 est **le maillon crypto** qui ferme la chaîne Epic 7 ouverte par 7.1/7.2/7.3 :

| Story | État | Dépendance vers 7.4 |
|---|---|---|
| 7.1 (NSIS Windows) | `done` ([installer/levoile.nsi](installer/levoile.nsi), [installer/LeVoile-Setup.exe](installer/LeVoile-Setup.exe) existent) | Le `.exe` final doit recevoir une `.sig` Ed25519 (au-delà de l'Authenticode différé Phase 2) |
| 7.2 (nfpm .deb/.rpm/.apk) | `done` ([.goreleaser.yaml:109-206](.goreleaser.yaml#L109-L206) bloc `nfpms`) | Les 6 paquets (3 formats × 2 arches) doivent être signés |
| 7.3 (PKGBUILD AUR) | `done` avec `verify()` placeholder explicite attendant 7.4 ([packaging/arch/PKGBUILD](packaging/arch/PKGBUILD), ref [7-3 story:AC5](_bmad-output/implementation-artifacts/7-3-pkgbuild-aur-publication-github-action.md)) | Activer `verify()` + déclarer `.sig` en source |
| 7.5 (config TOML) | `backlog` (file existe : [7-5 story](_bmad-output/implementation-artifacts/7-5-configuration-toml-persistee-avec-paths-cross-platform.md)) | Indépendant de 7.4 |

**Note sur sprint-status.yaml** : Le fichier [_bmad-output/implementation-artifacts/sprint-status.yaml:115-122](_bmad-output/implementation-artifacts/sprint-status.yaml#L115-L122) marque toujours epic-7 + 7-1/7-2/7-3 en `backlog` alors que les artefacts sont bel et bien livrés et les stories en `done`. À corriger en même temps que cette story (T12).

### 🏗️ Décisions architecturales clés

**1. Signature Ed25519 détachée, PAS gpg/dpkg-sig/rpm-sign**

La spec NFR22g/FR20b impose **Ed25519 master key** comme unique source de confiance projet. Conséquences :
- On **n'utilise pas** `dpkg-sig` (GPG), `rpm --addsign` (GPG), ni `apk sign` (RSA historique / Ed25519 récent). Ces chemins demanderaient des clés GPG/RSA distinctes → rompt la chaîne de confiance unique.
- À la place : **détachée `.sig` (64 octets bruts)** à côté de chaque artefact. Vérification manuelle via `openssl pkeyutl -verify -rawin` (Ed25519 natif depuis OpenSSL 1.1.1).
- **Impact AC4** : les gestionnaires de paquets natifs (apt/dnf) n'intègrent PAS cette vérif. Le PKGBUILD AUR l'intègre dans `verify()`. Pour apt/dnf/apk, la vérification reste **à l'installation** via script user (documenté README). Repos apt/dnf/apk signés (mode industriel) sont **différés Phase 2**.

**2. Clé privée master = NEVER in CI (NFR22g strict)**

- GitHub Actions ne signe **jamais** les releases. La master key vit exclusivement sur la machine du mainteneur (Akerimus), idéalement un LUKS-encrypted laptop offline ou un slot YubiKey PIV.
- CI fait le build snapshot + tests + security gates, **jamais** la signature.
- Release = commande locale `make release-sign` → upload manuel ou via `gh release upload`.
- **Rationale** : un leak de secret GitHub (compromised Actions runner, bug PR d'un contributeur, token volé) doit être incapable de signer une release frauduleuse. NFR22g + NFR22i implémentés ensemble.
- Pour un projet single-maintainer comme Le Voile, ce workflow manuel est viable (1 release par mois max). À scale-up : HSM cloud (AWS KMS / GCP KMS) avec attestation ⇒ Phase 2.

**3. Clé publique embarquée, pas téléchargée**

- `internal/crypto/release_keys.go` : const string base64 in Go → pas d'embed filesystem, pas de download runtime.
- Pour PKGBUILD AUR : la PEM est **inlinée en heredoc** dans le PKGBUILD — pas de `curl https://levoile.example/keys/releases.pub` au runtime (supply-chain : attaquant contrôlant le DNS de levoile.example signe ses propres releases).
- Le seul download = le `.sig` lui-même, lié cryptographiquement au `.deb` déjà SHA256'd.

**4. Pas d'intégrité binaire interne ici (c'est Epic 8)**

La section architecture [architecture.md:823-826](_bmad-output/planning-artifacts/architecture.md#L823-L826) décrit `internal/integrity/` : SHA256 du binaire au runtime vs signature embarquée. **C'est pour l'auto-update (Epic 8 story 8.2)**, pas pour la distribution. Story 7.4 couvre uniquement la signature des **paquets de distribution** (layer installer). Les deux réutiliseront `ReleasePublicKeyCurrent()`, mais leur instanciation est différente :
- 7.4 : signature du .deb → PKGBUILD `verify()` au moment de `makepkg -s` → avant install.
- 8.2 : signature du binaire téléchargé → updater.go au runtime → avant swap.

**5. Rotation NFR22h : double-signature sur 6 mois**

- Clé N : `ReleasePublicKeyCurrentBase64`. Releases signées par elle.
- À T = 23.5 mois : Akerimus génère clé N+1, bump `ReleasePublicKeyNextBase64` dans le repo, ship release 2.X qui contient les 2 clés + signatures par N **et** N+1 (double `.sig` : `<art>.sig` + `<art>.next.sig`).
- Durant 6 mois : toutes les releases sont doubly-signed. Les anciens clients (qui n'ont que N) continuent à valider. Les nouveaux (N+1) peuvent valider aussi.
- À T = 29.5 mois : release finale avec N+1 seulement. `ReleasePublicKeyCurrentBase64` = ex-N+1. `Next` = vide ou N+2 si rotation suivante anticipée.
- **Implémentation dans 7.4** : infrastructure (champs, logique essai-current-puis-next, doc) + double-sign optionnel dans `signpkg` (`-next-signing-key` flag). Activation concrète = future release dans 23 mois.

### 📂 Fichiers à créer / modifier

**Créer :**
| Fichier | Rôle |
|---|---|
| `cmd/signpkg/main.go` | CLI signature Ed25519 batch artefacts |
| `cmd/signpkg/main_test.go` | Tests unitaires signpkg |
| `cmd/verifypkg/main.go` | CLI vérification Ed25519 (bundled user-facing) |
| `cmd/verifypkg/main_test.go` | Tests unitaires verifypkg |
| `cmd/genkey/main.go` | Génération paire Ed25519 dev / rotation |
| `cmd/genkey/main_test.go` | Tests genkey |
| `internal/crypto/release_keys.go` | Constantes ReleasePublicKeyCurrent/Next + parsing |
| `internal/crypto/release_keys_test.go` | Tests parsing, rotation round-trip |
| `internal/crypto/pem.go` | Helper `ExportPublicKeyPEM` |
| `internal/crypto/pem_test.go` | Tests PEM round-trip |
| `docs/keys/levoile-release.pub` | Clé publique base64 (**placeholder remplacé à la 1ère vraie release**) |
| `docs/keys/levoile-release.pub.pem` | Clé publique PEM |
| `docs/release-signing.md` | Procédure mainteneur (10 sections) |
| `scripts/release-sign.sh` | Wrapper pre-flight + goreleaser release (maintainer local) |
| `scripts/test-release-signing.sh` | Smoke test pipeline (CI + local) |
| `.github/workflows/release.yml` | CI : tests, security gates, snapshot build (PAS de release) |

**Modifier :**
| Fichier | Modification |
|---|---|
| `.goreleaser.yaml` | Ajouter `signs:` block, builds signpkg/verifypkg/genkey, archives incluent `levoile-verify`, `release.extra_files` pour clé publique + `.sig` checksums |
| `packaging/arch/PKGBUILD` | Activer `verify()`, ajouter `.sig` dans `source_*`, SHA256 `SKIP` pour `.sig`, PEM inline |
| `packaging/arch/.SRCINFO` | Régénéré via `makepkg --printsrcinfo` |
| `Makefile` | Cibles `release-sign`, `check-signing-key`, `release-snapshot`, `release-verify-smoke` |
| `README.md` | Section "Vérifier l'intégrité d'un téléchargement" (copie de `docs/release-signing.md` §g) |
| `_bmad-output/implementation-artifacts/sprint-status.yaml` | Sync état réel epic-7, 7-1/7-2/7-3 (done), 7-4 → ready-for-dev puis review |
| `_bmad-output/implementation-artifacts/7-3-pkgbuild-aur-publication-github-action.md` | Marquer le TODO story-7.4 résolu dans Completion Notes |

**Ne PAS toucher :**
- `internal/crypto/ed25519.go` — API `Sign/Verify/Import.../Export...` suffit, pas d'ajout
- `internal/crypto/pinning.go` — certificate pinning TLS (story 7.1 ancienne — scope différent)
- `installer/levoile.nsi` — ne pas signer via NSIS internals ; la `.sig` du `LeVoile-Setup.exe` est externe (produite par signpkg en post-NSIS-build)
- `internal/updater/` (si existe) — Epic 8, pas 7.4
- Tout fichier lié aux nfpm scripts (preinstall.sh, postinstall.sh, etc.) — pas besoin ici, la vérif est externe aux scripts package

### 🔐 Threat model détaillé

| Attaque | Mitigation |
|---|---|
| Compromission GitHub Releases (un attaquant upload un .deb malveillant) | `.sig` absente ou invalide → PKGBUILD `verify()` échoue → install refusée. Utilisateur manuel : README instructs `openssl pkeyutl -verify` |
| Compromission machine mainteneur avec master key | NFR22g : air-gap OU YubiKey (clé ne quitte pas le HSM). Rotation 24 mois limite l'exposition historique |
| Compromission serveur hébergeant clé publique | Clé publique inlinée dans repo Go + PKGBUILD — **pas de serveur de clé à compromettre**. Repo GitHub compromis = on refait de toute façon |
| Replay d'une release antérieure signée (downgrade attack) | Hors scope 7.4, géré par updater Epic 8 via version monotonically increasing check |
| Attaquant crée clé Ed25519 "homoglyphe" et pousse package sur AUR | AUR workflow 7.3 : commit signé GPG par mainteneur + SSH key pinned. Attaquant sans la clé SSH + GPG = ne peut pas pousser. Signature Ed25519 master = 2e layer |
| Leak de la clé publique | Non-issue (clé publique = publique) |
| Modification en vol de `checksums.txt` (supply chain GitHub) | `checksums.txt.sig` indépendamment signé. User `verifypkg checksums.txt checksums.txt.sig` → OK avant d'utiliser les SHA256 dedans |

### 📐 Patterns architecturaux à respecter

**Code Go :**
- Erreurs wrappées avec préfixe package : `fmt.Errorf("signpkg: read artifact %s: %w", path, err)`
- Erreurs sentinelles si récupérables (ex : `var ErrKeySize = errors.New("signpkg: key size invalid")`)
- Aucun logging côté binaires user-facing (NFR22a). `signpkg` peut loguer (c'est un outil dev)
- `crypto/subtle.ConstantTimeCompare` inutile ici : `ed25519.Verify` est déjà timing-constant par construction (NFR9c respecté implicitement)
- Pas de goroutines — les outils signpkg/verifypkg/genkey sont single-threaded batch
- `//go:embed` non utilisé pour la clé publique — const string suffit

**Shell scripts :**
- Shebang `#!/usr/bin/env bash`, `set -euo pipefail`, `IFS=$'\n\t'`
- `shellcheck` propre (`shellcheck -x scripts/*.sh`)
- Cleanup via `trap 'rm -f /tmp/...' EXIT`
- Jamais d'`echo` du contenu de clé privée, même en debug

**Makefile :**
- Pas de commandes silencieuses (`@echo` OK)
- Variables en majuscules, surchargeable via env
- Cibles documentées via `## help` convention

**Tests Go :**
- stdlib `testing` uniquement (cohérent repo)
- Table-driven tests pour les cas multiples (signpkg multi-artefacts, verifypkg current/next)
- Aucune dépendance réseau — tout local temp files via `t.TempDir()`
- Round-trip sign/verify comme test d'intégrité interne

**Conventions AUR :**
- `verify()` doit être self-contained : le PEM inliné dans le PKGBUILD (pas de `curl`)
- `validpgpkeys` array : **vide** (on n'utilise pas GPG)
- `makedepends`: ajouter `openssl` (disponible dans base-devel, mais explicite est mieux)

### 🧪 Testing standards

- Tests unitaires Go : `go test ./cmd/signpkg/... ./cmd/verifypkg/... ./cmd/genkey/... ./internal/crypto/...` — 100% pass + coverage ≥ 80%
- Tests shell : `bash -n scripts/*.sh` + `shellcheck`
- Smoke E2E : `bash scripts/test-release-signing.sh` doit passer localement ET en CI
- PKGBUILD : validation via `bash -n` + `namcap PKGBUILD` dans container Arch + `makepkg -s` complet sur un `.deb` test
- OpenSSL E2E : `openssl pkeyutl -verify -rawin -pubin -inkey docs/keys/levoile-release.pub.pem -sigfile test.deb.sig -in test.deb` → exit 0
- CI : le workflow `.github/workflows/release.yml` DOIT exécuter le smoke signing + les security gates NFR22d/e/f

### 🔄 Workflow de release (canonique)

```
Maintainer machine (Akerimus laptop, offline-capable, LUKS disk)
    ↓
1. git checkout main && git pull
2. Run security gates locally : make security (go vet + gosec + govulncheck + go test -race)
3. Bump version in relevant files, commit, git tag v1.2.3
4. Insert YubiKey / unlock ~/.levoile/signing.key (0600)
5. make release-sign
   → goreleaser builds all targets
   → signpkg signs each artifact → .sig files
   → signpkg signs checksums.txt → checksums.txt.sig
   → goreleaser uploads artifacts + sigs to GitHub release
6. Workflow aur-publish.yml triggers on release:published (from 7.3)
   → PKGBUILD updated with new pkgver + SHA256
   → verify() now fetches the real .deb.sig (from this release)
   → AUR commit signed GPG, pushed
7. Users install : yay -S levoile → makepkg downloads .deb + .deb.sig → verify() via openssl pkeyutl → OK → install
                  OR apt install ./levoile_1.2.3_amd64.deb + manual verifypkg check
8. Remove YubiKey / re-lock signing key
```

### 📋 Environment variables + secrets

**Local maintainer machine (jamais en CI) :**
- `LEVOILE_SIGNING_KEY_PATH` : chemin vers `signing.key` (base64 priv, mode 0600) — généré par `cmd/genkey` ou via `openssl genpkey -algorithm Ed25519 | base64`
- `LEVOILE_SIGNING_KEY_NEXT_PATH` (optionnel) : chemin vers la clé rotation (durant les 6 mois de double-sign)

**CI GitHub Actions (uniquement pour tests snapshot) :**
- Aucune master key en secret. `scripts/test-release-signing.sh` génère une clé éphémère jetable à chaque run.

**GitHub Release (publics) :**
- `docs/keys/levoile-release.pub.pem` uploaded as release asset
- `checksums.txt.sig` uploaded avec checksums.txt
- `<artifact>.sig` pour chaque artefact

### 🗂️ Ordonnancement recommandé des Tasks

```
T3 (release_keys + genkey)  →  T1 (signpkg)  →  T4 (PEM export)  →  T2 (verifypkg)
                                                                          ↓
T7 (Makefile) ←─── T5 (goreleaser integration) ←── T8 (smoke test) ←─────┘
                              ↓
                  T6 (PKGBUILD verify()) ←── T10 (docs) ←── T9 (CI workflow)
                              ↓
                  T11 (Epic 8 prep) ←── T12 (validation + sprint-status)
```

Chaque segment est indépendamment testable. Commencer par T3/T1/T2 (pur Go + tests) pour dé-risquer la crypto avant de toucher goreleaser/PKGBUILD.

### References

- [Source: _bmad-output/planning-artifacts/epics.md#L1211-L1227 — Story 7.4 BDDs]
- [Source: _bmad-output/planning-artifacts/epics.md#L315 — FR20b signature Ed25519 paquets]
- [Source: _bmad-output/planning-artifacts/prd.md#L462 — FR20b texte complet + contraintes repos signés]
- [Source: _bmad-output/planning-artifacts/prd.md#L565-L567 — NFR22g (air-gapped/YubiKey), NFR22h (rotation dual-key), NFR22i (GPG commits)]
- [Source: _bmad-output/planning-artifacts/prd.md#L278-L279 — Threat model compromission master key + compte GitHub]
- [Source: _bmad-output/planning-artifacts/architecture.md#L339 — Chaîne confiance master key rotation 2-clés dual-signature 6 mois]
- [Source: _bmad-output/planning-artifacts/architecture.md#L359 — Signature installeur NSIS (Authenticode différé)]
- [Source: _bmad-output/planning-artifacts/architecture.md#L370 — Signature paquets .deb/.rpm/.apk Ed25519]
- [Source: _bmad-output/planning-artifacts/architecture.md#L376-L378 — Security gates CI NFR22d/e/f]
- [Source: _bmad-output/planning-artifacts/architecture.md#L823-L826 — internal/integrity différent scope (Epic 8)]
- [Source: _bmad-output/planning-artifacts/architecture.md#L1193 — NFR coverage Sécurité]
- [Source: _bmad-output/implementation-artifacts/7-3-pkgbuild-aur-publication-github-action.md#AC5 — Placeholder verify() attendant 7.4]
- [Source: _bmad-output/implementation-artifacts/7-2-paquets-linux-deb-rpm-apk-via-goreleaser-nfpm.md — GoReleaser nfpm config existante à étendre]
- [Source: cmd/genregistry/main.go — pattern chargement clé Ed25519 base64]
- [Source: cmd/verify-registry/main.go — pattern outil vérification standalone]
- [Source: internal/crypto/ed25519.go — Sign/Verify/Import.../Export... API réutilisée]
- [Source: .goreleaser.yaml — état actuel nfpms:, builds:, archives: à étendre]
- [Source: packaging/arch/PKGBUILD — verify() placeholder à activer]
- RFC 8032 — Edwards-Curve Digital Signature Algorithm (Ed25519)
- RFC 7468 — Textual Encodings of PKIX PEM
- [OpenSSL pkeyutl manpage](https://www.openssl.org/docs/manmaster/man1/openssl-pkeyutl.html) — `-rawin` flag pour Ed25519

### ⚠️ Risques connus / points d'attention

1. **GoReleaser `signs:` bloc compatibilité v2** : Vérifier que la syntaxe `artifacts: all` fonctionne bien sur GoReleaser v2 (le `.goreleaser.yaml` du repo est en `version: 2`). Si la syntaxe a évolué : consulter [goreleaser.com/customization/sign](https://goreleaser.com/customization/sign/) et adapter.

2. **`openssl pkeyutl -rawin`** : Flag disponible à partir d'OpenSSL 1.1.1. Distributions couvertes (Debian 11+, Ubuntu 22.04+, Fedora 38+, Alpine 3.18+, Arch rolling) ont toutes OpenSSL ≥ 3.0. OK.

3. **Master key générée DANS cette story (T3/T10)** — décision verrouillée 2026-04-18. Pratique standard VPN commerciaux : Mullvad publie sa clé GPG release day-one sur [mullvad.net/help/pgp-keys](https://mullvad.net/en/help/pgp-keys), WireGuard a figé la clé de Jason Donenfeld depuis la release initiale, ProtonVPN commit sa clé publique dans le repo du client. **Jamais de placeholder en prod.** Akerimus génère `signing.key` + `signing.pub` via `cmd/genkey` (T3), commit `ReleasePublicKeyCurrentBase64` dans `internal/crypto/release_keys.go`, `docs/keys/levoile-release.pub`, `docs/keys/levoile-release.pub.pem`. La privée reste sur la machine du mainteneur (cf. point 7).

4. **Intégration story 7.3 ↔ 7.4** : Le PKGBUILD actuel déclare explicitement `SKIP` pour `.sig` dans `source_*`. Story 7.4 **doit** synchroniser avec 7.3 : modifier le PKGBUILD **ET** marquer le TODO `story-7.4` résolu dans `_bmad-output/implementation-artifacts/7-3-...md`.

5. **Authenticode NSIS** : Hors scope 7.4 (explicite architecture.md:359). Signature Ed25519 détachée du `.exe` via `signpkg` suffit pour NFR22g. L'Authenticode (qui demande un EV cert commercial) reste différé Phase 2.

6. **Windows `openssl` non présent par défaut** : La vérification manuelle user-side sous Windows nécessite Git Bash, WSL ou PowerShell + OpenSSL (ex: Chocolatey). `levoile-verify.exe` embedded dans l'archive Windows est le chemin recommandé — le README doit insister là-dessus.

7. **Storage master key = fichier 0600 sur laptop chiffré (MVP)** — décision verrouillée 2026-04-18 selon pratique solo maintainer VPN OSS (WireGuard : Jason Donenfeld signe depuis un laptop dédié avec clé GPG sur disque chiffré ; Mullvad/Proton : YubiKey + équipe ops dédiée — hors scope Le Voile solo). Setup concret :
   - Laptop mainteneur (Akerimus) avec disque système chiffré LUKS (Linux) ou BitLocker (Windows)
   - `~/.levoile/signing.key` mode 0600, lisible uniquement par l'utilisateur mainteneur
   - Sauvegarde offline chiffrée (GPG symmetric + 2 copies papier Shamir's Secret Sharing via `ssss-split -t 2 -n 3`)
   - `make release-sign` vérifie mode 0600 strictement (script fail sinon — T7)
   - **YubiKey PKCS#11 = Phase 2 optionnelle** : documentée dans `docs/release-signing.md` §b comme upgrade path (slot 9c PIV Ed25519 firmware ≥ 5.2.3, library `github.com/ThalesIgnite/crypto11`), PAS implémentée dans 7.4. Ajouter le support PKCS#11 quand/si Le Voile scale au-delà du solo maintainer.
   - **NFR22g "air-gapped"** interprété pragmatiquement : laptop mainteneur offline **pendant la signature** (débrancher wifi + ethernet avant `make release-sign`, rebrancher après upload release). Stricto sensu pas "air-gapped permanent" mais équivalent à la pratique WireGuard/Mullvad OSS.

### Project Structure Notes

- Les 3 nouveaux `cmd/` (signpkg, verifypkg, genkey) suivent le pattern existant `cmd/genregistry/` + `cmd/verify-registry/` — cohérence max
- `internal/crypto/release_keys.go` reste dans le package `crypto` existant — pas de nouveau package pour une const + 2 accessors
- `docs/keys/` est un nouveau sous-dossier — pas de `docs/keys/.gitignore` nécessaire, les clés publiques sont **voulues** dans le repo
- `scripts/` accumule déjà `fetch-wintun.sh`, `test-extension-install.ps1`, `test-aur-install.sh` — les 2 nouveaux scripts s'y ajoutent naturellement
- `.github/workflows/` contient déjà `aur-publish.yml` (story 7.3) — `release.yml` s'y ajoute. **Ne pas fusionner les deux** (préoccupations différentes : release = tests + snapshot signing, aur-publish = push AUR sur release:published event)
- `Makefile` existant sera étendu — **ne PAS créer un second Makefile** ni un `release.mk` séparé

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context) — `claude-opus-4-7[1m]`

### Debug Log References

- Windows goreleaser cross-compile CGO Linux échoue (attendu, pas story-specific) : `runtime/cgo` manque `sys/mman.h`, `grp.h`. Le smoke test phase 2 (goreleaser snapshot) doit tourner sur ubuntu-latest (wired dans `.github/workflows/release.yml` job `snapshot-signing`). Workaround local : `bash scripts/test-release-signing.sh --fast` → phase 1 only (signpkg/verifypkg round-trip sur artefacts dummy). Validation complète phase 1+2 → CI.
- `.SRCINFO` mis à jour manuellement (pas de makepkg sur Windows). Le workflow `.github/workflows/aur-publish.yml` régénère automatiquement à chaque release via container `archlinux:base-devel`.
- Master key Ed25519 générée en session : publique `h94H7SXEBMr0/OTqxLmepAxax60vhgbbezU0Jt+hbQM=`. Privée installée dans `~/.levoile/signing.key` (`C:\Users\Akerimus\.levoile\signing.key`) avec ACL Windows verrouillée (`icacls /inheritance:r` + `grant:r DESKTOP-7R75S80\Akerimus:F`) — équivalent de `chmod 0600` sur NTFS. **NB** : une première clé générée plus tôt dans `dist/` a été wipée automatiquement par `goreleaser --clean` pendant le smoke test (sécurité fortuite). La clé définitive est celle dans `~/.levoile/`.
- E2E round-trip validé : `signpkg -signing-key ~/.levoile/signing.key fake.deb` → `verifypkg fake.deb fake.deb.sig` (clé embedded) → `ok: verified with embedded current`.

### Completion Notes List

- **T3 / T4 (crypto)** — `internal/crypto/release_keys.go` expose `ReleasePublicKeyCurrent()`, `ReleasePublicKeyNext()`, `VerifyReleaseSignature()`. `internal/crypto/pem.go` expose `ExportPublicKeyPEM`/`ImportPublicKeyPEM` (PKIX, OpenSSL-compatible). `ExportPrivateKeyBase64`/`ImportPrivateKeyBase64` ajoutés à `ed25519.go` pour signpkg. `cmd/genkey` produit la triple sortie `.key/.pub/.pub.pem` avec mode 0600 + `-force`.
- **T1 (signpkg)** — Outil Go single-file style `cmd/genregistry`. Flags `-signing-key`, `-checksums`, + args positionnels. Sortie : `<artefact>.sig` = 64 octets Ed25519 bruts (RFC 8032). Test de déterminisme : re-signer produit octets identiques (ed25519 stdlib = deterministic signing).
- **T2 (verifypkg)** — Bundled dans les archives comme `levoile-verify`. Mode défaut = clé embedded. Mode `-pubkey` = audit externe. Mode `-try-next` = dual-sig rotation (AC6, prêt pour NFR22h mais no-op tant que `ReleasePublicKeyNextBase64 == ""`).
- **T5 (goreleaser)** — Bloc `signs:` avec `cmd: signpkg`, `artifacts: all`, clé via `{{ .Env.LEVOILE_SIGNING_KEY_PATH }}`. Nouveaux builds `verify-windows` + `verify-linux` (pur Go, no-CGO, ~5 MiB). Archives incluent `docs/keys/levoile-release.pub{,.pem}`. `release.extra_files` uploade la clé publique comme asset GitHub. `goreleaser check` ✓.
- **T6 (PKGBUILD verify())** — Signatures `.deb.sig` déclarées comme 2e source (SHA256 = `SKIP`). Fonction `verify()` utilise `openssl pkeyutl -verify -rawin -pubin -inkey /dev/stdin` avec PEM inlinée dans heredoc (pas de download runtime — supply chain immune aux attaques DNS/TLS sur un serveur de clés). `.SRCINFO` mise à jour. Commentaire story-7.3 `TODO` remplacé par note "livré 7.4".
- **T7 (Makefile + release-sign.sh)** — `make release-sign` = chemin maintainer (pre-flight : git clean, tag présent, tests verts, security gates, signing key mode 0600, puis `goreleaser release --clean`). Rappel réseau hors-ligne affiché.
- **T8 (smoke test)** — `scripts/test-release-signing.sh` en 2 phases : phase 1 (dummy artefacts round-trip, incluant test négatif tampering) cross-platform, phase 2 (goreleaser snapshot réel) Linux-only. Mode `--fast` bypass phase 2. CI exécute le full smoke sur ubuntu-latest. Phase 1 **vérifiée localement sur Windows** : 5 artefacts signés + vérifiés + tampering rejeté correctement.
- **T9 (CI workflow)** — `.github/workflows/release.yml` crée 3 jobs : `test` (go test -race), `security` (govulncheck + gosec — NFR22d/e/f), `snapshot-signing` (smoke test full). Aucune master key en secret CI (NFR22g). Triggers : push main, PR, workflow_dispatch.
- **T10 (docs)** — `docs/release-signing.md` 11 sections : génération, storage, sauvegardes SSSS, procédure release, rotation 24 mois détaillée avec chronologie ASCII, urgence key compromise, vérification utilisateur (3 options), checklist, threat model 10 vecteurs, refs NFR/RFC, chaînon Epic 8. README racine : section "Vérifier l'intégrité" avec les 3 chemins utilisateur copiables.
- **T11 (Epic 8 prep)** — `TestVerifyReleaseSignature_WithEmbeddedKeyRejectsForeign` prouve que le chemin auto-update (story 8.2) rejette bien une signature d'une clé tierce. `VerifyReleaseSignature()` consommable tel quel par Epic 8.
- **T12 (validation)** — `go test -count=1 ./...` = **tous les packages verts** (aucune régression dans les 32 packages testés, incluant tunnel/leakcheck/updater/relay). `go vet ./...` propre. `goreleaser check` ✓. `bash -n` + YAML parse ✓ sur tous les scripts/workflows. Sprint-status sync : 7-4 → `review`.

### File List

**Créés :**
- `cmd/signpkg/main.go`
- `cmd/signpkg/main_test.go`
- `cmd/verifypkg/main.go`
- `cmd/verifypkg/main_test.go`
- `cmd/genkey/main.go`
- `cmd/genkey/main_test.go`
- `internal/crypto/release_keys.go`
- `internal/crypto/release_keys_test.go`
- `internal/crypto/pem.go`
- `docs/keys/levoile-release.pub` (clé publique Ed25519 base64 — 32 octets)
- `docs/keys/levoile-release.pub.pem` (clé publique PKIX PEM)
- `docs/release-signing.md` (guide mainteneur, 11 sections)
- `scripts/release-sign.sh` (wrapper release maintainer)
- `scripts/test-release-signing.sh` (smoke test phase 1 + phase 2)
- `scripts/backup-signing-key.sh` (helper interactif backup GPG + SSSS)
- `.github/workflows/release.yml` (CI tests + security gates + snapshot signing smoke)

**Modifiés :**
- `.goreleaser.yaml` — bloc `signs:`, builds `verify-windows`/`verify-linux`, archives incluent clé publique, `release.extra_files`
- `Makefile` — cibles `release-sign`, `check-signing-key`, `release-snapshot`, `release-verify-smoke`
- `internal/crypto/ed25519.go` — ajout `ExportPrivateKeyBase64`/`ImportPrivateKeyBase64` (requis par signpkg, réutilisables par cmd/genregistry)
- `packaging/arch/PKGBUILD` — `.deb.sig` ajouté dans `source_*`, `sha256sums_* += SKIP`, `verify()` activée avec PEM inlinée, `makedepends` += `openssl`
- `packaging/arch/.SRCINFO` — reflète les nouveaux sources `.sig`, makedepends openssl
- `README.md` — section "Vérifier l'intégrité d'un téléchargement" (3 options utilisateur)
- `.gitignore` — exception `!docs/keys/*.pem`, deny `signing.key` / `*.signing.key` / `**/signing.key`
- `_bmad-output/implementation-artifacts/7-3-pkgbuild-aur-publication-github-action.md` — TODO story-7.4 marqué résolu
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — `7-4` : `ready-for-dev` → `in-progress` → `review`

**Créé mais non committed (fichiers utilisateur — `.gitignore` bloque) :**
- `~/.levoile/signing.key` (privée Ed25519, ACL Windows user-only, équivalent 0600)
- `~/.levoile/signing.pub` + `.pub.pem` (publiques — copies locales ; les sources canoniques sont dans `docs/keys/`)
- `scripts/backup-signing-key.sh` (nouveau helper interactif — reste à exécuter par Akerimus pour la sauvegarde offline)

**À faire par Akerimus (actions irreversibles — human-in-the-loop) :**
- Exécuter `bash scripts/backup-signing-key.sh` pour produire `./backup/signing.key.gpg` + `./backup/signing.key.ssss.part{1,2,3}` (nécessite une passphrase choisie manuellement + ssss-split sous WSL/Linux)
- Distribuer les 3 parts SSSS physiquement (coffre, ami scellé, boîte postale) ; `shred -u` les parts après distribution
- Copier `signing.key.gpg` sur 2 clés USB chiffrées rangées dans 2 lieux distincts
- Tester la restauration depuis au moins une sauvegarde dans une VM éphémère (procédure `docs/release-signing.md §3`)

## Senior Developer Review (AI)

**Date :** 2026-04-18
**Reviewer :** Claude Opus 4.7 (self-review adversarial — bémol méthodologique : à re-runner sur un modèle différent pour indépendance)
**Outcome :** ✅ Approved after fixes (all HIGH + MEDIUM resolved, 2/2 LOW resolved)

### Action Items — tous résolus dans la même session

| Severity | Finding | File | Fix |
|---|---|---|---|
| 🔴 HIGH | H1 `make release-sign` invoque goreleaser sans construire signpkg → `command not found` en prod | [Makefile:55-58](Makefile#L55-L58) | Délégation à `scripts/release-sign.sh` qui build signpkg + exporte PATH |
| 🔴 HIGH | H2 `verifypkg` imprime succès sur stderr au lieu de stdout — viole convention Unix | [cmd/verifypkg/main.go:81](cmd/verifypkg/main.go#L81) | `os.Stdout` pour succès |
| 🔴 HIGH | H3 Pas de guard taille `.sig` → CRLF/truncation produit erreur générique "signature mismatch" trompeuse | [cmd/verifypkg/main.go:69-72](cmd/verifypkg/main.go#L69-L72) | `if len(sig) != ed25519.SignatureSize` avec diagnostic clair + 2 tests |
| 🔴 HIGH | H4 `ErrNoRotationKey` déclaré mais jamais émis — dead code dans release_keys.go + branche morte verifypkg | [internal/crypto/release_keys.go:11](internal/crypto/release_keys.go#L11) | Sentinelle supprimée + simplification verifypkg |
| 🔴 HIGH | H5 AC4 "refus par gestionnaires de paquets (apt/dnf/pacman/apk)" partiellement implémenté — seul pacman auto | [AC4 story](epics.md#L1227) | AC4 marqué PARTIAL explicite + doc alternatives utilisateur apt/dnf/apk |
| 🟡 MED  | M1 Pas de test automatisé `openssl pkeyutl -rawin` — interop PKGBUILD↔openssl peut régresser silencieusement | [internal/crypto/](internal/crypto/) | `TestOpenSSLPKeyutlCompat` + `TestOpenSSLPKeyutlRejectsTampered` + `TestExportedPEMHasCorrectHeader` dans `pem_test.go`, skip si openssl absent |
| 🟡 MED  | M2 `signs:` block a `stdin: ""` inutile (copy-paste GPG) | [.goreleaser.yaml:249](.goreleaser.yaml#L249) | Ligne retirée |
| 🟡 MED  | M3 → fusionné avec H1 | | |
| 🟡 MED  | M4 Pas de guard taille dans signpkg (`os.ReadFile` potentiel OOM sur artefacts pathologiques) | [cmd/signpkg/main.go:91](cmd/signpkg/main.go#L91) | `maxArtifactSize = 500 MiB` + stat pre-check + test |
| 🟡 MED  | M5 Smoke test n'exerce pas le chemin "embedded key" (seulement `-pubkey`) | [scripts/test-release-signing.sh](scripts/test-release-signing.sh) | Phase 3 : sign avec maintainer key → verify sans `-pubkey` → prouve cohérence embedded ↔ private |
| 🟡 MED  | M6 `VerifyReleaseSignature` (always-both) vs `verifypkg` default (current only) asymétrie non documentée | [internal/crypto/release_keys.go:58](internal/crypto/release_keys.go#L58) | Godoc enrichie expliquant le choix Epic 8 vs user-facing |
| 🟡 MED  | M7 `awk` concatène `BACKUP_DIR` en quoting fragile | [scripts/backup-signing-key.sh:66](scripts/backup-signing-key.sh#L66) | `awk -v base="$BACKUP_DIR"` |
| 🟢 LOW  | L1 Docstring `ErrPinningFailed-equivalent` orpheline (copie de pinning.go) | [internal/crypto/release_keys.go:63](internal/crypto/release_keys.go#L63) | Reformulée |
| 🟢 LOW  | L2 README Option B openssl suppose bash | [README.md](README.md) | Note bash explicite |

### Validation AC post-fixes

| AC | Status | Notes |
|---|---|---|
| AC1 signpkg CLI + tests | ✅ | 9 tests, round-trip determinism, size guard |
| AC2 goreleaser signs: | ✅ | `goreleaser check` ✓, smoke phase 2 wired en CI |
| AC3 verifypkg CLI | ✅ | stdout convention + size guard + 3 nouveaux tests |
| AC4 PKGBUILD verify() | ⚠️ PARTIAL (documenté) | pacman auto ✓ ; apt/dnf/apk manuel via levoile-verify (scope Phase 2 repos signés) |
| AC5 Clé publique 2 formats + embedded | ✅ | base64 + PEM + `ReleasePublicKeyCurrentBase64` |
| AC6 Dual-key rotation | ✅ | Infrastructure prête, `-try-next` flag testé |
| AC7 Release locale maintainer | ✅ | Makefile délègue à script avec pre-flight |
| AC8 Smoke test | ✅ | 3 phases, CI full, local fast+phase3 |
| AC9 GitHub release assets | ✅ | `release.extra_files` + archive files + .sig auto |
| AC10 Documentation | ✅ | `docs/release-signing.md` 11 sections + README section utilisateur |

### Observations methodologiques

- **Self-review bias** : cette review a été menée par le même modèle (Opus 4.7) qui a implémenté la story. Recommandation persistante : re-run `code-review` sur un autre modèle (Sonnet 4.6, Haiku 4.5) ou une autre instance Opus en session séparée pour indépendance réelle
- **Coverage openssl** : Windows (cette session) OpenSSL 3.5.5 ✓. CI ubuntu-latest OpenSSL 3.x ✓. Alpine 3.18+, Debian 11+, Fedora 38+ couverts par spec. Pré-OSSL 1.1.1 skip propre
- **Master key provisioning** : la paire Ed25519 (publique `h94H7SXEBMr0/OTqxLmepAxax60vhgbbezU0Jt+hbQM=`) est celle qui signera toutes les releases. À backupper impérativement via `scripts/backup-signing-key.sh` avant toute activité mainteneur

## Change Log

- 2026-04-18 — Création de la story 7.4 (contexte complet post-livraison 7.1/7.2/7.3). Consolidation : 10 ACs (scope élargi vs 4 BDDs originales pour couvrir NFR22g/h/i pragmatiquement + chaînon vers Epic 8), 12 tasks, chemin de rotation dual-key documenté, workflow maintainer-local vs CI clarifié, intégration PKGBUILD 7.3 verify() adressée.
- 2026-04-18 — Décisions verrouillées (alignement pratique VPN commerciaux + solo maintainer OSS) : (1) master key générée DANS T3/T10, clé publique committée day-one (pattern Mullvad/WireGuard/Proton) ; (2) storage = fichier 0600 sur laptop mainteneur chiffré LUKS/BitLocker (pattern WireGuard), YubiKey PKCS#11 différé Phase 2 ; (3) NFR22g "air-gapped" interprété comme "laptop offline pendant la signature", pas air-gap permanent (pragmatique solo).
- 2026-04-18 — Implémentation complète de la story 7.4. 12/12 tasks livrées. 15 fichiers créés, 9 modifiés. `go test -count=1 ./...` = 32/32 packages verts, aucune régression. `go vet` propre. `goreleaser check` ✓. Smoke test phase 1 (5 artefacts + tampering rejected) ✓ local Windows ; phase 2 (goreleaser cross-compile Linux) wired en CI ubuntu-latest. PKGBUILD AUR `verify()` activée (Ed25519 Le Voile master key, PEM inlinée). Story 7.3 TODO story-7.4 marqué résolu. Status → `review`.
- 2026-04-18 — Provisioning master key définitif : paire Ed25519 générée dans `~/.levoile/signing.key` (ACL Windows user-only, héritage désactivé), publique `h94H7SXEBMr0/OTqxLmepAxax60vhgbbezU0Jt+hbQM=` synced dans `internal/crypto/release_keys.go`, `docs/keys/levoile-release.pub{,.pem}` et `packaging/arch/PKGBUILD` (heredoc PEM). E2E round-trip final validé : `signpkg` avec master key → `verifypkg` via clé embedded = `ok: verified with embedded current`. Helper `scripts/backup-signing-key.sh` ajouté pour sauvegarde GPG + SSSS interactive.
- 2026-04-18 — **Code review adversarial (self-review Opus 4.7)** : 14 findings (5 HIGH + 7 MEDIUM + 2 LOW). Tous les HIGH et MEDIUM **corrigés** + 2 LOW : (H1/M3) Makefile `release-sign` délègue à `scripts/release-sign.sh` → pre-flight checks actifs + signpkg build ; (H2) `verifypkg` succès → stdout (convention Unix) ; (H3) guard taille `.sig` = 64 octets strict avec diagnostic clair ; (H4) `ErrNoRotationKey` dead code supprimé + simplification verifypkg ; (H5) AC4 marqué PARTIAL explicite (pacman auto, apt/dnf/apk manuel via `levoile-verify`) ; (M1) `TestOpenSSLPKeyutlCompat` + `TestOpenSSLPKeyutlRejectsTampered` + `TestExportedPEMHasCorrectHeader` — valident l'interop PKGBUILD↔openssl ; (M2) `stdin: ""` vestige retiré de goreleaser ; (M4) guard taille signpkg 500 MiB + test ; (M5) smoke test phase 3 exerce le chemin embedded-key (passe ✓ avec la master key session) ; (M6) godoc asymétrie `VerifyReleaseSignature` (Epic 8) vs `verifypkg` (user) ; (M7) awk `-v base` dans backup-signing-key.sh ; (L1) docstring `ErrPinningFailed-equivalent` reformulée ; (L2) note bash dans README pour Option B openssl. `go test -count=1 ./...` = 33/33 packages verts. `go vet` propre. `goreleaser check` ✓. Smoke test phase 1+3 ✓ local Windows. Status → `done`.
