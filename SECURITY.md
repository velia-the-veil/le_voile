# SECURITY

Politique de sécurité et journal des durcissements pour **Le Voile** (VPN client/relais Go).
Document vivant — mis à jour à chaque passe d'audit. Dernière révision : **2026-04-19**.

## Signaler une vulnérabilité

Pour signaler une faille de sécurité, ne pas ouvrir de ticket GitHub public.
Envoyer un rapport chiffré GPG à `security@levoile.dev` (clé publique dans
[`docs/keys/`](docs/keys/)) ou contact direct du mainteneur `velia-the-veil`
via GitHub DM. Engagement de réponse : accusé de réception sous 72 h, fix ou
mitigation proposé sous 30 jours pour les classes HIGH/CRITICAL.

## Modèle de menace

Le Voile protège la confidentialité du trafic utilisateur face à trois
classes d'adversaires :

1. **Réseau local hostile** (Wi-Fi captif, FAI, MITM actif). Le tunnel
   QUIC+TLS 1.3 masque le contenu. Le kill-switch nftables/WFP empêche
   toute fuite si le tunnel tombe.
2. **Adversaire supply-chain** (compromission CI, relais, clé release).
   Mitigations : clé master Ed25519 hors-CI (NFR22g), binaires signés,
   registry signé, GitHub Actions pinnées par SHA, anti-downgrade persistant.
3. **Attaquant local non-admin** (autre user sur la machine cliente). Le
   serveur HTTP UI bind loopback only, Origin/Host vérifiés, prefs en 0700.
   **Hors scope** : attaquant même user (process du même compte OS) —
   aucun moyen cryptographique fiable d'isoler deux process partageant
   un UID sans entitlements OS spécifiques.

### Hors scope

- Endpoints renforcés sur `127.0.0.1` n'assurent pas la confidentialité
  contre un attaquant root/admin local.
- Les attaques par canaux auxiliaires (timing, cache, power) sur la clé
  Ed25519 ne sont pas mitigées côté relai sans HSM.
- L'OS hôte et son noyau sont considérés non-compromis.

## Durcissements appliqués (2026-04-19)

Suite à un audit interne, les findings suivants ont été corrigés.
Chaque ligne indique l'ID du finding, la mitigation, et le fichier
principal touché.

