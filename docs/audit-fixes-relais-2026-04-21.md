# Audit fixes — 2026-04-21 (relais, server-side + ops)

Complément à [`audit-fixes-2026-04-21.md`](audit-fixes-2026-04-21.md) (client / UI)
qui excluait explicitement le code server-side des relais. Ce document couvre
l'audit approfondi du code relais et de l'infrastructure de déploiement, et
les trois catégories de fix livrées le même jour.

Validation : `go build ./...`, `go test ./internal/relay/ -count=1` verts.

---

## Fix #1 — Race HTTP sur `POST /tunnel` : `WriteHeader(200)` avant check ID aléatoire

**Problème constaté**

Dans [`internal/relay/tunnel_handler.go`](../internal/relay/tunnel_handler.go)
`serveTunnel()`, l'ordre était :

1. `w.WriteHeader(http.StatusOK)` + `Flush()` — le header 200 part tout de suite sur le fil ;
2. `newSessionID()` lit 16 octets depuis `crypto/rand` ;
3. en cas d'échec, `http.Error(w, "Internal Server Error", 500)`.

Conséquence : l'erreur 500 ne pouvait jamais atteindre le client — le header
200 était déjà flushé, `http.Error` essayait de réécrire une nouvelle status
line, ce que `net/http` refuse en silence (seul un warning est logué). Le
client voyait un 200 suivi d'un stream qui se fermait sans envoyer la moindre
frame — statut mensonger, retry retardé, diagnostic opaque.

L'échec `crypto/rand.Read` est hautement improbable mais le bug rend le
chemin d'erreur inobservable, donc non testable en intégration.

**Solution**

Déplacer `newSessionID()` AVANT `WriteHeader(200)`. Si la génération échoue,
on répond 500 proprement avant tout flush.

**Fichiers touchés**

- [`internal/relay/tunnel_handler.go`](../internal/relay/tunnel_handler.go) — réordonnancement de 8 lignes dans `serveTunnel()`, commentaire expliquant la contrainte.

**Impact compatibilité** : aucun. Le code-path nominal (200 + stream de
frames) est inchangé, seul le code-path erreur devient cohérent.

---

## Fix #2 — Renewal TLS inopérant côté process : ajout d'un deploy hook certbot

**Problème constaté**

`certbot.timer` tourne deux fois par jour sur tous les relais, avec
`renew_before_expiry = 30 days` dans la conf de renouvellement. Les
certificats sont donc correctement renouvelés dans `/etc/letsencrypt/`.

