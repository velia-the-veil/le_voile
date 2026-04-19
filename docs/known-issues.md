# Le Voile — Known Issues

Tracking des problèmes ouverts hors scope des stories courantes. Supprimer l'entrée dès qu'une story / PR la corrige.

---

## (aucun problème ouvert)

### Historique — entrées résolues

- **OP-001 — `us-002.levoile.dev` rejetait `/.well-known/relay-registry.json` avec HTTP 403**
  - Ouvert : 2026-04-17 (Story 4.1 smoke test)
  - Résolu : 2026-04-19, pivot "DNS-only canonical"
  - Résolution : le `CFSourceMiddleware` qui rejetait les sources non-Cloudflare a été **retiré du code**. Le relais est joint en direct (DNS A record → VPS origin), sans fronting CDN, donc il n'y a plus de notion de "source CF trusted". Voir `architecture.md` — section "rewrittenAt: 2026-04-19" pour le rationnel.
