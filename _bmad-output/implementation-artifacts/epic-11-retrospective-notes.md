# Epic 11 — Notes pré-retrospective

> Notes consolidées issues du code-review post-Epic 11 (2026-05-03). Préparation
> du retrospective formel (`epic-11-retrospective: optional` dans
> `sprint-status.yaml`). À transformer en livrable retrospective formel via
> `bmad-bmm-retrospective` quand l'utilisateur le décide.

## Status final post-review

- 8 stories Epic 11 livrées et passées au code-review (139 tests JVM verts, lint propre, `assembleDebug` vert)
- 16 findings identifiés (5 HIGH, 9 MEDIUM, 5 LOW), 13 fixés automatiquement, 3 décisions stratégiques tranchées par l'utilisateur (H2 = Option 2 confirmée + ADR-16, H3 = traçabilité acceptée, H4 = livraison conjointe acceptée)

## Décisions importantes à graver

### H2 — ADR-16 « Assets web Android = sources Android-natives versionnées »
- Décision : pas de sync depuis `windows/frontend/`. Les assets `assets/web/*` sont versionnés directement comme sources Android-natives
- Justifié par : (a) frontend desktop trop spécifique Windows (titlebar, sidebar, /api/*, modals) ; (b) Story 10.2 bandeau C17 livré directement Android ; (c) ADR-08 isolation OS maximale
- ADR-16 ajouté dans [architecture.md](../planning-artifacts/architecture.md) après ADR-15
- AC #1 et #2 originaux Story 11.1 (script de sync + .gitignore actif) sont obsolètes et préservés en bloc `<details>` à des fins historiques
- **Conséquence Phase 2 :** si Linux/iOS rejoignent le projet, chaque OS aura ses propres assets — aucune mutualisation imposée

### H3 — Traçabilité 11.1 vs 11.3/11.4/11.6/11.7 acceptée telle quelle
- Constat : Story 11.1 a livré `index.html` et `app.js` enrichis pour les composants C13/C14/C17 + Story 11.3/11.4/11.6/11.7 ont continué d'éditer ces fichiers
- `git log` ne distingue pas qui a livré quoi — les Completion Notes des stories sont la seule source d'attribution
- **Décision Akerimus 2026-05-03 :** accepter, ne pas découper rétroactivement les commits (coût > bénéfice)
- **Leçon retenue :** pour les futures Phase 2 (Linux, iOS), créer une story-zero qui livre le squelette des assets nus AVANT de commencer les composants enrichis. Réduit le couplage 11.1 ↔ 11.3-11.7

### H4 — Livraison conjointe 11.5 + 11.6
- Spec disait « Story 11.5 livre placeholder écran 3, Story 11.6 enrichit en C15 »
- Réalité : `onboarding_screen_3.xml` livré directement en version C15 complète + `OnboardingActivity` inclut `wireScreen3Enriched` + `triggerKillSwitchVerification` + `showSkipConfirmationDialog` dans la même session
- **Décision Akerimus 2026-05-03 :** accepter, ne pas séparer rétroactivement
- **Conséquence sur les tests :** invariant `onboarding_screen3_title_placeholder doit être supprimé` était trivialement satisfait (la string n'a jamais existé). Test enrichi avec un nouvel invariant `c15 strings sont presentes` (anti-régression vers placeholder)
- **Leçon retenue :** le pattern « enrichissement progressif sur 2 stories » est viable techniquement mais risque la fusion en 1 session. Pour les futures stories type 11.x, soit fusionner explicitement à create-story, soit séparer les zones de fichiers (11.5 livre `*.xml` placeholder, 11.6 livre `*.xml` enrichi — par 2 commits distincts si le dev peut s'y tenir)

### H5 — `currentIp` reste null en attente extension API gomobile
- L'API `StatusCallback.onStateChange(state, message)` Story 9.7 ne fournit pas l'IP visible côté relais
- Fix nécessite extension de `internal/tunnel/gomobile_facade.go` (HORS scope `android/`) — soit ajouter un champ `visibleIp` au callback, soit un nouveau callback `onConnected(country, ip)`
- **Décision Akerimus 2026-05-03 :** documenter l'absence dans le code (commentaire `LeVoileVpnService.currentIp`) et créer un follow-up explicite pour story 11.7-bis ou Phase 2
- La notification affiche « 🇩🇪 Allemagne » sans IP — fonctionnement dégradé mais non-bloquant pour ship MVP

## Action items à lancer pour Phase 2 / Story 11.7-bis

- [x] **Story 11.7-bis CRÉÉE et PLANIFIÉE (2026-05-03)** — `ready-for-dev` :
  consolide les 3 dettes (extension API gomobile `visibleIp`, bascule
  `NoOpPacketRelay → GoBackedPacketRelay`, registry loader Android). Effort
  estimé 3.5-5 jours senior Kotlin/Go. Story file :
  [11-7bis-wiring-go-backend-relay-registry-currentip.md](11-7bis-wiring-go-backend-relay-registry-currentip.md).
  Sequencing : à prendre en début de Phase 2 Android, AVANT Story 12.1
  F-Droid metadata (le tunnel doit être réel pour tester l'APK release).
- [ ] **M1 + M2 : couverture comportementale Espresso** dans Story 12.6 :
  - `LeVoileBridge.connect/disconnect/selectCountry/openAppDetailsSettings` — happy paths complets
  - C13 AppBar + drawer interactions
  - C14 bottom-sheet drag-down + back intercept
  - C15 onboarding flow complet incluant skip confirmation dialog
  - Notification persistante state transitions

## Sequencing Phase 2 Android recommandé

```
Epic 11 (review) → smoke tests émulateur → Epic 11 done
   ↓
Story 11.7-bis (ready-for-dev, ~3.5-5j)  ← CHEMIN CRITIQUE
   ↓ (tunnel réel + IP affichée + bascule GoBackedPacketRelay)
Epic 12 :
  Story 12.1 (F-Droid metadata)
  Story 12.2 (CI Android pipeline) — invoque sync-frontend.sh + audits
  Story 12.3 (signature APK v2/v3 Ed25519)
  Story 12.4 (reproductibilité APK ci)
  Story 12.5 (WorkManager 24h check-version)
  Story 12.6 (tests instrumentés Espresso) — couvre M1 + M2 + bout-en-bout 11.7-bis
   ↓
Tag release Android v0.1.0 → publication F-Droid + APK direct
```

**Justification placement 11.7-bis avant 12.x :** sans tunnel réel, les tests
instrumentés Story 12.6 ne peuvent pas valider le bout-en-bout (handshake
QUIC + IP du relais visible). Sans handshake QUIC réel, l'audit reproductibilité
APK Story 12.4 ne peut pas valider le binaire `gojni.so` final.

## Métriques sprint Epic 11

- 8 stories, 139 tests JVM verts, ~30 fichiers Kotlin/XML/CSS/JS modifiés ou créés
- Durée d'implémentation : ~1 jour (sessions 2026-05-02 → 2026-05-03)
- Code-review post-Epic 11 : 16 findings, 13 fixed automatiquement, 3 décisions stratégiques
- Aucune régression Stories 1-10 desktop (sprint-status `epic-1` à `epic-10` restent `done`)

## À ne pas oublier pour Phase 2 Android

- Tester le passage de `NoOpPacketRelay` → `GoBackedPacketRelay` dans LeVoileVpnService — le wiring `onStateChanged` (M4) est prêt à recevoir
- Ajouter `web/.gitkeep` SI une story future réintroduit un sync (cf. ADR-16 commentaire .gitignore)
- Considérer F-Droid metadata (`metadata/com.velia.levoile.yml`) Story 12.1 — sera affecté par les choix UX Phase 2
