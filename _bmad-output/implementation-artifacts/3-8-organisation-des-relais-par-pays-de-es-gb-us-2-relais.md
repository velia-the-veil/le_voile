# Story 3.8: Organisation des relais par pays (DE/ES/GB/US ≥ 2 relais)

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a opérateur,
I want déployer et référencer plusieurs relais dans les pays prioritaires (DE, ES, GB, US) via un registre généré sans édition manuelle,
So that le failover intra-pays soit possible, la latence minimisée par géolocalisation, et l'ajout d'un relais ne soit plus une opération manuelle risquée.

## Acceptance Criteria

1. **Given** `cmd/genregistry` est appelé avec une liste de relais (≥ 2 entrées DE, ≥ 2 entrées ES, ≥ 2 entrées GB, ≥ 2 entrées US), **When** le binaire produit `relay-registry.json`, **Then** le fichier contient exactement une entrée signée Ed25519 par relais fourni en entrée (pas de re-exécution manuelle) et chaque entrée a `id`, `domain`, `public_key`, `signature`, `added` non vides.
2. **Given** le registre généré, **When** il est chargé par `registry.Parse` puis `VerifyAll`, **Then** toutes les entrées passent la vérification de signature (la master key signe correctement chaque `public_key` individuel avec le préfixe `relay-key-v1:`).
3. **Given** un registre contenant des relais aux IDs `relay-de-001`, `relay-es-001`, `relay-gb-001`, `relay-us-001` et domaines `de-001.levoile.dev`, `es-001.levoile.dev`, `gb-001.levoile.dev`, `us-001.levoile.dev`, **When** `Discoverer.RelaysByCountry()` est appelé, **Then** la map retournée contient au moins les clés `"de"`, `"es"`, `"gb"`, `"us"` avec ≥ 2 relais dans chaque slice pour les pays prioritaires.
4. **Given** un domaine au format `{code}-NNN.levoile.dev` (ex : `us-001.levoile.dev`, `gb-002.levoile.dev`) et un `id` vide, **When** `registry.ExtractCountryCode("", domain)` est appelé, **Then** le code pays ISO 3166 alpha-2 correct est retourné (ex : `"us"`, `"gb"`) — bug actuel à corriger : `domain[:dot]` vaut `"us-001"` et ne match pas `CountryMetaMap`.
5. **Given** un pays prioritaire sans deuxième relais dans l'input du generator, **When** `genregistry -strict-priority` est passé, **Then** le binaire échoue avec un message clair listant les pays prioritaires sous-dotés (sinon en mode non-strict : warning stderr seulement, pour ne pas bloquer l'opérateur).
6. **Given** la référence opérationnelle `reference_relay_servers` (mémoire Claude), **When** la story est livrée, **Then** la mémoire est mise à jour pour refléter l'inventaire réel actuel : 8 relais en production (DE-001/002, ES-001/002, GB-001/002, US-001/002) avec leurs IPs, SSH et provider, remplaçant l'ancien inventaire IS/FI/DE.

## Tasks / Subtasks

