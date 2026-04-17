# Story 5.2: Affichage du pays sélectionné, du relais actif et de l'IP visible

Status: ready-for-dev

<!-- Note: Validation optionnelle. Lancer validate-create-story si besoin avant dev-story. -->

## Story

As a **utilisateur final**,
I want **voir le pays sélectionné, l'identifiant du relais actif et mon IP visible dans la fenêtre**,
so that **je puisse vérifier que ma protection fonctionne et depuis quel pays j'apparais**.

## Acceptance Criteria

**AC1** — *Pays sélectionné (drapeau + nom) affiché dans le panneau Statut*

- **Given** le tunnel est connecté (status = `connected`)
- **When** la fenêtre webview est affichée
- **Then** l'élément `#country-name` affiche `{flag_emoji} {nom_français}` (ex. `🇩🇪 ALLEMAGNE`)
- **And** la valeur vient de `GET /api/status` → champs `country_flag` + `country`
- **And** quand `status != connected` ou `country` absent → l'élément est vide (pas de placeholder)

**AC2** — *Identifiant du relais actif affiché*

- **Given** le tunnel est connecté
- **When** le panneau Statut est rendu
- **Then** l'élément `#relay-info` affiche l'ID court (ex. `de-01`, pas `relay-de-01`) concaténé avec la latence si disponible (`de-01 · 85ms`)
- **And** la source est `status.relay_id` avec suppression du préfixe `relay-`
- **And** si `relay_id == "default"` ou absent → l'élément est vide

**AC3** — *IP visible affichée*

- **Given** le tunnel est connecté
- **When** le panneau Statut est rendu
- **Then** l'élément `#ip-visible` affiche `IP dévoilée : {ip}` où `{ip}` = IP publique du relais actif vue par les services externes
- **And** la source est `status.ip` (backend : `Program.VisibleIP()` alimenté par `DetectVisibleIP` au connect)
- **And** si `status != connected` → l'élément est vide

**AC4** — *Lien « Tester ma protection »*

- **Given** le tunnel est connecté
- **When** le panneau Statut est rendu
- **Then** le lien `#test-link` est visible et pointe vers `https://plateformeliberte.fr/test-protection.html` (ouverture nouvel onglet via `target="_blank"`)
- **And** il est masqué si `status != connected`

**AC5** — *Cohérence temps réel (polling 2 s)*

- **Given** le frontend poll `fetch('/api/status')` toutes les 2 s
- **When** le relais / pays / IP change (failover, reconnexion, changement de pays via story 5.3)
- **Then** les trois champs (pays, relay-id, IP visible) se mettent à jour sans reload manuel
- **And** l'IPv6 badge (`#ipv6-badge`) n'est pas touché par cette story (géré par story 2.9 existante)

**AC6** — *Pas de régression sur les autres éléments du panneau Statut*

- **Given** les stories 2.x déjà livrées (captive banner, IPv6 badge, real IP, dot animé, bouton connect) existent
- **When** l'affichage story 5.2 est appliqué
- **Then** aucun de ces éléments n'est cassé ; test e2e frontend sur Windows et Linux confirme le rendu complet

## Tasks / Subtasks

- [ ] **Tâche 1** — Patcher `frontend/src/app.js` pour préfixer le drapeau au pays (AC1)
  - [ ] Dans `updateUI(s)` (ligne ~81) : remplacer `dom.countryName.textContent = s.country ? s.country.toUpperCase() : '';` par une concaténation `{country_flag} {country_uppercase}` avec garde-fous (champ absent = pas de préfixe, pas d'espace orphelin)
  - [ ] Ne pas styliser l'emoji en Bebas Neue (fontes web n'ont pas les glyphes emoji) — laisser le fallback système rendre le drapeau ; vérifier rendu Windows (WebView2 / Segoe UI Emoji) **et** Linux (webkit2gtk / Noto Color Emoji)

- [ ] **Tâche 2** — Raccourcir l'affichage du relay-id (AC2)
  - [ ] Dans `updateUI(s)` ajouter un helper `shortRelayID(id)` qui retire le préfixe `relay-` (`relay-de-01` → `de-01`) ; conserver l'id tel quel s'il ne commence pas par `relay-`
  - [ ] Continuer d'afficher la latence (`· 85ms`) quand présente
  - [ ] Garder le guard `s.relay_id !== 'default'` existant

