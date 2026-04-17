# Le Voile — Known Issues

Tracking des problèmes ouverts hors scope des stories courantes. Supprimer l'entrée dès qu'une story / PR la corrige.

---

## OP-001 — `us-002.levoile.dev` rejette `/.well-known/relay-registry.json` avec HTTP 403

- **Ouvert :** 2026-04-17 (Story 4.1 smoke test)
- **Sévérité :** MEDIUM (opérationnel, pas de régression utilisateur tant que le failover pioche un autre relais)
- **Observation :** smoke test sur les 8 relais de prod — 7/8 renvoient `HTTP 200` + registre signé identique (`sha256=58ca7e8f413b6624da84b17384f1575bc0f4e650fc6a677370090bd139748fe8`) ; `us-002.levoile.dev` renvoie `HTTP 403`.
- **Hypothèse :** dérive de configuration — `CFSourceMiddleware` en mode strict sur us-002 (absence du flag `-cf-insecure` au `ExecStart` ou validateur CF IP configuré différemment) alors que les 7 autres laissent passer les requêtes directes. Alternative : bascule DNS Cloudflare proxied vs DNS-only divergente.
- **Impact :** us-002 est présent dans le registre servi par les 7 autres, donc le failover / round-robin peut le sélectionner. Si le client utilise uniquement us-002 pour son bootstrap (premier démarrage), le DoH bootstrap route sur un autre relais — à valider dans la story 4.2.
- **Reproduire :**
  ```bash
  curl -v https://us-002.levoile.dev/.well-known/relay-registry.json
  # → HTTP 403 "Forbidden"
  curl -v https://de-001.levoile.dev/.well-known/relay-registry.json
  # → HTTP 200 + JSON signé
  ```
  Ou : `deploy/smoke_registry.sh` → liste la faille.
- **Prochaines étapes :**
  1. `ssh -i ~/.ssh/vpn_vps_rsa root@74.208.212.141` (us-002)
  2. `systemctl cat levoile-relay | grep ExecStart` → comparer aux 7 autres
  3. Aligner la ligne ExecStart + `systemctl daemon-reload && systemctl restart levoile-relay`
  4. Relancer `deploy/smoke_registry.sh --verify` → attendu 8/8 OK
- **Règle respectée :** `feedback_diff_before_deploy` — diff avant toute action corrective, ne pas pousser aveuglément.

---
