# Story 6.2: Comparaison IP STUN vs IP tunnel attendue

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

En tant qu'utilisateur final,
Je veux que le client compare l'IP retournee par STUN avec l'IP du relais attendue,
Afin qu'une divergence soit detectee comme une fuite potentielle (validation que la capture L3 fonctionne).

## Acceptance Criteria

1. **Given** un tunnel actif avec un relais identifie (`config.Relay.Domain` resolu en IP publique)
   **When** le `WebRTCLeakChecker.RunFullCheck()` est execute apres une emission STUN reussie (story 6.1)
   **Then** la reponse STUN est parsee, l'IP source (`XOR-MAPPED-ADDRESS`) est extraite
   **And** une `expectedIP` est calculee en resolvant `config.Relay.Domain` via DoH (story 4.2 / `internal/registry/doh_resolver.go`)
   **And** la comparaison `stunIP.Equal(expectedIP)` est faite via `net.IP.Equal` (gere v4/v6 correctement)

2. **Given** le resultat de la comparaison
   **When** `stunIP.Equal(expectedIP) == true`
   **Then** le `FullLeakReport.Status` est `StatusLeakOK` (constante IPC `"ok"`, remplace l'actuel `"pass"` cote contract)
   **And** le champ `report.STUNIP` est rempli avec l'IP STUN observee
   **And** le champ `report.ExpectedIP` (NOUVEAU) est rempli avec l'IP relais attendue

3. **Given** la comparaison
   **When** `stunIP.Equal(expectedIP) == false` (ex: IP ISP visible, indique une fuite ou une TUN down)
   **Then** le `FullLeakReport.Status` est `StatusLeakDetected` (NOUVEAU, valeur `"leak_detected"`, remplace `"fail"` cote contract)
   **And** le `LeakResult` correspondant a `Leaked = true`
   **And** une explication est exposee dans `report.LeakReason` (NOUVEAU): `"stun_ip_differs_from_relay"` ou `"tun_capture_likely_down"` selon heuristique

4. **Given** la garantie structurelle anti-fuite (Epic 2 — capture L3 + kill switch firewall)
   **When** un etat `LEAK_DETECTED` est observe en production
   **Then** la documentation interne (commentaires Go + `dev-notes` story) indique clairement: "ce check est une validation, pas une defense de premier niveau — un LEAK_DETECTED signale TUN down, mauvaise config ou bug, jamais une fuite VPN normale"
   **And** aucun retraitement defensif n'est tente ici (la reaction est dans la story 6.3)

5. **Given** la resolution de l'IP relais echoue (DoH down, domaine invalide)
   **When** `RunFullCheck` est appele
   **Then** une erreur explicite est retournee: `fmt.Errorf("leakcheck: resolve relay ip: %w", err)`
   **And** le scheduler (story 6.1) traite cette erreur comme transitoire et passe le cycle (pas d'alerte UI)
   **And** aucune comparaison n'est faite avec une `expectedIP` vide ou nil (jamais de faux LEAK_DETECTED)

6. **Given** la reponse IPC `get_status` ou `leak_check`
   **When** le service serialise le `FullLeakReport`
   **Then** les valeurs `"pass"`/`"fail"`/`"pending"` historiques sont mappees vers `"ok"`/`"leak_detected"`/`"pending"` au niveau IPC (constantes `StatusLeakOK`/`StatusLeakDetected`/`StatusLeakPending`)
   **And** les anciennes constantes `StatusLeakPass`/`StatusLeakFail` sont conservees comme alias deprecated (compat retro pendant la transition Epic 6, supprimees en story 6.3 ou code-review final)
   **And** le frontend webview recoit les nouveaux statuts via `/api/leak-status` sans modification cote serveur HTTP UI (pass-through)

## Tasks / Subtasks

- [x] Task 1: Etendre `FullLeakReport` avec champs comparaison explicite (AC: #2, #3)
  - [x] 1.1 Dans `internal/leakcheck/webrtc.go`, ajouter au struct `FullLeakReport`:
    - `ExpectedIP string` (json:"expected_ip,omitempty") — IP relais resolue
    - `LeakReason string` (json:"leak_reason,omitempty") — code court explicatif (`stun_ip_differs_from_relay` | `tun_capture_likely_down` | vide)
  - [x] 1.2 Renommer constantes internes:
    - `statusPass` → `statusOK = "ok"`
    - `statusFail` → `statusLeakDetected = "leak_detected"`
  - [x] 1.3 Mettre a jour `RunFullCheck`: assigner `report.Status = statusOK` ou `statusLeakDetected` selon comparaison
  - [x] 1.4 Tests `TestRunFullCheck_StatusOK` (IP STUN == expected) et `TestRunFullCheck_StatusLeakDetected` (IP STUN != expected)

- [x] Task 2: Refactor `RunFullCheck` pour utiliser une `ExpectedIPFunc` dediee (AC: #1, #5)
  - [x] 2.1 Dans `internal/leakcheck/webrtc.go`, ajouter type `ExpectedIPFunc func(ctx context.Context) (net.IP, error)`
  - [x] 2.2 Renommer le champ `getPublicIP PublicIPFunc` en `getExpectedIP ExpectedIPFunc` dans `WebRTCLeakChecker` (le PublicIPFunc reste exporte comme alias deprecated pour eviter de casser `cmd/portable` et tests externes au premier passage)
  - [x] 2.3 `NewWebRTCLeakChecker(getExpectedIP ExpectedIPFunc)` — meme signature visible (alias type), comportement clarifie via godoc: "Returns the IP that the relay SHOULD present to STUN servers (typically the relay's public IPv4 resolved from `config.Relay.Domain`)."
  - [x] 2.4 Si `getExpectedIP` retourne erreur → propager `fmt.Errorf("leakcheck: resolve relay ip: %w", err)` (AC #5)
  - [x] 2.5 Si `getExpectedIP` retourne `nil` ou IP non valide → retourner erreur `"leakcheck: empty expected ip"` (jamais comparer contre nil — eviterait un faux negatif)
  - [x] 2.6 Test `TestRunFullCheck_NilExpectedIP` (verifie erreur, pas de crash)

- [x] Task 3: Implementer le resolveur d'IP relais via DoH (AC: #1, #5)
  - [x] 3.1 Dans `internal/leakcheck/relay_ip.go` (NOUVEAU fichier), implementer:
    ```go
    type RelayIPResolver struct {
        domain   string
        resolver DoHResolver // interface : Resolve(ctx, host) ([]net.IP, error)
        cache    atomic.Pointer[cachedIP]
        ttl      time.Duration // 5 minutes par defaut
    }
    type cachedIP struct { ip net.IP; expiresAt time.Time }
    func NewRelayIPResolver(domain string, doh DoHResolver) *RelayIPResolver
    func (r *RelayIPResolver) ExpectedIP(ctx context.Context) (net.IP, error)
    ```
  - [x] 3.2 Implementer `ExpectedIP`:
    - Si cache valide (`now < cache.expiresAt`) → retourner cache.ip
    - Sinon: `ips, err := r.resolver.Resolve(ctx, r.domain)` ; prendre `ips[0]` (premier A record); cacher pendant 5 min
    - Erreur de resolution → propager (AC #5)
    - Resultat vide ou IPs[0] est nil → erreur `"resolver returned no ip for "+domain`
  - [x] 3.3 Definir interface `DoHResolver` dans `relay_ip.go` (la concrete est `internal/registry.DohResolver`, on injecte pour tests)
  - [x] 3.4 Tests `relay_ip_test.go`:
    - `TestRelayIPResolver_FreshLookup` (cache vide → DoH hit)
    - `TestRelayIPResolver_CacheHit` (deuxieme appel dans la TTL → pas de DoH)
    - `TestRelayIPResolver_CacheExpiry` (apres TTL → re-DoH)
    - `TestRelayIPResolver_ResolverError` (erreur propagee, cache pas pollue)
    - `TestRelayIPResolver_EmptyResult` (DoH retourne `[]net.IP{}` → erreur)

- [x] Task 4: Heuristique de classification du `LeakReason` (AC: #3)
  - [x] 4.1 Dans `RunFullCheck`, apres detection `stunIP != expectedIP`, classifier:
    - Si `stunIP` est dans une plage RFC1918 (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) ou loopback → `LeakReason = "tun_capture_likely_down"` (la STUN voit l'IP locale → la requete n'a meme pas atteint Internet via TUN, signe d'un retour direct OS)
    - Sinon (IP publique mais pas celle du relais — typiquement IP ISP) → `LeakReason = "stun_ip_differs_from_relay"`
  - [x] 4.2 Helper `func classifyLeak(stunIP net.IP) string` dans `webrtc.go`, exporte pour tests (`ClassifyLeak`)
  - [x] 4.3 Tests `TestClassifyLeak_PrivateIP_TunDown`, `TestClassifyLeak_PublicIP_DiffersFromRelay`, `TestClassifyLeak_Loopback`, `TestClassifyLeak_IPv6Private`

- [x] Task 5: Constantes IPC nouvelles + alias deprecated (AC: #6)
  - [x] 5.1 Dans `internal/ipc/messages.go`, ajouter:
    ```go
    StatusLeakOK            = "ok"
    StatusLeakDetected      = "leak_detected"
    // Deprecated: use StatusLeakOK. Kept until story 6.3 transition complete.
    StatusLeakPass = StatusLeakOK
    // Deprecated: use StatusLeakDetected. Kept until story 6.3 transition complete.
    StatusLeakFail = StatusLeakDetected
    ```
  - [x] 5.2 Ajouter au struct `Response`:
    - `LeakExpectedIP string `json:"leak_expected_ip,omitempty"`` — IP relais attendue
    - `LeakReason     string `json:"leak_reason,omitempty"`` — code de classification
  - [x] 5.3 Verifier compilation: les anciens consommateurs (tray polling, webview frontend, tests handler) qui comparent contre `StatusLeakPass`/`StatusLeakFail` continuent a compiler et matcher (alias = meme string)
  - [x] 5.4 Note dev: pas de migration JSON requise — le wire format change de `"pass"`/`"fail"` vers `"ok"`/`"leak_detected"` mais il n'y a pas de persistance, juste polling live. La story 6.3 finalisera la suppression des alias

- [x] Task 6: Brancher la nouvelle comparaison dans le service et le handler IPC (AC: #1, #6)
  - [x] 6.1 Dans `internal/service/service.go`, dans la zone "5b. Leak scheduler start" (~ligne 1380):
    - Avant `leakcheck.NewWebRTCLeakChecker(getPublicIP)`, instancier `relayResolver := leakcheck.NewRelayIPResolver(p.config.Relay.Domain, p.dohResolver)` (le `dohResolver` est deja construit a l'etape registry/discovery — verifier le champ existant `*Program`, sinon ajouter `dohResolver registry.DoHResolver`)
    - Remplacer `getPublicIP` par `relayResolver.ExpectedIP`
  - [x] 6.2 Dans `internal/ipchandler/handler.go::handleLeakCheck` (~ligne 410):
    - Supprimer toute la closure `getPublicIP` qui faisait `tc.SendSTUNRelay(...)` (ancien design — STUN relaye via /tunnel HTTP/3, deprecated par l'architecture restructuree)
    - Construire un `RelayIPResolver` a partir de `prg.Config().Relay.Domain` + le DoH resolver expose par `prg.DoHResolver()` (ajouter accesseur si manquant)
    - Passer cette `ExpectedIPFunc` a `NewWebRTCLeakChecker`
  - [x] 6.3 Dans `fillLeakStatus` (handler.go ~ligne 267):
    - Mapper `result.Status` (deja `"ok"`/`"leak_detected"`/`""`) directement
    - Si vide → `resp.LeakStatus = ipc.StatusLeakPending`
    - Remplir `resp.LeakExpectedIP = result.ExpectedIP` et `resp.LeakReason = result.LeakReason` (vides si OK)
  - [x] 6.4 Tests `handler_test.go`:
    - `TestHandleGetStatus_LeakOK` (verifie `LeakStatus="ok"`, `LeakExpectedIP` rempli, `LeakReason=""`)
    - `TestHandleGetStatus_LeakDetected` (verifie `LeakStatus="leak_detected"`, `LeakReason="stun_ip_differs_from_relay"`)
    - `TestHandleLeakCheck_Success_NewSemantics`
    - `TestHandleLeakCheck_ResolverError` (DoH down → `Status: "error"`, `Error: "leakcheck: resolve relay ip: ..."`)

- [x] Task 7: Pass-through HTTP UI + verification frontend (AC: #6)
  - [x] 7.1 Dans `internal/ui/httpserver.go`, struct `APILeakStatusResponse`: ajouter `ExpectedIP string `json:"expected_ip,omitempty"`` et `Reason string `json:"reason,omitempty"``
  - [x] 7.2 Le handler `handleLeakStatus` recopie depuis la reponse IPC sans transformation (`resp.LeakExpectedIP`, `resp.LeakReason`)
  - [x] 7.3 Test `TestHandleLeakStatus_FullPayload` (verifie le pass-through des nouveaux champs)
  - [x] 7.4 Verification manuelle: le frontend webview (`internal/ui/embed/`) consomme `/api/leak-status`. Inspecter le HTML/JS pour identifier ou `status === "pass"` est compare. Si trouve, ouvrir un grep et compter les sites a migrer (la migration UI peut etre groupee avec la story 6.3 qui ajoutera l'alerte visuelle — le ticket 6.2 se contente de produire le bon payload backend). Si aucune comparaison frontale → noter "pas d'impact webview".

- [x] Task 8: Tests d'integration end-to-end (AC: tous)
  - [x] 8.1 Test `service_test.go::TestLeakScheduler_Integration_OK`:
    - Mock `dohResolver.Resolve(domain)` → retourne `[1.2.3.4]`
    - Mock `RunFullCheck` indirect via STUN servers stub (deja present dans `webrtc_test.go`) qui repond avec IP `1.2.3.4`
    - Attendre 1 cycle scheduler → verifier `LastResult().Status == "ok"`
  - [x] 8.2 Test `service_test.go::TestLeakScheduler_Integration_LeakDetected`:
    - Mock DoH retourne `[1.2.3.4]`
    - STUN stub retourne `5.6.7.8` (IP ISP fictive)
    - Verifier `LastResult().Status == "leak_detected"`, `LeakReason == "stun_ip_differs_from_relay"`
    - Verifier que le callback `onLeak` est appele exactement une fois
  - [x] 8.3 Test `service_test.go::TestLeakScheduler_Integration_TunDownHeuristic`:
    - Mock DoH retourne `[1.2.3.4]`
    - STUN stub retourne `192.168.1.5` (IP RFC1918)
    - Verifier `LeakReason == "tun_capture_likely_down"`

## Dev Notes

### Contexte Critique — Epic 6 Refonte

Cette story est la **2eme du nouveau Epic 6 "Validation Anti-Fuite"** (post-restructure 2026-04-15). Differences cruciales par rapport a l'ancien design (avant restructure):

| Avant (ancien Epic 5/6) | Maintenant (nouveau Epic 6) |
|---|---|
| Le STUN etait relaye via le tunnel HTTP/3 (`/tunnel` ou `/stun-relay`) — la VPS forwardait au serveur STUN | Le STUN est emis par l'OS et **route naturellement par la TUN** (Wintun/Linux), thanks a la capture L3 (Epic 2 done). La VPS NAT-forwards comme tout autre paquet IP. |
| `getPublicIP` = closure qui appelle `tc.SendSTUNRelay()` (HTTP/3 stream) | `getExpectedIP` = closure qui resout `config.Relay.Domain` via DoH et retourne l'IP publique attendue |
| Status `"pass"`/`"fail"` (cote contract IPC) | Status `"ok"`/`"leak_detected"` (cote contract IPC) — alias retro-compat 6.3 |
| Role: defense de premier niveau anti-fuite WebRTC | Role: **validation** que la capture L3 + firewall fonctionnent (defense structurelle est dans Epic 2) |

**Architecture cible (extrait):**
> "Le check emet un STUN request via l'OS (qui passe par la TUN); la reponse doit venir avec l'IP du relais, pas l'IP ISP. Si different = TUN down ou fuite (alerte UI + log)."
> [Source: architecture.md ligne 334]

**Story 6.1 (prerequis logique):** emet la STUN Binding Request via la TUN. Cette story 6.2 ne touche PAS l'emission — elle se concentre sur la **comparaison du resultat** avec l'IP attendue.

**Decision: ne pas attendre 6.1.** Le code actuel (`leakcheck/webrtc.go`) fait deja l'emission STUN via `stunDirect` (ou `STUNRelayFunc` en fallback). En l'absence de TUN (avant Epic 2) ou avec un STUNRelayFunc legacy, le test fonctionne mais ne valide pas la TUN. Avec Epic 2 done, `stunDirect` route automatiquement par la TUN — donc 6.2 peut deja apporter de la valeur en mettant en place la bonne comparaison. La story 6.1 viendra plus tard supprimer le legacy `STUNRelayFunc` et garantir que `stunDirect` est le seul chemin.

### Architecture du Comparateur — Nouvelle Sequence

```
Scheduler.runCheck()
  → checker.RunFullCheck(ctx)
      → expectedIP, err := getExpectedIP(ctx)        // NOUVEAU: DoH resolve
            → relayResolver.ExpectedIP(ctx)
                → cache hit? OUI → retourner cache
                → cache miss → doh.Resolve(domain)
      → pour chaque STUN server (defaultSTUNServers):
            → stunResp := CheckSTUNLeak(ctx, server)
            → stunIP := ParseXORMappedAddress(stunResp)
            → if !stunIP.Equal(expectedIP):
                  result.Leaked = true
                  report.Status = "leak_detected"
                  report.LeakReason = classifyLeak(stunIP)
      → report.ExpectedIP = expectedIP.String()
      → report.STUNIP = premiereIPobservee
      → return report
```

### Pattern: Resolveur DoH avec Cache TTL

Le resolveur DoH existe deja (`internal/registry/doh_resolver.go` — story 4.2). Il faut:
1. **L'exposer** depuis `Program` via un getter `DoHResolver() registry.DoHResolver` (probablement deja present — verifier)
2. **L'injecter** dans le `RelayIPResolver` via constructeur
3. **Cache 5 minutes**: l'IP du relais change rarement (round-robin DNS Cloudflare → IP edge stable). 5 min est un compromis entre frais reseau et reactivite a un changement d'IP relais
4. **Pas de fallback DNS systeme**: si DoH echoue, on echoue le check (pas de DNS systeme — ce serait un trou de securite, le DNS systeme n'est pas authentifie et peut etre poisoned)

### Heuristique `classifyLeak` — Justification

Le but est de donner au DEV (et plus tard a l'UI story 6.3) un signal exploitable:
- **`tun_capture_likely_down`**: l'IP STUN est privee/loopback → le paquet STUN n'a meme pas atteint Internet. Probablement la TUN est down ou un firewall local intervient. Action humaine: verifier `ip link show levoile0` / `Get-NetAdapter levoile0`, redemarrer le service.
- **`stun_ip_differs_from_relay`**: l'IP STUN est publique mais pas celle du relais → le trafic STUN sort directement par l'ISP (ex: route ipv4_default plus prioritaire que la route TUN, fuite de captive portal en mode relaxe, bug routing). Action humaine: verifier `routing.Status()`, verifier que le firewall kill switch est actif.

Pas plus de finesse pour l'instant — l'UI story 6.3 affichera un message generique. Le code de classification reste deterministe et testable.

### Patterns Go a Respecter

- **Package**: tout dans `internal/leakcheck/` (pas de nouveau package)
- **Nouveau fichier**: `internal/leakcheck/relay_ip.go` + `relay_ip_test.go`
- **Erreurs**: `fmt.Errorf("leakcheck: <op>: %w", err)` — convention deja en place dans webrtc.go
- **Concurrence**: `atomic.Pointer[cachedIP]` pour le cache (sans mutex — read path hot)
- **Tests**: table-driven, mocks via interfaces (DoHResolver), `t.Context()` pour cancellation
- **Dependances**: 0 nouvelle. Tout est stdlib + le DoH resolver interne.

### Migration des Statuts — Strategie Sans Casse

L'approche **alias deprecated** (Task 5.1) permet:
1. La story 6.2 livre les nouvelles constantes ET garde les anciennes comme alias `=` au meme string
2. Tous les tests existants qui matchent `StatusLeakPass` continuent de fonctionner (aliasing au meme `"ok"` / `"leak_detected"`)
3. Le webview frontend qui (peut-etre) compare `status === "pass"` cassera silencieusement → tracer en Task 7.4
4. La story 6.3 fera le grand nettoyage: suppression des alias, mise a jour du frontend si necessaire

**Anti-pattern a eviter**: ne PAS faire de double mapping cote handler IPC ("si interne dit `pass`, j'envoie `ok` sur le wire"). C'est une source de bugs. Source de verite unique = constante exportee.

### Intelligence Code Existant — Reutilisations Cles

- `WebRTCLeakChecker.CheckSTUNLeak` — INCHANGE, deja parse XOR-MAPPED-ADDRESS correctement
- `BuildBindingRequest`, `ParseXORMappedAddress` — INCHANGES, deja audite story 6.1 historique
- `PeriodicScheduler` — INCHANGE, callbacks `onLeak`/`onRecovery` deja en place pour story 6.3
- `defaultSTUNServers` (Google ×2 + Cloudflare) — INCHANGE
- Tests existants `webrtc_test.go`, `webrtc_edge_test.go`, `xor_mapped_test.go` — la majorite continue a passer; seuls les tests qui asserent sur la valeur litterale `"pass"`/`"fail"` doivent etre updates (en pratique, les alias les sauvent au runtime mais il vaut mieux migrer les litteraux dans cette story pour garder le code propre)

### Securite — Points Critiques

1. **DoH obligatoire pour resolution relais**: jamais le DNS systeme. Sinon un attaquant DNS-poisoner injecte sa propre IP comme "expected" et fait passer LEAK_DETECTED pour OK. Le DoH (Cloudflare/Quad9) authentifie via TLS la reponse.
2. **`net.IP.Equal` pas `string ==`**: `1.2.3.4` et `::ffff:1.2.3.4` sont la meme IP mais des strings differentes. `Equal` gere v4-mapped v6.
3. **Pas de log de l'IP comparee en clair sur disque** (NFR20): les IPs sont en RAM uniquement, exposees via IPC pour l'UI mais pas ecrites dans Event Log/journald (cela viendra story 6.3 — log uniquement le code `LeakReason`, jamais les IPs).
4. **`getExpectedIP` retourne nil → erreur EXPLICITE**: jamais comparer contre nil/empty. Sinon `nil.Equal(stunIP) == false` → faux LEAK_DETECTED (ou faux OK selon le sens de Equal). Erreur explicite forces le scheduler a skipper proprement.
5. **Cache TTL borne (5 min)**: pas de cache infini. Si le relais change d'IP (failover Epic 4), le cache expirera dans la fenetre de la prochaine reconnexion automatique.

### Project Structure Notes

Nouveaux fichiers a creer:
```
internal/
+-- leakcheck/
    +-- relay_ip.go              # NOUVEAU - RelayIPResolver, cache TTL, ExpectedIP()
    +-- relay_ip_test.go         # NOUVEAU - Tests cache, DoH error, empty result
```

Fichiers existants modifies:
- `internal/leakcheck/webrtc.go` — Ajout `ExpectedIP`/`LeakReason` dans `FullLeakReport`, renommage statuts internes, type `ExpectedIPFunc`, fonction `classifyLeak`
- `internal/leakcheck/webrtc_test.go` — Ajout tests StatusOK/LeakDetected/NilExpectedIP
- `internal/leakcheck/webrtc_edge_test.go` — Ajout tests `classifyLeak` (RFC1918, loopback, IPv6, IP publique)
- `internal/ipc/messages.go` — Ajout `StatusLeakOK`/`StatusLeakDetected`, alias deprecated, champs `LeakExpectedIP`/`LeakReason` dans `Response`
- `internal/ipchandler/handler.go` — Refactor `handleLeakCheck` (DoH au lieu de SendSTUNRelay), enrichissement `fillLeakStatus`
- `internal/ipchandler/handler_test.go` — Ajout tests handlers etendus
- `internal/service/service.go` — Instanciation `RelayIPResolver` dans bloc "5b. Leak scheduler start", expose accesseur `DoHResolver()` si pas deja present
- `internal/service/service_test.go` — Ajout tests integration scheduler avec mock DoH
- `internal/ui/httpserver.go` — Ajout champs `ExpectedIP`/`Reason` dans `APILeakStatusResponse`, pass-through
- `internal/ui/httpserver_test.go` — Ajout test pass-through `TestHandleLeakStatus_FullPayload`

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Epic 6 — Story 6.2]
- [Source: _bmad-output/planning-artifacts/architecture.md (ligne 334) — Detection fuites WebRTC role redefini]
- [Source: _bmad-output/planning-artifacts/architecture.md (ligne 296) — Gestion STUN, suppression /stun-relay]
- [Source: _bmad-output/planning-artifacts/architecture.md (ligne 253) — Anti-fuite validation pas defense]
- [Source: _bmad-output/planning-artifacts/architecture.md (ligne 341) — DoH Bootstrap registry/doh_resolver.go]
- [Source: _bmad-output/planning-artifacts/prd.md#FR33 — Comparaison IP STUN vs IP tunnel attendue]
- [Source: internal/leakcheck/webrtc.go — WebRTCLeakChecker, FullLeakReport, RunFullCheck]
- [Source: internal/leakcheck/scheduler.go — PeriodicScheduler, callbacks onLeak/onRecovery]
- [Source: internal/leakcheck/xor_mapped.go — ParseXORMappedAddress]
- [Source: internal/ipc/messages.go (lignes 66-68) — StatusLeakPass/Fail/Pending actuels]
- [Source: internal/ipchandler/handler.go (lignes 267-280) — fillLeakStatus actuel]
- [Source: internal/ipchandler/handler.go (lignes 410-455) — handleLeakCheck actuel avec SendSTUNRelay deprecated]
- [Source: internal/service/service.go (lignes 1380-1400) — Bloc 5b. Leak scheduler start]
- [Source: internal/registry/doh_resolver.go — DoHResolver concret a injecter]
- [Source: internal/ui/httpserver.go (ligne 329) — APILeakStatusResponse]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context)

### Debug Log References

- `go build ./...` clean
- `go vet ./...` clean
- `go test ./...` : tous les packages verts
- `go test ./internal/leakcheck/...` : tests nouveaux + regression passent (30s)
- `go test ./internal/ipchandler/...` : tests handler etendus + alias check passent (13s)
- `go test ./internal/ui/...` : tests HTTP UI passent (0.5s)

### Completion Notes List

- **Task 1 & 2 & 4** : Refonte de `internal/leakcheck/webrtc.go`. `FullLeakReport` etendu avec `ExpectedIP`/`LeakReason` (+ `HTTPIP` retire car doublon semantique de `ExpectedIP`). Constantes internes renommees `statusOK`/`statusLeakDetected`. Type `ExpectedIPFunc` introduit, `PublicIPFunc` conserve comme alias deprecated. `RunFullCheck` valide que la fonction ne retourne ni erreur ni nil avant de comparer. Helper `ClassifyLeak(net.IP)` exporte : RFC1918/loopback/link-local/unspec → `tun_capture_likely_down`, sinon `stun_ip_differs_from_relay`.
- **Task 3** : Cree `internal/leakcheck/relay_ip.go` avec `RelayIPResolver` (DoH + cache TTL 5 min). Interface locale `DoHResolver` (Resolve → netip.Addr) injectable, evite la dependance dure sur `internal/registry`. Cache non pollue par erreurs (chaque erreur re-declenche DoH). `Invalidate()` expose pour usage post-failover (story 4.4/6.3). Tests : FreshLookup, CacheHit, CacheExpiry, ResolverError, EmptyResult, NilResolver, Invalidate.
- **Task 5** : `internal/ipc/messages.go` : nouvelles constantes `StatusLeakOK` (`"ok"`) et `StatusLeakDetected` (`"leak_detected"`). `StatusLeakPass`/`StatusLeakFail` conservees comme alias deprecated (`StatusLeakPass = StatusLeakOK` etc). Champs `LeakExpectedIP` et `LeakReason` ajoutes a `Response`.
- **Task 6** : `internal/service/service.go` bloc 5b : construction d'un DoH resolver dedie pour le leak check (independant du DoH registry qui est conditionnel), puis `RelayIPResolver` + injection de `resolver.ExpectedIP` dans le checker. Fallback gracieux (log + checker sans expectedIP) si la config DoH est invalide. `internal/ipchandler/handler.go::handleLeakCheck` remplace l'ancienne closure `SendSTUNRelay` par une delegation au scheduler (`TriggerCheck` + `LastResult`) — une seule source de verite pour la configuration du checker. `fillLeakStatus` est un pass-through strict (pas de double mapping).
- **Task 7** : `internal/ui/httpserver.go` : `APILeakStatusResponse` etendu avec `ExpectedIP`/`Reason` en pass-through depuis l'IPC. Verification frontend : aucun usage de `leak_status`/`leakStatus`/STUN dans `frontend/src/app.js` ni `frontend/index.html` — aucune migration webview requise pour cette story (ca viendra avec 6.3 qui ajoutera l'alerte visuelle).
- **Task 8** : Tests d'integration ajoutes dans `internal/leakcheck/integration_story6_2_test.go` : Checker+RelayIPResolver+mock STUN bout-en-bout pour les trois scenarios (OK, LeakDetected ISP, TUNDown heuristic) + DoH failure propagation (pas de faux OK). Tests handler dans `handler_test.go` : `TestHandle_GetStatus_LeakOK`, `TestHandle_GetStatus_LeakDetected`, `TestHandle_LeakCheck_NoScheduler`, `TestStatusLeakAliases`. Tests HTTP UI : `TestLeakStatus_OK`, `TestLeakStatus_LeakDetected` (ex-`TestLeakStatus_Pass`/`Fail` migres).
- **Helper test** : ajoute `PeriodicScheduler.ForTestSetLastResult(report, when)` pour permettre aux tests handler de seeder un resultat sans declencher un vrai check STUN.
- **Note deprecated alias** : `StatusLeakPass`/`StatusLeakFail` restent litteralement `= StatusLeakOK`/`StatusLeakDetected`. Les anciens consommateurs (tests) qui comparent contre ces constantes continuent a compiler. Les comparaisons de valeurs `== "pass"`/`== "fail"` cote JSON changent ("pass" devient "ok" sur le wire) — les tests HTTP UI ont ete migres explicitement.

### Change Log

- 2026-04-18 : Implementation story 6.2 complete — `FullLeakReport` etendu (ExpectedIP/LeakReason), statuts renommes ok/leak_detected, `RelayIPResolver` via DoH avec cache TTL, heuristique `ClassifyLeak` (TUN-down vs ISP), branchement service+handler, pass-through HTTP UI, tests unitaires et d'integration. Tous les tests Go passent (`go vet` et `go test ./...` clean).
- 2026-04-18 : Code review fixes — H1: test `TestHandle_LeakCheck_SchedulerNil` avec tunnel en StateConnected pour exercer reellement la branche `scheduler == nil`. M1: `PeriodicScheduler.runCheck` serialize via `sync.Mutex.TryLock` (evite le double dial STUN quand un click utilisateur tombe sur le tick periodique). M2: erreur `RunFullCheck` incremente maintenant `consecutiveSkips` (une panne DoH persistente finira par declencher l'alarme stuck-check au lieu d'un "pending" perpetuel). M3: `Program.leakRelayResolver` persiste le resolveur ; le callback de failover (`tunnel.WithFailoverFn` dans le bloc reconnector) appelle `Invalidate()` pour eviter une fenetre de 5 min de faux LEAK_DETECTED apres basculement inter-pays. Tests ajoutes : `TestPeriodicScheduler_IncrementsSkipsOnCheckerError`, `TestPeriodicScheduler_SerializesConcurrentRunCheck`, `TestHandle_LeakCheck_SchedulerNil`, `TestHandle_LeakCheck_NoTunnel` (split de l'ex test trompeur).

### File List

**Nouveaux fichiers :**
- `internal/leakcheck/relay_ip.go`
- `internal/leakcheck/relay_ip_test.go`
- `internal/leakcheck/integration_story6_2_test.go`

**Fichiers modifies :**
- `internal/leakcheck/webrtc.go` (reecriture semantique : status ok/leak_detected, ExpectedIPFunc, ClassifyLeak, FullLeakReport etendu, HTTPIP retire)
- `internal/leakcheck/webrtc_test.go` (migration des valeurs litterales vers constantes)
- `internal/leakcheck/webrtc_edge_test.go` (PublicIPError renomme ExpectedIPError + NilExpectedIP test)
- `internal/leakcheck/scheduler.go` (statusFail → statusLeakDetected ; ajout helper `ForTestSetLastResult` ; code review : `checkMu sync.Mutex` + TryLock dans runCheck pour M1 ; incrementation `consecutiveSkips` sur erreur RunFullCheck pour M2)
- `internal/leakcheck/scheduler_test.go` (rename statusPass/Fail ; code review : ajout `erroringChecker`, `slowChecker`, `TestPeriodicScheduler_IncrementsSkipsOnCheckerError`, `TestPeriodicScheduler_SerializesConcurrentRunCheck`)
- `internal/ipc/messages.go` (StatusLeakOK/Detected + alias deprecated + champs LeakExpectedIP/LeakReason)
- `internal/ipchandler/handler.go` (fillLeakStatus enrichi, handleLeakCheck delegue au scheduler)
- `internal/ipchandler/handler_test.go` (tests handler etendus + alias check ; code review : split en `TestHandle_LeakCheck_NoTunnel` + nouveau `TestHandle_LeakCheck_SchedulerNil` avec tunnel en StateConnected)
- `internal/service/service.go` (bloc 5b Leak scheduler : DoH + RelayIPResolver + ExpectedIPFunc ; code review M3 : champ `leakRelayResolver` persiste le resolveur + appel `Invalidate()` dans le callback `WithFailoverFn`)
- `internal/ui/httpserver.go` (APILeakStatusResponse etendu)
- `internal/ui/httpserver_test.go` (TestLeakStatus_OK + TestLeakStatus_LeakDetected)

**Note pre-existant hors scope 6.2 :** l'arbre de travail contient des changements non-commites issus de story 6.1 (suppression `internal/stun/` + `internal/relay/stun_handler.go`, refactors `cmd/client/main.go`, `cmd/relay/main.go`, `internal/config/config.go`, `internal/tunnel/client.go`, `README.md`, `config.example.toml` ; ajout `internal/leakcheck/e2e_story6_1_test.go`). La story 6.2 touche UNIQUEMENT les fichiers listes ci-dessus ; les autres entrees de `git status` proviennent de story 6.1 qui n'a pas ete commitee separement avant le demarrage de 6.2.

## Senior Developer Review (AI)

**Reviewer :** Claude Opus 4.7 (1M context) — adversarial code review
**Review date :** 2026-04-18
**Outcome :** Changes Requested → All HIGH + MEDIUM fixed → **Approved**

### Findings

#### 🔴 HIGH (1)

- **H1 — Test mensonger `TestHandle_LeakCheck_NoScheduler`** : sortait sur `tc == nil` (service_not_ready) sans jamais atteindre la branche `scheduler == nil`. Un bug futur sur `LeakScheduler()` passerait inapercu.
  - **Resolution :** split en deux tests. `TestHandle_LeakCheck_NoTunnel` garde la couverture originale ; `TestHandle_LeakCheck_SchedulerNil` injecte un `tunnel.Client` en StateConnected via `prg.ForTestSetTunnelClient` pour exercer reellement la branche. Verifie `Error == "leak_scheduler_not_running"`.

#### 🟡 MEDIUM (4)

- **M1 — Race handler vs periodique** : `PeriodicScheduler.TriggerCheck` n'avait pas de verrou "check-en-cours" ; un click UI pile au tick de 10 min dialait les memes serveurs STUN deux fois.
  - **Resolution :** ajout `checkMu sync.Mutex` ; `runCheck` fait `TryLock` en entree, drop si verrou deja pris. Test `TestPeriodicScheduler_SerializesConcurrentRunCheck` valide qu'une seconde invocation parallele dropppe (via `slowChecker` qui bloque jusqu'a release).
- **M2 — Echec DoH silencieux infini** : `runCheck` swallow l'erreur sans toucher `consecutiveSkips` → DoH persistent down = "pending" a vie, aucune alarme operationnelle.
  - **Resolution :** erreur `RunFullCheck` incremente desormais `consecutiveSkips`. Test `TestPeriodicScheduler_IncrementsSkipsOnCheckerError` valide que 3 echecs consecutifs laissent `ConsecutiveSkips == 3` et `LastResult == nil`.
- **M3 — `Invalidate()` dead code** : methode documentee "utile post-failover" mais aucun callsite. Apres failover inter-pays, cache 5 min de fausses `LEAK_DETECTED` contre l'ancien relais.
  - **Resolution :** champ `Program.leakRelayResolver` persiste le resolveur ; `tunnel.WithFailoverFn` appelle `p.leakRelayResolver.Invalidate()` dans le callback de succes, force une resolution DoH contre le nouveau domaine relais. Couverture : `TestRelayIPResolver_Invalidate` (deja present) + inspection de `service.go:1340-1348`.
- **M4 — File List decale** : `internal/leakcheck/e2e_story6_1_test.go` liste comme "modifie" mais `git status` le montre `??` (pre-existant 6.1, jamais commite).
  - **Resolution :** section "Note pre-existant hors scope 6.2" ajoutee au File List, listant les changements 6.1 non-commites qui apparaissent dans `git status` mais ne relevent pas de cette story.

#### 🟢 LOW (4) — deferred

- L1 : `ClassifyLeak(nil)` renvoie `stun_ip_differs_from_relay` ; argument pour `tun_capture_likely_down` par defaut. **Non-bloquant**, rarement observable (getExpectedIP retourne d'abord une erreur pour nil IP).
- L2 : `ClassifyLeak` ignore multicast/broadcast. **Non-bloquant**, aucune IP STUN reelle ne tombe dans ces plages.
- L3 : `ForTestSetLastResult` expose sur type prod. **Non-bloquant**, convention Go `ForTest*` idiomatique (voir net/http).
- L4 : aucun flag config pour desactiver la comparaison. **Non-bloquant**, mode validation-only reste accessible en passant `nil` a `NewWebRTCLeakChecker`.

### Action Items

Tous les HIGH + MEDIUM sont resolus dans cette iteration. Les LOW sont documentes pour decision future mais non-bloquants pour le passage en `done`.