- [ ] **Tâche 3** — Vérifier le plumbing backend bout-en-bout (AC1–AC3, AC5)
  - [ ] Lancer le service + UI localement, se connecter à un relais de test, inspecter `curl http://127.0.0.1:{ui.http_port}/api/status` : vérifier présence et cohérence de `country`, `country_flag`, `relay_id`, `ip`, `real_ip`
  - [ ] Confirmer que `ipchandler.handleGetStatus` remplit bien `CountryFlag` (via `registry.CountryMetaMap[code]`) pour un relais `relay-de-01` / domaine `de.levoile.dev`
  - [ ] Aucune modification Go attendue — si un champ est vide, diagnostiquer la cause (registry pas peuplé ? DetectVisibleIP pas appelé ?) avant de patcher

- [ ] **Tâche 4** — Tests automatisés
  - [ ] Étendre `internal/ui/httpserver_test.go` : ajouter un cas `TestStatusCountryFlagAndVisibleIP` qui injecte une `ipc.Response` avec `Country="Allemagne"`, `CountryFlag="🇩🇪"`, `RelayID="relay-de-01"`, `IP="203.0.113.7"` et vérifie que le JSON renvoyé par `/api/status` contient ces quatre champs
  - [ ] Vérifier qu'aucun test existant ne casse (`go test ./internal/ui/...`, `go test ./internal/ipchandler/...`, `go test ./internal/registry/...`)

