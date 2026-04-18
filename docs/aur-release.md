# Publication AUR — procédure mainteneur

Story 7.3 — Automatisation de la publication du paquet AUR `levoile` à chaque
release GitHub, via le workflow [.github/workflows/aur-publish.yml](../.github/workflows/aur-publish.yml).

Ce document décrit les **prérequis humains** : création de comptes, génération
de clés, provisionnement des secrets GitHub, et premier push manuel du repo AUR.
Une fois cette procédure faite **une seule fois**, chaque `gh release create`
pousse automatiquement un commit signé GPG sur `aur.archlinux.org/levoile.git`.

---

## 1 — Compte AUR

1. Créer un compte sur https://aur.archlinux.org/register/
2. Nom d'utilisateur recommandé : `velia-the-veil` (cohérence avec GitHub).
3. Activer 2FA si disponible côté AUR (à date : non — conservation rigoureuse du mot de passe).

## 2 — Paire SSH dédiée AUR

**Ne pas réutiliser** la clé SSH personnelle d'Akerimus : la clé sera copiée dans
un secret GitHub Actions, donc potentiellement lisible par tout admin du repo.

```bash
# Sur la machine de confiance (pas dans un container).
ssh-keygen -t ed25519 -C "aur-bot@levoile" -f ~/.ssh/aur_ed25519 -N ''

# → ~/.ssh/aur_ed25519       (privée  — à coller dans GitHub secret AUR_SSH_PRIVATE_KEY)
# → ~/.ssh/aur_ed25519.pub   (publique — à coller dans AUR account settings)
```

Ajouter la clé publique sur https://aur.archlinux.org/account/ → « SSH Public Key »

## 3 — `known_hosts` AUR (pinning pré-vérifié)

**Ne JAMAIS** faire `ssh-keyscan aur.archlinux.org >> known_hosts` en runtime
dans le workflow — premier run MITM-exposable. On pré-calcule offline :

```bash
# 1. Scan local depuis une machine de confiance (pas un cloud runner).
ssh-keyscan -t ed25519 aur.archlinux.org > /tmp/aur_known_hosts

# 2. Afficher le fingerprint local.
ssh-keygen -lf /tmp/aur_known_hosts
#   → "256 SHA256:<hash> aur.archlinux.org (ED25519)"
```

**⚠️  VÉRIFICATION MANUELLE OBLIGATOIRE AVANT DE CONTINUER — NE PAS SAUTER :**

Ce guide **ne documente volontairement aucun fingerprint de référence** : la
valeur courante doit être récupérée en direct depuis une source autoritative
le jour du provisioning. Si le repo AUR migre vers un nouveau host ou si la
clé hôte est rotatée, un fingerprint hardcodé dans ce document deviendrait
faux et laisserait passer un MITM silencieux.

**Sources autoritatives à recroiser (au moins 2) :**

- Wiki Arch officiel (https → vérifier cert TLS) :
  https://wiki.archlinux.org/title/AUR_submission_guidelines#Authentication
- Annonce officielle du mainteneur AUR (GitLab ou mailing list archlinux-aur)
- Demande directe sur `#archlinux-aur` IRC (libera.chat) à un membre reconnu

Si les sources divergent → arrêter le provisioning, investiguer.

Le contenu complet de `/tmp/aur_known_hosts` (une seule ligne
`aur.archlinux.org ssh-ed25519 AAAA...`) devient le secret GitHub
`AUR_SSH_KNOWN_HOSTS`.

## 4 — Sous-clé GPG dédiée release AUR

**Séparation des rôles** : la clé GPG de signature de commits AUR est :
- Distincte de la master key Ed25519 de signature des releases (qui vit sur
  une machine air-gapped ou YubiKey — NFR22g).
- Distincte de la clé GPG perso d'Akerimus (pour limiter le blast radius d'une
  fuite de secret GitHub).

Créer une sous-clé dédiée ou une clé indépendante :

```bash
gpg --quick-generate-key "Velia the Veil (AUR bot) <aur-release@levoile.dev>" \
    ed25519 sign 2y

# Récupérer l'ID (long format) :
gpg --list-secret-keys --keyid-format=long

# Exporter la clé privée (à coller dans AUR_GPG_PRIVATE_KEY) :
gpg --armor --export-secret-keys <KEYID> > /tmp/aur-bot-priv.asc

# Exporter la clé publique et la publier (site, PRD, optionnellement dans le
# paquet) pour que l'on puisse vérifier nos commits AUR :
gpg --armor --export <KEYID> > /tmp/aur-bot-pub.asc
```

Choisir une passphrase forte → devient le secret `AUR_GPG_PASSPHRASE`.

**Publication de la clé publique** : ajouter `/tmp/aur-bot-pub.asc` dans
`docs/keys/aur-bot.pub.asc` (committé dans le repo principal) pour que n'importe
qui puisse vérifier les commits du repo AUR.

## 5 — Secrets GitHub à provisionner

Dans https://github.com/velia-the-veil/le_voile/settings/secrets/actions, créer :

| Nom | Contenu |
|---|---|
| `AUR_SSH_PRIVATE_KEY` | Contenu complet de `~/.ssh/aur_ed25519` (incluant `-----BEGIN`/`-----END`) |
| `AUR_SSH_KNOWN_HOSTS` | Contenu de `/tmp/aur_known_hosts` (une seule ligne `aur.archlinux.org ssh-ed25519 …`) |
| `AUR_GPG_PRIVATE_KEY` | Contenu ASCII-armor de `/tmp/aur-bot-priv.asc` |
| `AUR_GPG_PASSPHRASE` | Passphrase de la clé GPG (caractère par caractère) |

