# Story 5.4 : Bouton Connect/Disconnect

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
je veux pouvoir connecter et déconnecter Le Voile via un bouton dans la fenêtre,
afin de contrôler ma protection sans devoir passer par le tray.

## Acceptance Criteria

1. **AC1 — Clic "Connecter" depuis l'état Déconnecté**
   **Given** la fenêtre webview est ouverte et `/api/status` rapporte `status == "disconnected"`
   **And** aucun captive portal n'est détecté
   **When** l'utilisateur clique sur le bouton vert "Connecter"
   **Then** le frontend appelle `fetch('/api/connect', {method: 'POST'})`
   **And** le serveur HTTP local (`internal/ui/httpserver.go`) envoie `ipc.ActionConnect` au service
   **And** au polling `/api/status` suivant (≤ 2 s) le dot passe orange (connecting, pulse 1.5 s), puis vert (connected)
   **And** l'icône tray se met à jour au polling IPC indépendant du tray (≤ 2 s)
   **And** si la réponse JSON contient `error`, le champ est affiché dans la zone statut

2. **AC2 — Clic "Déconnecter" depuis l'état Connecté**
   **Given** `/api/status` rapporte `status == "connected"`
   **When** l'utilisateur clique sur le bouton transparent bordé rouge "Déconnecter"
   **Then** le frontend appelle `fetch('/api/disconnect', {method: 'POST'})`
   **And** le serveur HTTP local envoie `ipc.ActionDisconnect` au service via IPC
   **And** le service exécute la séquence stricte `tunnel.Disconnect() → firewall.Deactivate() → routing.Teardown() → tun.Close()` (déjà implémentée dans `internal/ipchandler/handler.go:handleDisconnect`)
   **And** au polling suivant le dot passe rouge et le bouton redevient "Connecter"
   **And** l'icône tray se met à jour (rouge)

3. **AC3 — Visibilité du bouton selon l'état**
   **Given** le frontend poll `/api/status` toutes les 2 secondes
   **When** `updateUI(status)` traite la réponse
   **Then** le bouton s'adapte selon le tableau (source : `ux-design-specification.md` lignes 882-886) :
   | État service | Action visible |
   |---|---|
   | `connected` | Bouton "Déconnecter" (transparent, rouge) |
   | `connecting` (transition / reconnect) | Aucun bouton |
   | `disconnected` (hors captive portal) | Bouton "Connecter" (vert `#4ade80`) |
   | `error` / aucun relais | Aucun bouton |
   | captive portal détecté (`s.captive_portal == true`) | Aucun bouton (banner captive à la place) |
   **And** lors d'un pays sélectionné ≠ pays actuellement connecté, le bouton devient "Connecter" (vert) pour déclencher une re-connexion au nouveau pays

4. **AC4 — Idempotence et protection double-clic**
   **Given** l'utilisateur clique rapidement plusieurs fois sur le bouton
   **When** la requête est en cours
   **Then** le bouton est `disabled` tant que `fetch()` n'a pas résolu
   **And** aucune requête redondante n'est envoyée
   **And** le service est idempotent côté IPC : Connect appelé quand déjà connecté = no-op, Disconnect appelé quand déjà déconnecté = no-op (comportement existant de `handleConnect` / `handleDisconnect`)

5. **AC5 — Styling conforme charte**
   **Given** le bouton est rendu
   **When** l'état est `disconnected`
   **Then** fond `#4ade80`, texte `#0b1526`, Rajdhani 600, `min-height: 44px` (cible tactile WCAG)
   **And** hover : filter brightness(1.1) + `box-shadow: 0 0 16px rgba(74, 222, 128, 0.3)`
   **And** `aria-label` dynamique "Se connecter à [pays]"
   **Given** l'état est `connected`
   **When** le bouton "Déconnecter" est rendu
   **Then** fond transparent, bordure `#d42b2b` à 30 % opacité, texte `#d42b2b`, Rajdhani 600
   **And** hover : fond rouge 10 % opacité, bordure pleine
   **And** `aria-label` "Se déconnecter"
   **And** `disabled` → `opacity: 0.5; pointer-events: none`