- [ ] **Tâche 5** — Validation manuelle cross-platform (AC4, AC5, AC6)
  - [ ] **Windows** : build `cmd/ui` + `cmd/service`, lancer le service, ouvrir la fenêtre, se connecter → vérifier affichage pays (drapeau + nom), relay-id court, IP visible, lien `Tester ma protection` (ouvre navigateur par défaut vers `https://plateformeliberte.fr/test-protection.html`)
  - [ ] **Linux** : même chose via systemd user ou lancement direct (webkit2gtk) — valider spécifiquement le rendu du drapeau emoji (nécessite un package `noto-fonts-emoji` ou équivalent sur l'OS)
  - [ ] Simuler un failover (arrêter le relais en cours) : les trois champs doivent se mettre à jour en ≤ 2 cycles de polling

## Dev Notes

### État actuel du code (brownfield — la plupart du plumbing existe déjà)

Cette story est à **95 % une story de vérification + un petit patch frontend**. Le pipeline données est entièrement en place :

| Couche | Fichier | État |
|---|---|---|
| Relais → `/ip` | [internal/relay/ip_handler.go:1-37](internal/relay/ip_handler.go) | ✅ existe (utilise `CloudflareIPValidator` ou `RemoteAddr` en fallback) |
| Service : détection visible IP | [internal/service/service.go:2178-2191](internal/service/service.go#L2178-L2191) | ✅ existe — **mais** utilise `net.LookupHost(relayDomain)` et **non** un GET `/ip` comme le laissait entendre la spec initiale — voir « Décision d'architecture » ci-dessous |
| Service : storage | [internal/service/service.go:618-629](internal/service/service.go#L618-L629) | ✅ `VisibleIP()` / `SetVisibleIP()` atomic.Value |
| IPC handler → status | [internal/ipchandler/handler.go:76-182](internal/ipchandler/handler.go#L76-L182) | ✅ remplit `Country`, `CountryFlag`, `RelayID`, `RelayLatency`, `IP`, `RealIP` depuis `prg.Discoverer()` + `registry.CountryMetaMap` |
| Registry : map pays → drapeau | [internal/registry/countries.go:15-23](internal/registry/countries.go#L15-L23) | ✅ `{is, de, fi, us, fr, es, gb}` + emoji |
| UI HTTP : `/api/status` | [internal/ui/httpserver.go:17-145](internal/ui/httpserver.go#L17-L145) | ✅ `APIStatusResponse` inclut `country`, `country_flag`, `relay_id`, `relay_latency`, `ip`, `real_ip` |
| Frontend HTML : éléments DOM | [frontend/index.html:47-56](frontend/index.html#L47-L56) | ✅ `#country-name`, `#ip-visible`, `#relay-info`, `#test-link` déjà présents, lien déjà correct |
| Frontend JS : rendu | [frontend/src/app.js:81-102](frontend/src/app.js#L81-L102) | ⚠️ **À patcher** : drapeau non affiché, préfixe `relay-` non retiré |

### Ce qu'il reste réellement à faire

1. **2 lignes de JS** dans `updateUI()` :
   - concaténer `country_flag` devant `country`
   - retirer le préfixe `relay-` de `relay_id`
2. **1 test unitaire** dans `internal/ui/httpserver_test.go` pour verrouiller le contrat JSON (champs `country_flag` présents, non vides pour un pays connu)
3. **Validation bout-en-bout** sur Windows + Linux (drapeau emoji rendu correctement)

### Décision d'architecture — « via `/ip` » vs `net.LookupHost`

L'AC originale de l'epic mentionne « *IP visible récupérée via `/api/status` qui appelle le relais `/ip`* ». Le code actuel utilise `net.LookupHost(relayDomain)` dans [service.go:2181-2191](internal/service/service.go#L2181-L2191). **Les deux approches produisent le même résultat pratique** : dans l'architecture L3 TUN, tout le trafic sort par l'IP publique du relais, donc l'IP vue par un service externe = l'IP du domaine relais. **Ne pas changer** `DetectVisibleIP` pour cette story — le refactor (si utile) appartient à une story dédiée. Si l'argument « pourquoi ne pas appeler `/ip` » remonte en review : documenter dans la complétion que le comportement est équivalent et qu'une bascule pure GET `/ip` ajouterait une dépendance réseau au connect sans gain fonctionnel.

### Gotchas — pièges LLM à éviter

- ❌ **Ne pas** toucher à `DetectVisibleIP` ni à `ip_handler.go` — ils sont corrects.
- ❌ **Ne pas** ajouter de nouveau champ IPC ou de nouvelle action — tout est déjà dans `ipc.Response` ([internal/ipc/messages.go:64-91](internal/ipc/messages.go#L64-L91)).
- ❌ **Ne pas** réintroduire `CountryFlag` côté frontend comme un second fetch — il arrive déjà dans `/api/status`.
- ❌ **Ne pas** styliser l'emoji avec `font-family: 'Bebas Neue'` — les fontes web du projet (Bebas Neue / Rajdhani / Inter) **n'incluent pas** les glyphes emoji régionaux. Laisser le navigateur tomber en fallback sur Segoe UI Emoji (Windows) / Noto Color Emoji (Linux). Le `.country-name-display` applique Bebas Neue — le drapeau sera rendu via le fallback automatique, **c'est correct et voulu**.
- ❌ **Ne pas** uppercaser le drapeau (inutile, emoji invariant) — uppercaser seulement `country`.
- ❌ **Ne pas** casser la logique « empty when disconnected » — AC1 AC3 AC4 exigent des éléments vides hors `connected`.

### Extraction du relay_id court

Le registre produit des IDs sous la forme `relay-{code}-{num}` (ex. `relay-de-01`, cf. [internal/registry/countries_test.go:12-16](internal/registry/countries_test.go#L12-L16)). L'AC demande `de-01`. Helper minimal en JS :

```js
function shortRelayID(id) {
    if (!id) return '';
    return id.startsWith('relay-') ? id.slice('relay-'.length) : id;
}
```

Puis dans `updateUI` :

```js
if (st === 'connected' && s.relay_id && s.relay_id !== 'default') {
    var info = shortRelayID(s.relay_id);
    if (s.relay_latency) info += ' \u00b7 ' + s.relay_latency;
    dom.relayInfo.textContent = info;
} else {
    dom.relayInfo.textContent = '';
}
```

### Pattern d'affichage pays avec drapeau

```js
if (st === 'connected' && s.country) {
    var flag = s.country_flag ? s.country_flag + ' ' : '';
    dom.countryName.textContent = flag + s.country.toUpperCase();
} else {
    dom.countryName.textContent = '';
}
```

### Référence IPC / API REST

`ipc.Response` expose :
- `Country` : nom français ("Allemagne") — [handler.go:147](internal/ipchandler/handler.go#L147)
- `CountryFlag` : emoji ("🇩🇪") — [handler.go:148](internal/ipchandler/handler.go#L148)
- `RelayID` : id complet ("relay-de-01") — [handler.go:141](internal/ipchandler/handler.go#L141)
- `IP` : IP visible (résultat de `DetectVisibleIP`) — [handler.go:131](internal/ipchandler/handler.go#L131)

`APIStatusResponse` re-expose ces mêmes champs en `country`, `country_flag`, `relay_id`, `ip` — [httpserver.go:17-32](internal/ui/httpserver.go#L17-L32).

### Project Structure Notes

- **Aucun nouveau module / package** requis. Patch frontend uniquement + test Go unitaire.
- Fichiers touchés prévus : `frontend/src/app.js` (modif), `internal/ui/httpserver_test.go` (ajout d'un test). Aucun autre.
- Pas de nouveau composant UI, pas de nouvel endpoint, pas de nouvelle config.

### Testing Standards

- Test unitaire Go : `testing` stdlib + `httptest` (pattern déjà utilisé dans `httpserver_test.go`) — voir conventions [architecture.md:453-457](_bmad-output/planning-artifacts/architecture.md#L453-L457).
- Pas de framework JS de test sur ce projet — validation frontend = smoke test manuel cross-OS.
- Cible de couverture : pas de seuil strict sur le frontend ; maintenir 80 %+ sur le package `ui` Go ([architecture.md:440-458](_bmad-output/planning-artifacts/architecture.md)).

### References

- Epic story source : [epics.md:903-916](_bmad-output/planning-artifacts/epics.md#L903-L916) — AC originales (Given/When/Then)
- PRD FR10 : [prd.md:432](_bmad-output/planning-artifacts/prd.md#L432) — « voir pays sélectionné, relais actif, IP visible »
- Architecture UI : [architecture.md:624-646](_bmad-output/planning-artifacts/architecture.md#L624-L646) — layout 420×540, sidebar 150px, polling 2 s
- Architecture API REST : [architecture.md:481-497](_bmad-output/planning-artifacts/architecture.md#L481-L497) — endpoints `/api/status` `/api/registry`
- Pipeline Country/Flag : [internal/registry/countries.go:14-23](internal/registry/countries.go#L14-L23) + [internal/ipchandler/handler.go:138-153](internal/ipchandler/handler.go#L138-L153)
- UX — lien confiance : [ux-design-specification.md:52,137,208](_bmad-output/planning-artifacts/ux-design-specification.md) — `plateformeliberte.fr/test-protection.html`
- Story 5.1 (prérequis) : **pas encore créée comme story file** ; le binaire UI unique (cmd/ui + systray + webview + httpserver + frontend embed) existe déjà en brownfield. Les fichiers impactés ici (`frontend/src/app.js`, `internal/ui/httpserver.go`) sont issus de ce travail antérieur.

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List

---

### Git Intelligence Summary

Derniers commits pertinents (contexte) :

- `16a275a` — fix(deploy): align install.sh/README/service with prod — **relais**, pas d'impact UI
- `c2e1c0e` — Epic 3 complet (relais stateless) — backend, pas d'impact UI
- `bd11612` — IPv6 leak toggle (Story 2.9) — a ajouté `#ipv6-badge` et le pattern `updateUI`/settings qu'on suit ici
- `ece3270` — Sprint 2 (firewall, routing, captive) — a consolidé le modèle IPC `StatusResponse` et le polling 2 s

Patterns établis par ces commits à réutiliser :
- Extension de `APIStatusResponse` par ajout de champ optionnel JSON (zero value = non affiché côté frontend)
- Ajout de test unitaire `httpserver_test.go` couplé à chaque nouveau champ affiché
- Messages et labels frontend en français systématiquement

### Questions / Clarifications (post-implémentation)

1. **Story 5.1 pas encore contextualisée** : faudra-t-il créer rétroactivement un fichier story `5-1-binaire-ui-unique-...` pour traquer le brownfield déjà livré, ou la laisser en `backlog` et la fermer via create-story lors du prochain passage ? → Question à lever en sprint-status review.
2. **Rendu emoji sur distributions Linux minimales** (Alpine) : `noto-fonts-emoji` n'est pas dans les deps listées dans le PRD ([prd.md:357](_bmad-output/planning-artifacts/prd.md#L357)). Si absent, le drapeau s'affichera en glyphes tofu. **Décision à prendre** : ajouter la dépendance aux paquets Linux (epic 7.2) ou accepter le fallback gracieux (nom pays seul) ?

**Ultimate context engine analysis completed — comprehensive developer guide created.**
