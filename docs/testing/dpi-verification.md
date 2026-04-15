# Vérification indiscernabilité DPI — Story 1.1 / NFR4

## Objectif

Valider que le trafic tunnel client ↔ relais est **indiscernable d'une connexion HTTPS/HTTP3 standard** par analyse DPI (Deep Packet Inspection). Cible NFR4 : 0 pattern-match VPN sur 100 échantillons.

## Périmètre

- Handshake TLS 1.3 (ClientHello fingerprint JA3/JA4)
- Négociation ALPN (`h3`)
- Trames QUIC (Initial, Handshake, 1-RTT) — en clair visible jusqu'au chiffrement 1-RTT
- Absence de pattern WireGuard / OpenVPN / IKEv2 / L2TP / SSTP

## Procédure manuelle (MVP)

### Pré-requis

- Wireshark ≥ 4.0 (support QUIC natif) ou `tshark` CLI
- Accès à un relais de test (local ou VPS)
- Keylog TLS : `export SSLKEYLOGFILE=/tmp/levoile-sslkeys.log` avant démarrage client (debug uniquement)

### Capture

```bash
sudo tshark -i <interface-sortante> -f "host <relay-ip>" -w /tmp/levoile-dpi.pcap -a duration:30
# Côté client :
levoile-service --once  # ou scénario 100 handshakes via script
```

### Checks automatisables (tshark filters)

```bash
# 1. Aucun pattern WireGuard (type 1-4 sur port UDP quelconque)
tshark -r /tmp/levoile-dpi.pcap -Y "wg" -q -z io,stat,0
# Attendu : 0 paquet

# 2. Trafic QUIC correctement détecté comme HTTP/3 (pas de heuristique VPN)
tshark -r /tmp/levoile-dpi.pcap -Y "quic && http3" -T fields -e quic.frame_type | wc -l
# Attendu : > 0 (handshake observable)

# 3. ALPN = h3
tshark -r /tmp/levoile-dpi.pcap -Y "tls.handshake.extensions_alpn_str" \
  -T fields -e tls.handshake.extensions_alpn_str | sort -u
# Attendu : "h3" (pas "wireguard" ni "openvpn")

# 4. Version TLS 1.3 minimum
tshark -r /tmp/levoile-dpi.pcap -Y "tls.handshake.version" \
  -T fields -e tls.handshake.version | sort -u
# Attendu : 0x0304 (TLS 1.3)

# 5. SNI cohérent avec Cloudflare edge
tshark -r /tmp/levoile-dpi.pcap -Y "tls.handshake.extensions_server_name" \
  -T fields -e tls.handshake.extensions_server_name | sort -u
# Attendu : {de,es,gb,us}.levoile.dev ou sous-domaine Cloudflare
```

## Fréquence

- **Pré-release** : à chaque version majeure (X.0.0) + à chaque changement de bibliothèque QUIC
- **CI** : non automatisé au MVP (reporté — nécessite infra de capture réseau en CI). Les gates CI couvrent NFR22d (gosec, govulncheck, `go test -race`).
- **Monitoring prod** : hors scope (aucun log trafic — NFR20)

## Seuil de réussite

- **0** pattern-match WireGuard / OpenVPN / IKE / L2TP / SSTP sur 100 handshakes
- Fingerprint JA3 du ClientHello doit rester cohérent avec un navigateur Chromium-based (objectif pour Phase 2 : rotation fingerprint)

## Responsable

Pré-release : opérateur release (velia-the-veil).
Questions / remontées : issues GitHub tag `security/dpi`.

## Historique

| Date | Version | Résultat | Notes |
|------|---------|----------|-------|
| _TODO_ | v0.1.0-mvp | _TBD_ | Baseline initiale à établir lors du premier release MVP |