**Mais** [`internal/relay/server.go:58`](../internal/relay/server.go#L58)
appelle `tls.LoadX509KeyPair(certFile, keyFile)` UNE seule fois au démarrage
et stocke la `tls.Certificate` parsée dans `tls.Config.Certificates`. Il n'y
a pas de callback `GetCertificate`, pas de watcher `inotify` sur les fichiers
`.pem`, pas de handler `SIGHUP` pour recharger.

Conséquence silencieuse : **après le premier renewal effectif (~2026-07-12
pour les certs émis le 13 avril 2026), les 8 relais auraient continué à servir
l'ancien cert jusqu'à un restart manuel** — time bomb de trois mois, invisible
via `certbot certificates` (qui dit "tout va bien") et invisible via
`openssl s_client` (qui ne montrerait l'expiration qu'au jour J).

Le dossier `/etc/letsencrypt/renewal-hooks/deploy/` était vide sur les 5 relais
audités.

**Solution**

Installer sur chaque relais un deploy hook certbot qui redémarre le service
`levoile-relay` après un renouvellement réussi :

```bash
# /etc/letsencrypt/renewal-hooks/deploy/restart-levoile-relay.sh
#!/bin/bash
logger -t levoile-cert-deploy "restarting levoile-relay after renewal of ${RENEWED_LINEAGE:-unknown}"
systemctl restart levoile-relay
```

Coût : ~1–2 s de downtime TCP/443 toutes les ~90 jours (renewal trimestriel
effectif dans la fenêtre 30-jours-avant-expiry de certbot). Les tunnels
actifs se reconnectent automatiquement via la logique de backoff côté client
(voir [`internal/tunnel/client.go`](../internal/tunnel/client.go)).

Alternative architecturale rejetée pour cette itération : remplacer
`tls.Config.Certificates` par `tls.Config.GetCertificate` qui relit à chaque
handshake (ou via un cache mtime-keyed). Évite le restart mais ajoute une
lecture disque + parse par handshake et un watcher. Gardé en backlog.

**Fichiers touchés** (repo)

- [`deploy/renewal-hook-restart-relay.sh`](../deploy/renewal-hook-restart-relay.sh) — nouveau, source de vérité du hook.
- [`deploy/install.sh`](../deploy/install.sh) — installe le hook dans `/etc/letsencrypt/renewal-hooks/deploy/` s'il est présent dans le répertoire de staging.
- [`deploy/README.md`](../deploy/README.md) — section "Post-install ops" ajoutée.

**Impact compatibilité** : aucun côté client. Côté ops : 1–2 s d'indispo
TCP/443 par relais et par renewal, évitant l'expiration silencieuse.

---

## Fix #3 — Watchdog journal quotidien : alerte expiration cert à <30 j

**Problème constaté**

Même avec le deploy hook de Fix #2, il restait aucune alerte si :

- certbot échouait à renouveler (rate limit ACME, DNS cassé, port 80 fermé
  par un hébergeur capricieux pour la challenge HTTP-01) ;
- le hook échouait silencieusement (`systemctl restart` KO) ;
- un cert expirait pour une raison hors-scope (certbot désactivé, symlink
  cassé manuellement).

Pas de canal de notification out-of-band. Un opérateur ne le verrait qu'au
jour J, via un incident utilisateur.

**Solution**

Installer un timer systemd quotidien qui vérifie le cert **effectivement servi
par le relais** (`/opt/levoile/cert.pem`, en suivant la chaîne de symliks),
et escalade vers journald selon les jours restants :

| Jours restants | Priorité journal |
|---|---|
| >30 | silencieux |
| ≤30 | `info` |
| ≤14 | `warning` |
| ≤7  | `crit` |

Lecture : `journalctl -t levoile-cert-expiry --since -7d`.

Le watchdog lit le chemin du cert depuis `ExecStart` de `levoile-relay.service`
— pas de glob approximatif. Si le chemin est modifié, la vérification suit
automatiquement.

**Artefacts installés** (par relais)

- `/opt/levoile/cert-expiry-check.sh` — script de vérification (exécutable).
- `/etc/systemd/system/levoile-cert-check.service` — unité oneshot.
- `/etc/systemd/system/levoile-cert-check.timer` — timer `OnCalendar=daily`,
  `RandomizedDelaySec=1h` pour éviter la bousculade 00:00 UTC.

**Fichiers touchés** (repo)

- [`deploy/cert-expiry-check.sh`](../deploy/cert-expiry-check.sh) — nouveau.
- [`deploy/levoile-cert-check.service`](../deploy/levoile-cert-check.service) — nouveau.
- [`deploy/levoile-cert-check.timer`](../deploy/levoile-cert-check.timer) — nouveau.
- [`deploy/install.sh`](../deploy/install.sh) — installation opportuniste.
- [`deploy/README.md`](../deploy/README.md) — documentation.

**Impact compatibilité** : aucun. Overhead : 1 `openssl x509 -noout -enddate`
par jour, négligeable.

---

## Pollution historique nettoyée

Sur **us-002** (74.208.212.141), les dossiers `live/us-001.levoile.dev` et
`renewal/us-001.levoile.dev.conf` subsistaient — reliquat du clonage d'image
us-001 → us-002. Conséquence pratique : `certbot` tentait à chaque passage de
renouveler us-001.levoile.dev depuis un host dont le DNS ne pointe pas sur
lui, générant des erreurs ACME répétées dans `/var/log/letsencrypt/`. Nettoyé
via `certbot delete --cert-name us-001.levoile.dev --non-interactive` ; plus
de conf résiduelle.

---

## Couverture et TODO

**Fait sur 5 relais joignables** (audit-fixes #2 et #3 appliqués) :

| Host | Fix #2 deploy hook | Fix #3 watchdog | Pollution nettoyée |
|---|---|---|---|
| es-001 | ✓ | ✓ | — |
| es-002 | ✓ | ✓ | — |
| gb-001 | ✓ | ✓ | — |
| us-001 | ✓ | ✓ | — |
| us-002 | ✓ | ✓ | ✓ |

**À faire avant 2026-07-05** sur les 3 relais non audités :

- DE-001 (217.160.59.54)
- DE-002 (82.165.167.26)
- GB-002 (77.68.54.202)

Cause du blocage : l'IP ISP de l'opérateur (bouygues-fr, 90.66.218.27) est
vraisemblablement dans un fail2ban côté hébergeur sur ces 3 hosts — `ssh`
timeout sur port 22, alors que HTTPS/443 répond normalement. À rouvrir depuis
une autre IP source (tether 4G, autre ISP, ou rebond via es-001 qui est joignable).

Procédure : copier les 4 artefacts de [`deploy/`](../deploy/) (cert-expiry-check.sh, les deux
unités systemd, renewal-hook-restart-relay.sh) puis lancer :

```bash
install -m 0755 -D cert-expiry-check.sh /opt/levoile/cert-expiry-check.sh
install -m 0644 -D levoile-cert-check.service /etc/systemd/system/levoile-cert-check.service
install -m 0644 -D levoile-cert-check.timer   /etc/systemd/system/levoile-cert-check.timer
install -m 0755 -D renewal-hook-restart-relay.sh /etc/letsencrypt/renewal-hooks/deploy/restart-levoile-relay.sh
systemctl daemon-reload
systemctl enable --now levoile-cert-check.timer
```

---

## Faux positif signalé puis retiré

Mon premier passage d'audit avait signalé que `/opt/levoile/cert.pem` pointait
sur `archive/<domain>/fullchain1.pem` (version figée #1) au lieu de
`live/<domain>/fullchain.pem` (que certbot met à jour). **C'était une erreur
de lecture** : `readlink -f` suit toute la chaîne de symlinks (Let's Encrypt
fait `live/X/fullchain.pem -> archive/X/fullchainN.pem` en interne), alors que
`readlink` nu n'en suit qu'un hop. Le premier hop était bien sur `live/`, la
symlink était donc déjà correcte. Les 5 symlinks ont quand même été réaffirmés
de façon idempotente avec vérification fingerprint post-changement — no-op
vérifié, rien n'a bougé côté cert servi.

Note pour les futurs audits cert : vérifier à la fois `readlink` (hop 1) et
`readlink -f` (cible finale) pour distinguer "symlink figée sur `archive/`"
de "chaîne LE normale".