| ID | Finding | Mitigation | Emplacement |
|----|---------|------------|-------------|
| C1 | Pas d'anti-downgrade persistant | `max_seen_version.txt` persistant (0600), rejet `ErrDowngradeRejected` si release candidate < version max vue | [internal/updater/rollback.go](internal/updater/rollback.go), [updater.go](internal/updater/updater.go) |
| C2 | HTTP `/api/*` sans vérif Origin/Host | Middleware `originGuard` : rejette toute requête dont `Host`, `Origin` ou `Referer` pointe hors loopback (bloque DNS rebinding + page web malveillante) | [internal/ui/httpserver.go](internal/ui/httpserver.go) |
| C3 | Windows pipe DACL permissive + empty `req.Auth` accepté | Audit-log stderr pour chaque action mutante sans Auth + gate `LEVOILE_IPC_STRICT_AUTH=1` qui rejette (Status=error, `auth_required`). Limite architecturale du même-user documentée ci-dessous. | [internal/ipchandler/handler.go](internal/ipchandler/handler.go) |
| C4 | Session token relay sans nonce | Champ `Nonce` (16 bytes hex aléatoires) ajouté à `SessionTokenPayload` ; couvert par la signature Ed25519 ; deux tokens pour la même IP sont garantis distincts → plumbing en place pour cache de replay server-side. | [internal/relay/verify_handler.go](internal/relay/verify_handler.go) |
| C5 | Race TUN↔firewall dans `recoverTUN` | Réordonné : `firewall.Activate` AVANT `routing.Setup`. Le flush+replace atomique (nftables/WFP) ferme la fenêtre microscopique où le routing pouvait pointer sur un TUN sans règles à jour. | [internal/service/service.go](internal/service/service.go) |
| C6 | Extension navigateur : permissions `<all_urls>` + bypass sans `proxyAlive` | **Extension supprimée intégralement** (WebRTC couvert par les policies firefox/chromium + SysProxy + STUN watchdog). Voir [commit 879fc18](https://github.com/velia-the-veil/le_voile/commit/879fc18) | — |
| C7 | install.sh relais distribué sans signature | `install.sh` inclus comme release asset, co-uploadé avec `install.sh.sig` via `signs: artifacts: all`. Wrapper `deploy/install-bootstrap.sh` télécharge + vérifie (pubkey pinning SHA-256) + exécute. En-tête de `install.sh` documente la procédure. | [deploy/install.sh](deploy/install.sh), [deploy/install-bootstrap.sh](deploy/install-bootstrap.sh), [.goreleaser.yaml](.goreleaser.yaml) |
| H1 | Pas de révocation de clé release | `RevokedReleaseKeysBase64` : liste opt-in consultée avant succès de signature ; `ErrReleaseKeyRevoked` distinct de la signature invalide pour alerting. | [internal/crypto/release_keys.go](internal/crypto/release_keys.go) |
| H2 | Systemd relay hardening limité | Ajout `PrivateDevices`, `ProtectKernelTunables/Modules/Logs`, `ProtectControlGroups`, `ProtectHostname`, `ProtectClock`, `RestrictAddressFamilies`, `RestrictNamespaces/Realtime/SUIDSGID`, `LockPersonality`, `SystemCallFilter=@system-service`, `SystemCallArchitectures=native`. | [deploy/levoile-relay.service](deploy/levoile-relay.service) |
| H3 | Dossier prefs UI en 0755 | Changé en 0700 — autre user local ne peut plus lister le dossier ni lire/modifier `ui-prefs.json`. | [internal/ui/prefs.go](internal/ui/prefs.go) |
| H4 | Quotas relais agrégés par IP (bypassables par rotation) | Bucket supplémentaire agrégé par `/24` (IPv4) ou `/64` (IPv6), cap = `4 × per-IP`. Rotation dans un subnet unique atteint le cap agrégé. Légitime NAT multi-user conservé via le multiplicateur `4`. | [internal/relay/bandwidth_limiter.go](internal/relay/bandwidth_limiter.go) |
| H5 | SessionID dérivé de `IPHash@UnixNano` (brute-forceable) | `TunnelSession.ID` = 32 hex random (128 bits) minté à l'ouverture de stream via `crypto/rand`. `sessionKey` préfère `ID`, fallback legacy uniquement pour test literals. | [internal/relay/tunnel_handler.go](internal/relay/tunnel_handler.go), [nat_table.go](internal/relay/nat_table.go) |
| H6 | IPv6 leak togglable runtime sans audit | Audit trail stderr à chaque `SetAllowIPv6Leak`. `LEVOILE_LOCK_SECURITY_POLICY=1` transforme le toggle IPC en no-op (`ErrSecurityPolicyLocked`) : recovery out-of-band via `config.toml` + restart. | [internal/service/service.go](internal/service/service.go) |
| H7 | Wintun : SHA256 du ZIP seulement | Post-extraction, vérification Authenticode via `signtool` (Windows) ou `osslsigncode` (Linux/CI) avec fail-closed si échec. Fallback warn-only si aucun outil dispo. | [scripts/fetch-wintun.sh](scripts/fetch-wintun.sh) |
| H8 | Actions GitHub pinnées par tag mobile | Toutes les actions (`checkout`, `setup-go`, `goreleaser-action`, `upload-artifact`) pinnées par SHA 40-hex avec tag en commentaire pour lisibilité. | [.github/workflows/release.yml](.github/workflows/release.yml), [aur-publish.yml](.github/workflows/aur-publish.yml) |
| H10 | Pas de rate-limit per-IP sur `/verify` | `IPLimitMiddleware` ajouté devant `/verify` : cap concurrence `IPLimiter.maxPer=200` par client. Retourne `429` (distinct du `503` global). | [internal/relay/middleware.go](internal/relay/middleware.go), [server.go](internal/relay/server.go) |

## Limites connues — defense-in-depth plutôt que certitude

Certaines classes d'attaque n'ont pas de solution cryptographiquement
solide dans le modèle actuel. Documentées ici pour éviter la fausse
confiance et tracer le chemin de remédiation future.

### C3 — Attaquant du même user OS (Windows)

Le DACL du named pipe `\\.\pipe\levoile` autorise tout `Interactive User`
en `GRGW` (lecture+écriture). Sans cette ouverture, la UI (qui tourne en
user context) ne peut pas parler au service (SYSTEM). Deux process du
même user — UI légitime et malware — partagent le DACL : Windows n'a
pas de primitive OS qui distingue "binaire X" de "binaire Y" dans le
même compte utilisateur sans Integrity Levels (AppContainer), qui
nécessite une restructuration complète du binaire.

**Mitigation appliquée** :
- Chaque action mutante sans `req.Auth` émet une ligne
  `SECURITY AUDIT: mutating IPC without req.Auth action=X` sur stderr
  (journald / Event Log) — un SIEM détecte les patterns anormaux.
- `LEVOILE_IPC_STRICT_AUTH=1` rejette ces actions : déploiement-par-déploiement,
  une fois que la UI ship un token ctl/ui valide, les opérateurs
  flippent ce flag pour fermer la voie empty-Auth.

**Fix cryptographique futur** : token `ui.token` distribué par le service
au démarrage dans un emplacement lisible uniquement par l'user interactif,
UI lit et envoie dans `req.Auth`. Ne résout PAS l'attaquant même-user
(qui peut aussi lire le token), mais relève la barre et rend le contournement
détectable. Estimé 1 jour + tests Windows dédiés.

### H9 — wireguard-go sans tag stable

`go.mod` consomme une pseudo-version `v0.0.0-20250521234502-f333402bd9cb`.
Le SHA est pinné donc pas de supply-chain via tag mobile, mais l'absence
de release tagguée par upstream signifie aucune garantie formelle de
qualité. Surveillé sur [git.zx2c4.com/wireguard-go](https://git.zx2c4.com/wireguard-go) ;
upgrade dès qu'un `v0.1.0` sort. Non-bloquant.

### Attaquant root/admin local

Un attaquant disposant de privilèges root (Linux) ou Administrators
(Windows) sur la machine peut contourner toutes les mitigations : lire
la clé privée de signature, modifier le binaire du service, désactiver
le firewall hors-Le-Voile. Aucune mitigation VPN n'est pertinente à ce
niveau de compromission — l'attaquant contrôle déjà le système.

## Politique de rotation des clés

- **Clé master de signature release** (Ed25519, `ReleasePublicKeyCurrentBase64`)
  — rotation tous les 24 mois (NFR22h). Pendant la fenêtre dual-signature,
  `ReleasePublicKeyNextBase64` est renseignée, les binaires sont double-signés.
- **Clé de signature relay** (Ed25519, `signing.key` per-relais) — rotation
  annuelle ou sur compromission. Le registry v2 expose `signing_key_id` pour
  permettre la rotation sans downtime.
- **Clé TLS relais** (cert.pem/key.pem) — ACME/Let's Encrypt auto-rotation
  tous les 90 jours.

## Opérations sensibles — checklist pré-déploiement

Avant de merger un changement dans un de ces répertoires, faire un passage
adversarial supplémentaire (agent code-reviewer, ou relecture humaine
indépendante) :

- `internal/crypto/`
- `internal/updater/`
- `internal/firewall/`
- `internal/relay/verify_handler.go`
- `internal/relay/connect_handler.go`
- `internal/relay/nat_table.go`
- `internal/relay/bandwidth_limiter.go`
- `internal/service/service.go` (fonctions `recoverTUN`, `SetAllowIPv6Leak`,
  `SetKillSwitchMode`)
- `deploy/*.service`, `deploy/install.sh`
- `.github/workflows/*.yml`
- `scripts/fetch-wintun.sh`, `scripts/release-sign.sh`
