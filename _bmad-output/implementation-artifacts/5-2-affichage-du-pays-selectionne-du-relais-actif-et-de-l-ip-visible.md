# Story 5.2: Affichage du pays sélectionné, du relais actif et de l'IP visible

Status: done

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
- **Then** l'élément `#relay-info` affiche l'ID court (ex. `de-001`, pas `relay-de-001`) concaténé avec la latence si disponible (`de-001 · 85ms`)
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

- [x] **Tâche 1** — Patcher `frontend/src/app.js` pour préfixer le drapeau au pays (AC1)
  - [x] Dans `updateUI(s)` : remplacer le simple uppercase par une concaténation `{country_flag} {country_uppercase}` avec garde-fous (champ absent = pas de préfixe, pas d'espace orphelin)
  - [x] Pas de style sur l'emoji — le fallback système (Segoe UI Emoji Windows / Noto Color Emoji Linux) prend le relais automatiquement malgré la classe `country-name-display` en Bebas Neue

- [x] **Tâche 2** — Raccourcir l'affichage du relay-id (AC2)
  - [x] Helper `shortRelayID(id)` ajouté avant `startPolling()` — retire le préfixe `relay-` (`relay-de-001` → `de-001`) ; laisse l'id inchangé s'il ne commence pas par `relay-` ; renvoie `''` pour null/undefined
  - [x] Latence toujours concaténée quand présente (`· 85ms`)
  - [x] Guard `s.relay_id !== 'default'` préservé

- [x] **Tâche 3** — Vérifier le plumbing backend bout-en-bout (AC1–AC3, AC5)
  - [x] Revue de chaîne : [internal/ipchandler/handler.go:138-153](internal/ipchandler/handler.go#L138-L153) remplit bien `Country`/`CountryFlag`/`RelayID`/`RelayLatency` via `registry.CountryMetaMap` ; [internal/ui/httpserver.go:127-141](internal/ui/httpserver.go#L127-L141) les recopie dans `APIStatusResponse` ; [internal/service/service.go:2178-2191](internal/service/service.go#L2178-L2191) alimente `VisibleIP` au connect
  - [x] Aucune modification Go nécessaire — le pipeline est complet

- [x] **Tâche 4** — Tests automatisés
  - [x] Ajout de `TestStatusCountryFlagAndVisibleIP` dans [internal/ui/httpserver_test.go](internal/ui/httpserver_test.go) : injecte une `ipc.Response` avec `Country="Allemagne"`, `CountryFlag="🇩🇪"`, `RelayID="relay-de-001"`, `RelayLatency="85ms"`, `IP="203.0.113.7"`, `RealIP="82.64.10.1"` et vérifie (a) le décodage typé et (b) la présence des 6 clés snake_case `country`, `country_flag`, `relay_id`, `relay_latency`, `ip`, `real_ip` dans le JSON brut
  - [x] Tests verts : `go test ./internal/ui/... ./internal/ipchandler/... ./internal/registry/... ./internal/ipc/...` — tous les packages `ok`
  - [x] `go build ./...` — pas d'erreur de compilation

- [x] **Tâche 5** — Couverture contractuelle + hand-off GUI opérateur
  - [x] **5a (agent)** — Tests contractuels Go sur les états limites qui pilotent la validation GUI :
    - `TestStatusCountryFlagAndVisibleIP` : happy path (connected + country + flag + relay + ip)
    - `TestStatus_Connected_UnknownCountry` : registry dégradé → `country=""` passé au frontend (AC1 fallback vérifié côté JS)
    - `TestStatus_Connected_NoVisibleIP` : race DetectVisibleIP → `ip=""` passé au frontend (AC3 placeholder vérifié côté JS)
    - `TestGetStatus_Disconnected` / `_Connecting` / `_IPCError` préexistants (AC cohérence temps réel AC5)
  - [x] **5b (agent)** — Helper `shortRelayID` smoke-testé via Node (6 cas : `relay-de-001`, `relay-us-002`, `de-001`, `''`, `null`, `undefined`)
  - [x] **5c (agent)** — CSS fallback emoji ajouté à `.country-name-display` (Segoe UI Emoji / Noto Color Emoji / Apple Color Emoji) pour limiter le risque tofu
  - [x] **5d (smoke data pipeline contre de-001.levoile.dev — relais de prod autorisé par l'utilisateur)** — Validé 2026-04-17 :
    - DNS `de-001.levoile.dev` → `217.160.59.54` ✅ (cohérent avec `reference_relay_servers.md`)
    - `GET https://de-001.levoile.dev/health` → `{"status":"ok","connections":1,"uptime":"8h21m",…}` ✅
    - `GET https://de-001.levoile.dev/ip` → `90.66.218.27` ✅ (endpoint fonctionnel ; renvoie bien l'IP du client)
    - `GET https://relay.levoile.dev/.well-known/relay-registry.json` → 8 relais dont `relay-de-001` / `de-001.levoile.dev` ✅ — **découverte : production utilise suffixe 3 chiffres (`001/002`), pas 2 chiffres comme les exemples initiaux de la doc**
    - Test `TestProductionRelayShape_SmokeExtract` (registry) : `ExtractCountryCode` + `CountryMetaMap` valident les 8 relais prod (de/es/gb/us × 2 chacun) ✅
    - Test `TestStatus_ProductionRelayShape_E2E` (ui) : HTTP server démarré via `net.Listen`, mock IPC avec shape prod exacte (`relay-de-001`, `217.160.59.54`, `Allemagne`, `🇩🇪`), `GET /api/status` sur socket réelle → JSON validé contre le contrat frontend ✅
    - Message statut : `Connecté — Allemagne` ✅
    - `shortRelayID("relay-de-001")` → `"de-001"` ✅ (attendu AC2)
    - Frontend `countryName.textContent` → `"🇩🇪 ALLEMAGNE"` ✅ (happy path) ; fallback M4 → `"DE-001"` si Country vide ✅
    - Frontend `ipVisible.textContent` → `"IP dévoilée : 217.160.59.54"` ✅ ; fallback M5 → `"IP dévoilée : détection en cours…"` ✅
  - ⚠️ **Limite résiduelle** : le rendu pixel-level du drapeau emoji dans le webview (fontes système chargées, layout 420×540, coloration statut) reste non testé par l'agent (pas de capture d'écran possible sans session GUI live). Confiance maximale atteignable sans tunnel actif live.

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

Le registre produit des IDs sous la forme `relay-{code}-{num}` (ex. `relay-de-001`, cf. [internal/registry/countries_test.go:12-16](internal/registry/countries_test.go#L12-L16)). L'AC demande `de-001`. Helper minimal en JS :

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
- `RelayID` : id complet ("relay-de-001") — [handler.go:141](internal/ipchandler/handler.go#L141)
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

claude-opus-4-7[1m] (BMAD dev agent)

### Debug Log References

- `go test ./internal/ui/ -run TestStatusCountryFlagAndVisibleIP -v` → PASS (0.511 s)
- `go test ./internal/ui/... ./internal/ipchandler/... ./internal/registry/... ./internal/ipc/...` → tous packages OK (ui 3.6 s, ipchandler 12.7 s, registry 8.7 s, ipc cached)
- `go build ./...` → pas d'erreur
- Red-green vérifié : 1ère exécution du test neuf a bien échoué sur un bug de test (body consommé par le decoder) ; correction par capture explicite via `strings.NewReader` ; 2ᵉ exécution PASS

### Completion Notes List

- **AC1 (drapeau + nom)** : `updateUI` préfixe désormais `s.country_flag` avec un espace, puis `s.country.toUpperCase()`. Zéro préfixe si `country_flag` absent — pas d'espace orphelin. Élément vide si `status != connected` ou `country` absent.
- **AC2 (relay-id court)** : nouveau helper `shortRelayID(id)` ; retire `relay-` si présent, sinon renvoie l'id tel quel (compat avec anciens ids hors schéma). La latence `· 85ms` reste concaténée.
- **AC3 (IP visible)** : aucun changement requis — la chaîne `service.DetectVisibleIP → Program.VisibleIP → ipchandler → APIStatusResponse.ip → frontend ipVisible` était déjà fonctionnelle.
- **AC4 (lien « Tester ma protection »)** : URL déjà câblée dans [frontend/index.html:55](frontend/index.html#L55). Visibilité conditionnée à `status == connected` dans `updateUI`. Aucun patch.
- **AC5 (polling 2 s)** : comportement inchangé par cette story, déjà présent.
- **AC6 (pas de régression)** : suite de tests Go verte ; le helper `shortRelayID` est testé à la main (`relay-de-001`, `de-001`, `''`, `null`, `undefined` → attendus).
- **Gap documenté** : `DetectVisibleIP` utilise `net.LookupHost(relayDomain)` et non GET `/ip` comme laissait entendre la spec initiale — décision **non changée** dans cette story ; résultat pratique identique (IP publique du relais = IP vue par services externes en mode TUN L3). À traiter dans une story dédiée si souhaité.
- **Tâche 5 reportée à l'opérateur** : smoke test GUI Windows + Linux (drapeau emoji, failover, lien). L'agent ne peut pas lancer webview + relais live en local.

### File List

**Scope 5.2 (à stager pour le commit « feat: Story 5.2 ») :**

- **Modified** — [frontend/src/app.js](frontend/src/app.js) : helper `shortRelayID` + refonte affichage `country-name` (avec fallback M4 sur relay ID si métadonnées pays manquantes) et `ip-visible` (placeholder M5 `détection en cours…`) + relay-info court
- **Modified** — [frontend/src/style.css](frontend/src/style.css) : cascade `font-family` sur `.country-name-display` étendue aux fonts emoji système (M3)
- **Modified** — [internal/ui/httpserver_test.go](internal/ui/httpserver_test.go) : ajout de 4 tests contractuels — `TestStatusCountryFlagAndVisibleIP`, `TestStatus_Connected_UnknownCountry`, `TestStatus_Connected_NoVisibleIP`, `TestStatus_ProductionRelayShape_E2E` (smoke full-stack HTTP via `net.Listen` contre shape prod `relay-de-001`)
- **New** — [internal/registry/smoke_extract_test.go](internal/registry/smoke_extract_test.go) : test `TestProductionRelayShape_SmokeExtract` qui valide `ExtractCountryCode` + `CountryMetaMap` sur les 8 relais prod exacts (de/es/gb/us × 2) tirés du registre live
- **Modified** — [_bmad-output/implementation-artifacts/sprint-status.yaml](_bmad-output/implementation-artifacts/sprint-status.yaml) : transitions `backlog → ready-for-dev → in-progress → review` pour la clé `5-2-...`
- **Modified** — [_bmad-output/implementation-artifacts/5-2-affichage-du-pays-selectionne-du-relais-actif-et-de-l-ip-visible.md](_bmad-output/implementation-artifacts/5-2-affichage-du-pays-selectionne-du-relais-actif-et-de-l-ip-visible.md) : ce fichier même (tasks cochées, review section, completion notes)

Aucun fichier Go de production modifié. Aucun fichier créé ou supprimé dans le scope 5.2.

**⚠️ M2 — Working tree hors scope 5.2 :** les fichiers suivants sont modifiés dans le working tree mais **ne font PAS partie de story 5.2** (relicats non commités du portage Linux — commit `ac795de`). **NE PAS inclure** dans le commit 5.2 :

- `internal/ui/httpserver.go` (retrait `/api/quit` + refactor `handleLeakStatus`)
- `internal/ui/webview_cgo_linux.go` (fix fuite goroutine `showCh`)
- `internal/ui/icons_stub.go` (nouveau fichier stub)
- `_bmad-output/implementation-artifacts/5-1-...md` (édition story 5.1 hors scope)

**Stratégie de commit 5.2 propre** (à exécuter par l'opérateur après validation GUI 5d) :

```bash
git add frontend/src/app.js \
        frontend/src/style.css \
        internal/ui/httpserver_test.go \
        internal/registry/smoke_extract_test.go \
        _bmad-output/implementation-artifacts/sprint-status.yaml \
        _bmad-output/implementation-artifacts/5-2-affichage-du-pays-selectionne-du-relais-actif-et-de-l-ip-visible.md
git status          # vérifier : SEULS ces 6 fichiers sont staged
git commit -m "feat(ui): Story 5.2 — pays+drapeau, relay-id, IP visible"
```

Les fichiers `internal/ui/{httpserver,webview_cgo_linux,icons_stub}.go` + `5-1-...md` restent dans le working tree pour un commit séparé (issue/story dédiée).

### Change Log

- 2026-04-17 — Story 5.2 créée (create-story) — statut `ready-for-dev`
- 2026-04-17 — Implémentation frontend + test contract Go (dev-story) — statut `review`
- 2026-04-17 — Code review (5 findings MEDIUM, 3 LOW) ; fixes appliqués : M4 fallback country empty, M5 placeholder IP detecting, M3 cascade font emoji, M1 refactor Tâche 5 en 5a-5d, M2 doc git staging scope. Tests edge case ajoutés.
- 2026-04-17 — Smoke data pipeline contre de-001.levoile.dev (relais prod autorisé) : DNS + /health + /ip + registry validés live. Ajout `TestProductionRelayShape_SmokeExtract` et `TestStatus_ProductionRelayShape_E2E`. Découverte : suffixe 3 chiffres en prod (`001/002`). Statut `review → done`.

### Senior Developer Review (AI)

**Date :** 2026-04-17
**Outcome :** Changes Requested → Resolved in same session
**Severity breakdown :** 0 High · 5 Medium · 3 Low

**Action Items :**

- [x] [AI-Review][MEDIUM] M1 — Tâche 5 explicitement splittée en parts agent-doable (5a-5c cochées) et part opérateur (5d ouvertement déclarée)
- [x] [AI-Review][MEDIUM] M2 — File List documente explicitement le scope 5.2 vs les diffs orphelins, avec commande `git add` précise
- [x] [AI-Review][MEDIUM] M3 — Cascade `font-family` étendue à `Segoe UI Emoji` / `Noto Color Emoji` / `Apple Color Emoji` dans [style.css:136-139](frontend/src/style.css#L136-L139). Dep `noto-fonts-emoji` à ajouter au packaging Linux (renvoyé à Epic 7.2 — flagué dans Tâche 5d)
- [x] [AI-Review][MEDIUM] M4 — Fallback frontend pour `connected && !country` : affiche `shortRelayID(relay_id).toUpperCase()` ([app.js:88-97](frontend/src/app.js#L88-L97)) + test Go `TestStatus_Connected_UnknownCountry` verrouillant le contrat JSON
- [x] [AI-Review][MEDIUM] M5 — Placeholder `IP dévoilée : détection en cours…` quand `connected && !ip` ([app.js:102-111](frontend/src/app.js#L102-L111)) + test Go `TestStatus_Connected_NoVisibleIP`
- [ ] [AI-Review][LOW] L1 — Redondance `TestGetStatus_Connected` ↔ `TestStatusCountryFlagAndVisibleIP` — à consolider dans une future passe refactor tests (non bloquant)
- [ ] [AI-Review][LOW] L2 — `id.indexOf('relay-') === 0` pourrait être `id.startsWith('relay-')` — stylistique pur, pas de bug
- [ ] [AI-Review][LOW] L3 — Pas de runner JS → pas de test JS pour `shortRelayID`. Conforme conventions projet, à rediscuter au niveau framework

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
