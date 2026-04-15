# Story 3.1: Binaire relais Go HTTP/3 stateless déployable via systemd

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur,
I want un binaire Go autonome déployable sur n'importe quel VPS Linux via systemd,
So that je puisse ajouter un nouveau pays en déployant simplement le binaire et un certificat TLS.

## Acceptance Criteria

1. **Given** un VPS Linux fraîchement provisionné, **When** `deploy/install.sh` est exécuté avec `relay`, `cert.pem`, `key.pem` placés à côté, **Then** un user système `levoile` est créé (sans shell, sans home), le binaire est installé dans `/opt/levoile/relay` (mode 0755), les certificats dans `/opt/levoile/{cert,key}.pem` (mode 0600), et le répertoire est `chown levoile:levoile`.
2. **Given** install.sh exécuté, **When** le service est activé, **Then** l'unit `levoile-relay.service` est installée dans `/etc/systemd/system/` avec `ProtectSystem=strict`, `ProtectHome=true`, `NoNewPrivileges=true`, `Restart=always`, `RestartSec=5`, `PrivateTmp=true`, `LimitNOFILE=65536`, et `AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN` (CAP_NET_ADMIN requis pour NAT/raw sockets ajoutés en stories 3.3-3.4).
3. **Given** install.sh exécuté, **When** la séquence se termine, **Then** `systemctl daemon-reload` puis `systemctl enable --now levoile-relay.service` ont démarré le service, et `systemctl status levoile-relay.service` retourne `active (running)`.
4. **Given** le service relais tourne, **When** la commande `systemctl restart levoile-relay.service` est exécutée, **Then** aucune donnée persistée ne survit (NAT table reset, pas de fichier d'état créé sous `/opt/levoile/` ni `/var/lib/`) — confirmation NFR3 + FR18.
5. **Given** un restart du service, **When** mesuré entre `systemctl restart` et `curl -k https://localhost/health` retournant HTTP 200, **Then** le délai est < 5 secondes.
6. **Given** un VPS sans `cert.pem`/`key.pem` à côté de `install.sh`, **When** install.sh est exécuté, **Then** il échoue proprement (set -e) avec un message clair, sans laisser le service démarré dans un état dégradé.

## Tasks / Subtasks

- [ ] Tâche 1 — Mettre à jour l'unit systemd (AC: 2)
  - [ ] Éditer [deploy/levoile-relay.service](deploy/levoile-relay.service) : remplacer `AmbientCapabilities=CAP_NET_BIND_SERVICE` par `AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN`
  - [ ] Idem pour `CapabilityBoundingSet` (étendre à `CAP_NET_BIND_SERVICE CAP_NET_ADMIN`)
  - [ ] Vérifier que toutes les directives de durcissement listées en AC2 sont présentes (déjà le cas : `ProtectSystem`, `ProtectHome`, `NoNewPrivileges`, `Restart`, `RestartSec`, `PrivateTmp`, `LimitNOFILE`)

- [ ] Tâche 2 — Durcir install.sh (AC: 1, 6)
  - [ ] Vérifier dans [deploy/install.sh](deploy/install.sh) que `set -euo pipefail` est en tête (déjà OK)
  - [ ] Ajouter une vérification explicite en début de script : si `${SCRIPT_DIR}/relay`, `${SCRIPT_DIR}/cert.pem` ou `${SCRIPT_DIR}/key.pem` manquent, échouer avec un message clair (AC6)
  - [ ] Garder les permissions actuelles : binaire 0755, cert/key 0600, owner `levoile:levoile`

- [ ] Tâche 3 — Vérifier l'absence d'écriture d'état (AC: 4)
  - [ ] Auditer [cmd/relay/main.go](cmd/relay/main.go) et [internal/relay/](internal/relay/) : confirmer qu'aucun fichier d'état (NAT, sessions, compteurs) n'est écrit sur disque hors logs systemd
  - [ ] Si une persistence est trouvée (sauf cert/key statiques), la documenter ou la supprimer ; sinon noter dans Completion Notes que le binaire est confirmé stateless

- [ ] Tâche 4 — Smoke test sur VPS (AC: 1, 3, 4, 5)
  - [ ] Sur un des trois relais existants (IS/FI/DE — voir références), copier `relay` + `deploy/install.sh` + `deploy/levoile-relay.service` + cert/key
  - [ ] Exécuter `sudo bash install.sh` sur un VPS propre OU `systemctl daemon-reload && systemctl restart levoile-relay` sur un existant
  - [ ] Vérifier `systemctl status levoile-relay` → `active (running)`
  - [ ] Mesurer restart : `time (systemctl restart levoile-relay && until curl -k -s -o /dev/null -w '%{http_code}' https://localhost/health | grep -q 200; do sleep 0.1; done)` → résultat consigné dans Completion Notes
  - [ ] Vérifier qu'aucun fichier nouveau n'apparaît dans `/opt/levoile/` après quelques minutes de trafic (AC4)

- [ ] Tâche 5 — Documenter README.md ou docs/ (optionnel, AC: 1)
  - [ ] Si pas déjà fait, ajouter une note dans [docs/](docs/) ou commentaire en tête de install.sh : prérequis = binaire Linux/amd64, cert/key TLS valides, accès root

## Dev Notes

### Contexte business

Story socle de l'Epic 3 (Relais Stateless Multi-VPS). Le binaire et le packaging systemd doivent être triviaux à déployer pour permettre à l'opérateur d'ajouter un pays en quelques minutes. La contrainte stateless (NFR3 + FR18) garantit qu'aucune donnée client ne survit à un redémarrage — propriété de privacy fondamentale.

### État existant (très important — à ne PAS réécrire)

L'essentiel de la story est **déjà implémenté**. Les composants ci-dessous existent et fonctionnent en production sur 3 relais (voir mémoire `reference_relay_servers.md`) :

- [cmd/relay/main.go](cmd/relay/main.go) — binaire CLI complet avec flags `-addr`, `-cert`, `-key`, `-upstream`, `-fallback`, `-signing-key`, `-registry-file`, `-cf-insecure`. Démarre le serveur HTTP/3, gère SIGINT/SIGTERM, charge la clé de signature Ed25519 base64.
- [deploy/install.sh](deploy/install.sh) — création user `levoile`, copie binaire + certs, install unit systemd, `enable --now`. `set -euo pipefail` déjà actif.
- [deploy/levoile-relay.service](deploy/levoile-relay.service) — unit systemd avec quasi-toutes les directives de durcissement requises.

### Gap réel à combler

1. **CAP_NET_ADMIN manquant** dans `AmbientCapabilities` et `CapabilityBoundingSet` du service unit. Requis par les stories 3.3 (handler /tunnel) et 3.4 (NAT table avec sockets) qui suivront. Ajouter dès maintenant évite un re-déploiement.
2. **Validation explicite du restart < 5s** non mesurée dans les tests existants — à mesurer manuellement sur VPS.
3. **Validation stateless** par audit code + observation runtime — à confirmer.
4. **install.sh** ne vérifie pas la présence des fichiers requis avant de commencer (peut laisser un état partiel si cert.pem manque).

### Source tree à toucher

- [deploy/levoile-relay.service](deploy/levoile-relay.service) — édition (Tâche 1)
- [deploy/install.sh](deploy/install.sh) — édition mineure (Tâche 2)
- [cmd/relay/main.go](cmd/relay/main.go) — audit lecture seule (Tâche 3), pas d'édition attendue
- [internal/relay/](internal/relay/) — audit lecture seule (Tâche 3)

### Ne PAS toucher

- Les handlers `/verify`, `/connect`, `/ip`, `/health`, `/.well-known/relay-registry.json` (couverts par d'autres stories, déjà fonctionnels)
- Le code de signature Ed25519, IP limiter, bandwidth limiter (stories 3.2/3.6)
- Le `/tunnel` handler n'existe pas encore — c'est la story 3.3 (NE PAS l'ajouter ici)

### Standards de test

- Pas de nouveau test Go requis — le binaire est déjà couvert par [cmd/relay/main_test.go](cmd/relay/main_test.go)
- Validation = smoke test manuel sur un VPS réel (Tâche 4). Documenter résultats dans Completion Notes
- Pas besoin de bash-test pour install.sh ; vérification visuelle suffit

### Project Structure Notes

- Conforme à l'architecture (section "Infrastructure & Déploiement" lignes 348-358 de [_bmad-output/planning-artifacts/architecture.md](_bmad-output/planning-artifacts/architecture.md))
- L'architecture précise explicitement : `CAP_NET_BIND_SERVICE + CAP_NET_ADMIN` pour la NAT (ligne 357) — confirme la modification de Tâche 1
- Pas de conflit avec packaging Linux client (`packaging/`) qui est totalement séparé (Epic 7) — `deploy/` est exclusivement pour le relais

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.1] — user story + AC originaux
- [Source: _bmad-output/planning-artifacts/architecture.md#Infrastructure-Déploiement] — lignes 350, 357 (CAP_NET_ADMIN, ProtectSystem, NoNewPrivileges)
- [Source: cmd/relay/main.go] — point d'entrée binaire existant
- [Source: deploy/install.sh] — script de déploiement existant
- [Source: deploy/levoile-relay.service] — unit systemd existante (à modifier)

### Previous Story Intelligence

Stories 1-1, 1-2, 1-3 d'Epic 1 traitent du tunnel client → relais (QUIC/HTTP/3, reconnexion, accept côté relais). Cette story 3.1 est le socle de déploiement du relais, prérequis avant 3.2-3.8.

Pas de story précédente dans Epic 3 — c'est la première. L'epic 3 vient d'être marqué `in-progress` dans sprint-status.yaml.

### Latest Tech Information

- systemd ≥ 245 supporte `AmbientCapabilities` avec plusieurs caps séparées par espace (syntax utilisée ici)
- `CAP_NET_ADMIN` est nécessaire pour : raw sockets, manipulation route locale, future intégration NAT userspace
- `ProtectSystem=strict` + `ProtectHome=true` + `PrivateTmp=true` interdisent toute écriture hors `/var/lib/`, `/var/log/`, `/run/` — confirme la garantie stateless si aucun de ces paths n'est utilisé

## Dev Agent Record

### Agent Model Used

claude-opus-4-6[1m]

### Debug Log References

### Completion Notes List

### File List