## Tasks / Subtasks

- [ ] Task 1 — Endpoint `POST /api/disconnect` (AC: #2)
  - [ ] 1.1 Dans `internal/ui/httpserver.go`, enregistrer `s.mux.HandleFunc("/api/disconnect", s.handleDisconnect)` dans `NewHTTPServer` (à la ligne suivant l'enregistrement de `/api/connect` ligne 57)
  - [ ] 1.2 Implémenter `handleDisconnect` calqué sur `handleConnect` (ligne 147) : `POST` uniquement, envoie `ipc.ActionDisconnect` via `s.sendIPC`, encode la réponse avec `actionResponse(resp)`
  - [ ] 1.3 Aucune modification de `internal/ipc/messages.go` (`ActionDisconnect = "disconnect"` existe déjà ligne 16)
  - [ ] 1.4 Aucune modification de `internal/ipchandler/handler.go` (`handleDisconnect` existe déjà ligne 240, case ligne 43)

- [ ] Task 2 — Tests Go handler (AC: #2, #4)
  - [ ] 2.1 Dans `internal/ui/httpserver_test.go`, ajouter `TestHandleDisconnect_Success` (mock IPC retourne `StatusDisconnected`, vérifie JSON `{"status":"disconnected"}`, code HTTP 200)
  - [ ] 2.2 Ajouter `TestHandleDisconnect_IPCError` (mock IPC retourne erreur → `sendIPC` renvoie `Status: StatusDisconnected`, vérifier absence de panique)
  - [ ] 2.3 Ajouter `TestHandleDisconnect_MethodNotAllowed` (GET → 405)
  - [ ] 2.4 Ajouter `TestHandleDisconnect_ErrorFieldIncluded` (IPC retourne `Response{Status: StatusError, Error: "msg"}` → JSON contient `"error":"msg"`)
  - [ ] 2.5 Calquer exactement la structure des tests existants `TestHandleConnect_*` — même mock IPC (`SafeIPCClient` avec handler injecté)

- [ ] Task 3 — Frontend `toggleConnect()` bidirectionnel (AC: #1, #2, #4)
  - [ ] 3.1 Dans `frontend/src/app.js`, modifier `toggleConnect()` (ligne 200) pour lire l'état courant via variable module (ex: `currentStatus` ou `lastStatus.status`)
  - [ ] 3.2 Si `status === 'connected'` → `fetch('/api/disconnect', {method: 'POST'})` sinon `fetch('/api/connect', {method: 'POST'})`
  - [ ] 3.3 Désactiver le bouton (`btn.disabled = true`) avant l'appel, ré-activation gérée naturellement par le prochain cycle `updateUI` (ne pas forcer ré-activation manuelle — évite race avec re-render)
  - [ ] 3.4 Si la réponse JSON contient `error`, afficher dans `dom.text` (déjà fait pour connect ligne 206 — étendre à disconnect)
  - [ ] 3.5 Stocker le dernier status (ex: `var lastStatus = null;` au top-level, mis à jour en début de `updateUI(s)`) pour que `toggleConnect` puisse décider sans refetch

- [ ] Task 4 — Frontend `updateUI()` : visibilité/style bouton selon état (AC: #3, #5)
  - [ ] 4.1 Dans `frontend/src/app.js:updateUI`, remplacer le bloc ligne 115-124 pour couvrir les 3 cas : `connected` → bouton "Déconnecter" classe `btn btn-disconnect`, `disconnected` (+ pas captive) → bouton "Connecter" classe `btn btn-connect`, autres → classe `btn hidden`
  - [ ] 4.2 Mettre à jour `aria-label` dynamiquement : "Se connecter à [pays]" quand "Connecter", "Se déconnecter" quand "Déconnecter"
  - [ ] 4.3 Gérer le country mismatch : si `selectedCountryName && selectedCountryName !== s.country` et `status === 'connected'`, afficher "Connecter" (vert) pour déclencher re-connexion au nouveau pays — logique déjà partiellement présente (selectedCountryName géré dans `renderCountryList` et `selectCountry`)
  - [ ] 4.4 En état `error` ou sans relais (status === 'error' ou status === 'disconnected' avec champ error) → masquer le bouton
  - [ ] 4.5 Utiliser `btn.textContent` (jamais `innerHTML`) pour le libellé — learning 10-2 #1

- [ ] Task 5 — Styling `.btn-disconnect` (AC: #5)
  - [ ] 5.1 Dans `frontend/src/style.css`, ajouter à proximité de `.btn-connect` (ligne 155) :
    ```css
    .btn-disconnect {
        background: transparent;
        color: var(--alert, #d42b2b);
        border: 1px solid rgba(212, 43, 43, 0.3);
    }
    .btn-disconnect:hover {
        background: rgba(212, 43, 43, 0.1);
        border-color: rgba(212, 43, 43, 1);
    }
    ```
  - [ ] 5.2 Vérifier que les règles communes `.btn` (min-height 44px, disabled opacity/pointer-events) s'appliquent déjà à `.btn-disconnect` via cascade
  - [ ] 5.3 Si la variable CSS `--alert` n'existe pas, utiliser `#d42b2b` littéral (cohérent avec `.btn-connect` qui utilise `var(--status-secure)`)

- [ ] Task 6 — Tests frontend (AC: #1, #2, #3, #5)
  - [ ] 6.1 Tests manuels DOM : `go test ./internal/ui/...` avec tests qui servent le frontend embarqué et vérifient la présence de `btn-connect` dans `index.html` (smoke test)
  - [ ] 6.2 Validation manuelle E2E (requise en Dev Agent Record — cocher après vérif) :
    - [ ] Service démarré, UI lancée → état déconnecté → bouton vert "Connecter" visible
    - [ ] Clic "Connecter" → transition orange (sans bouton) → vert (bouton "Déconnecter" rouge visible)
    - [ ] Clic "Déconnecter" → retour rouge déconnecté, bouton "Connecter" vert réapparaît
    - [ ] Double-clic rapide → une seule requête observée (DevTools Network)
    - [ ] Sélection d'un pays ≠ pays connecté → bouton "Connecter" vert (pas de disconnect button)
    - [ ] Portail captif détecté (simulation `/api/status` → `captive_portal: true`) → aucun bouton connect/disconnect

- [ ] Task 7 — Build + régressions (AC: tous)
  - [ ] 7.1 `go build ./cmd/ui/...` OK
  - [ ] 7.2 `go build ./cmd/client/... ./cmd/relay/...` OK (aucune régression inter-module)
  - [ ] 7.3 `go test ./internal/ui/... ./internal/ipchandler/... ./internal/ipc/...` → tous passent
  - [ ] 7.4 Vérifier qu'aucun nouveau `log.Println` / `fmt.Println` n'a été introduit (learning 10-2 #7)

## Dev Notes

### Pourquoi cette story existe : re-introduction du bouton Déconnecter

Le story 10-3 (précédente itération de cette fonctionnalité, marquée `done` 2026-04-08) avait **supprimé** le bouton Déconnecter (révisé 2026-04-12, cf. header de `10-3-bouton-connect-disconnect-et-integration-tray-webview.md`). Dans la restructuration 2026-04-15 (Epic 5 nouveau), l'AC explicite de l'epic (`epics.md` lignes 955-959) **réintroduit** le bouton Déconnecter aligné sur la charte UX (`ux-design-specification.md` ligne 882 : état `Connected` → action visible "Bouton Déconnecter").

Cette story ne repart donc PAS de zéro : elle **complète** l'existant en ajoutant ce qui manque par rapport à la cible epic.

### État actuel du code (vérifié au 2026-04-17)

| Élément | Emplacement | État | Action requise |
|---|---|---|---|
| `ipc.ActionConnect` / `ipc.ActionDisconnect` | `internal/ipc/messages.go:15-16` | ✅ existe | Aucune |
| `handleConnect` / `handleDisconnect` IPC | `internal/ipchandler/handler.go:43,240` | ✅ existe + fonctionnel | Aucune |
| Séquence Disconnect ordonnée (tunnel→firewall→routing→tun) | `internal/service/service.go` via `TunnelClient.Disconnect()` | ✅ existe | Aucune |
| `POST /api/connect` handler HTTP local | `internal/ui/httpserver.go:57,147` | ✅ existe | Aucune |
| `POST /api/disconnect` handler HTTP local | `internal/ui/httpserver.go` | ❌ **absent** | **CRÉER** (Task 1) |
| Bouton HTML `btn-connect` | `frontend/index.html:54` | ✅ existe avec `onclick="toggleConnect()"` | Aucune (réutilisation) |
| `toggleConnect()` JS | `frontend/src/app.js:200` | ⚠️ appelle uniquement `/api/connect` | **MODIFIER** (Task 3) |
| `updateUI()` visibilité bouton | `frontend/src/app.js:115-124` | ⚠️ masque quand connecté (design obsolète) | **MODIFIER** (Task 4) |
| `.btn-connect` CSS | `frontend/src/style.css:155` | ✅ existe | Aucune |
| `.btn-disconnect` CSS | `frontend/src/style.css` | ❌ **absent** | **CRÉER** (Task 5) |
| Menu tray toggle connect/disconnect | `internal/ui/ui.go` | **HORS SCOPE** | Couvert par Story 5.5 (clic gauche tray) et 5.8 (Quitter) |

### Architecture — chemin de requête (inchangé)

```
[Clic bouton dans webview]
    ↓
[frontend/src/app.js → fetch('/api/disconnect', {method: 'POST'})]
    ↓ (HTTP loopback 127.0.0.1:{ui.http_port})
[internal/ui/httpserver.go → handleDisconnect → ipc.Client.Send(ActionDisconnect)]
    ↓ (named pipe Windows / Unix socket Linux)
[internal/ipc/server.go → dispatch]
    ↓
[internal/ipchandler/handler.go → handleDisconnect → service.TunnelClient.Disconnect()]
    ↓
[service exécute : tunnel → firewall → routing → tun (ordre strict)]
    ↓ (réponse IPC ipc.Response{Status: "disconnected"})
[httpserver → JSON → frontend]
    ↓ (polling 2s suivant)
[updateUI: dot rouge, bouton "Connecter" réapparaît]
```

Source : `architecture.md` §Flux IPC lignes 1044-1056 + `architecture.md` §IPC Patterns lignes 616-620 (ordre Disconnect strict).

### Pattern de synchronisation tray ↔ webview (pas de canal direct)

Chacun poll indépendamment toutes les 2 s (cf. `architecture.md` ligne 633, confirmé par learning 10-3). Pas de callback, pas d'événement push. Délai max de désynchronisation : 2 s. Le service étant idempotent, aucune course ne peut casser l'état. **Ne pas tenter d'ajouter un canal dédié tray↔ui** — complexité injustifiée pour un bénéfice nul.

### Learnings réutilisés de 10-2 / 10-3 (issus des code reviews passées)

1. **XSS** — `textContent` uniquement, jamais `innerHTML` (learning 10-2 #1) — applicable à la mise à jour du libellé bouton
2. **Erreurs IPC** — Le champ `error` de `ipc.Response` doit être propagé au JSON via `actionResponse(resp)` (existant) ET affiché au frontend (learning 10-3 #M4) — étendre à disconnect
3. **`const` vs `var`** — `const` pour valeurs non réassignées, `let` pour variables (learning 10-2 #5, renforcé 10-3 #M3)
4. **Zero-log** — Aucun `log.Println` / `fmt.Println` nouveau — erreurs uniquement via IPC/JSON (learning 10-2 #7)
5. **Zone cliquable 44×44px** — `min-height: 44px` déjà dans `.btn` générique (learning 10-3 #M2) — vérifier que `.btn-disconnect` hérite
6. **`pointer-events: none`** sur disabled (learning 10-3 #L2) — hérité via `.btn:disabled`
7. **aria-label dynamique** — "Se connecter à [pays]" / "Se déconnecter" (learning 10-3 #H3)
8. **Context timeout IPC** — 5 s pour tous les appels IPC (learning 10-2 #8) — déjà géré par `SafeIPCClient.SendContext` via `r.Context()`
9. **Préfixe erreurs Go** — `fmt.Errorf("ui: httpserver: ...: %w", err)` (existant, ne pas dévier)
10. **Tests table-driven** quand > 2 cas, `t.Cleanup()` teardown (architecture.md ligne 455-457)

### Styling — charte plateformeliberte.fr

- Bouton Connecter : `#4ade80` vert, texte `#0b1526` navy, Rajdhani 600
- Bouton Déconnecter : transparent, bordure `#d42b2b` 30 %, texte `#d42b2b`, Rajdhani 600
- Transition désactivée pendant connecting (pas de bouton, dot orange pulse 1.5 s)
- Source : `ux-design-specification.md` §C12 lignes 813-826 + §Button Hierarchy lignes 853-863

### Contraintes hors scope (à ne PAS toucher)

| Composant | Raison |
|---|---|
| `internal/tunnel/` | Déjà fonctionnel, l'ordre Disconnect est déclenché par le service |
| `internal/firewall/`, `internal/routing/`, `internal/tun/` | Géré par la séquence service existante |
| `internal/service/service.go` | Orchestration Disconnect déjà correcte (cf. architecture.md ligne 619) |
| `internal/ipc/` (protocole) | Actions et handlers existent, ne rien renommer |
| `internal/ipchandler/handler.go` | `handleConnect` et `handleDisconnect` fonctionnels — ne pas modifier |
| `internal/ui/ui.go` (tray) | Story 5.5 traitera les clics tray, Story 5.8 le "Quitter" |
| `cmd/ui/main.go` | Initialisation binaire UI — pas d'impact |

### Project Structure Notes

- Fichiers à modifier : `internal/ui/httpserver.go`, `internal/ui/httpserver_test.go`, `frontend/src/app.js`, `frontend/src/style.css`
- Fichiers inchangés mais référencés : `frontend/index.html` (bouton déjà en place), `internal/ipc/messages.go`, `internal/ipchandler/handler.go`
- Pas de nouveau fichier, pas de nouveau package, pas de nouvelle dépendance externe

### Bibliothèques et versions (inchangées)

| Bibliothèque | Version | Utilisation |
|---|---|---|
| Go `net/http` | stdlib (Go 1.26) | HTTPServer + handlers |
| Go `encoding/json` | stdlib | Sérialisation réponse |
| Go `testing` | stdlib | Tests unitaires handlers |
| `microsoft/go-winio` | v0.6.2 | IPC pipe Windows (indirect via `internal/ipc`) |
| `fyne.io/systray` | v1.12.0 | Tray (pas touché cette story) |
| `webview/webview` | latest | Webview (pas touché cette story) |

### References

- [Source : _bmad-output/planning-artifacts/epics.md — §Epic 5 Story 5.4, lignes 940-959]
- [Source : _bmad-output/planning-artifacts/architecture.md — §IPC Patterns lignes 292-323, §Format Patterns IPC/API lignes 461-497, §UI Patterns lignes 624-640, §Flux IPC lignes 1044-1056, §Ordre Connect/Disconnect ligne 202+613-620]
- [Source : _bmad-output/planning-artifacts/ux-design-specification.md — §C12 Connect Button lignes 813-826, §Button Hierarchy lignes 851-868, §Feedback Patterns lignes 880-889]
- [Source : _bmad-output/planning-artifacts/prd.md — FR12 "Connect/disconnect via fenêtre ou tray"]
- [Source : _bmad-output/implementation-artifacts/10-3-bouton-connect-disconnect-et-integration-tray-webview.md — Story antérieure, learnings et historique de révision bouton Disconnect]
- [Source : internal/ui/httpserver.go:147 handleConnect — modèle à dupliquer]
- [Source : internal/ipchandler/handler.go:240 handleDisconnect — handler IPC déjà fonctionnel]
- [Source : internal/ipc/messages.go:15-16 ActionConnect/ActionDisconnect — constantes existantes]
- [Source : frontend/src/app.js:200 toggleConnect() — fonction à étendre]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
