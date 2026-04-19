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

Suite à un audit interne, les findings suivants ont été corrigés dans ce
commit. Chaque ligne indique l'ID du finding, la mitigation, et le
fichier principal touché.

| ID | Finding | Mitigation | Emplacement |
|----|---------|------------|-------------|
| C1 | Pas d'anti-downgrade persistant | `max_seen_version.txt` persistant (0600), rejet `ErrDowngradeRejected` si release candidate < version max vue | [internal/updater/rollback.go](internal/updater/rollback.go), [updater.go](internal/updater/updater.go) |
| C2 | HTTP `/api/*` sans vérif Origin/Host | Middleware `originGuard` : rejette toute requête dont `Host`, `Origin` ou `Referer` pointe hors loopback (bloque DNS rebinding + page web malveillante) | [internal/ui/httpserver.go](internal/ui/httpserver.go) |
| C5 | Race TUN↔firewall dans `recoverTUN` | Réordonné : `firewall.Activate` AVANT `routing.Setup`. Le flush+replace atomique (nftables/WFP) ferme la fenêtre microscopique où le routing pouvait pointer sur un TUN sans règles à jour. | [internal/service/service.go](internal/service/service.go) |
| C6 | Extension navigateur : permissions `<all_urls>` + bypass sans `proxyAlive` | **Extension supprimée intégralement** (WebRTC couvert par les policies firefox/chromium + SysProxy + STUN watchdog). Voir [commit 879fc18](https://github.com/velia-the-veil/le_voile/commit/879fc18) | — |
| H1 | Pas de révocation de clé release | `RevokedReleaseKeysBase64` : liste opt-in consultée avant succès de signature ; `ErrReleaseKeyRevoked` distinct de la signature invalide pour alerting. | [internal/crypto/release_keys.go](internal/crypto/release_keys.go) |
| H2 | Systemd relay hardening limité | Ajout `PrivateDevices`, `ProtectKernelTunables/Modules/Logs`, `ProtectControlGroups`, `ProtectHostname`, `ProtectClock`, `RestrictAddressFamilies`, `RestrictNamespaces/Realtime/SUIDSGID`, `LockPersonality`, `SystemCallFilter=@system-service`, `SystemCallArchitectures=native`. | [deploy/levoile-relay.service](deploy/levoile-relay.service) |
| H3 | Dossier prefs UI en 0755 | Changé en 0700 — autre user local ne peut plus lister le dossier ni lire/modifier `ui-prefs.json`. | [internal/ui/prefs.go](internal/ui/prefs.go) |
| H6 | IPv6 leak togglable runtime sans audit | Audit trail stderr à chaque `SetAllowIPv6Leak`. `LEVOILE_LOCK_SECURITY_POLICY=1` transforme le toggle IPC en no-op (`ErrSecurityPolicyLocked`) : recovery out-of-band via `config.toml` + restart. | [internal/service/service.go](internal/service/service.go) |
| H7 | Wintun : SHA256 du ZIP seulement | Post-extraction, vérification Authenticode via `signtool` (Windows) ou `osslsigncode` (Linux/CI) avec fail-closed si échec. Fallback warn-only si aucun outil dispo. | [scripts/fetch-wintun.sh](scripts/fetch-wintun.sh) |
| H8 | Actions GitHub pinnées par tag mobile | Toutes les actions (`checkout`, `setup-go`, `goreleaser-action`, `upload-artifact`) pinnées par SHA 40-hex avec tag en commentaire pour lisibilité. | [.github/workflows/release.yml](.github/workflows/release.yml), [aur-publish.yml](.github/workflows/aur-publish.yml) |
| H10 | Pas de rate-limit per-IP sur `/verify` | `IPLimitMiddleware` ajouté devant `/verify` : cap concurrence `IPLimiter.maxPer=200` par client. Retourne `429` (distinct du `503` global). | [internal/relay/middleware.go](internal/relay/middleware.go), [server.go](internal/relay/server.go) |

## Follow-ups — non adressés par ce commit

Ces findings nécessitent un changement architectural ou un environnement
de test que cette passe n'a pas couvert. Chacun a un emplacement précis
dans le code pour reprise.

### C3 — Windows named pipe DACL (audit)

**Finding** : DACL `(A;;GRGW;;;IU)` laisse tout Interactive User
lire/écrire sur `\\.\pipe\levoile`. Combiné avec `req.Auth == ""`
accepté par [internal/ipchandler/handler.go](internal/ipchandler/handler.go)
ligne 839-850, tout process du même user peut commander le service.

**Limite fondamentale** : la UI (user context) doit pouvoir envoyer des
commandes au service (SYSTEM). Sur Windows desktop, il n'existe pas de
bar plus fin que "même user interactif" sans token dérivé DPAPI.

**Plan** : [internal/ipc/pipe_windows.go](internal/ipc/pipe_windows.go) —
réduire DACL à `(A;;GR;;;IU)` (read-only) + introduire un token
`LEVOILE_UI_TOKEN` fichier 0600 user-only, lu par UI et envoyé dans
`req.Auth` pour toute action mutante. Estimation 1 jour + tests Windows.

### C4 — Session token relay sans nonce

**Finding** : `SessionTokenPayload{IPHash, Issued, TTL=14400s}`
([internal/relay/verify_handler.go:36-41](internal/relay/verify_handler.go))
n'embarque pas de nonce aléatoire ; replay possible pendant 4 h.

**Plan** : ajouter `Nonce [32]byte` généré par le relay à `Verify()`,
embarqué dans la signature. Côté client, stocker le nonce retourné ;
côté relay, pas besoin de track — la signature Ed25519 inclut le nonce
donc un token sans `Nonce == payload.Nonce` échoue `VerifySessionToken`.
Changement cassant pour les clients < version qui négocient ce champ —
gérer via transition dual-format. Estimation 2 jours.

### C7 — install.sh relais distribué sans signature

**Finding** : instructions actuelles ([deploy/install.sh](deploy/install.sh))
demandent `scp` du script + `sudo bash /tmp/install.sh` — MITM possible.

**Plan** : publier `install.sh` + `install.sh.sig` dans chaque release
GitHub. Ajouter un bootstrap `curl | gpg --verify | bash` documenté
dans [docs/relay-setup.md](docs/relay-setup.md). Nécessite clé GPG mainteneur
enregistrée en keyring reproductible. Estimation 0.5 jour + doc.

### H4 — Quotas relais agrégés par IP (bypassable par rotation)

**Finding** : [internal/relay/bandwidth_limiter.go:44-45](internal/relay/bandwidth_limiter.go)
indexe les quotas par `string(clientIP)`. Une rotation IP (via pool de
proxies) multiplie trivialement le quota.

**Plan** : dériver la clé quota de `sha256(ClientSigningPubKey)`, transmise
dans le SessionTokenPayload. Requiert que la clé publique client soit
transmise à `/verify` et embarquée dans le token. Lié à C4.
Estimation 1.5 jour.

### H5 — SessionID non authentifié

**Finding** : `sessionKey = fmt.Sprintf("%s@%d", ClientIPHash, OpenedAt.UnixNano())`
([internal/relay/nat_table.go:172-175](internal/relay/nat_table.go)) —
entropie ~13 bits sur 1s, un client A peut deviner sessionID de client B.

**Plan** : `sessionID = HMAC-SHA256(ServerSecret, ClientPubKey || OpenedAt || Rand32)`.
Lié à C4/H4. Estimation 1 jour.

### H9 — wireguard-go pseudo-version

**Finding** : [go.mod](go.mod) utilise `v0.0.0-20250521234502-f333402bd9cb`,
pseudo-version d'un commit non-taggé.

**Plan** : surveiller [git.zx2c4.com/wireguard-go](https://git.zx2c4.com/wireguard-go)
pour une release stable `v0.1.0`. En l'état, le commit pinne par SHA — donc
pas de supply-chain attack via tag mobile, juste un manque de garantie
release quality par upstream. Non-bloquant.

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
