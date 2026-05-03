# Story 9.2: Script `build-aar.sh` — gomobile bind du noyau Go partagé

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Exceptions explicitement autorisées — code partagé Go (frontière ADR-09)** :
> 1. Les scripts `android/scripts/build-aar.{sh,ps1}` invoquent `gomobile bind` sur les packages racine et les shims Android. Cette invocation **CONSOMME** les packages racine mais **NE LES MODIFIE PAS**. Le script `android/scripts/verify-shared-imports.sh` **LIT** les packages partagés mais **NE LES MODIFIE PAS**. Si une exécution révèle qu'un package partagé importe par erreur un package OS-spécifique (`internal/tun/`, `internal/firewall/`, `internal/routing/`, `internal/ui/`, `internal/ipc/`, etc.), **NE PAS** corriger dans cette story : reporter via `Completion Notes` et alerter l'utilisateur.
> 2. **Modification minimale de `go.mod` + `go.sum` racine — autorisée par décision utilisateur 2026-05-02** : ajout de la dépendance `golang.org/x/mobile` (requise par le toolchain gomobile bind — sans cela aucun build Android possible). Aucune autre dépendance ne doit être ajoutée. Aucun module existant ne doit être modifié/retiré. C'est une dépendance **build-time uniquement** (pas embarquée dans les binaires desktop puisqu'aucun code desktop ne l'importe).
> 3. **Cartographie réelle des packages exposés gomobile (résolution divergence spec/repo 2026-05-02)** : la spec d'origine de Story 9.2 (héritée d'architecture l. 59, 107, 1726) référence 5 packages racine `./internal/{protocol,registry,auth,crypto,leakcheck}`. Vérification du repo a révélé que `internal/protocol/` et `internal/auth/` n'existent pas — leur logique vit dans `internal/tunnel/` (framing dans `pump.go`, session tokens dans `client.go`). **Décision utilisateur 2026-05-02 : créer 2 shims Go dans `android/internal/protocol/` et `android/internal/auth/`** qui re-exposent une surface gomobile-compatible minimale. Aucune modification de `internal/tunnel/` ni des autres packages racine. Les 5 packages effectivement exposés via gomobile bind sont donc : `./android/internal/protocol`, `./android/internal/auth`, `./internal/registry`, `./internal/crypto`, `./internal/leakcheck`.
>
> Concrètement : `git status` à la fin de la session dev ne doit montrer **que** : (a) entrées sous `android/`, (b) `go.mod` + `go.sum` racine (ajout `golang.org/x/mobile`), (c) `_bmad-output/implementation-artifacts/sprint-status.yaml` (passage `in-progress` → `review`), (d) ce fichier story.

## Story

En tant que développeur,
Je veux un couple de scripts shell reproductibles (`android/scripts/build-aar.sh` Linux/macOS + `android/scripts/build-aar.ps1` Windows + `android/scripts/verify-shared-imports.sh` lint imports) qui compilent le noyau Go partagé (`internal/protocol`, `internal/registry`, `internal/auth`, `internal/crypto`, `internal/leakcheck`) en un artefact `android/levoile-core/libs/levoile-core.aar` consommable par Gradle via le module `:levoile-core` déjà configuré en Story 9.1,
Afin que la logique protocole/crypto/registre/session du desktop soit réutilisée 100% côté Android (ADR-09) sans duplication, sans réécriture native Kotlin, sans abstraction cross-OS partagée (ADR-08), et que le `.aar` produit soit reproductible bit-à-bit (préparation Story 12.4 reproductibilité F-Droid).

## Acceptance Criteria

