# Le Voile — Relay Deployment

## Adding a New Relay

### 1. Provision VPS

- Any Linux VPS with a public IPv4 (Hetzner, OVH, etc.)
- Open firewall ports: 22 (SSH), 80 (HTTP), 443 (TCP+UDP)
- Disable IPv6 if not needed: `sysctl -w net.ipv6.conf.all.disable_ipv6=1`

### 2. Generate relay Ed25519 key pair (on VPS)

```bash
openssl genpkey -algorithm ed25519 -outform DER -out /tmp/relay.key
openssl pkey -in /tmp/relay.key -inform DER -pubout -outform DER | tail -c 32 | base64 > /tmp/relay.pub
```

Collect the base64 public key from `/tmp/relay.pub`.

### 3. Obtain TLS certificate

Use Let's Encrypt via certbot:

```bash
apt install certbot
certbot certonly --standalone -d {code}-{num}.levoile.dev
```

### 4. Generate the relay registry (BEFORE install)

Le relais refuse de démarrer sans `signing.key` et `relay-registry.json` (référencés par l'ExecStart du .service). Il faut donc les préparer d'abord :

- `signing.key` : clé privée Ed25519 du relais (base64, 64 octets décodés). Générée localement ou provisionnée depuis un coffre d'ops.
- `relay-registry.json` : registre signé par le master, incluant le nouveau relais. Ajouter d'abord le nouveau relais à `relays.json`, puis :

```bash
genregistry \
  -signing-key master.key \
  -relays relays.json \
  -strict-priority \
  -out relay-registry.json
```

### 5. Deploy relay binary

```bash
scp relay cert.pem key.pem signing.key relay-registry.json install.sh levoile-relay.service root@{vps}:/tmp/levoile-install/
ssh root@{vps} 'cd /tmp/levoile-install && bash install.sh'
```

`install.sh` valide la présence des 6 fichiers requis, crée le user `levoile`, pose les bons modes (signing.key 0600, registry 0644), installe l'unit systemd et fait `enable --now`.

En plus, `install.sh` pose **de façon opportuniste** (seulement s'ils sont présents dans le répertoire de staging) les artefacts ops introduits par [audit-fixes-relais-2026-04-21](../docs/audit-fixes-relais-2026-04-21.md) :

- `cert-expiry-check.sh` + `levoile-cert-check.{service,timer}` — watchdog quotidien qui alerte dans journald si le cert TLS arrive à <30 j. Lecture : `journalctl -t levoile-cert-expiry --since -7d`.
- `renewal-hook-restart-relay.sh` — deploy hook certbot installé dans `/etc/letsencrypt/renewal-hooks/deploy/restart-levoile-relay.sh`. Redémarre `levoile-relay` après chaque renewal réussi (le process lit le cert une seule fois au démarrage, cf. `server.go`). ~1-2 s de downtime TCP/443 par renewal trimestriel.

Pour un relais déjà déployé sans ces artefacts, voir la procédure manuelle en fin de [audit-fixes-relais-2026-04-21.md](../docs/audit-fixes-relais-2026-04-21.md).

### 6. Propager le registry mis à jour aux autres relais

```bash
for host in de-001 de-002 es-001 es-002 gb-001 gb-002 us-001 us-002; do
  scp relay-registry.json root@${host}.levoile.dev:/opt/levoile/relay-registry.json
done
```

## Relay Naming Convention

- **Relay ID**: `relay-{iso2}-{NNN}` (e.g., `relay-de-001`, `relay-us-002`)
- **Domain**: `{iso2}-{NNN}.levoile.dev` (e.g., `de-001.levoile.dev`)
- **ISO codes**: `de` (Germany), `es` (Spain), `gb` (UK), `us` (USA), `is` (Iceland), `fi` (Finland), `fr` (France)

## Priority Countries (FR19b)

DE, ES, GB, US require at least 2 relays each for intra-country failover. Use `-strict-priority` to enforce this at registry generation time.

## relays.json Format

```json
[
  {"id": "relay-de-001", "domain": "de-001.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-de-002", "domain": "de-002.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-es-001", "domain": "es-001.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-es-002", "domain": "es-002.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-gb-001", "domain": "gb-001.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-gb-002", "domain": "gb-002.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-us-001", "domain": "us-001.levoile.dev", "public_key": "<base64 Ed25519 pub>"},
  {"id": "relay-us-002", "domain": "us-002.levoile.dev", "public_key": "<base64 Ed25519 pub>"}
]
```

## Chemin canonique du registre

Le registre est servi depuis **`/opt/levoile/relay-registry.json`** — **pas** `/etc/levoile/`. Raisons :

- `/opt/` est la convention Linux pour les applications auto-contenues (binaire + config + assets ensemble).
- `ProtectSystem=strict` + `WorkingDirectory=/opt/levoile` dans l'unit systemd : cohérent si tout est sous `/opt/`, nécessiterait un `ReadWritePaths=` supplémentaire si le registre était sous `/etc/`.
- Tous les 8 relais de prod utilisent déjà ce chemin ; aucune migration prévue. Toute doc qui mentionne `/etc/levoile/` est obsolète — se référer à [`install.sh`](install.sh) et [`levoile-relay.service`](levoile-relay.service) comme sources de vérité.

Permissions : `0644` owner `levoile:levoile` (lu par le handler HTTP, public-safe — le contenu signé est conçu pour être diffusé).

## Bootstrap domain & DoH (Story 4.2 / NFR9i)

Au premier lancement, le client résout `relay.levoile.dev` (hardcodé dans `installer/config-default.toml`) via **DoH** (Cloudflare puis Quad9) avant d'établir le tunnel — le resolver DNS système n'est jamais interrogé pour ce domaine. Conséquences opérationnelles :

- `relay.levoile.dev` doit pointer (via DNS public) vers **au moins un** relais opérationnel. Round-robin DNS entre plusieurs relais est acceptable : chaque relais sert le registre complet via `/.well-known/relay-registry.json` (pas de single point of failure).
- La rotation du domaine bootstrap n'est possible qu'en rebuild client (domaine embedded). Si rotation requise, publier une release dual-signée (clé courante + clé de rotation NFR22h) et attendre l'adoption par les clients.
- Le filtre de défense en profondeur rejette toute réponse DoH privée (loopback, RFC1918, link-local). Si un upstream DoH retourne une IP interne, le client bascule sur l'autre upstream puis échoue proprement — aucun fallback silencieux vers le resolver système.
- Test manuel de sanité : `curl -H "accept: application/dns-message" --data-binary @query.bin "https://cloudflare-dns.com/dns-query" -H "content-type: application/dns-message"` doit retourner une réponse DNS wireformat non-vide pour `relay.levoile.dev`.

## Files

| File | Purpose |
|------|---------|
| `install.sh` | Installs relay binary, certs, systemd unit on a VPS |
| `levoile-relay.service` | systemd unit with hardening directives |
| `cert-expiry-check.sh` | Daily watchdog — logs to journald if cert <30 d from expiry |
| `levoile-cert-check.service` | Oneshot systemd unit that runs `cert-expiry-check.sh` |
| `levoile-cert-check.timer` | Daily timer (`RandomizedDelaySec=1h`) for the watchdog |
| `renewal-hook-restart-relay.sh` | Certbot deploy hook — restarts `levoile-relay` after successful renewal |
| `smoke_registry.sh` | Post-install smoke test of the registry endpoint |
| `README.md` | This file |