**Vérifier** que les 4 secrets sont bien visibles dans la liste (masqués par
des points) **avant** de déclencher le workflow.

## 6 — Premier push manuel du repo AUR

Le workflow clone `ssh://aur@aur.archlinux.org/levoile.git` — le repo AUR doit
**déjà exister**. Pour le créer, on fait manuellement le **premier** push :

```bash
# Depuis la machine de confiance, avec la clé SSH AUR chargée :
eval "$(ssh-agent -s)"
ssh-add ~/.ssh/aur_ed25519

# Créer le squelette du repo AUR — un PKGBUILD version 0.0.1 minimal suffit
# (l'Action prendra le relais dès la v0.1.0 release).
git clone ssh://aur@aur.archlinux.org/levoile.git /tmp/aur-init
cd /tmp/aur-init

cp "$REPO_ROOT/packaging/arch/PKGBUILD"        PKGBUILD
cp "$REPO_ROOT/packaging/arch/levoile.install" levoile.install

# 1. Remplacer les placeholders par une version publiée (v0.0.1 supposée).
sed -i 's/^pkgver=.*/pkgver=0.0.1/' PKGBUILD

# 2. Calculer les SHA256 réels des .deb v0.0.1 publiés sur GitHub release.
curl -fL -o /tmp/amd64.deb https://github.com/velia-the-veil/le_voile/releases/download/v0.0.1/levoile_0.0.1_amd64.deb
curl -fL -o /tmp/arm64.deb https://github.com/velia-the-veil/le_voile/releases/download/v0.0.1/levoile_0.0.1_arm64.deb
SHA_AMD64=$(sha256sum /tmp/amd64.deb | awk '{print $1}')
SHA_ARM64=$(sha256sum /tmp/arm64.deb | awk '{print $1}')
sed -i "s|REPLACE_ME_AMD64|$SHA_AMD64|" PKGBUILD
sed -i "s|REPLACE_ME_ARM64|$SHA_ARM64|" PKGBUILD

# 3. ⚠️  Régénérer .SRCINFO LOCALEMENT via makepkg — NE PAS réutiliser le
#    .SRCINFO du repo principal (template avec placeholders). Le `.SRCINFO`
#    affiché par AUR est celui-ci ; un format subtilement cassé ou obsolète
#    rendrait la page paquet illisible jusqu'au prochain push par l'Action.
makepkg --printsrcinfo > .SRCINFO

# 4. Vérifier que makepkg accepte le PKGBUILD complet avant push.
makepkg --verifysource

git add PKGBUILD .SRCINFO levoile.install
git commit -S -m "Initial AUR release v0.0.1"
git push origin master
```

Vérifier sur https://aur.archlinux.org/packages/levoile que le paquet apparaît.

## 7 — Premier test automatique : dry-run via `workflow_dispatch`

1. https://github.com/velia-the-veil/le_voile/actions → sélectionner
   « aur-publish » → **Run workflow**.
2. Remplir :
   - `version` : `0.0.1` (la version qu'on vient de pousser manuellement).
   - `dry_run` : **coché** (valeur par défaut).
3. Le workflow doit :
   - Télécharger les .deb v0.0.1 depuis GitHub.
   - Calculer les SHA256.
   - Éditer `PKGBUILD` + régénérer `.SRCINFO`.
   - Importer les secrets SSH/GPG.
   - Cloner le repo AUR, copier les fichiers, détecter qu'ils sont identiques
     au HEAD AUR, et exit 0 sans push.
   - Logguer « aucun changement — exit propre. »
4. Si tout passe → Action validée. Si échec → consulter les logs, vérifier les
   secrets.

## 8 — Publication réelle (automatique)

Une fois le dry-run OK, chaque `gh release create vX.Y.Z` déclenche le workflow
en mode production : le PKGBUILD est mis à jour, signé GPG, poussé sur AUR.

L'utilisateur final peut alors :

```bash
yay -S levoile
# ou
paru -S levoile
```

## 9 — Rollback d'une release AUR

En cas de release GitHub retirée ou release AUR cassée :

```bash
git clone ssh://aur@aur.archlinux.org/levoile.git /tmp/aur-rollback
cd /tmp/aur-rollback
git log --oneline  # identifier le commit à retenir

# Revenir à la version précédente :
git reset --hard <commit_previous_good>
git push --force-with-lease origin master
```

Pousser ensuite une nouvelle release GitHub corrigée — le workflow prendra le
relais automatiquement.

## 10 — Monitoring

- **Flux RSS AUR** : https://aur.archlinux.org/rss/packages/levoile — à
  surveiller pour détecter toute publication NON initiée par nous (indicateur
  fort de compromission du compte AUR).
- **Signatures GPG** : chaque commit AUR doit afficher l'ID de notre clé dans
  `git log --show-signature`. Si un commit n'est pas signé ou signé par une
  autre clé, alerter immédiatement → rotation SSH + GPG + passwords.

## Références

- [Story 7.3](../_bmad-output/implementation-artifacts/7-3-pkgbuild-aur-publication-github-action.md)
- [Workflow Action](../.github/workflows/aur-publish.yml)
- [PKGBUILD](../packaging/arch/PKGBUILD) + [levoile.install](../packaging/arch/levoile.install)
- PRD NFR22i — signing commits GPG obligatoire
- https://wiki.archlinux.org/title/AUR_submission_guidelines
