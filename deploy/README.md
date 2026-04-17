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

### 4. Deploy relay binary

```bash
scp relay cert.pem key.pem install.sh levoile-relay.service root@{vps}:/opt/levoile/
ssh root@{vps} 'cd /opt/levoile && bash install.sh'
```

### 5. Regenerate the relay registry

Add the new relay to `relays.json`, then:

```bash
genregistry \
  -signing-key master.key \
  -relays relays.json \
  -strict-priority \
  -out relay-registry.json
```

### 6. Deploy registry to all relays

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

## Files

| File | Purpose |
|------|---------|
| `install.sh` | Installs relay binary, certs, systemd unit on a VPS |
| `levoile-relay.service` | systemd unit with hardening directives |
| `README.md` | This file |
