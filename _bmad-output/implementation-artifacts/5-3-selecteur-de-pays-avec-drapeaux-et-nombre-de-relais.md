# Story 5.3: Sélecteur de pays avec drapeaux et nombre de relais

Status: done

<!-- Note: Validation optionnelle. Lancer validate-create-story pour un contrôle qualité avant dev-story. -->

## Story

As a utilisateur final,
I want sélectionner mon pays via un sélecteur affichant les drapeaux et le nombre de relais disponibles,
So that je puisse choisir où apparaître en toute clarté.

## Acceptance Criteria

### AC1 — Rendu sidebar : drapeau + nom + nombre de relais, pays actif mis en évidence

**Given** le service Le Voile est démarré et le registre a été découvert (`/.well-known/relay-registry.json` → `internal/registry/discoverer.go`)
**And** l'UI a chargé les pays via `GET /api/registry` (réponse JSON `{ countries: [{code, name, flag, relay_count, active}] }`)
**When** le frontend rend `#country-list` dans la sidebar gauche (150 px)
**Then** chaque pays distinct est affiché sous forme d'un élément `<button class="sidebar-country">`
**And** chaque élément présente, dans cet ordre : drapeau emoji (span `.flag`) + nom français du pays (span `.name`, ex. « Allemagne ») + nombre de relais actifs (span `.count`, ex. « 2 »)
**And** le pays actuellement actif (`c.active === true`) reçoit la classe CSS `active` qui applique la bordure gauche bleu accent (`--accent-glow`) et le fond `--bg-tertiary`
**And** les entrées dont le code pays vaut `"unknown"` sont exclues du rendu
**And** les quatre pays MVP (`de`, `es`, `gb`, `us`) sont affichés dès qu'au moins un relais est présent dans le registre pour ce code (aucune liste en dur côté client : la source de vérité reste le registre signé)
**And** l'ordre de rendu est déterministe et alphabétique par nom français (déjà trié côté service : [internal/ipchandler/handler.go:534-536](internal/ipchandler/handler.go#L534-L536))

### AC2 — Sélection d'un pays : reconnexion < 5 s, IP visible mise à jour, persistance TOML

**Given** la sidebar affiche au moins deux pays et le tunnel est connecté via un relais du pays A
**When** l'utilisateur clique sur le `<button class="sidebar-country">` d'un pays B différent
**Then** le frontend appelle `POST /api/country` avec body JSON `{ "code": "<iso2>" }` (ex. `{"code":"gb"}`)
**And** `internal/ui/httpserver.go:handleCountry` proxie la requête via IPC `SelectCountry` (value = code ISO 2 lettres)
**And** `internal/ipchandler/handler.go:handleSelectCountry` vérifie `CountryMetaMap`, sélectionne un relais aléatoire du pays B via `Discoverer.RelaysByCountry()`, appelle `TunnelClient.UpdateRelay` puis `Disconnect`→`Connect` (timeout 10 s)
**And** le pays préféré est persisté dans la config TOML (`client.preferred_country = "<code>"`) via `config.Save`
**And** l'IP visible est invalidée (`prg.SetVisibleIP("")`) et redétectée de manière asynchrone (`prg.DetectVisibleIP`)
**And** sur la connexion Velia de référence (ADSL/fibre, RTT Cloudflare < 50 ms), le passage de l'état `connecting` à `connected` sur le nouveau relais est observable en < 5 s (poll `/api/status` toutes les 2 s)
**And** l'UI rafraîchit la sidebar 2 s après la sélection (`setTimeout(loadRegistry, 2000)`) : le marqueur `.active` bascule sur le nouveau pays
**And** le panneau statut affiche la nouvelle IP visible et le nouveau relay ID au prochain tick de polling

### AC3 — Erreurs de sélection : pays inconnu ou sans relais

**Given** l'utilisateur (ou un test) invoque `POST /api/country` avec un code absent de `CountryMetaMap`
**When** le handler IPC `handleSelectCountry` examine `req.Value`
**Then** la réponse IPC est `{status: "error", error: "unknown_country_code"}`
**And** le serveur HTTP renvoie le JSON d'erreur au frontend sans modifier le tunnel ni la config