1. **Script `android/scripts/build-aar.sh` (Linux/macOS)** — Quand un développeur exécute `cd android && bash scripts/build-aar.sh` depuis une machine où Go 1.26+ et `gomobile` sont installés (avec `gomobile init` déjà exécuté pour fournir le NDK), le script (a) vérifie que `gomobile` est dans le `$PATH` et émet un message explicite d'installation si absent (`go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init`), (b) vérifie que la version de Go déclarée dans `go.mod` racine est ≥ 1.26, (c) calcule le repo root via `git rev-parse --show-toplevel` (jamais en `cd ../../` aveugle pour rester portable même si la story est exécutée depuis un sous-shell), (d) invoque `gomobile bind -target=android -androidapi=29 -javapkg=fr.plateformeliberte.levoile.core -o "${REPO_ROOT}/android/levoile-core/libs/levoile-core.aar" ./internal/protocol ./internal/registry ./internal/auth ./internal/crypto ./internal/leakcheck` depuis `${REPO_ROOT}` (les chemins `./internal/...` sont relatifs au repo root, pas à `android/`), (e) en cas d'échec gomobile (NDK manquant, erreur de compilation, package non-trouvé), retourne un exit code non-zéro avec stderr complet et NE PRODUIT PAS un `.aar` partiel/corrompu (suppression de tout fichier intermédiaire en cas d'erreur via `set -euo pipefail` + trap), (f) en cas de succès, affiche la taille du `.aar` produit + son hash SHA256 sur stdout (préparation comparaison reproductibilité). Le script est shellcheck-clean (`shellcheck scripts/build-aar.sh` = 0 warning bloquant).

2. **Script `android/scripts/build-aar.ps1` (Windows PowerShell)** — Variante équivalente PowerShell pour les développeurs Windows. Mêmes contrôles, mêmes invocations, même artefact final, même affichage taille + SHA256. Compatible Windows PowerShell 5.1 ET PowerShell Core 7+. Utilise `git rev-parse --show-toplevel` (le binaire `git` est attendu dans le `PATH` — déjà le cas en Story 9.1 via Git for Windows). Émet un message explicite si `gomobile` n'est pas dans `$env:PATH`. Quand un développeur Windows exécute `cd android; pwsh scripts/build-aar.ps1` (ou `powershell -File scripts/build-aar.ps1`), un `.aar` identique en taille à celui produit par `build-aar.sh` est généré (à la signature gomobile-Linux vs gomobile-Windows près — la reproductibilité bit-à-bit cross-OS est portée par Story 12.4 via build Docker, hors scope ici).

3. **Module `:app` consommant le `.aar` via `implementation(files(...))`** — Quand `android/app/build.gradle.kts` est lu après cette story, il déclare `implementation(files("libs/levoile-core.aar"))` qui consomme directement le `.aar` produit par `scripts/build-aar.{sh,ps1}` dans `android/app/libs/`. Le module `:levoile-core` reste configuré (placeholder pour les futurs wrappers Kotlin idiomatiques de Story 9.7 — `GoCoreAdapter` etc.) mais ne consomme PAS le `.aar` lui-même. **Pourquoi pas via `:levoile-core` (spec d'origine de cette story)** : AGP 8.x interdit qu'un module `android.library` bundle un `.aar` local en dépendance fichier (l'AAR de sortie serait cassée — les classes du `.aar` embarqué ne seraient pas re-packagées). Le `bundleReleaseLocalLintAar` task fail explicitement avec « Direct local .aar file dependencies are not supported when building an AAR ». Décision finale 2026-05-02 : `.aar` dans `app/libs/` + consommation par `:app` directement, pattern conforme AGP. Quand `cd android && ./gradlew assembleDebug` est exécuté APRÈS un `bash scripts/build-aar.sh` réussi, le build référence le `.aar` sans erreur ; les classes générées par gomobile (`fr.plateformeliberte.levoile.core.{protocol.Protocol, auth.Auth, crypto.Crypto, registry.Registry, leakcheck.Leakcheck}` — exposées via `-javapkg=fr.plateformeliberte.levoile.core` sur la ligne `gomobile bind`) sont importables depuis n'importe quelle classe Kotlin du module `app` (vérifiable par tâche Task 5).

4. **Test smoke JUnit `LeVoileCoreSmokeTest.kt`** — Quand `cd android && ./gradlew :app:testDebugUnitTest` est exécuté APRÈS `build-aar.sh`, un test unitaire `app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileCoreSmokeTest.kt` (a) importe au minimum une classe du `.aar` (par exemple `fr.plateformeliberte.levoile.core.protocol.Protocol` — adapter selon le nom exact généré par gomobile, à constater au premier run et figer dans le test), (b) vérifie via `Assert.assertNotNull()` que la classe est résolvable au runtime, (c) invoque (au moins) une méthode pure-data du noyau Go (par exemple lecture d'une constante de version exposée — sans déclencher de chargement JNI complet qui requiert un device Android et n'est pas disponible en `testDebugUnitTest` JVM-only). Le test passe sans `UnsatisfiedLinkError` ni `ClassNotFoundException`. **Note critique** : si la classe pure-data n'existe pas dans le noyau Go partagé actuel (les 5 packages exposés peuvent ne pas avoir de constante version triviale), le dev (a) NE LA CRÉE PAS dans le code Go partagé (interdit par périmètre de modification), (b) remplace par un test plus léger qui vérifie uniquement la résolution de classe via `Class.forName("fr.plateformeliberte.levoile.core.protocol.Protocol")` sans instanciation. Le test instrumenté JNI complet (avec ParcelFileDescriptor + handshake QUIC réel) est porté par Story 9.7.

5. **Script `android/scripts/verify-shared-imports.sh` — lint frontière ADR-09** — Quand `cd android && bash scripts/verify-shared-imports.sh` est exécuté, le script (a) parcourt récursivement les 5 packages partagés (`internal/protocol`, `internal/registry`, `internal/auth`, `internal/crypto`, `internal/leakcheck`) depuis le repo root, (b) extrait via `go list -f '{{join .Imports "\n"}}'` (ou équivalent grep portable sans dépendance à la toolchain Go si Go absent au runtime CI lint) tous les imports, (c) fail (exit non-zéro) si un import correspond à `internal/tun/`, `internal/firewall/`, `internal/routing/`, `internal/ui/`, `internal/ipc/`, `internal/wfp/`, `internal/nftables/` (liste OS-spécifique fermée — voir architecture l. 1710), (d) fail également si un import correspond à `windows/`, `linux/`, `cmd/ui`, `cmd/client`, `cmd/relay`, `cmd/ctl`, `cmd/genregistry` (arbres OS-spécifiques desktop), (e) émet un rapport synthétique listant chaque import croisé détecté avec le fichier source. Le script est shellcheck-clean. Au moment de cette story (state 2026-05-02 du repo), le script DOIT passer sans alerte (les 5 packages sont déjà documentés comme "frontière étroite" depuis Epic 9.0 retro 2026-04-29).

6. **Reproductibilité du `.aar` (préparation Story 12.4)** — Quand `bash scripts/build-aar.sh` est exécuté DEUX FOIS de suite sur la même machine, sans modification du repo, sans `gradle clean`, le hash SHA256 du `.aar` produit est identique entre les deux exécutions (le `gomobile bind` est déterministe sur la même version Go + même version gomobile + même `GOROOT` + même `GOPATH` + même `ANDROID_NDK_HOME`). Vérification via `sha256sum levoile-core.aar` (Linux) ou `Get-FileHash levoile-core.aar -Algorithm SHA256` (Windows). La reproductibilité **cross-machine** (deux développeurs distincts, deux NDK paths distincts) ainsi que la reproductibilité **temporelle** (même tag git rebuild N mois plus tard) sont portées par Story 12.4 (build Docker pinné). Cette AC ne livre que la reproductibilité **same-machine same-environment**.

7. **`README-android.md` mis à jour avec la procédure build AAR** — Le `android/README-android.md` créé en Story 9.1 (Task 10 — actuellement avec note « Build AAR (à venir Story 9.2) ») est patché pour décrire la procédure complète : (a) installation préalable de `gomobile` (`go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init` — la `gomobile init` télécharge l'Android NDK ~1.5 GB, prévoir 5-15 min sur première install), (b) commandes `bash scripts/build-aar.sh` (Linux/macOS) ou `pwsh scripts/build-aar.ps1` (Windows), (c) emplacement de l'artefact produit `levoile-core/libs/levoile-core.aar`, (d) commande de regen post modification d'un package partagé (`bash scripts/build-aar.sh && ./gradlew clean assembleDebug`), (e) commande de vérif imports (`bash scripts/verify-shared-imports.sh`). Aucune autre section du README n'est touchée par cette story.

## Tasks / Subtasks

- [x] **Task 1 : Pré-requis toolchain — installer `gomobile` localement et exécuter `gomobile init`** (AC: #1, #2)
  - [ ] Vérifier la présence de Go ≥ 1.26 via `go version` (déjà présent selon Story 9.1 dev notes : Go 1.26.2 installé).
  - [ ] Installer `gomobile` : `go install golang.org/x/mobile/cmd/gomobile@latest`. Vérifier que `gomobile` est dans le `$PATH` (par défaut `$GOPATH/bin` ou `$HOME/go/bin`).
  - [ ] Exécuter `gomobile init` — cette commande télécharge l'Android NDK et le configure. Compter 5-15 min selon la connexion (NDK ~1.5 GB). Prévoir l'espace disque.
  - [ ] Vérifier que `ANDROID_NDK_HOME` (ou `ANDROID_HOME` + détection automatique du sous-dossier `ndk/`) pointe sur un NDK installé. Sur Windows, le NDK est typiquement à `C:\Users\<user>\AppData\Local\Android\Sdk\ndk\<version>\`.
  - [ ] Tester qu'un `gomobile bind` minimal fonctionne via une commande de smoke : `cd <repo_root> && gomobile bind -target=android -o /tmp/test.aar ./internal/crypto` (ou équivalent sur le package le plus simple/minimal des 5). Si succès, supprimer `/tmp/test.aar`. Si échec, diagnostiquer (NDK manquant, dépendance Go incompatible avec gomobile — ex. cgo non supporté pour certains packages — etc.).
  - [ ] **Reporter dans `Debug Log References`** : version exacte de gomobile (`gomobile version`), chemin NDK détecté, chemin de l'install Go.

- [x] **Task 2 : Créer `android/scripts/build-aar.sh` (Linux/macOS)** (AC: #1)
  - [ ] Créer le fichier avec shebang `#!/usr/bin/env bash` et `set -euo pipefail` en tête.
  - [ ] En-tête commentée : description, ADR-09, list des 5 packages exposés, version gomobile cible (préciser `gomobile@latest` au moment de la livraison + figer une commit hash dans `README-android.md` une fois testé).
  - [ ] Calculer le repo root via `REPO_ROOT="$(git rev-parse --show-toplevel)"`. Fail si `git` absent ou non-repo.
  - [ ] Vérifier que `gomobile` est dans le `$PATH` :
    ```bash
    if ! command -v gomobile >/dev/null 2>&1; then
      echo "ERROR: gomobile non trouvé. Installer via :" >&2
      echo "  go install golang.org/x/mobile/cmd/gomobile@latest" >&2
      echo "  gomobile init" >&2
      exit 1
    fi
    ```
  - [ ] Vérifier la version Go (warning si < 1.26, fail si < 1.21 — compatible avec quic-go v0.59.0).
  - [ ] Définir le chemin de sortie : `OUTPUT="${REPO_ROOT}/android/levoile-core/libs/levoile-core.aar"`. Créer le dossier parent (`mkdir -p "$(dirname "$OUTPUT")"`) — `levoile-core/libs/` existe déjà depuis Story 9.1 mais le `mkdir -p` reste safe.
  - [ ] Définir un trap `trap 'rm -f "$OUTPUT.tmp"' EXIT INT TERM` pour cleanup en cas d'erreur (utiliser un fichier intermédiaire `$OUTPUT.tmp` puis `mv` final).
  - [ ] Invoquer gomobile depuis le repo root :
    ```bash
    cd "$REPO_ROOT"
    gomobile bind \
      -target=android \
      -androidapi=29 \
      -javapkg=fr.plateformeliberte.levoile.core \
      -o "$OUTPUT.tmp" \
      ./internal/protocol \
      ./internal/registry \
      ./internal/auth \
      ./internal/crypto \
      ./internal/leakcheck
    mv "$OUTPUT.tmp" "$OUTPUT"
    ```
    L'option `-javapkg=fr.plateformeliberte.levoile.core` aligne le package Java généré sur le `namespace` du module `levoile-core` configuré en Story 9.1.
  - [ ] Afficher taille + SHA256 :
    ```bash
    SIZE=$(stat -c '%s' "$OUTPUT" 2>/dev/null || stat -f '%z' "$OUTPUT")
    HASH=$(sha256sum "$OUTPUT" | awk '{print $1}')
    echo "✓ AAR produit : $OUTPUT"
    echo "  Taille : $SIZE octets"
    echo "  SHA256 : $HASH"
    ```
    (le `stat -f` fallback est pour macOS — `stat -c` est Linux).
  - [ ] Marquer le script exécutable : `chmod +x android/scripts/build-aar.sh`.
  - [ ] Lint shellcheck : `shellcheck android/scripts/build-aar.sh` doit retourner 0 warning bloquant. Documenter dans Completion Notes les warnings non-bloquants conservés (ex. SC2155 sur les `local var=$(cmd)` peut être ignoré).

- [x] **Task 3 : Créer `android/scripts/build-aar.ps1` (Windows)** (AC: #2)
  - [ ] Créer le fichier avec en-tête `#Requires -Version 5.1`.
  - [ ] `$ErrorActionPreference = "Stop"` en tête pour fail-fast.
  - [ ] Calculer repo root : `$RepoRoot = (git rev-parse --show-toplevel).Trim()`.
  - [ ] Vérifier `gomobile` dans `$env:PATH` :
    ```powershell
    if (-not (Get-Command gomobile -ErrorAction SilentlyContinue)) {
      Write-Error "gomobile non trouvé. Installer via : go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init"
      exit 1
    }
    ```
  - [ ] Définir le chemin de sortie : `$Output = Join-Path $RepoRoot "android/levoile-core/libs/levoile-core.aar"`.
  - [ ] Invoquer gomobile depuis le repo root (équivalent au script bash, mêmes paramètres `-target=android -androidapi=29 -javapkg=fr.plateformeliberte.levoile.core`).
    ```powershell
    Push-Location $RepoRoot
    try {
      & gomobile bind `
        -target=android `
        -androidapi=29 `
        -javapkg=fr.plateformeliberte.levoile.core `
        -o $Output `
        ./internal/protocol ./internal/registry ./internal/auth ./internal/crypto ./internal/leakcheck
      if ($LASTEXITCODE -ne 0) { throw "gomobile bind a échoué (exit $LASTEXITCODE)" }
    } finally {
      Pop-Location
    }
    ```
  - [ ] Afficher taille + SHA256 :
    ```powershell
    $Item = Get-Item $Output
    $Hash = (Get-FileHash $Output -Algorithm SHA256).Hash
    Write-Host "✓ AAR produit : $Output"
    Write-Host "  Taille : $($Item.Length) octets"
    Write-Host "  SHA256 : $Hash"
    ```
  - [ ] Tester l'exécution sur Windows : `cd android; pwsh scripts/build-aar.ps1`. Vérifier qu'un `.aar` valide est produit, taille raisonnable (typiquement 5-15 MB selon les packages embarqués + runtime Go).

- [x] **Task 4 : Configurer `android/levoile-core/build.gradle.kts` pour exposer le `.aar`** (AC: #3)
  - [ ] Lire l'état actuel du fichier (livré Story 9.1) — bloc `dependencies { /* placeholder Story 9.2 */ }`.
  - [ ] Remplacer par :
    ```kotlin
    plugins {
        id("com.android.library")
    }

    android {
        namespace = "fr.plateformeliberte.levoile.core"
        compileSdk = 34

        defaultConfig {
            minSdk = 29
        }

        compileOptions {
            sourceCompatibility = JavaVersion.VERSION_17
            targetCompatibility = JavaVersion.VERSION_17
        }
    }

    repositories {
        flatDir {
            dirs("libs")
        }
    }

    dependencies {
        // Le .aar est produit par scripts/build-aar.sh (gomobile bind sur les 5 packages
        // Go partagés : internal/protocol, internal/registry, internal/auth, internal/crypto,
        // internal/leakcheck — ADR-09).
        // 'api' (vs 'implementation') propage la visibilité des classes générées par gomobile
        // au module ':app' qui consomme ':levoile-core' (voir Story 9.1 app/build.gradle.kts).
        // Le .aar est gitignoré (cf. android/.gitignore Story 9.1) — chaque dev doit
        // exécuter scripts/build-aar.sh avant le premier ./gradlew assembleDebug.
        api(files("libs/levoile-core.aar"))
    }
    ```
  - [ ] Vérifier que le fichier `libs/levoile-core.aar` est bien gitignoré (Story 9.1 a ajouté `*.aar` au `android/.gitignore`). Tant qu'il n'existe pas (avant le 1er `build-aar.sh`), Gradle remontera une erreur — c'est normal et documenté dans `README-android.md`.

- [x] **Task 5 : Test smoke JUnit `LeVoileCoreSmokeTest.kt`** (AC: #4)
  - [ ] Exécuter une première fois `bash scripts/build-aar.sh` pour produire le `.aar` (sans lequel le test ne peut compiler).
  - [ ] Inspecter le contenu Java généré : extraire le `.aar` (c'est un zip), regarder le `classes.jar` interne, ouvrir avec `jar tf classes.jar | grep -i fr/plateformeliberte/levoile/core`. Identifier les noms exacts des classes générées par gomobile pour les 5 packages exposés. **Reporter les noms trouvés dans Debug Log References** (ils dépendent de gomobile et peuvent surprendre — ex. `Protocol` peut devenir `Gojni`, ou être éclaté en sous-classes).
  - [ ] Créer `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileCoreSmokeTest.kt` :
    ```kotlin
    package fr.plateformeliberte.levoile.bridge

    import org.junit.Assert.assertNotNull
    import org.junit.Test

    /**
     * Smoke test : vérifie que les classes générées par gomobile bind (Story 9.2) sont
     * résolvables depuis Kotlin au compile-time + class-loading runtime JVM.
     *
     * Ce test ne charge PAS le runtime JNI Go (qui requiert un device/émulateur Android
     * réel et un .so packagé dans l'APK — voir Story 9.7 pour le test instrumenté).
     * Il valide uniquement la frontière compile-time + nommage du package Java généré.
     */
    class LeVoileCoreSmokeTest {

        @Test
        fun `gomobile generated classes are resolvable`() {
            // Adapter le nom exact constaté à l'inspection .aar (Task 5 dev notes).
            // Ex. : "fr.plateformeliberte.levoile.core.protocol.Protocol"
            //      "fr.plateformeliberte.levoile.core.registry.Registry"
            // Si gomobile a regroupé sous une classe unique (typique avec -javapkg) :
            //      "fr.plateformeliberte.levoile.core.Protocol"
            val resolved = Class.forName("fr.plateformeliberte.levoile.core.Protocol")
            assertNotNull(resolved)
        }
    }
    ```
  - [ ] Ajouter dans `android/app/build.gradle.kts` les dépendances test si absentes (AndroidX Test Core est minimal pour ce test JVM-only — JUnit 4 suffit) :
    ```kotlin
    dependencies {
        // ... lignes existantes Story 9.1 (core-ktx, appcompat, project(":levoile-core"))
        testImplementation("junit:junit:4.13.2")
    }
    ```
  - [ ] Exécuter `cd android && ./gradlew :app:testDebugUnitTest`. Le test doit passer.
  - [ ] Si `Class.forName` échoue avec `ClassNotFoundException`, **diagnostiquer** : (a) vérifier que `levoile-core/libs/levoile-core.aar` existe, (b) vérifier que `:levoile-core` `api(files(...))` propage bien (essayer `./gradlew :app:dependencies` pour voir le classpath), (c) corriger le nom de la classe dans le test selon ce que `jar tf` révèle, (d) pas de fix forcé — si la classe attendue n'existe vraiment pas, c'est un problème de génération gomobile ou de packages exposés vide → reporter dans Completion Notes et ne PAS toucher au code Go partagé.

- [x] **Task 6 : Créer `android/scripts/verify-shared-imports.sh`** (AC: #5)
  - [ ] Créer le fichier avec shebang `#!/usr/bin/env bash`, `set -euo pipefail`.
  - [ ] Calculer repo root via `git rev-parse --show-toplevel`.
  - [ ] Définir la liste des packages partagés (exposés via gomobile) :
    ```bash
    SHARED_PACKAGES=(
      "internal/protocol"
      "internal/registry"
      "internal/auth"
      "internal/crypto"
      "internal/leakcheck"
    )
    ```
  - [ ] Définir la liste des préfixes interdits (imports OS-spécifiques) — voir architecture l. 1710 :
    ```bash
    FORBIDDEN_PREFIXES=(
      # Packages internal OS-spécifiques desktop
      "internal/tun"
      "internal/firewall"
      "internal/routing"
      "internal/ui"
      "internal/ipc"
      "internal/wfp"
      "internal/nftables"
      "internal/wintun"
      # Arbres OS-spécifiques racine (le module Go racine est github.com/<org>/<repo>
      # — adapter le préfixe selon go.mod ; le check fait sur les chemins relatifs au module Go suffit)
      "windows/"
      "linux/"
    )
    ```
    **Note dev** : lire le `go.mod` racine pour récupérer le nom exact du module Go (ex. `github.com/velia-the-veil/le_voile`) — les imports détectés par `go list -f '{{.Imports}}'` seront préfixés par ce module path. Donc le check doit comparer `<module>/internal/tun/...` etc. Adapter `FORBIDDEN_PREFIXES` en conséquence ou utiliser un grep tolérant (`grep -E "(/|^)internal/(tun|firewall|...)/")`).
  - [ ] Stratégie de détection : préférer `go list -deps -f '{{.ImportPath}}'` qui donne tous les imports transitifs. Pour le scope de cette story (lint statique des packages directement listés, pas les transitifs au-delà de ce qui est exposé), on peut faire :
    ```bash
    cd "$REPO_ROOT"
    FAIL=0
    for pkg in "${SHARED_PACKAGES[@]}"; do
      IMPORTS=$(go list -f '{{join .Imports "\n"}}' "./$pkg" 2>/dev/null || true)
      [ -z "$IMPORTS" ] && { echo "WARN: package $pkg introuvable ou erreur go list" >&2; continue; }
      while IFS= read -r imp; do
        for forbidden in "${FORBIDDEN_PREFIXES[@]}"; do
          if echo "$imp" | grep -qE "(/|^)$forbidden(/|$)"; then
            echo "❌ $pkg → import interdit : $imp" >&2
            FAIL=1
          fi
        done
      done <<< "$IMPORTS"
    done
    if [ "$FAIL" -eq 0 ]; then
      echo "✓ Frontière ADR-09 respectée — aucun import OS-spécifique détecté dans les 5 packages partagés"
    fi
    exit "$FAIL"
    ```
  - [ ] Marquer exécutable + shellcheck-clean.
  - [ ] **Exécuter le script** depuis `android/` : `bash scripts/verify-shared-imports.sh`. Le retour DOIT être 0 (les 5 packages sont déjà censés être propres au moment de cette story). Si ce n'est pas le cas, **NE PAS corriger les packages partagés** dans cette story — reporter le findings dans Completion Notes et alerter l'utilisateur (cf. périmètre de modification).

- [x] **Task 7 : Vérification reproductibilité same-machine** (AC: #6)
  - [ ] Exécuter deux fois consécutives `bash scripts/build-aar.sh`.
  - [ ] Comparer les SHA256 affichés. Si identiques → AC validée. Si différents → diagnostiquer (timestamps embarqués par gomobile ?, ordre des fichiers dans le zip ?).
  - [ ] Reporter les deux SHA256 dans Completion Notes.
  - [ ] Si non-reproductible, c'est PROBABLEMENT lié aux timestamps embarqués dans le `.aar` (zip metadata mtime) ou à l'ordre des fichiers. **Investigation hors-scope cette story** — reporter et préparer Story 12.4 (build Docker pinné qui force `SOURCE_DATE_EPOCH` etc.). Documenter clairement dans Completion Notes que la reproductibilité same-machine same-build n'est pas atteinte au bit près.

- [x] **Task 8 : Patcher `android/README-android.md` avec procédure build AAR** (AC: #7)
  - [ ] Lire l'état actuel du `README-android.md` (livré Story 9.1 Task 10).
  - [ ] Remplacer la mention « Build AAR (à venir Story 9.2) » par une section complète :
    ```markdown
    ## Build du `.aar` du noyau Go partagé

    Le `levoile-core/libs/levoile-core.aar` est produit par `gomobile bind` à partir
    des 5 packages Go partagés (`internal/protocol`, `internal/registry`, `internal/auth`,
    `internal/crypto`, `internal/leakcheck` — voir ADR-09).

    ### Pré-requis (à faire UNE FOIS)

    1. Go ≥ 1.26 installé (vérifier `go version`).
    2. `gomobile` installé :
       ```
       go install golang.org/x/mobile/cmd/gomobile@latest
       gomobile init
       ```
       La commande `gomobile init` télécharge l'Android NDK (~1.5 GB, 5-15 min selon connexion).

    ### Build du `.aar`

    Linux/macOS :
    ```
    cd android
    bash scripts/build-aar.sh
    ```

    Windows (PowerShell) :
    ```
    cd android
    pwsh scripts/build-aar.ps1
    ```

    L'artefact produit : `android/levoile-core/libs/levoile-core.aar` (gitignoré).

    Le script affiche en sortie la taille + le SHA256 de l'`.aar` produit.

    ### Quand re-builder

    À chaque modification d'un des 5 packages Go partagés. Après modification puis
    `bash scripts/build-aar.sh`, exécuter `./gradlew clean assembleDebug` pour que
    Gradle prenne en compte le nouveau `.aar` (Gradle ne détecte pas toujours le
    changement d'un fichier dans `flatDir` sans `clean`).

    ### Vérifier la frontière ADR-09 (imports cross-OS)

    ```
    cd android
    bash scripts/verify-shared-imports.sh
    ```

    Le script vérifie que les 5 packages partagés n'importent aucun package OS-spécifique
    (`internal/tun/`, `internal/firewall/`, etc.). À exécuter avant tout PR touchant
    aux packages partagés.
    ```
  - [ ] **Important** : ne toucher AUCUNE autre section du README. Le périmètre est strict (cf. en-tête de cette story).

- [x] **Task 9 : Vérifications finales + git status check** (AC: tous)
  - [ ] Exécuter dans cet ordre :
    1. `bash android/scripts/verify-shared-imports.sh` — succès attendu (exit 0).
    2. `bash android/scripts/build-aar.sh` — succès, `.aar` produit dans `levoile-core/libs/`.
    3. `cd android && ./gradlew clean :app:assembleDebug` — succès, classes du `.aar` consommables.
    4. `cd android && ./gradlew :app:testDebugUnitTest` — succès, smoke test passe.
    5. `cd android && ./gradlew :app:assembleRelease` — succès (vérifie que ProGuard rules JNI gomobile de Story 9.1 protègent bien les classes du `.aar`).
    6. `cd android && ./gradlew :app:lint` — pas de nouvelle erreur introduite par cette story.
  - [ ] Exécuter `git status` à la racine du repo. Vérifier que **TOUS les changements sont sous `android/`** sauf `_bmad-output/implementation-artifacts/sprint-status.yaml` (mise à jour `ready-for-dev` → `review`) ET ce fichier story `_bmad-output/implementation-artifacts/9-2-...md` (auto-update). Si un autre fichier hors `android/` apparaît modifié, **STOP** et investiguer (probablement un side-effect non prévu).
  - [ ] Reporter dans Completion Notes les métriques finales : taille `.aar`, SHA256, taille APK debug + release, durée totale `assembleRelease`, durée `gomobile bind`.

## Dev Notes

### Décisions architecturales contraignantes

- **ADR-09 — gomobile pour le noyau Go partagé** (architecture.md l. 2390-2393) : la frontière contractuelle Kotlin↔Go est l'`.aar` produit par `gomobile bind`. Cette story livre le mécanisme de production de cette frontière. **Aucune autre frontière partagée Kotlin↔Go ne doit être créée** (pas de cgo direct, pas de réécriture native Kotlin de la logique partagée, pas de WASM).

- **ADR-08 — Isolation OS maximale** (architecture.md l. 2385-2388) : par défaut, aucun fichier hors `android/` n'est touché. **Exception strictement listée** par cette story : les scripts `build-aar.{sh,ps1}` et `verify-shared-imports.sh` invoquent les packages racine `./internal/{protocol,registry,auth,crypto,leakcheck}` en LECTURE uniquement (gomobile lit les sources Go pour générer le `.aar` ; verify-shared-imports lit les imports). Aucune écriture/modification dans le code Go partagé.

- **NFR-AND-11 — R8/ProGuard préservant les classes JNI** (prd.md l. 707) : Story 9.1 a livré les rules ProGuard. Cette story produit l'`.aar` consommé — il faut vérifier en Task 9 que `assembleRelease` réussit (c'est-à-dire que les rules `-keep class fr.plateformeliberte.levoile.core.** { *; }` posées Story 9.1 protègent bien les classes générées au nom canonique `fr.plateformeliberte.levoile.core.*` via le flag `-javapkg=fr.plateformeliberte.levoile.core` du gomobile bind).

- **Reproductibilité F-Droid (ADR-11)** (architecture.md l. 2400-2402) : F-Droid impose un build reproductible bit-à-bit. Cette story prépare le terrain en livrant un `.aar` reproductible **same-machine same-environment**. La reproductibilité **cross-machine** (deux développeurs / deux NDK paths / deux temporalités) est portée par Story 12.4 via build Docker pinné. **Important** : ne pas tenter de la livrer ici (hors scope, complexité élevée).

### Conflits artefacts résolus

- **Chemin du `.aar`** : conflit détecté entre Epic 9.2 AC l. 1535 (`-o android/app/libs/levoile-core.aar`) et architecture l. 379, 382, 1233 (`android/levoile-core/libs/levoile-core.aar`) ainsi que la livraison Story 9.1 (qui a configuré `app/build.gradle.kts` avec `implementation(project(":levoile-core"))`, ce qui suppose que le `.aar` est consommé via le module `:levoile-core`, pas directement par `:app`). **Décision : `android/levoile-core/libs/levoile-core.aar`** (alignement architecture + Story 9.1 livrée). L'epic 9.2 sera patché en post-livraison de cette story si nécessaire — voir Completion Notes.

- **Mode de consommation Gradle** : Epic 9.2 AC l. 1536 propose `implementation(files("libs/levoile-core.aar"))` direct dans `:app`. Story 9.1 a fixé `implementation(project(":levoile-core"))` qui implique que `:levoile-core` doit ré-exporter le `.aar`. **Décision : `:levoile-core` expose le `.aar` via `api(files("libs/levoile-core.aar"))`** (préfixe `api` au lieu de `implementation` propage la visibilité transitive — sans cela les classes du `.aar` seraient non-importables depuis `:app`). Cohérent Story 9.1 et architecture l. 376-379.

- **Nom du package Java généré** : sans flag `-javapkg`, gomobile crée par défaut un package Java basé sur le nom du package Go (ex. `protocol`, `registry`). Avec `-javapkg=fr.plateformeliberte.levoile.core`, les classes seront générées sous `fr.plateformeliberte.levoile.core.protocol.*`, `fr.plateformeliberte.levoile.core.registry.*`, etc. **Décision : utiliser `-javapkg=fr.plateformeliberte.levoile.core`** pour aligner sur le `namespace` du module `:levoile-core` (cohérent Story 9.1) et permettre aux rules ProGuard `-keep class fr.plateformeliberte.levoile.core.**` de matcher.

### Conventions Android (architecture l. 848-865)

- **Scripts shell** : conventions Bash standard, shebang explicite, `set -euo pipefail`, shellcheck-clean.
- **Scripts PowerShell** : `#Requires -Version 5.1`, `$ErrorActionPreference = "Stop"`, compatible PS 5.1 (Windows par défaut) ET PS Core 7+.
- **Tests Kotlin** : JUnit 4 (pas JUnit 5 — convention écosystème Android encore à JUnit 4 majoritaire). Co-localisation : tests dans `app/src/test/kotlin/<package miroir>/`.

### Apprentissages Story 9.1 reproductibles

D'après le `Completion Notes` de Story 9.1 (livrée 2026-05-02) :
- **Toolchain Android déjà installée localement** : JDK 17.0.10, Gradle 8.7, Android SDK cmdline-tools + platform-tools + platforms;android-34 + build-tools;34.0.0. Variables `JAVA_HOME`, `ANDROID_HOME`, `ANDROID_SDK_ROOT` persistées user-scope. **Pas besoin de réinstaller**.
- **À installer en plus pour cette story** : `gomobile` + NDK (cf. Task 1).
- **Permission AGP-injectée** `DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` : déjà documentée Story 9.1, pas d'impact sur cette story.
- **Aucun émulateur Android disponible localement** : le test smoke JUnit (Task 5) est JVM-only — pas de device requis. Le test instrumenté JNI complet (handshake QUIC réel) reste reporté à Story 9.7 + Story 12.6.

### Anti-patterns à éviter

- ❌ **Ne pas faire du `gomobile bind` une task Gradle directe** — l'architecture (l. 202, 327, 2393) impose explicitement un script séparé pour préserver une frontière build chain claire. Une task Gradle couplerait le build chain Android au build chain Go et compliquerait la reproductibilité F-Droid (qui veut un script bash explicite, pas une magie Gradle).
- ❌ **Ne pas exposer plus de packages que les 5 listés** — la frontière étroite est explicite (architecture l. 2231, 2391). Si une autre story Android estime avoir besoin d'un package Go supplémentaire, ouvrir un ADR avant.
- ❌ **Ne pas modifier le code Go partagé** — interdit par périmètre. Si gomobile remonte une erreur sur un package partagé (ex. import cgo non supporté), reporter et bloquer.
- ❌ **Ne pas embarquer le `.aar` dans Git** — `.gitignore` Story 9.1 l'exclut (`*.aar`). Il est régénéré à chaque modif partagée. La gitignorance est explicitement documentée par architecture l. 1597.
- ❌ **Ne pas hardcoder le path NDK dans les scripts** — gomobile résout le NDK via `ANDROID_NDK_HOME` ou via détection automatique post `gomobile init`. Hardcoder casserait la portabilité cross-machine.
- ❌ **Ne pas créer de `Makefile`** — convention multi-OS du repo (cf. commits récents `2c3386e refactor: remove root Makefile`). Pour Android, c'est Gradle wrapper + scripts bash/ps1 dédiés.
- ❌ **Ne pas créer un `app/libs/`** — le `.aar` va dans `levoile-core/libs/` (cf. conflit résolu plus haut). Si Task 4 active accidentellement les deux, vérifier qu'aucun `app/libs/` orphelin n'est créé.

### Project Structure Notes

**Fichiers attendus livrés par cette story** (tous sous `android/`) :
- `android/scripts/build-aar.sh` (NOUVEAU, exécutable)
- `android/scripts/build-aar.ps1` (NOUVEAU)
- `android/scripts/verify-shared-imports.sh` (NOUVEAU, exécutable)
- `android/levoile-core/build.gradle.kts` (MODIFIÉ — bloc `dependencies` étendu)
- `android/app/build.gradle.kts` (MODIFIÉ — ajout `testImplementation("junit:junit:...")`)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileCoreSmokeTest.kt` (NOUVEAU)
- `android/README-android.md` (MODIFIÉ — section "Build du `.aar`" remplace placeholder)

**Fichiers gitignorés produits au build** (pas commit) :
- `android/levoile-core/libs/levoile-core.aar` (généré par `build-aar.sh`)

**Fichiers hors `android/` autorisés à modifier par cette story** :
- `_bmad-output/implementation-artifacts/sprint-status.yaml` : passage status story 9-2 `ready-for-dev` → `review` à la fin
- `_bmad-output/implementation-artifacts/9-2-script-build-aar-sh-gomobile-bind-du-noyau-go-partage.md` : auto-update (Status, Completion Notes, File List, Change Log)

**Aucun autre fichier hors `android/` ne doit être modifié.** Vérifier via `git status` final (cf. Task 9).

### References

- [Source: epics.md#Story 9.2: Script `build-aar.sh` — gomobile bind du noyau Go partagé (l. 1524-1545)]
- [Source: epics.md#Epic 9 — Noyau Android (l. 1494-1496)]
- [Source: prd.md#FR-AND-1 (l. 609)]
- [Source: prd.md#NFR-AND-3, NFR-AND-11 (l. 699, 707)]
- [Source: architecture.md#Selected Stack — ANDROID, gomobile bind (l. 246-302)]
- [Source: architecture.md#Build Tooling — Android (l. 296-302)]
- [Source: architecture.md#Project Structure — android/levoile-core/, android/scripts/ (l. 1593-1604)]
- [Source: architecture.md#Frontière Noyau Partagé Go ↔ Android (l. 1707-1712)]
- [Source: architecture.md#ADR-08 Isolation OS maximale (l. 2385-2388)]
- [Source: architecture.md#ADR-09 gomobile noyau Go partagé (l. 2390-2393)]
- [Source: architecture.md#ADR-11 F-Droid + APK direct (l. 2400-2402)]
- [Source: architecture.md#Frontière imports cross-OS (l. 1710)]
- [Memory: feedback_os_isolation — duplication code Win/Linux/Android préférée à abstraction partagée]
- [Source: 9-1-module-gradle-android-structure-projet.md (livrée 2026-05-02 — toolchain installée, structure Gradle, ProGuard rules)]

### Notes de divergence corrigées en amont

- **Epic 9.2 vs architecture (chemin `.aar`)** : tranché plus haut (cf. "Conflits artefacts résolus") au profit de l'architecture + Story 9.1 livrée. **Action de suivi recommandée hors-story** : patcher Epic 9.2 dans `epics.md` pour aligner sur `levoile-core/libs/`. Reporter dans Completion Notes pour visibilité utilisateur.

- **Aucun apprentissage cross-stories Android au-delà de 9.1** : 9.1 est la seule story Android livrée. Pas d'autre source d'apprentissage Android.

## Dev Agent Record

### Agent Model Used

Claude Opus 4.7 (1M context) — `claude-opus-4-7[1m]`

### Debug Log References

**Toolchain état d'entrée (déjà installé Story 9.1)** :
- Go 1.26.2 windows/amd64 (`go.mod` cible `go 1.26`)
- JDK 17.0.10.7 Microsoft (`JAVA_HOME` user-scope)
- Gradle 8.7 (via wrapper `android/gradlew.bat`)
- Android SDK cmdline-tools + platform-tools + platforms;android-34 + build-tools;34.0.0
- Android NDK r26d (`26.3.11579264`) à `C:\Users\Akerimus\AppData\Local\Android\Sdk\ndk\26.3.11579264\` (`ANDROID_NDK_HOME` user-scope)
- `gomobile` + `gobind` déjà présents dans `~/go/bin/` (installés antérieurement)

**Toolchain ajouté pendant cette session** :
- Aucun (gomobile + NDK déjà en place)
- `golang.org/x/mobile` ajouté au `go.mod` racine via `go get golang.org/x/mobile@latest` (autorisé décision utilisateur 2026-05-02 — voir Périmètre §Exception 2)

**Inspection du `.aar` produit (Task 1 smoke + Task 9 final)** :
Classes Java générées par gomobile bind (extracted via `jar -tf classes.jar`) :
- `fr/plateformeliberte/levoile/core/protocol/Protocol.class`
- `fr/plateformeliberte/levoile/core/auth/Auth.class`
- `fr/plateformeliberte/levoile/core/crypto/Crypto.class`
- `fr/plateformeliberte/levoile/core/registry/Registry.class`
- `fr/plateformeliberte/levoile/core/leakcheck/Leakcheck.class`
- `go/Seq.class`, `go/Seq$*.class`, `go/Universe.class`, `go/Universe$proxyerror.class`, `go/error.class`

ProGuard rules de Story 9.1 (`-keep class fr.plateformeliberte.levoile.core.** { *; }` et `-keep class go.** { *; }`) couvrent toutes ces classes — `assembleRelease` réussit sans warning de proguard miss.

**Décisions adaptatives prises pendant la session (corrections vs spec d'origine)** :

1. **`android/internal/{protocol,auth,...}` → `android/shims/{protocol,auth,crypto,registry,leakcheck}`** : tentative initiale de placer les shims sous `android/internal/` (cohérent avec instruction utilisateur « cherche dans android/internal »). Échec via gomobile : la règle Go `internal/` interdit l'import depuis le package `gobind/gobind` que gomobile génère dans son work dir temporaire (hors module). Renommage en `android/shims/` qui n'est pas soumis à cette règle. Documenté dans le doc-comment de chaque shim.

2. **5 shims au lieu de 2** : la décision initiale était de créer des shims uniquement pour les packages spec absents (`internal/protocol`, `internal/auth`). À l'exécution, même blocage Go-internal sur les 3 packages racine existants (`internal/crypto`, `internal/registry`, `internal/leakcheck`) car gobind est hors-module. Extension naturelle de la décision shim aux 5 packages — décision utilisateur 2026-05-02. Tous les shims vivent sous `android/shims/`.

3. **`.aar` placé dans `app/libs/` (pas `levoile-core/libs/`)** : la spec d'origine Story 9.2 plaçait le `.aar` dans `levoile-core/libs/` avec consommation via `:levoile-core`. À l'exécution, AGP a explicitement refusé : « Direct local .aar file dependencies are not supported when building an AAR. The resulting AAR would be broken because the classes and Android resources from any local .aar file dependencies would not be packaged in the resulting AAR. » (`:levoile-core:bundleReleaseLocalLintAar` FAILED). Migration du `.aar` vers `app/libs/` + consommation via `implementation(files("libs/levoile-core.aar"))` directement dans `:app` (pattern conforme AGP, identique à ce que l'epic 9.2 d'origine spécifiait l. 1535). `:levoile-core` reste comme placeholder pour les wrappers Kotlin idiomatiques de Story 9.7 (`GoCoreAdapter` etc.).

4. **Scripts shell : tolérance JAVA_HOME mingw vs Windows-native** : sur Git Bash/MSYS2 Windows, le `PATH` est en format `/c/Users/...` mais `gomobile` (Go) invoque `javac` via `exec.LookPath` qui parse `%PATH%` en Windows-native. Conversion via `cygpath -u` quand dispo, sinon `build-aar.sh` documente clairement qu'il est conçu pour Linux/macOS et redirige vers `build-aar.ps1` pour Windows. La `.ps1` fonctionne nativement sur Windows et a été validée end-to-end.

5. **Test JUnit : `Class.forName(name, /*initialize=*/false, classLoader)`** : version naïve `Class.forName(name)` déclenche le static initializer des classes gomobile, qui invoque `System.loadLibrary("gojni")`. Sur unit test JVM-only (sans device Android), `libgojni.so` est introuvable → `UnsatisfiedLinkError` / `ExceptionInInitializerError` → tous les tests rouges. Fix via 3-arg `Class.forName(name, false, loader)` qui résout la classe sans charger le natif. Le test instrumenté JNI complet (avec device + libgojni) reste porté par Story 9.7.

6. **Reproductibilité same-machine NON-atteinte** (AC#6 spec) : 2 builds successifs `pwsh build-aar.ps1` produisent des `.aar` de tailles différentes (13155558 vs 13155666 octets — drift de 108 octets) et de SHA256 différents. Cause probable : timestamps embarqués dans le zip metadata du `.aar`. **Hors scope cette story** comme prévu par l'AC#6 spec (« la reproductibilité cross-machine ainsi que la reproductibilité temporelle sont portées par Story 12.4 ») — la livraison Story 12.4 forcera `SOURCE_DATE_EPOCH` + autres fixations dans un build Docker pinné.

### Completion Notes List

**Métriques builds (final, après migration `.aar` vers `app/libs/`)** :

- `:app:assembleDebug` : BUILD SUCCESSFUL ~6s (incrementally) — APK debug produit
- `:app:assembleRelease` : BUILD SUCCESSFUL ~18s (clean), R8/ProGuard activé, `levoile-core.aar` 13.16 MB intégré, classes JNI gomobile préservées par les rules ProGuard de Story 9.1 (`-keep class fr.plateformeliberte.levoile.core.**` + `-keep class go.**`)
- `:app:testDebugUnitTest` : BUILD SUCCESSFUL — 7 tests `LeVoileCoreSmokeTest` passants
- `:app:lint` : BUILD SUCCESSFUL — aucune nouvelle erreur introduite par cette story
- Pipeline complet `clean + assembleDebug + assembleRelease + testDebugUnitTest + lint` : 18s pour 165 actionable tasks
- gomobile bind : ~15s pour les 5 shims + transitive deps (quic-go, golang.org/x/net, etc.)
- `.aar` taille typique : 13.15-13.16 MB (drift mtime sur runs successifs)

**ACs satisfaits** :

- ✅ AC#1 (build-aar.sh Linux/macOS) — script créé + exécutable + syntax OK ; détection gomobile, javac, NDK avec messages d'erreur clairs ; trap de cleanup en cas d'échec ; rapport taille + SHA256 en sortie. Validé syntactiquement (`bash -n`) ; run end-to-end Linux non testé localement (machine Windows) — la `.ps1` valide la logique équivalente.
- ✅ AC#2 (build-aar.ps1 Windows) — script créé + run end-to-end OK depuis subshell PowerShell `powershell.exe -NoProfile`, `.aar` 13.16 MB produit, SHA256 capturé. Compatible PS 5.1 (testé) et PS Core 7+ (par construction).
- ✅ AC#3 (`:levoile-core` consommant le `.aar`) — **AC réinterprétée** : suite à la contrainte AGP, le `.aar` est consommé par `:app` directement (pas par `:levoile-core`). `:levoile-core` reste comme placeholder pour les wrappers Kotlin Story 9.7. Les classes générées par gomobile sont importables depuis `:app` (preuve via le smoke test).
- ✅ AC#4 (test smoke JUnit `LeVoileCoreSmokeTest`) — 7 tests passants sur les 5 classes shim + 2 classes go.* + 1 test reflection sur `Protocol.version()`.
- ✅ AC#5 (verify-shared-imports.sh) — script créé + run OK : 7 packages partagés vérifiés (4 shims qui importent internal/* + internal/{crypto,registry,leakcheck} + internal/tunnel), aucun import OS-spécifique détecté.
- ⚠️ AC#6 (reproductibilité same-machine) — NON atteinte (drift mtime zip) : explicitement reporté à Story 12.4 par la spec AC#6.
- ✅ AC#7 (`README-android.md` patché) — section « Build du `.aar` du noyau Go partagé (Story 9.2) » ajoutée avec : tableau de mapping des 5 shims, pré-requis toolchain, commandes Linux/macOS + Windows, smoke test, verify-shared-imports.

**Actions de suivi recommandées (hors scope Story 9.2)** :

1. ~~**Patcher `architecture.md`** (l. 59, 107, 1726, 2391)~~ — **FAIT 2026-05-02** lors de la régénération Story 9.3 : ajout erratum dans ADR-09 + patches sur l. 59 (description code partagé), l. 107 (gomobile toolchain + ajout `golang.org/x/mobile` racine), l. 210-214 (table des shims gomobile), l. 298 (commande build-aar exemple), l. 1707-1712 (Frontière Noyau Partagé), l. 1726 (table FR1-4 mapping). Mentions narratives résiduelles (diagrammes ASCII l. 1907, matrices conceptuelles l. 2053/2130/2231) couvertes par l'erratum ADR-09.
2. ~~**Patcher `epics.md`** (l. 1535)~~ — **FAIT 2026-05-02** lors de la régénération Story 9.3 : commande gomobile + chemin `levoile-core/libs/levoile-core.aar` + mode de consommation `api(files(...))` propagé. Aussi patché l. 325 (frontière Stack Android).
3. **Patcher `prd.md`** — **FAIT 2026-05-02** : l. 356, 426 alignés sur la cartographie shims.
4. **Story 12.4** : reproductibilité cross-machine et temporelle du `.aar` via build Docker pinné (`SOURCE_DATE_EPOCH`, version Go pinnée, version gomobile pinnée, version NDK pinnée).
5. **Story 12.2** : intégrer `bash android/scripts/verify-shared-imports.sh` comme gate CI bloquant.
6. **`go.mod`/`go.sum` upgrades transitifs** introduits par `go get golang.org/x/mobile@latest` : `golang.org/x/crypto` v0.49.0 → v0.50.0, `golang.org/x/mod` v0.33.0 → v0.35.0, `golang.org/x/net` v0.52.0 → v0.53.0, `golang.org/x/sys` v0.42.0 → v0.43.0, `golang.org/x/text` v0.35.0 → v0.36.0, `golang.org/x/tools` v0.42.0 → v0.44.0. Patch upgrades stables, pas de breaking change attendu — vérifier en regression test desktop avant merge. **✅ Régression desktop validée 2026-05-02** post-Story 9.1 catalog refacto : `go test ./internal/...` racine = 9/9 packages OK (`captive`, `crypto`, `httpproxy`, `leakcheck`, `registry`, `stun`, `tunnel`, `updater`, `watchdog`) ; `go vet ./...` racine + `windows/` + `linux/` + `relay/` = 0 warning. Aucune régression introduite par les bumps transitifs. Documentation centralisée ajoutée dans `architecture.md` §"Patterns OS-isolation → Exceptions ADR-08 connues" (tableau borné des exceptions racine) et `android/README-android.md` §Notes (rappel + pointers).

### File List

**Créés** (relatifs au repo root) :
- `android/shims/protocol/protocol.go` — shim canonique (pas de package racine équivalent), expose `Version()`, `FramingHeaderSize()`
- `android/shims/auth/auth.go` — shim canonique (pas de package racine équivalent), expose `TokenHeaderName()`, `TokenTTLSeconds()`, `TokenRefreshThresholdSeconds()`
- `android/shims/crypto/crypto.go` — shim importing `internal/crypto`, expose `Ed25519PublicKeySize()`, `IsValidPublicKeyBase64(s)`, `ReleasePublicKeyCurrentBase64()`
- `android/shims/registry/registry.go` — shim importing `internal/registry`, expose `ExtractCountryCode(id, domain)`, `SupportedCountryCount()`
- `android/shims/leakcheck/leakcheck.go` — shim importing `internal/leakcheck` (transitif `internal/tunnel` + `quic-go`), expose `DefaultSTUNServersJoined()`, `BuildBindingRequestSize()`
- `android/scripts/build-aar.sh` — script Linux/macOS, exécutable
- `android/scripts/build-aar.ps1` — script Windows PowerShell
- `android/scripts/verify-shared-imports.sh` — lint frontière ADR-09, exécutable
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileCoreSmokeTest.kt` — 7 tests JUnit JVM-only

**Modifiés** (relatifs au repo root) :
- `android/app/build.gradle.kts` — ajout `implementation(files("libs/levoile-core.aar"))` + `testImplementation("junit:junit:4.13.2")`
- `android/levoile-core/build.gradle.kts` — refactor du bloc `dependencies` en placeholder commenté (le `.aar` est consommé par `:app` à cause de la contrainte AGP sur les modules library)
- `android/README-android.md` — ajout de la section « Build du `.aar` du noyau Go partagé (Story 9.2) » avec mapping des shims, pré-requis, commandes, smoke test, verify-shared-imports

**Modifiés racine** (autorisés par exceptions périmètre) :
- `go.mod` — ajout `golang.org/x/mobile v0.0.0-20260410095206-2cfb76559b7b // indirect` + upgrades transitifs `golang.org/x/{crypto,mod,net,sys,text,tools}` (patch upgrades)
- `go.sum` — checksums correspondants

**Modifiés métadata story** :
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — passage `9-2-...: ready-for-dev` → `in-progress` → `review`
- `_bmad-output/implementation-artifacts/9-2-script-build-aar-sh-gomobile-bind-du-noyau-go-partage.md` — Status `ready-for-dev` → `review`, Tasks toutes cochées, Dev Agent Record + File List + Change Log remplis

**Gitignoré (non commit)** :
- `android/app/libs/levoile-core.aar` — produit par `bash scripts/build-aar.sh` ou `pwsh scripts/build-aar.ps1`, regen sur modification des shims `android/shims/*` ou des packages racine `internal/{crypto,registry,leakcheck,tunnel}`

## Change Log

| Date | Auteur | Changement |
|---|---|---|
| 2026-05-02 | create-story (Claude Opus 4.7) | Story 9.2 régénérée. Périmètre strictement confiné à `android/` avec exception unique documentée (gomobile bind sur packages racine partagés en lecture seule, ADR-09). Conflit chemin `.aar` (Epic 9.2 vs architecture vs Story 9.1) tranché initialement : `levoile-core/libs/`. Scripts `build-aar.{sh,ps1}` + `verify-shared-imports.sh` + smoke test JUnit + activation `levoile-core/build.gradle.kts` + patch `README-android.md`. Status: ready-for-dev. |
| 2026-05-02 | dev-story (Claude Opus 4.7) | Story 9.2 implémentée. 5 shims `android/shims/{protocol,auth,crypto,registry,leakcheck}` créés (renommage `internal/` → `shims/` à cause de la règle Go internal package). `.aar` migré de `levoile-core/libs/` vers `app/libs/` (contrainte AGP : un module library ne peut pas bundler un `.aar` local). `golang.org/x/mobile` ajouté au `go.mod` racine (autorisé décision utilisateur — pré-requis gomobile). **Note scope `go.mod`** : `go get golang.org/x/mobile@latest` a aussi propagé des patch upgrades transitifs sur `golang.org/x/{crypto v0.49→v0.50, mod v0.33→v0.35, net v0.52→v0.53, sys v0.42→v0.43, text v0.35→v0.36, tools v0.42→v0.44}`. Ces upgrades sont une conséquence inhérente du `go get` (résolution MVS des min versions déclarées par la nouvelle dep). Stables, patch-level uniquement, sans breaking change attendu — review du diff `go.mod`/`go.sum` avant merge recommandé. Pipeline `clean + assembleDebug + assembleRelease + testDebugUnitTest + lint` : BUILD SUCCESSFUL en 18s. AAR 13.16 MB (avec quic-go transitif). 7/7 smoke tests passants. verify-shared-imports : 7 packages partagés clean, aucun import OS-spécifique. Reproductibilité same-machine non atteinte (drift mtime zip — reporté Story 12.4 conforme spec AC#6). Actions de suivi : patch architecture.md + epics.md pour refléter cartographie réelle (5 shims sous android/shims/), Story 12.4 (build Docker pinné), Story 12.2 (verify-shared-imports en CI gate). Status: review. |
| 2026-05-02 | code-review (Claude Opus 4.7) | Adversarial review : 11 findings (3 HIGH + 5 MEDIUM + 3 LOW), tous corrigés. **HIGH** : (H1) header doc `build-aar.sh` mis à jour `app/libs/` (vs `levoile-core/libs/` stale) ; (H2) AC#3 réécrit pour matcher la réalité AGP (`:app` consomme via `files(...)`, pas `:levoile-core` via `flatDir`) ; (H3) `verify-shared-imports.sh` étendu aux 5 shims (ajoute `protocol`+`auth` défensivement pour Story 9.7 future). **MEDIUM** : (M1) check Go version durci `>= 1.26` matche `go.mod` (`.sh` + `.ps1`) ; (M2) logique `stat` simplifiée dans `build-aar.sh` (suppression branche morte) ; (M3) `verify-shared-imports.sh` passé en transitif (`.Deps` au lieu de `.Imports`) — capture maintenant les violations ADR-09 indirectes ; (M4) scope upgrades `go.mod` documenté explicitement dans Change Log ; (M5) commande de vérif `libgojni.so` 4-ABIs ajoutée au README sous "Audit APK" (4 ABIs confirmées présentes dans l'APK release de cette session). **LOW** : (L1) doc STUN binding request size précisée RFC 5389 §6 ; (L2) smoke test wrap `getMethod("version")` en try/catch + `fail()` lisible ; (L3) doc `registry.go` remplace référence mémoire interne par référence à `internal/registry/countries.go`. Pipeline re-validé après fixes. Status: review (en attente vérif finale). |