- [ ] Tâche 1 — Corriger `ExtractCountryCode` pour le format `xx-NNN.levoile.dev` (AC: 4)
  - [ ] Dans [internal/registry/countries.go:40-46](internal/registry/countries.go#L40-L46), reconnaître aussi le motif `{code}-{num}.{tld...}` : si `domain[:dot]` contient un `-`, prendre la partie avant le `-` comme `code`
  - [ ] Préserver le cas existant `{code}.levoile.dev` (sans tiret)
  - [ ] Ajouter un test unitaire dans [internal/registry/countries_test.go](internal/registry/countries_test.go) pour `("", "us-001.levoile.dev") → "us"`, `("", "gb-002.levoile.dev") → "gb"`, `("", "de-001.levoile.dev") → "de"`
  - [ ] Vérifier que les cas existants passent toujours (`relay-is-01`, `is.levoile.dev`, legacy `relay-iceland`)

- [ ] Tâche 2 — Refactor `cmd/genregistry` pour supporter N relais (AC: 1, 2, 5)
  - [ ] Dans [cmd/genregistry/main.go](cmd/genregistry/main.go), ajouter un flag `-relays` pointant vers un fichier TSV/JSON décrivant une liste `[{id, domain, public_key_b64}, ...]` (public keys fournies par l'opérateur ; elles sont générées sur chaque VPS, puis collectées)
  - [ ] Conserver la rétrocompatibilité : si `-relay-id` + `-relay-domain` sont fournis (cas mono-relais historique), utiliser la master public key comme public key du relais (comportement actuel)
  - [ ] Pour chaque relais, signer via `ed25519.Sign(masterPriv, []byte("relay-key-v1:") + relayPubKeyBytes)` — réutiliser exactement la logique existante (lignes 63-66)
  - [ ] Ajouter un flag `-strict-priority` : si présent, lister les pays prioritaires `{de, es, gb, us}` qui ont < 2 relais via `registry.ExtractCountryCode` et échouer avec `os.Exit(1)` + message clair
  - [ ] En mode non-strict, émettre un warning stderr si un pays prioritaire a < 2 relais
  - [ ] Horodatage `added` : conserver `time.Now().UTC().Truncate(time.Second)` pour toutes les entrées d'un même run (ou accepter un override via le fichier d'entrée)

- [ ] Tâche 3 — Documentation opérateur (AC: 1)
  - [ ] Créer [deploy/README.md](deploy/README.md) (ou éditer s'il existe) avec la procédure d'ajout d'un nouveau relais : (1) provisionner VPS, (2) générer clé Ed25519 locale `openssl genpkey -algorithm ed25519 ...`, (3) uploader la pub key à l'opérateur, (4) regénérer le registre avec `genregistry -signing-key master.key -relays relays.json -strict-priority -out relay-registry.json`, (5) redéployer le registre sur tous les relais (`scp relay-registry.json` → `/opt/levoile/`)
  - [ ] Inclure un exemple de fichier `relays.json` avec les 8 relais actuels

- [ ] Tâche 4 — Smoke test sur un relais de prod (AC: 1, 2)
  - [ ] Récupérer `/opt/levoile/relay-registry.json` depuis un relais existant (ex : `de-001`)
  - [ ] Reconstruire localement via la nouvelle version de `genregistry` à partir d'un `relays.json` dérivé du JSON de prod (même ids/domains/public_keys)
  - [ ] Vérifier que les signatures produites sont strictement identiques à celles en prod (déterminisme Ed25519 garanti par le préfixe + public key) — si différence, c'est un bug
  - [ ] Charger via `registry.Parse` puis `VerifyAll` et confirmer `len(verified) == 8`

- [ ] Tâche 5 — Mise à jour de la mémoire opérateur (AC: 6)
  - [ ] Mettre à jour `C:\Users\Akerimus\.claude\projects\d--AI-Bmad-bmad-vpn-le-voile\memory\reference_relay_servers.md`
  - [ ] Nouvelle table : 8 relais (DE/ES/GB/US × 2), IPs réelles récupérées via `dig +short {domain}`, chemins SSH, provider (probablement Hetzner pour DE, à confirmer pour US/ES/GB)
  - [ ] Noter la master public key : `rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk=`
  - [ ] Signaler explicitement que IS et FI ne sont plus dans le registre de prod (remplacés par DE/ES/GB/US)

## Dev Notes

### Contexte business

Dernière story de l'Epic 3. Elle formalise la topologie multi-pays et fournit l'outillage pour ajouter des relais sans éditer manuellement le JSON signé. C'est un prérequis opérationnel pour les Epics 4 (failover multi-pays), 5 (UI sélecteur de pays) et 6 (validation anti-fuite).

**FR19b (PRD §2.2)** : « Les relais peuvent être organisés par pays. Chaque pays dispose d'au moins 1 relais. Les pays prioritaires (DE, ES, GB, US) sont ciblés à 2 relais ou plus pour permettre le failover intra-pays. »

### État existant (très important — à ne PAS réécrire)

L'infrastructure opérationnelle est **déjà en place** :

- **8 relais en production** (inspection live via `ssh root@relay.levoile.dev cat /opt/levoile/relay-registry.json`) : `relay-de-001`/`de-001.levoile.dev`, `relay-de-002`/`de-002.levoile.dev`, `relay-es-001`, `relay-es-002`, `relay-gb-001`, `relay-gb-002`, `relay-us-001`, `relay-us-002`. Master public key : `rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk=`.
- [internal/registry/countries.go](internal/registry/countries.go) — `CountryMetaMap` contient déjà `is`, `de`, `fi`, `us`, `fr`, `es`, `gb` (commit `c1d7c3a`). `RelaysByCountry()` groupe correctement les entrées à partir de l'ID (`relay-{code}-{num}`).
- [internal/registry/registry.go](internal/registry/registry.go) — `Parse`/`VerifyAll` sont déjà implémentés correctement pour N relais. Le registre actuel de prod passe déjà `VerifyAll` sans modification.
- Tests existants : [internal/registry/countries_test.go](internal/registry/countries_test.go) couvre `ExtractCountryCode` (ID formats) et `RelaysByCountry` avec un setup IS/FI/DE. Les ajouter/adapter plutôt que dupliquer.

### Gap identifié (ce qui reste à faire)

1. **`cmd/genregistry` est mono-relais** — [cmd/genregistry/main.go:31-98](cmd/genregistry/main.go#L31-L98) n'accepte que `-relay-id` + `-relay-domain` (une seule entrée, signée avec la master key comme relay key). Le registre de prod a été construit manuellement/par script externe. Cette story refactor l'outil pour accepter N relais chacun avec sa propre `public_key` fournie en entrée.
2. **Bug latent dans `ExtractCountryCode`** — quand appelé avec `id=""` et un domaine au format `{code}-NNN.levoile.dev` (format actuel de la prod), la fonction retourne `""` car `domain[:dot]` vaut `"us-001"` qui n'est pas dans `CountryMetaMap`. Appelé dans [internal/desktop/app.go:289](internal/desktop/app.go#L289) avec id vide — donc impact réel UI. À corriger en reconnaissant le motif `{code}-{num}.{tld...}`.

### Modèles / conventions à suivre

- **Signature Ed25519** : `ed25519.Sign(masterPriv, []byte("relay-key-v1:") + relayPubKeyBytes)`. Déterministe, préfixe de domaine obligatoire — ne jamais changer le préfixe.
- **Format ID** : `relay-{iso2}-{NNN}` (3 chiffres avec zéros de gauche : `001`, `002`). Éviter `relay-xx-1` sans padding → `ExtractCountryCode` gère déjà grâce à `idx >= 2`, mais rester cohérent.
- **Format domain** : `{iso2}-{NNN}.levoile.dev` (nouveau, ex : `de-001.levoile.dev`). Pour le premier relais historique d'un pays, `{iso2}.levoile.dev` (ex : `de.levoile.dev`) reste un alias DNS acceptable.
- **Permissions** : `relay-registry.json` mode `0644` (lecture world, écriture root). Servi via handler statique `/.well-known/relay-registry.json` → garder mode 0644.

### Constraints & Non-goals

- **Hors scope** : provisionnement automatique de VPS, signature DNSSEC, rotation de la master key. La master key reste statique pour ce sprint.
- **Pays secondaires** : aucun n'est requis par l'AC. L'absence de IS/FI/FR dans le registre de prod est acceptée. Si l'opérateur veut les remettre, il passe simplement leurs entrées au generator.
- **Pas de persistence d'état** : rappel NFR3 — le relais reste stateless. Le registre est un fichier statique signé off-line, pas un service.

### Testing standards

- `go test ./internal/registry/... -race` doit passer : ajouter cas `ExtractCountryCode` pour `("", "us-001.levoile.dev")`, `("", "gb-002.levoile.dev")`.
- `go test ./cmd/genregistry/...` : créer si absent. Cas : input JSON 8 relais → output parseable + `VerifyAll` OK + `RelaysByCountry` retourne ≥ 2 par pays prioritaire.
- Déterminisme signature : régénérer le registre à partir des 8 entrées de prod et vérifier octet-à-octet que les 8 signatures sont identiques à celles de `/opt/levoile/relay-registry.json` sur un relais de prod.

### Project Structure Notes

- [cmd/genregistry/main.go](cmd/genregistry/main.go) — refactor (flag parsing + boucle N relais)
- [internal/registry/countries.go](internal/registry/countries.go) — fix `ExtractCountryCode` motif `{code}-{num}`
- [internal/registry/countries_test.go](internal/registry/countries_test.go) — nouveaux cas
- [deploy/README.md](deploy/README.md) — nouveau (doc opérateur)
- Aucune touche au service, au client, à l'UI : cette story est purement outillage + fix localisé.

### References

- [Source: _bmad-output/planning-artifacts/prd.md §FR19b](../../_bmad-output/planning-artifacts/prd.md)
- [Source: _bmad-output/planning-artifacts/epics.md#Story-3.8](../../_bmad-output/planning-artifacts/epics.md)
- [Source: _bmad-output/planning-artifacts/architecture.md#Registre-distribué](../../_bmad-output/planning-artifacts/architecture.md)
- Mémoire : `reference_relay_servers.md` (à mettre à jour — Task 5)
- Registre live : `ssh root@relay.levoile.dev cat /opt/levoile/relay-registry.json` (8 entrées, master key `rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk=`)

### Previous Story Intelligence (Story 3.2)

- Story 3.2 a exposé `relay.SessionTokenTTL` comme constante. Pattern à imiter : exposer des constantes nommées plutôt que des littéraux (ici : prefix `"relay-key-v1:"` existe déjà comme `signaturePrefix` dans [internal/registry/registry.go:20](internal/registry/registry.go#L20) — le réutiliser dans genregistry au lieu de dupliquer le littéral).
- Story 3.2 insiste sur « aucun log ne contient d'IP ». Pour genregistry : outil offline, pas d'impact — mais si on ajoute un log stderr listant les relais, ne pas logger d'IP ni la master pub key non plus par prudence.

### Git Intelligence

- Commit `c1d7c3a` (13 avr. 2026) : « feat: add ES/GB countries, raise quotas » — a ajouté `es` et `gb` à `CountryMetaMap`. Aucune modification de genregistry ni de `ExtractCountryCode` → le bug domaine-avec-tiret date de cette extension.
- Commit `0b5314e` : « random relay selection, proxy cleanup, MaxConnections 1000 » — sélection aléatoire désormais par défaut dans `Discoverer.Relays()`. Le tri latence survit pour `RelaysByCountry` car la méthode utilise l'ordre `d.Relays()`.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
