# Release Signing — guide mainteneur

Story 7.4 — signature Ed25519 de tous les paquets de distribution.

Ce document est la référence canonique pour la génération, le stockage, la
rotation et l'utilisation de la **master key Ed25519** qui signe les releases
Le Voile. Il est destiné au mainteneur (Akerimus), pas aux utilisateurs
finaux (eux lisent la section correspondante du README).

---

## Table des matières

1. [Génération initiale de la master key](#1-génération-initiale-de-la-master-key)
2. [Storage air-gapped / YubiKey](#2-storage-air-gapped--yubikey)
3. [Sauvegardes hors ligne](#3-sauvegardes-hors-ligne)
4. [Procédure de release](#4-procédure-de-release)
5. [Rotation 24 mois (NFR22h)](#5-rotation-24-mois-nfr22h)
6. [Procédure d'urgence — compromission de clé](#6-procédure-durgence--compromission-de-clé)
7. [Vérification utilisateur final](#7-vérification-utilisateur-final)
8. [Checklist pre-release](#8-checklist-pre-release)
9. [Threat model](#9-threat-model)
10. [Références NFR + RFC](#10-références-nfr--rfc)
11. [Chaînon Epic 8 — auto-update](#11-chaînon-epic-8--auto-update)

---

## 1. Génération initiale de la master key

La clé Ed25519 master est générée une seule fois par période de rotation
(24 mois, voir §5). Deux chemins équivalents :

### Via `cmd/genkey` (recommandé — cohérent avec cmd/genregistry)

```bash
go run ./cmd/genkey -out "$HOME/.levoile/signing" -pem
# Produit :
#   $HOME/.levoile/signing.key     (base64 priv, mode 0600)
#   $HOME/.levoile/signing.pub     (base64 pub)
#   $HOME/.levoile/signing.pub.pem (PKIX PEM pour openssl)
```

### Via `openssl genpkey` (alternative portable)

```bash
openssl genpkey -algorithm Ed25519 -out signing.pem
openssl pkey -in signing.pem -pubout -out signing.pub.pem
# Export en base64 brut pour cmd/signpkg :
openssl pkey -in signing.pem -text -noout | grep -A3 priv: | tail -3 \
    | tr -d ': \n' | xxd -r -p | base64 > signing.key
chmod 0600 signing.key signing.pem
```

### Activation dans le repo

Une fois la clé générée :

1. Copier `signing.pub` + `signing.pub.pem` dans `docs/keys/levoile-release.pub{,.pem}`
2. Bumper `ReleasePublicKeyCurrentBase64` dans `internal/crypto/release_keys.go`
   avec le contenu de `signing.pub` (base64 32 octets)
3. Mettre à jour le heredoc PEM dans `packaging/arch/PKGBUILD` (fonction `verify()`)
4. Commit + tag de la release contenant la nouvelle clé

**Ne jamais committer `signing.key` / `signing.pem` / `signing.pub` base64 privé.**
`.gitignore` bloque déjà `signing.key`, `*.pem` non-publics, mais rester vigilant.

---

## 2. Storage air-gapped / YubiKey

### MVP — laptop mainteneur chiffré (Phase 1)

- Laptop dédié ou partitionné (disque système chiffré LUKS Linux / BitLocker Windows / FileVault macOS)
- `~/.levoile/signing.key` mode 0600 (Unix) ou ACL restrictive au user (Windows)
- **Débrancher le réseau pendant la signature** : `make release-sign` affiche un
  rappel. Débrancher wifi + ethernet physiquement avant d'exécuter goreleaser.
- Rebrancher seulement après que `dist/` soit prêt à uploader.
- Ne jamais déverrouiller la clé sur une machine qui tourne avec des LLM actifs,
  des extensions de navigateur non-auditées, ou des processus non-reconnus.

### Phase 2 — YubiKey 5 PIV (optionnel, non implémenté dans 7.4)

- YubiKey 5 series firmware ≥ 5.2.3 supporte Ed25519 dans le slot 9c PIV
- Signature via PKCS#11 : library `github.com/ThalesIgnite/crypto11` OU
  wrapper cli `yubico-piv-tool --slot 9c --algorithm Ed25519 --sign`
- La clé privée ne quitte JAMAIS le HSM — même en cas de compromission complète
  du laptop, un attaquant ne peut pas exfiltrer la clé
- Adapter `scripts/release-sign.sh` pour appeler `yubico-piv-tool` au lieu de
  lire `signing.key` depuis le disque
- Upgrade à planifier quand Le Voile aura un budget ops dédié ou passera
  multi-mainteneur

### Comparatif avec d'autres VPN

| Projet | Storage | Échelle |
|---|---|---|
| WireGuard | Laptop dédié Jason Donenfeld | Solo (similaire Le Voile) |
| Mullvad | YubiKey + équipe ops | Commerciale |
| ProtonVPN | HSM cloud | Commerciale |
| Tailscale | KMS cloud | Commerciale |

Le Voile = modèle WireGuard pour Phase 1, upgrade vers Mullvad-like en Phase 2.

---

## 3. Sauvegardes hors ligne

Une perte de la master key = impossibilité de signer de nouvelles releases +
obligation de rotation d'urgence (NFR22h). Stratégie multi-copies :

### Sauvegarde chiffrée primaire

```bash
gpg --symmetric --cipher-algo AES256 --output signing.key.gpg signing.key
# Passphrase forte stockée dans gestionnaire offline (KeePassXC, Bitwarden local)
```

Stocker `signing.key.gpg` sur :
- Clé USB chiffrée #1 (domicile mainteneur)
- Clé USB chiffrée #2 (coffre-fort / autre lieu physique)

### Sauvegarde Shamir's Secret Sharing (recommandé)

```bash
# Découpe en 3 parts, 2 suffisent à reconstruire.
ssss-split -t 2 -n 3 -w "levoile-master-2026" < signing.key
# Répartir les 3 parts dans 3 lieux physiques distincts (coffre, ami de confiance
# avec enveloppe scellée, boîte postale louée).
# Reconstruction : ssss-combine -t 2
```

### Test annuel de restauration

Tous les 12 mois, restaurer depuis au moins une sauvegarde dans un
environnement éphémère (VM Linux live), vérifier round-trip sign/verify,
puis détruire la VM. Sans test, une sauvegarde corrompue = perte silencieuse.

---

## 4. Procédure de release

### Pré-requis

- Laptop mainteneur avec `signing.key` présent
- Tag git annoté `vX.Y.Z` sur le commit à releaser
- Tests verts en local + CI
- Changelog à jour

### Exécution

```bash
# 1. S'assurer que le working tree est propre
git status

# 2. Tagger si pas déjà fait
git tag -a -s v1.2.3 -m "release: v1.2.3"

# 3. (recommandé) Débrancher le réseau
# 4. Lancer la release
export LEVOILE_SIGNING_KEY_PATH="$HOME/.levoile/signing.key"
make release-sign
# équivalent à : bash scripts/release-sign.sh
```

Le script exécute :

1. Pre-flight : working tree clean, tag présent, tests verts, security gates (go vet + govulncheck + gosec)
2. Build goreleaser (cross-compile Windows + Linux amd64/arm64)
3. Signature Ed25519 de chaque artefact via `signs:` → `<artefact>.sig`
4. Signature de `checksums.txt` → `checksums.txt.sig`
5. Publication GitHub release avec tous les `.sig` + clé publique en assets

### Après signature

- Rebrancher le réseau
- Vérifier sur github.com/velia-the-veil/le_voile/releases que les `.sig` sont bien présents
- Le workflow `.github/workflows/aur-publish.yml` se déclenche automatiquement
  (story 7.3) → met à jour PKGBUILD + SHA256 + pousse sur AUR

---

## 5. Rotation 24 mois (NFR22h)

La master key doit être rotée tous les 24 mois. La transition utilise un
mécanisme **dual-signature** de 6 mois pour ne casser aucun client.

### Chronologie

```
T=0           T=23.5m       T=24m                        T=29.5m         T=30m
│             │             │                            │               │
│ clé N       │ généré N+1  │ première release dual-sig  │ dernière       │ release
│ signe tout  │ publié next │ (N + N+1)                  │ dual-sig       │ mono N+1
│             │ dans repo   │ 6 mois de dual-sig         │                │
└─────────────┴─────────────┴────────────────────────────┴───────────────┴─────────
```

### Étapes concrètes

**Mois T=23.5 — préparer**

1. Générer la clé N+1 : `go run ./cmd/genkey -out "$HOME/.levoile/signing-N+1" -pem -force`
2. Bumper `ReleasePublicKeyNextBase64` dans `internal/crypto/release_keys.go` avec
   la clé publique N+1
3. Publier `docs/keys/levoile-release-next.pub{,.pem}`
4. Release X.Y intermédiaire : contient les 2 clés mais signe encore mono (clé N)

**Mois T=24 — démarrer dual-sign**

1. Modifier `.goreleaser.yaml` `signs:` → 2 entrées (id ed25519-master-current + id ed25519-master-next)
2. Passer `LEVOILE_SIGNING_KEY_PATH` + `LEVOILE_SIGNING_KEY_NEXT_PATH` à `make release-sign`
3. Chaque artefact reçoit 2 signatures : `<art>.sig` (clé N) + `<art>.next.sig` (clé N+1)
4. Les clients avec la clé N embarquée continuent de valider via `.sig`
5. Les nouveaux clients (clé N+1) valident via `.next.sig` avec `-try-next`

**Mois T=29.5 — basculer**

1. Bumper `ReleasePublicKeyCurrentBase64` = ex-N+1, `ReleasePublicKeyNextBase64 = ""`
2. Retirer le 2e bloc `signs:` dans `.goreleaser.yaml`
3. Release finale mono-sig clé N+1
4. Détruire `signing.key` de la clé N via `shred -u` (Linux) ou suppression sécurisée

**Mois T=30 — mono N+1**

- Les clients upgradés depuis T=24 ont embarqué la clé N+1 comme current
- Les clients qui n'ont jamais mis à jour depuis >6 mois avant T=24 ne peuvent
  plus vérifier les nouvelles releases → invités à télécharger manuellement
  la nouvelle version depuis un canal de confiance (README redirige)

---

## 6. Procédure d'urgence — compromission de clé

Si la master key Ed25519 est leakée (laptop volé + déverrouillé, backup
exposé, phishing) :

1. **Rotation immédiate** : générer une nouvelle clé, publier comme `Next`,
   release dual-sign dans les 48h
2. **Avis CVE** : publier un avis de sécurité sur le repo + canaux habituels
3. **Révocation** : ajouter la clé compromise à une liste de révocation
   embarquée (champ `RevokedReleaseKeys []string` dans `release_keys.go` à
   créer en T6 de la story de rotation)
4. **Rollback des releases** potentiellement frauduleuses signées avec la
   clé compromise, à republier signées par la nouvelle
5. **Post-mortem** public : expliquer l'incident, les mitigations, les
   leçons — transparence = confiance

---

## 7. Vérification utilisateur final

Copié dans le README racine. Trois chemins :

### A. `levoile-verify` (bundled dans les archives)

```bash
# Linux / macOS
tar xf LeVoile_1.2.3_linux_amd64.tar.gz
cd LeVoile_1.2.3_linux_amd64
./levoile-verify ../LeVoile_1.2.3_linux_amd64.tar.gz ../LeVoile_1.2.3_linux_amd64.tar.gz.sig
# Sortie : ok: ...tar.gz (verified with embedded current)
```

### B. `openssl pkeyutl` (toute distro avec OpenSSL 1.1.1+)

```bash
curl -LO https://github.com/velia-the-veil/le_voile/releases/download/v1.2.3/levoile_1.2.3_amd64.deb
curl -LO https://github.com/velia-the-veil/le_voile/releases/download/v1.2.3/levoile_1.2.3_amd64.deb.sig
curl -LO https://github.com/velia-the-veil/le_voile/releases/download/v1.2.3/levoile-release.pub.pem

openssl pkeyutl -verify -pubin -inkey levoile-release.pub.pem \
    -rawin -in levoile_1.2.3_amd64.deb \
    -sigfile levoile_1.2.3_amd64.deb.sig
# Sortie : "Signature Verified Successfully"
```

### C. AUR (Arch Linux) — automatique

`yay -S levoile` → `makepkg` appelle `verify()` du PKGBUILD → signature
vérifiée avant l'installation. Aucune action utilisateur requise.

### Signature invalide — que faire ?

1. **Ne pas installer**
2. Vérifier avec un deuxième canal (miroir, ami, VPN) pour écarter un MITM local
3. Ouvrir une issue sur le repo — aider à détecter une compromission

---

## 8. Checklist pre-release

Imprimer cette liste avant chaque release :

- [ ] Changelog à jour + commit + signature GPG du commit
- [ ] Tag `vX.Y.Z` annoté + signé GPG (`git tag -a -s`)
- [ ] `go test -race -count=1 ./...` verts
- [ ] `go vet ./...` propre
- [ ] `govulncheck ./...` aucune vuln severity ≥ medium
- [ ] `gosec -severity medium ./...` propre
- [ ] `bash scripts/test-release-signing.sh` smoke OK (validation pipeline)
- [ ] `signing.key` mode 0600 vérifié
- [ ] Réseau débranché (recommandé)
- [ ] `make release-sign` lancé, tous les `.sig` produits
- [ ] GitHub release contient : binaires, `.sig`, `checksums.txt`, `checksums.txt.sig`, `levoile-release.pub{,.pem}`
- [ ] AUR workflow triggered et passe (vérifier GitHub Actions)
- [ ] Téléchargement manuel depuis un autre poste + vérification réussie
- [ ] Rebrancher le réseau + verrouiller le disque

---

## 9. Threat model

| Attaquant | Vecteur | Impact | Mitigation |
|---|---|---|---|
| Script-kiddie upload .deb sur mirror | SHA256 différent, `.sig` absente | Aucun — install refusée | NFR20 verify() PKGBUILD, `levoile-verify` |
| Attaquant contrôlant GitHub Releases | Peut changer les binaires + sigs | `.sig` produite avec clé **pirate** → verify() rejette | Clé publique embarquée dans le binaire (pas downloadable) |
| Attaquant MITM user↔GitHub | Intercepte téléchargement HTTPS | Bloqué par TLS GitHub ; mais même si bypass, `.sig` rejette | TLS + signature indépendante |
| Leak accidentel de `signing.key` dans un commit | Commit public exposant la privée | Catastrophique — peut signer des releases frauduleuses | `.gitignore`, hooks pre-commit (à ajouter Phase 2), scanning secrets |
| Vol laptop non chiffré | Accès direct à `signing.key` | Catastrophique | FDE obligatoire (LUKS/BitLocker/FileVault) |
| Compromission compte GitHub | Peut forger tag + description release | Peut pas forger `.sig` sans la clé privée | NFR22i : 2FA hardware YubiKey, GPG commits, branch protection |
| Ingénierie sociale du mainteneur | Phishing, fausse PR "fix CI secret" | Pas de secret CI à exfiltrer (NFR22g) | Pas de master key en CI, jamais |
| Compromission machine CI GitHub Actions | Accès aux secrets CI | Secrets CI ne contiennent pas la master key | Architecture NFR22g |
| Malware sur laptop mainteneur | Keylogger + capture clavier | Peut voler la key au déverrouillage | YubiKey (Phase 2), séparation machines dev/signing |
| Attaquant état-nation, ressources illimitées | Brute force Ed25519 | Aucun (Ed25519 = 128-bit security) | Néant — pas de menace réaliste court terme |

---

## 10. Références NFR + RFC

- **NFR22g** — Master key storée exclusivement air-gapped / YubiKey. Rotation 24 mois
- **NFR22h** — Chaîne de confiance client avec clé actuelle + next. Dual-sig 6 mois
- **NFR22i** — Compte GitHub protégé 2FA hardware, commits signés GPG, branch protection
- **NFR9c** — Comparaisons crypto timing-constant (ed25519.Verify l'est natif)
- **FR20b** — Signature Ed25519 de tous les paquets (.exe, .deb, .rpm, .apk)
- **RFC 8032** — Edwards-Curve Digital Signature Algorithm (Ed25519). Signatures deterministic, 64 octets
- **RFC 7468** — Textual Encodings of PKIX PEM
- **OpenSSL pkeyutl manpage** — flag `-rawin` pour Ed25519 (OpenSSL ≥ 1.1.1)

---

## 11. Chaînon Epic 8 — auto-update

Story 8.2 (auto-update) consommera la même infrastructure :

- `crypto.VerifyReleaseSignature(binary, sig)` valide le binaire téléchargé
- Essaie `ReleasePublicKeyCurrent` puis `ReleasePublicKeyNext` si configurée
- Refus d'appliquer une update si la signature ne matche aucune clé trusted
- Log syslog/Event Log sans leaker d'information (NFR22a)

Implémenté dans 7.4 : `internal/crypto/release_keys.go` expose l'API,
`TestVerifyReleaseSignature_WithEmbeddedKeyRejectsForeign` prouve le round-trip.
Epic 8 n'a qu'à consommer — aucune duplication de logique crypto.