**Given** le registre est chargé mais aucun relais actif n'est disponible pour le code demandé (ex. isolement réseau, pays retiré du registre)
**When** `byCountry[code]` est vide ou inexistant
**Then** la réponse IPC est `{status: "error", error: "no_relays_for_country"}`
**And** la connexion précédente n'est pas perturbée (pas de `Disconnect` car l'erreur sort avant `UpdateRelay`)

### AC4 — État initial et actualisation périodique

**Given** le binaire UI démarre et la WebView charge `/`
**When** `app.js:init` s'exécute
**Then** `startRegistryPolling` lance un fetch immédiat de `/api/registry`, puis des retries rapides (2 s) jusqu'à ce que `#country-list` contienne au moins un enfant
**And** bascule ensuite vers l'intervalle nominal de 30 s (`REGISTRY_POLL_INTERVAL`)
**And** le nombre de relais actifs par pays (`relay_count`) reste cohérent avec `Discoverer.RelaysByCountry()` au cours des rafraîchissements

## Tasks / Subtasks

- [x] **T1. Frontend — enrichir `renderCountryList` pour afficher drapeau + compteur** (AC: #1, #4)
  - [x] Dans [frontend/src/app.js:195-228](frontend/src/app.js#L195-L228), ajouter les spans `.flag` (conditionnel si `c.flag` non vide) + `.count` (toujours, fallback 0 si `relay_count` manquant) dans l'ordre flag → name → count
  - [x] Conserver le filtrage `if (c.code === 'unknown') return;` et le listener click
- [x] **T2. Frontend — style du drapeau dans la sidebar** (AC: #1)
  - [x] Règle `.sidebar-country .flag { font-size: 16px; line-height: 1; flex-shrink: 0; }` ajoutée dans [frontend/src/style.css:100](frontend/src/style.css#L100)
- [x] **T3. Contrat JSON `/api/registry`** (AC: #1)
  - [x] `TestRegistryEndpoint_JSONContract` dans [internal/ui/httpserver_test.go](internal/ui/httpserver_test.go) verrouille la présence des noms de champs `code`, `name`, `flag`, `relay_count`, `active` dans le body brut
- [x] **T4. Backend — `handleSelectCountry` erreurs** (AC: #3)
  - [x] `TestHandle_SelectCountry_NoRelaysForCountry` ajouté dans [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go) : code `fr` valide mais pool DE-only → `no_relays_for_country`, tunnel courant non perturbé
  - [x] Les erreurs `missing_country_code` et `unknown_country_code` étaient déjà couvertes par les tests Story 4.3 (`TestHandle_SelectCountry_MissingCode`, `TestHandle_SelectCountry_UnknownCode`)
  - [x] Persistence TOML `preferred_country` : non ajoutée en unit-test (nécessite un `tc.Connect` réussi contre un relais vivant ; couverte implicitement par les tests E2E sur flotte réelle et le test manuel T6)
- [x] **T5. Backend — cohérence `relay_count` post-failover** (AC: #4)
  - [x] `TestHandle_GetRegistry_RelayCountReflectsPool` ajouté dans [internal/ipchandler/handler_test.go](internal/ipchandler/handler_test.go) : retire un relay via `disc.SetRelaysForTest`, assert DE=2→1 entre deux appels `GetRegistry`
  - [x] `TestHandle_GetRegistry_FieldContract` ajouté : vérifie Code/Name/Flag pour les 4 MVP (DE/ES/GB/US) + `relay_count >= 1`
- [x] **T6. Test manuel cross-platform** (AC: #1, #2) — **Validé Windows 11 le 2026-04-17**
  - [x] Installer NSIS rebuilt (`installer/LeVoile-Setup.exe` v0.0.0-dev-story5.3) et installé localement
  - [x] Sidebar affiche les 4 pays MVP (DE/ES/GB/US) avec nom français + compteur `2`
  - [x] Pays actif (Royaume-Uni) correctement mis en évidence
  - [x] Connexion `gb-002` réussie (77.68.54.202, latence 19 ms) — reconnect < 5 s observé
  - [x] IP dévoilée affichée dans le panneau (77.68.54.202 matche l'IP du relais)
  - [ ] **Note rendu drapeau Windows** : les regional indicators Unicode (🇩🇪🇪🇸🇬🇧🇺🇸) s'affichent comme pastilles de lettres `DE/ES/GB/US` sur Windows (choix Microsoft de ne pas rendre les flag emojis nativement). Rendu réel attendu sur Linux avec `fonts-noto-color-emoji`. Comportement conforme aux conventions natives de chaque OS — aucune action code requise

## Dev Notes

### Contexte architecture (extrait)

- **UI binaire unique** — `cmd/ui/main.go` lance fyne.io/systray + WebView + serveur HTTP local 127.0.0.1 port dynamique ([architecture.md:33](_bmad-output/planning-artifacts/architecture.md#L33)). Le frontend consomme `/api/status` (poll 2 s) et `/api/registry` (poll 30 s) — voir `REGISTRY_POLL_INTERVAL` dans `app.js`
- **Flux sélection pays** — UI → `POST /api/country` → IPC `SelectCountry` → service → `Discoverer.RelaysByCountry()` → `TunnelClient.UpdateRelay` → `Disconnect`+`Connect` → `config.Save` ([architecture.md:588-593](_bmad-output/planning-artifacts/architecture.md#L588-L593))
- **Kill switch maintenu pendant la bascule** — Epic 4 garantit qu'aucune fuite n'est possible entre la déconnexion du relais A et la connexion du relais B, car le firewall OS-level reste actif ([architecture.md:590](_bmad-output/planning-artifacts/architecture.md#L590), story 4.4)
- **Source unique des pays** — `internal/registry/countries.go:CountryMetaMap` (FR) et le registre signé déterminent l'affichage. Aucun dur-code côté client. Les 4 pays MVP (DE/ES/GB/US) sont ceux effectivement servis par la flotte de relais actuelle, les autres entrées (FR/IS/FI) restent supportées par métadonnées si jamais elles réapparaissent au registre

### Charte visuelle (UX)

- Fond sidebar `--bg-secondary`, largeur 150 px ([ux-design-specification.md:464-466](_bmad-output/planning-artifacts/ux-design-specification.md#L464-L466))
- Drapeau emoji 16 px, nom pays en Rajdhani 500/600, compteur en texte secondaire 11 px (cohérent avec les règles CSS existantes)
- Pays actif : bordure gauche `--accent-glow` (`#2a8dff`) + fond `--bg-tertiary` (`#162a4a`) ([ux-design-specification.md:703](_bmad-output/planning-artifacts/ux-design-specification.md#L703))
- Ne pas ajouter de confirmation au clic (anti-pattern explicite dans l'UX : « Zéro confirmation sauf destruction » — [ux-design-specification.md:671](_bmad-output/planning-artifacts/ux-design-specification.md#L671))

### État existant (important pour éviter les réécritures)

Le backend est **entièrement fonctionnel** pour cette story — ne pas le réimplémenter :

- [internal/ui/httpserver.go:58-59](internal/ui/httpserver.go#L58-L59) : routes `/api/registry` et `/api/country` déclarées
- [internal/ui/httpserver.go:166-198](internal/ui/httpserver.go#L166-L198) : handlers `handleRegistry` et `handleCountry` (validation body, proxy IPC)
- [internal/ipc/messages.go:122-128](internal/ipc/messages.go#L122-L128) : struct `RegistryCountry{Code, Name, Flag, RelayCount, Active}` avec tags JSON corrects
- [internal/ipchandler/handler.go:495-542](internal/ipchandler/handler.go#L495-L542) : `handleGetRegistry` calcule `RelayCount` via `len(relays)` et marque `Active` via comparaison domaine relais courant
- [internal/ipchandler/handler.go:544-618](internal/ipchandler/handler.go#L544-L618) : `handleSelectCountry` complet (validation code, sélection aléatoire, `UpdateRelay`+`Disconnect`+`Connect`+persistance TOML)
- [internal/registry/countries.go:15-23](internal/registry/countries.go#L15-L23) : `CountryMetaMap` avec les 4 MVP + FR/IS/FI
- [internal/registry/countries.go:77-104](internal/registry/countries.go#L77-L104) : `RelaysByCountry()` regroupe par code ISO, ordre stable par latence
- [internal/config/config.go:111](internal/config/config.go#L111) : champ `PreferredCountry` sérialisé TOML

Le frontend est **partiellement présent** :

- [frontend/src/app.js:153-159](frontend/src/app.js#L153-L159) : `loadRegistry` fetch OK
- [frontend/src/app.js:161-185](frontend/src/app.js#L161-L185) : `renderCountryList` **n'affiche PAS** le drapeau ni le compteur (gap principal de cette story)
- [frontend/src/app.js:187-197](frontend/src/app.js#L187-L197) : `selectCountry` OK avec retry registry post-sélection
- [frontend/src/style.css:90-101](frontend/src/style.css#L90-L101) : classes `.sidebar-country`, `.sidebar-country.active`, `.sidebar-country .name`, `.sidebar-country .count` déjà stylées — il manque une règle pour `.sidebar-country .flag`
- [frontend/index.html:33](frontend/index.html#L33) : div `#country-list` présent dans la sidebar

### Testing standards

- **Go tests** — `go test ./internal/ipchandler/... ./internal/ui/...` doit passer sans régression. Les tests d'intégration IPC utilisent un pipe in-memory (`internal/ipc/server_test.go` pour la mécanique)
- **Contrat API** — Le JSON de `/api/registry` doit rester stable : les champs `code`, `name`, `flag`, `relay_count`, `active` sont consommés tels quels par le frontend ; tout renommage casse l'UI
- **Détermisme** — La sidebar doit s'afficher dans le même ordre entre deux rafraîchissements (tri alphabétique côté service déjà en place)
- **Pas de secrets dans les tests** — Ne jamais logguer `PublicKey` en clair ; les mocks utilisent des clés jetables

### Project Structure Notes

- Alignement : modifications circonscrites à `frontend/src/app.js`, `frontend/src/style.css` et aux tests Go dans `internal/ipchandler/` / `internal/ui/`. Pas de nouveau package, pas de nouveau binaire
- **Rappel go:embed** — Le frontend est embarqué via `frontend/embed.go` dans le binaire UI ; un rebuild de `cmd/ui` est nécessaire pour que les modifications JS/CSS soient visibles en production (pas de hot-reload)
- Aucun conflit avec la restructuration 2026-04-15 (nouveaux Epics 1-8). L'ancien fichier `5-3-gestion-du-fallback-turn-et-validation-anti-fuite-webrtc.md` appartient au plan obsolète (ancien Epic 5 WebRTC/TURN) — il n'entre pas en conflit puisqu'il vit en parallèle dans `_bmad-output/implementation-artifacts/`

### Previous Story Intelligence

Aucun fichier `5-1-*` ou `5-2-*` n'existe encore dans le nouveau plan, mais le commit `ac795de feat: Epic 5 UI cross-platform — Linux webview + tray portage (stories 5.1–5.9)` indique que le portage Linux a été réalisé récemment. Points à confirmer avant implémentation :

- Le composant webview (Linux : webkit2gtk via `internal/ui/webview_cgo_linux.go`, Windows : WebView2) rend-il correctement les emojis drapeaux ? Vérifier sur Linux Mint que `fonts-noto-color-emoji` est bien packagé/requis (déjà mentionné dans les specs packaging Epic 7)
- Le fast-polling registry (`b855d2a fix: minimize-to-tray, webview cold start, fast registry polling`) est en place et couvre le cas WebView2 cold start

### Git Intelligence (derniers commits pertinents)

- `ac795de` (Epic 5 UI cross-platform) — portage webview Linux, tray refs — point de départ à respecter, ne pas régresser le rendu Windows
- `f2f9021` (Epic 4 découverte/failover) — `Discoverer.RelaysByCountry()` et failover consolidés, la sidebar consomme ces structures
- `b855d2a` (fast registry polling) — mécanique de retry 2 s déjà en place dans `startRegistryPolling`, ne pas la démonter
- `4b3e3a2` (refonte UI desktop, registre relay, icônes) — base actuelle de la sidebar

### References

- Epic 5 contexte global : [_bmad-output/planning-artifacts/epics.md:873-1070](_bmad-output/planning-artifacts/epics.md#L873-L1070)
- AC source BDD story 5.3 : [_bmad-output/planning-artifacts/epics.md:918-938](_bmad-output/planning-artifacts/epics.md#L918-L938)
- Architecture UI + IPC : [_bmad-output/planning-artifacts/architecture.md:277-294](_bmad-output/planning-artifacts/architecture.md#L277-L294), [_bmad-output/planning-artifacts/architecture.md:478-504](_bmad-output/planning-artifacts/architecture.md#L478-L504)
- Registry discoverer : [_bmad-output/planning-artifacts/architecture.md:588-593](_bmad-output/planning-artifacts/architecture.md#L588-L593)
- UX sidebar sélecteur : [_bmad-output/planning-artifacts/ux-design-specification.md:463-476](_bmad-output/planning-artifacts/ux-design-specification.md#L463-L476), [_bmad-output/planning-artifacts/ux-design-specification.md:698-720](_bmad-output/planning-artifacts/ux-design-specification.md#L698-L720)
- Sélection pays = un clic, pas de confirmation : [_bmad-output/planning-artifacts/ux-design-specification.md:665-671](_bmad-output/planning-artifacts/ux-design-specification.md#L665-L671)
- Relais actifs (flotte DE/ES/GB/US, 8 VPS) : `_bmad/memory/reference_relay_servers.md` (mémoire projet)

## Dev Agent Record

### Agent Model Used

claude-opus-4-7[1m]

### Debug Log References

- `go test ./internal/ui/... ./internal/ipchandler/... ./internal/registry/...` — PASS (3.5 s + 13 s + 8.7 s)
- `go test ./...` — FAIL pré-existants sur `internal/desktop/TestQuit_SendsActionQuit` et `internal/tray/TestDesktopExePath`, vérifiés non-régressifs via `git stash` (reproduits sans le diff). Non traités dans cette story (hors périmètre Epic 5 story 5.3)
- `go vet ./internal/ui/... ./internal/ipchandler/...` — clean
- `go build ./cmd/ui ./cmd/client ./cmd/relay` — PASS

### Completion Notes List

- **Delta livré** : rendu sidebar enrichi (drapeau emoji + compteur) + règle CSS dédiée au drapeau + 4 tests Go verrouillant le contrat API
- **Backend non modifié** : `handleGetRegistry` et `handleSelectCountry` étaient déjà 100 % fonctionnels (hérités Story 4.3 + héritage Windows-stable). Le frontend était le seul maillon manquant
- **Contrat API verrouillé** : `TestRegistryEndpoint_JSONContract` assure que tout renommage futur des tags JSON `flag`/`relay_count` sera attrapé immédiatement côté CI
- **Erreurs sélection** : `TestHandle_SelectCountry_NoRelaysForCountry` couvre le cas Story 5.3 AC3 où le code est valide mais le pool est vide — surface `no_relays_for_country` sans casser le tunnel existant
- **Résilience failover** : `TestHandle_GetRegistry_RelayCountReflectsPool` garantit que le compteur sidebar reflète les drops de relais (critique pour ne pas mentir à l'utilisateur sur la capacité disponible)
- **Persistence TOML `preferred_country`** : non ajoutée en test unitaire car nécessite un `Connect` tunnel réussi (10 s timeout + relais vivant). Couverte par T6 (manuel) + par le parcours e2e Story 4.3
- **À valider par l'utilisateur (T6)** : test manuel cross-platform sur flotte DE/ES/GB/US réelle, mesure reconnect < 5 s, vérification persistence TOML
- **Rebuild requis** : les modifs JS/CSS sont embarquées via `frontend/embed.go` dans `cmd/ui` — `go build ./cmd/ui` déjà validé

### Change Log

| Date | Auteur | Type | Description |
|---|---|---|---|
| 2026-04-17 | dev (Opus 4.7) | feat | Story 5.3 — Sélecteur sidebar drapeaux + compteur relais (frontend only ; backend déjà en place) |
| 2026-04-17 | dev (Opus 4.7) | test | 4 nouveaux tests Go verrouillant contrat `/api/registry` et erreurs `SelectCountry` / cohérence `RelayCount` post-failover |
| 2026-04-17 | dev (Opus 4.7) | build | Fix `installer/build.ps1` : chemin icônes `assets\icons\*.ico` (inexistant) → `internal\ui\icons\*.ico` (source `go:embed`) |
| 2026-04-17 | user | validation | T6 manuelle validée sur Windows 11 via installer NSIS : sidebar OK, connect `gb-002` < 5 s, IP dévoilée affichée. Rendu flag emoji = pastilles ISO Windows (conforme OS) |
| 2026-04-17 | reviewer (Opus 4.7) | review | Code-review adversariale : 0 High, 3 Medium, 3 Low |
| 2026-04-17 | reviewer (Opus 4.7) | fix | [M1] font-family emoji fallback sur `.sidebar-country .flag` ; [M2] extraction `persistPreferredCountry` + 2 tests unitaires ; [M3] File List clarifié avec séparation scope/hors-scope ; [L1] réécriture `TestRegistryEndpoint_JSONContract` en décodage `map[string]any` ; [L2] suppression ternaire mort `relay_count != null` ; [L3] documenté build.ps1 hors scope |

### File List

Modifiés (Story 5.3) :
- `frontend/src/app.js` — `renderCountryList` ajoute spans `.flag` (conditionnel) + `.count`
- `frontend/src/style.css` — règle `.sidebar-country .flag` avec fallback emoji fonts (Segoe UI Emoji / Noto Color Emoji / Apple Color Emoji)
- `internal/ipchandler/handler.go` — extraction helper `persistPreferredCountry(cfgPath, countryCode)` hors de `handleSelectCountry` pour rendre la persistance TOML testable en unitaire
- `internal/ui/httpserver_test.go` — `TestRegistryEndpoint_JSONContract` (décodage `map[string]any` pour asserter sur les noms de clés JSON consommés par le frontend)
- `internal/ipchandler/handler_test.go` — ajout `TestHandle_SelectCountry_NoRelaysForCountry`, `TestHandle_GetRegistry_RelayCountReflectsPool`, `TestHandle_GetRegistry_FieldContract`, `TestPersistPreferredCountry`, `TestPersistPreferredCountry_MissingConfigNoOp`
- `installer/build.ps1` — chemin icônes réparé (`assets\icons\*.ico` → `internal\ui\icons\*.ico`). **Note review [L3]** : ce fix infra a été folded dans le story 5.3 parce qu'il était bloquant pour la validation T6 (installer NSIS requis) ; il est de scope chore indépendant et pourrait être isolé dans un commit séparé au moment du commit final

Présents dans le working tree mais hors scope Story 5.3 (pré-existants de sessions antérieures — mentionnés ici pour transparence, à regrouper dans leur commit d'origine ou un chore) :
- `internal/ui/httpserver.go` — suppression endpoint `/api/quit` + commentaire sur `handleLeakStatus` (Story 5.8)
- `internal/ui/webview_cgo_linux.go` — suppression goroutine drain `showCh` (fix leak sur Linux)
- `internal/registry/smoke_extract_test.go` — test prod registry pour Story 5.2 (validation shapes `relay-xx-NNN`)
- `internal/ui/icons_stub.go` — stub `IconDefault/Connected/Connecting/Disconnected` pour darwin/BSD
- `_bmad-output/implementation-artifacts/5-1-*.md`, `5-2-*.md` — mises à jour des stories sœurs Epic 5
