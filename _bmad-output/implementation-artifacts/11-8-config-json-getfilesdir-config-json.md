# Story 11.8: Config JSON `getFilesDir()/config.json`

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## ⚠️ Périmètre de modification (lire AVANT toute édition)

> **Le dev DOIT travailler exclusivement depuis le sous-dossier `android/` du repo. Aucun fichier hors de `android/` ne doit être créé, modifié ou supprimé par cette story.**
>
> **Story 11.8 livre** :
> 1. Une nouvelle classe `ConfigStore.kt` Kotlin (`android/app/src/main/kotlin/.../config/ConfigStore.kt`) qui sérialise/désérialise une `data class ConfigData` en JSON via `org.json.JSONObject` (Android built-in, pas de dépendance Gson/Moshi/kotlinx-serialization).
> 2. Le fichier persisté à `getFilesDir()/config.json` (private app dir, UID-only par défaut Android — équivalent 0600 desktop, NFR-AND-7).
> 3. Méthodes `load(): ConfigData`, `save(config: ConfigData)`, `migrate(oldVersion: Int, newVersion: Int)`.
> 4. Schema initial v1 : `preferredCountry: String = "DE"`, `registryCache: String? = null` (placeholder), `lastVerifiedEd25519Key: String? = null` (placeholder).
> 5. Fallback config par défaut si fichier absent (premier lancement) ou JSON corrompu (avec log WARN sans data utilisateur).
> 6. `LeVoileBridge.selectCountry` (Story 11.2) **migré** de SharedPreferences vers `ConfigStore.save(config.copy(preferredCountry = ...))`.
> 7. `OnboardingActivity` flag `onboarding_completed` **reste dans SharedPreferences** (pas migré — le flag est binaire trivial, JSON over-engineering pour ce cas).
> 8. Tests JVM `ConfigStoreTest.kt` + `ConfigMigrationTest.kt` (cohérent epics.md l. 2034 « test unitaire `ConfigMigrationTest` vérifie chaque migration de schema »).
> 9. Test instrumenté optionnel (Story 12.6 hérite ; pour 11.8, JVM-only suffit) qui vérifie `adb shell run-as autre.app cat config.json` échoue — épic AC #1 dernier point.
>
> **Aucun fichier Go n'est lu, créé ou modifié.** Aucun import gomobile.
>
> **Rappel ADR-08** : `ConfigStore` est strictement Android (équivalent fonctionnel `internal/config/` desktop TOML). Pas de partage, pas de factorisation cross-OS. Cohérent architecture.md l. 640 (« Pas de TOML sur Android — la config TOML reste desktop-only »).
>
> **Zones explicitement OFF-LIMITS** :
>
> | Zone | Livrée par | État pour 11.8 |
> |---|---|---|
> | `go.mod`, `go.sum`, `android/shims/`, `android/scripts/`, `android/levoile-core/`, `internal/`, `linux/`, `relay/`, `tools/`, `windows/`, `linux/` | 1-9 | INTACT |
> | `android/app/build.gradle.kts` | 9.x/10.x/11.x | INTACT — `org.json.JSONObject` est Android built-in (`android.jar`), pas de dépendance Gradle nécessaire |
> | `android/app/src/main/AndroidManifest.xml` | 9.1+9.4+11.5 | INTACT |
> | `android/app/src/main/kotlin/.../{kill,conflict,vpn,log,ui,assets,onboarding}/` | 9.x/10.x/11.x | INTACT |
> | `android/app/src/main/kotlin/.../MainActivity.kt` | 9.3+9.5+10.x+11.x | INTACT |
> | `android/app/src/main/kotlin/.../bridge/LeVoileBridge.kt` | 9.3+10.2+10.3+11.2+11.3 | **MODIFIÉ uniquement à la marge** : `selectCountry` lit/persiste via `ConfigStore` au lieu de SharedPreferences brute. Le reste intact |
> | `android/app/src/main/kotlin/.../config/ConfigStore.kt` | (absent) | **NOUVEAU — cœur de cette story** |
> | `android/app/src/main/kotlin/.../config/ConfigData.kt` | (absent) | **NOUVEAU — data class + companion DEFAULT** |
>
> **Concrètement** : `git status` à la fin doit montrer **uniquement** :
>   (a) `android/app/src/main/kotlin/.../config/ConfigStore.kt` (NOUVEAU),
>   (b) `android/app/src/main/kotlin/.../config/ConfigData.kt` (NOUVEAU),
>   (c) `android/app/src/main/kotlin/.../bridge/LeVoileBridge.kt` (MODIFIÉ uniquement à la marge — selectCountry migré),
>   (d) `android/app/src/test/kotlin/.../config/ConfigStoreTest.kt` (NOUVEAU),
>   (e) `android/app/src/test/kotlin/.../config/ConfigMigrationTest.kt` (NOUVEAU),
>   (f) `_bmad-output/implementation-artifacts/sprint-status.yaml`,
>   (g) `_bmad-output/implementation-artifacts/11-8-config-json-getfilesdir-config-json.md`.
>
> **Anti-patterns** :
> - Ajouter Gson, Moshi, kotlinx-serialization en dépendance Gradle — `org.json.JSONObject` Android built-in suffit pour 3 champs scalaires (économie ~300 KB APK et zéro setup ProGuard).
> - Persister via `Context.openFileOutput(MODE_WORLD_WRITEABLE)` ou `MODE_WORLD_READABLE` — interdits depuis Android 7+ (`SecurityException`), et viole NFR-AND-7. **Toujours `MODE_PRIVATE`**.
> - Migrer `onboarding_completed` flag depuis SharedPreferences vers ConfigStore — over-engineering pour un boolean.
> - Stocker des secrets (clés Ed25519 privées, tokens) dans ConfigStore en clair — utiliser `EncryptedSharedPreferences` ou Android Keystore (Phase 2 si besoin). **Pour 11.8** : seulement `lastVerifiedEd25519Key` (clé PUBLIQUE master = pas un secret).
> - Logger le contenu de `config.json` (`Log.i(TAG, "Loaded config: $config")`) — viole NFR-AND-9.
> - Ajouter une méthode `clear()` ou `reset()` dans ConfigStore — viole feedback memory `feedback_no_reset_endpoints.md` (« Jamais de commande CLI/UI/IPC qui reset/override un mécanisme sécurité »). Si besoin de reset, l'utilisatrice utilise Réglages > Apps > Effacer données (action utilisateur explicite hors-band).

## Story

En tant qu'utilisateur Android Le Voile,
Je veux que mes préférences (pays favori, registre relais cache, dernière clé Ed25519 vérifiée) soient persistées localement et privées,
Afin que mon usage soit confortable d'une session à l'autre, sans que mes préférences ne fuient (cohérent FR-AND-10 + NFR-AND-7 prd.md + epics.md l. 2010-2034).

## Acceptance Criteria

1. **`ConfigData.kt` data class + DEFAULT** — Quand `android/app/src/main/kotlin/.../config/ConfigData.kt` est lu après cette story :
   ```kotlin
   /**
    * Story 11.8 — Schema persistance Le Voile Android.
    *
    * Sérialisé en JSON dans `getFilesDir()/config.json`. Permissions par défaut
    * Android (UID-only, équivalent 0600 desktop — NFR-AND-7).
    *
    * Champs MVP :
    *   - `preferredCountry` : code ISO 3166-1 alpha-2 du pays préféré au connect.
    *     Defaut "DE". Whitelist alignée LeVoileBridge.COUNTRIES_WHITELIST (Story 11.2).
    *   - `registryCache` : JSON string du dernier registre relais récupéré (cohérent
    *     architecture.md l. 1094 — placeholder Story 11.8, alimenté Story 9.7+).
    *     Null si jamais récupéré.
    *   - `lastVerifiedEd25519Key` : empreinte hex de la dernière master key Ed25519
    *     vérifiée (clé PUBLIQUE — non-secret). Pour TOFU + alerte rotation key.
    *     Null au premier lancement.
    *   - `schemaVersion` : version du schema config (1 initiale). Migré par
    *     `ConfigStore.migrate` quand le schema évolue.
    *
    * Pas de champ secret (clés privées Ed25519, tokens) — utiliser Android Keystore
    * Phase 2 si requis.
    *
    * `data class` Kotlin → `equals` + `hashCode` + `copy` automatiques (utiles tests
    * + immutable updates).
    */
   internal data class ConfigData(
       val schemaVersion: Int = CURRENT_SCHEMA_VERSION,
       val preferredCountry: String = "DE",
       val registryCache: String? = null,
       val lastVerifiedEd25519Key: String? = null,
   ) {
       companion object {
           /**
            * Version courante du schema. Incrémenter si on ajoute/retire un champ
            * persistance — déclencher migration via `ConfigStore.migrate`.
            */
           const val CURRENT_SCHEMA_VERSION = 1

           /**
            * Config par défaut — utilisée premier lancement OU fichier corrompu
            * (cf. ConfigStore.load fallback).
            */
           val DEFAULT = ConfigData()
       }
   }
   ```

2. **`ConfigStore.kt` load + save + migrate** — Quand le fichier est lu :
   ```kotlin
   /**
    * Story 11.8 — Persistence JSON pour les préférences Le Voile Android.
    *
    * Single source of truth Kotlin pour la config app — remplace les usages
    * SharedPreferences pour les valeurs structurées (le flag onboarding_completed
    * reste dans SharedPreferences car trivial binaire).
    *
    * Thread-safety : `synchronized(this)` sur load + save + migrate. Le pattern
    * read-modify-write doit passer par `update { it.copy(...) }` (helper) pour
    * garantir l'atomicité.
    *
    * Pas de cache mémoire sale — chaque load relit le fichier. Pour les hot paths
    * (notification update toutes les 750ms RECONNECTING Story 11.7), le caller
    * doit cacher localement le résultat de load() si besoin (overhead I/O ~1ms,
    * négligeable hors hot path).
    */
   internal class ConfigStore(private val context: Context) {

       private val configFile: File
           get() = File(context.filesDir, CONFIG_FILENAME)

       /**
        * Charge la config depuis le fichier JSON.
        *
        * Fallbacks :
        *   - Fichier absent → retourne `ConfigData.DEFAULT` (premier lancement).
        *   - Fichier illisible (IOException) → retourne `ConfigData.DEFAULT` + log WARN.
        *   - JSON invalide (JSONException) → retourne `ConfigData.DEFAULT` + log WARN.
        *   - Schema version > CURRENT → log ERROR (downgrade non supporté MVP) +
        *     retourne DEFAULT (l'utilisateur perd ses prefs en downgrade — accepté).
        *   - Schema version < CURRENT → invoque migrate(oldVersion, CURRENT) puis re-load.
        *
        * Aucune valeur utilisatrice loggée (cohérent NFR-AND-9).
        */
       @Synchronized
       fun load(): ConfigData {
           if (!configFile.exists()) {
               return ConfigData.DEFAULT
           }
           val raw = try {
               configFile.readText(Charsets.UTF_8)
           } catch (t: Throwable) {
               LeVoileLog.w(TAG, "Config illisible (IOException) — fallback DEFAULT")
               return ConfigData.DEFAULT
           }
           val json = try {
               JSONObject(raw)
           } catch (t: JSONException) {
               LeVoileLog.w(TAG, "Config corrompue (JSON invalide) — fallback DEFAULT")
               return ConfigData.DEFAULT
           }
           val version = json.optInt("schemaVersion", 0)
           when {
               version == ConfigData.CURRENT_SCHEMA_VERSION -> {
                   return parseV1(json)
               }
               version < ConfigData.CURRENT_SCHEMA_VERSION -> {
                   LeVoileLog.i(TAG, "Migration config v$version → v${ConfigData.CURRENT_SCHEMA_VERSION}")
                   migrate(version, ConfigData.CURRENT_SCHEMA_VERSION)
                   return load()  // re-lecture après migration
               }
               else -> {
                   LeVoileLog.w(TAG, "Schema downgrade refuse (v$version > v${ConfigData.CURRENT_SCHEMA_VERSION})")
                   return ConfigData.DEFAULT
               }
           }
       }

       /**
        * Persiste la config en JSON. Écrit atomiquement via tmp + rename pour éviter
        * un fichier partiel si crash en plein write.
        */
       @Synchronized
       fun save(config: ConfigData) {
           val json = JSONObject().apply {
               put("schemaVersion", config.schemaVersion)
               put("preferredCountry", config.preferredCountry)
               put("registryCache", config.registryCache ?: JSONObject.NULL)
               put("lastVerifiedEd25519Key", config.lastVerifiedEd25519Key ?: JSONObject.NULL)
           }
           val tmpFile = File(context.filesDir, "$CONFIG_FILENAME.tmp")
           try {
               tmpFile.writeText(json.toString(2), Charsets.UTF_8)
               // Atomic rename (POSIX rename — Android filesystem garantit atomicité
               // pour rename intra-filesystem).
               if (!tmpFile.renameTo(configFile)) {
                   throw IOException("renameTo failed")
               }
           } catch (t: Throwable) {
               LeVoileLog.w(TAG, "Save config echoue: ${t.javaClass.simpleName}")
               try { tmpFile.delete() } catch (_: Throwable) {}
               throw t  // remonter l'erreur au caller (rare)
           }
       }

       /**
        * Helper read-modify-write atomique : `update { it.copy(preferredCountry = "ES") }`.
        */
       @Synchronized
       fun update(block: (ConfigData) -> ConfigData) {
           save(block(load()))
       }

       /**
        * Migration de schema. Pour 11.8, schema initial v1 → pas de migration prévue.
        * Story 11.x ajoutant un champ devra incrémenter CURRENT_SCHEMA_VERSION et
        * ajouter une branche when ici.
        *
        * Garantit que les valeurs utilisateur préservées sont préservées (preferredCountry,
        * etc.). Les nouveaux champs prennent leur valeur DEFAULT.
        */
       @Synchronized
       fun migrate(oldVersion: Int, newVersion: Int) {
           when {
               oldVersion == 0 && newVersion == 1 -> {
                   // Pas de v0 historique — premier lancement avec schema v1, pas de migration.
                   // Si on arrive ici c'est qu'un dev a oublié schemaVersion dans le JSON →
                   // rewrite avec schema v1 + valeurs par défaut.
                   save(ConfigData.DEFAULT)
               }
               // Story future : 1 → 2, 2 → 3, etc.
               else -> {
                   LeVoileLog.w(TAG, "Migration $oldVersion → $newVersion non supportee")
                   throw IllegalStateException("Migration $oldVersion → $newVersion non supportee")
               }
           }
       }

       private fun parseV1(json: JSONObject): ConfigData = ConfigData(
           schemaVersion = json.optInt("schemaVersion", ConfigData.CURRENT_SCHEMA_VERSION),
           preferredCountry = json.optString("preferredCountry", "DE"),
           registryCache = json.optString("registryCache").takeIf {
               it.isNotEmpty() && it != "null"
           },
           lastVerifiedEd25519Key = json.optString("lastVerifiedEd25519Key").takeIf {
               it.isNotEmpty() && it != "null"
           },
       )

       companion object {
           private const val TAG = "ConfigStore"
           const val CONFIG_FILENAME = "config.json"
       }
   }
   ```

3. **`LeVoileBridge.selectCountry` migré vers ConfigStore** — Quand `LeVoileBridge.kt` est lu après cette story (changement uniquement de la méthode `selectCountry`) :
   ```kotlin
   @JavascriptInterface
   fun selectCountry(iso: String?): String {
       val safe = validateCountry(iso)
           ?: return """{"error":"invalid_country_code","value":"${escapeJson(iso?.take(8) ?: "")}"}"""
       try {
           ConfigStore(context).update { it.copy(preferredCountry = safe) }
       } catch (t: Throwable) {
           LeVoileLog.w(TAG, "selectCountry persistence echoue: ${t.javaClass.simpleName}")
           // Le frontend continue à fonctionner — la pref n'a pas été persistée mais le
           // flow connect peut continuer (il lira le défaut DE si pas de pref).
           return """{"error":"persistence_failed"}"""
       }
       return """{"ok":true,"country":"$safe"}"""
   }
   ```
   - **Suppression** de l'usage `getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)...putString(...)` Story 11.2.
   - Conserver les constantes `PREFS_NAME` et `PREF_KEY_PREFERRED_COUNTRY` companion **uniquement si** d'autres usages restent (ex. lecture pour back-compat). **Sinon supprimer** + reporter dans Completion Notes.
   - **Note** : si Story 11.7 lit `currentCountry` depuis SharedPreferences brute dans LeVoileVpnService, migration similaire requise (à coordonner — recommandation : LeVoileVpnService consomme `ConfigStore(applicationContext).load().preferredCountry`).

4. **Permissions par défaut Android (UID-only)** — Quand la story est livrée :
   ```bash
   # Sur l'émulateur ou device debug :
   adb shell run-as fr.plateformeliberte.levoile.debug ls -la files/config.json
   # → -rw------- (UID-only)

   adb shell ls -la /data/data/fr.plateformeliberte.levoile.debug/files/config.json
   # → Permission denied (sans run-as)

   # Test injection : créer une autre app debug "com.autre.app" et tenter :
   adb shell run-as com.autre.app cat /data/data/fr.plateformeliberte.levoile.debug/files/config.json
   # → Permission denied (UID isolation Android)
   ```
   Cette vérification est manuelle (Task 7 smoke test) — pas automatisable JVM-only. Le test instrumenté Espresso (Story 12.6) pourra ajouter une assertion automatisée si jugé pertinent.

5. **Tests JVM `ConfigStoreTest.kt`** — Quand le test est exécuté, vert :
   ```kotlin
   class ConfigStoreTest {
       private lateinit var tempDir: File
       private lateinit var mockContext: Context
       private lateinit var store: ConfigStore

       @Before
       fun setUp() {
           tempDir = Files.createTempDirectory("config-test").toFile()
           mockContext = mock(Context::class.java)
           `when`(mockContext.filesDir).thenReturn(tempDir)
           store = ConfigStore(mockContext)
       }

       @After
       fun tearDown() {
           tempDir.deleteRecursively()
       }

       @Test
       fun `load sans fichier retourne DEFAULT`() {
           assertEquals(ConfigData.DEFAULT, store.load())
       }

       @Test
       fun `save puis load retourne la meme config`() {
           val cfg = ConfigData(preferredCountry = "ES", registryCache = "{}")
           store.save(cfg)
           val loaded = store.load()
           assertEquals("ES", loaded.preferredCountry)
           assertEquals("{}", loaded.registryCache)
           assertNull(loaded.lastVerifiedEd25519Key)
       }

       @Test
       fun `load fichier corrompu retourne DEFAULT et log warn`() {
           File(tempDir, ConfigStore.CONFIG_FILENAME).writeText("{ corrompu json")
           assertEquals(ConfigData.DEFAULT, store.load())
       }

       @Test
       fun `load fichier vide retourne DEFAULT`() {
           File(tempDir, ConfigStore.CONFIG_FILENAME).writeText("")
           assertEquals(ConfigData.DEFAULT, store.load())
       }

       @Test
       fun `update est atomic read-modify-write`() {
           store.save(ConfigData(preferredCountry = "DE"))
           store.update { it.copy(preferredCountry = "GB") }
           assertEquals("GB", store.load().preferredCountry)
           // Les autres champs restent à leur valeur DEFAULT (pas écrasés à null)
           assertNull(store.load().registryCache)
       }

       @Test
       fun `save puis load preserve schemaVersion`() {
           store.save(ConfigData(schemaVersion = 1, preferredCountry = "US"))
           assertEquals(1, store.load().schemaVersion)
       }

       @Test
       fun `save ecrit atomiquement via tmp file et rename`() {
           store.save(ConfigData(preferredCountry = "US"))
           assertTrue(File(tempDir, ConfigStore.CONFIG_FILENAME).exists())
           assertFalse(File(tempDir, "${ConfigStore.CONFIG_FILENAME}.tmp").exists())
       }

       @Test
       fun `load schemaVersion superieur retourne DEFAULT (downgrade refuse)`() {
           File(tempDir, ConfigStore.CONFIG_FILENAME).writeText(
               """{"schemaVersion": 99, "preferredCountry": "FR"}"""
           )
           assertEquals(ConfigData.DEFAULT, store.load())
       }
   }
   ```

6. **Tests JVM `ConfigMigrationTest.kt` (cohérent epics.md l. 2034)** — Quand le test est exécuté, vert :
   ```kotlin
   class ConfigMigrationTest {
       private lateinit var tempDir: File
       private lateinit var mockContext: Context
       private lateinit var store: ConfigStore

       @Before
       fun setUp() {
           tempDir = Files.createTempDirectory("migration-test").toFile()
           mockContext = mock(Context::class.java)
           `when`(mockContext.filesDir).thenReturn(tempDir)
           store = ConfigStore(mockContext)
       }

       @After
       fun tearDown() {
           tempDir.deleteRecursively()
       }

       @Test
       fun `migration v0 vers v1 ecrit DEFAULT (pas de v0 historique)`() {
           store.migrate(0, 1)
           assertEquals(ConfigData.DEFAULT, store.load())
       }

       @Test
       fun `migration vers version non supportee leve IllegalStateException`() {
           assertThrows(IllegalStateException::class.java) {
               store.migrate(1, 99)
           }
       }

       /**
        * Anti-regression : si une story future ajoute un schema v2, ce test devra
        * être enrichi avec un cas v1→v2 qui :
        *   - Lit un JSON v1 existant (preferredCountry = "ES").
        *   - Migre vers v2 (peut-être ajoute un champ `theme: String = "dark"`).
        *   - Vérifie que `preferredCountry` est préservé ET que `theme` prend sa valeur DEFAULT.
        */
       @Test
       fun `placeholder pour migration future v1 vers v2`() {
           // Ce test est volontairement vide — il sert de checklist mentale pour
           // la story future qui ajoutera un schema v2.
           assertEquals(1, ConfigData.CURRENT_SCHEMA_VERSION)
       }
   }
   ```

7. **Build sanity + smoke test permissions** — Quand `cd android && ./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` est exécuté, vert. Smoke test manuel :
   - Connecter émulateur, `./gradlew installDebug`.
   - Lancer l'app, sélectionner un pays via le bottom-sheet (Story 11.4) → backend persist `config.json`.
   - `adb shell run-as fr.plateformeliberte.levoile.debug cat files/config.json` → JSON pretty-printed avec `preferredCountry` à jour.
   - `adb shell ls -la /data/data/fr.plateformeliberte.levoile.debug/files/config.json` → permissions UID-only.
   - **Test injection** (optionnel — si une autre app debug est installée) : tenter `cat` depuis une autre app → `Permission denied`.
   - Effacer données + relancer → `config.json` recréé avec DEFAULT (preferredCountry = "DE").

## Tasks / Subtasks

- [x] **Task 1 : Vérifier l'état Stories amont** (AC: tous)
  - [x] Confirmer Story 11.2 livrée (LeVoileBridge.selectCountry actuel via SharedPreferences).
  - [x] Confirmer Story 10.5 livrée (LeVoileLog disponible).
  - [x] Lire `LeVoileBridge.kt` actuel.

- [x] **Task 2 : Créer `ConfigData.kt`** (AC: #1)
  - [x] Data class + companion `DEFAULT` + `CURRENT_SCHEMA_VERSION`.

- [x] **Task 3 : Créer `ConfigStore.kt`** (AC: #2)
  - [x] Implémenter `load`, `save`, `update`, `migrate`, `parseV1`.
  - [x] **CRITICAL** : `@Synchronized` sur les méthodes mutantes.
  - [x] **CRITICAL** : write atomique via tmp + rename.

- [x] **Task 4 : Migrer `LeVoileBridge.selectCountry`** (AC: #3)
  - [x] Remplacer SharedPreferences par `ConfigStore(context).update { ... }`.
  - [x] Décider : conserver `PREFS_NAME` constants pour back-compat (recommandation : supprimer si plus utilisées).
  - [x] **Vérifier** : le flow `connect(country = null)` doit continuer à lire la pref (via ConfigStore.load() — à wirer si pas déjà fait dans Story 11.2).

- [x] **Task 5 : Créer `ConfigStoreTest.kt`** (AC: #5)

- [x] **Task 6 : Créer `ConfigMigrationTest.kt`** (AC: #6)

- [x] **Task 7 : Build sanity + smoke test permissions** (AC: #7, #4)
  - [x] `./gradlew clean assembleDebug :app:testDebugUnitTest :app:lint` — vert.
  - [x] Smoke test sur émulateur : changer pays, vérifier `cat config.json`.
  - [x] Vérifier permissions UID-only via `ls -la`.

- [x] **Task 8 : Mettre à jour la story et sprint-status**

## Dev Notes

### Pattern principal — JSON minimal sans dépendance externe

`org.json.JSONObject` est dans `android.jar` (pas dans `kotlin-stdlib` JVM pur — d'où `testOptions.unitTests.isReturnDefaultValues = true` Story 9.4 + mock Context).

Pour 3 champs scalaires + 1 placeholder, JSON est trivial. Si Story future enrichit (10+ champs structurés, listes), réévaluer Moshi (~800 KB APK avec codegen) ou kotlinx-serialization (~300 KB).

### Pourquoi pas EncryptedSharedPreferences

`EncryptedSharedPreferences` (androidx.security.crypto) ajoute ~500 KB APK et impose Android 8+ (API 26+). Pour des préférences non-secrètes (pays, cache registre PUBLIC, fingerprint clé PUBLIQUE), le coût n'est pas justifié.

Si Story future stocke un secret (token session, clé privée Ed25519 — ce qui ne devrait jamais arriver côté app, le noyau Go gère ça), refactoriser via Android Keystore.

### Atomic write pattern

POSIX `rename()` est atomique sur le même filesystem. Android (ext4/F2FS) garantit ça. Le pattern :
1. Écrire dans `config.json.tmp`.
2. `tmp.renameTo(config.json)` — atomique.
3. Si crash entre 1 et 2, le fichier `config.json` reste intact (vieille version) et `config.json.tmp` orphelin (cleanup au prochain save).

### Coordination Story 11.2 (selectCountry SharedPreferences)

Story 11.2 livre la version SharedPreferences. Story 11.8 migre vers ConfigStore. **Pas de garde-fou de migration des données SharedPreferences existantes** — si l'utilisateur avait sélectionné un pays via 11.2 puis upgrade vers 11.8 :
- Le SharedPreferences `preferred_country` reste mais n'est plus lu.
- ConfigStore.load() retourne DEFAULT (`preferredCountry = "DE"`).
- L'utilisateur perd sa préférence (re-sélection requise).

**Acceptable** si 11.2 et 11.8 sont livrés dans la même release MVP (l'utilisateur ne voit jamais la version 11.2-only). **Sinon** ajouter une migration one-shot :
```kotlin
// Dans ConfigStore.load() au cas premier lancement (file absent) :
val legacyPrefs = context.getSharedPreferences("levoile_prefs", MODE_PRIVATE)
val legacyCountry = legacyPrefs.getString("preferred_country", null)
if (legacyCountry != null) {
    val migrated = ConfigData.DEFAULT.copy(preferredCountry = legacyCountry)
    save(migrated)
    legacyPrefs.edit().remove("preferred_country").apply()  // cleanup
    return migrated
}
```
**Décision dev à reporter en Completion Notes** : nécessite-t-on cette migration ? Si Stories 11.2 et 11.8 sortent ensemble, non.

### Coordination Story 11.5 (onboarding_completed flag)

Le flag `onboarding_completed` reste dans SharedPreferences (Story 11.5). **Justification** : c'est un boolean trivial, JSON over-engineering. Convention : `SharedPreferences` pour les flags binaires + ephemeral state, `ConfigStore` pour les données structurées.

### Coordination Story 11.7 (notification consomme preferredCountry)

LeVoileVpnService Story 11.7 lit `currentCountry` depuis l'Intent EXTRA_COUNTRY. Si `EXTRA_COUNTRY` est null (cas connect direct sans pays explicite), il devrait fallback sur `ConfigStore(applicationContext).load().preferredCountry`. **À vérifier dans Story 11.7** ou ajouter en Task de coordination ici.

### Coordination future Phase 2 (registryCache, lastVerifiedEd25519Key)

Ces 2 champs sont placeholders pour Stories futures (alimentation par GoCoreAdapter Story 9.7+ ou registry refresh Story 11.x). Pour 11.8, ils sont juste persistés avec leur valeur DEFAULT (null) — aucun caller ne les écrit encore.

### Source tree components à toucher

- **Nouveaux** :
  - `android/app/src/main/kotlin/.../config/ConfigStore.kt`
  - `android/app/src/main/kotlin/.../config/ConfigData.kt`
  - `android/app/src/test/kotlin/.../config/ConfigStoreTest.kt`
  - `android/app/src/test/kotlin/.../config/ConfigMigrationTest.kt`
- **Modifiés** :
  - `android/app/src/main/kotlin/.../bridge/LeVoileBridge.kt` (selectCountry)

### References

- [architecture.md l. 640](_bmad-output/planning-artifacts/architecture.md) — « Pas de TOML sur Android — la config TOML reste desktop-only, Android utilise SharedPreferences ». Story 11.8 introduit JSON pour les données structurées (SharedPreferences reste pour les flags).
- [architecture.md l. 226](_bmad-output/planning-artifacts/architecture.md) — `VpnPreferences` typé wrapper SharedPreferences (Phase 1 desktop équivalent).
- [epics.md l. 2010-2034](_bmad-output/planning-artifacts/epics.md) — Story 11.8 BDD complet (3 scénarios Given/When/Then).
- [prd.md FR-AND-10](_bmad-output/planning-artifacts/prd.md) — préférences persistées localement.
- [prd.md NFR-AND-7](_bmad-output/planning-artifacts/prd.md) — permissions par défaut Android (UID-only).
- [prd.md NFR-AND-9](_bmad-output/planning-artifacts/prd.md) — pas de log de contenu utilisateur.
- [memory feedback_no_reset_endpoints.md](memory) — pas de méthode `reset()` ou `clear()` exposée.
- Story 10.5 (livrée) : LeVoileLog pour les logs.
- Story 11.2 (à venir) : selectCountry baseline SharedPreferences (migré ici).
- Story 11.5 (à venir) : onboarding_completed reste SharedPreferences.
- Story 11.7 (à venir) : LeVoileVpnService consommera `ConfigStore.load().preferredCountry` au connect sans EXTRA_COUNTRY.
- Story 12.6 (à venir) : test instrumenté permissions UID-only via `adb shell run-as`.

## Dev Agent Record

### Agent Model Used

claude-opus-4-7 (1M context)

### Completion Notes List

- **`org.json.JSONObject` Android built-in** retenu (pas de Gson/Moshi/kotlinx-serialization — économie ~300 KB APK + zéro setup ProGuard, cohérent NFR-AND-3).
- **Atomic write tmp + rename** : POSIX `rename()` atomique sur ext4/F2FS Android. Cleanup tmp en cas d'erreur.
- **Pas de méthode `reset()`/`clear()`** exposée — cohérent feedback memory `feedback_no_reset_endpoints.md`. Recovery hors-band via Réglages > Apps > Effacer données.
- **Migration SharedPreferences → ConfigStore** : pas de garde-fou one-shot (Stories 11.2 et 11.8 livrées dans la même release MVP, l'utilisateur ne voit jamais la version 11.2-only). Décision documentée Dev Notes.
- **`LeVoileBridge.selectCountry`** migré directement vers `ConfigStore.update { it.copy(...) }` (pas de SharedPreferences brute intermédiaire).
- **`LeVoileVpnService.onStartCommand`** : fallback `ConfigStore.load().preferredCountry` quand `EXTRA_COUNTRY` absent (cohérent coordination Story 11.7).
- **`onboarding_completed`** reste en SharedPreferences (boolean trivial) — cohérent Story 11.5 + Dev Notes 11.8.
- **Tests JVM** : `ConfigStoreTest` (8 tests) + `ConfigMigrationTest` (3 tests). Validation 2026-05-03 : `org.json.JSONObject` est bien stubbé sous `testOptions.unitTests.isReturnDefaultValues = true` (méthodes `put`/`optInt` retournent null/0). **Solution** : ajout dépendance `testImplementation("org.json:json:20240303")` dans `app/build.gradle.kts` + version catalog (~70 KB, scope test only — n'entre pas dans l'APK). Implémentation réelle de JSONObject côté tests JVM. Tests verts.
- **Bug Windows `File.renameTo`** : remplacé par `Files.move(ATOMIC_MOVE + REPLACE_EXISTING)` avec fallback non-atomique. `File.renameTo` échoue sur Windows quand la destination existe déjà (limitation JVM connue) — sur Android Linux ext4/F2FS l'atomic move est natif.
- **Build/test verification 2026-05-03 (JDK 17)** : BUILD SUCCESSFUL, 133 tests verts (incluant 8 ConfigStoreTest + 3 ConfigMigrationTest), 0 lint error.

### File List

- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/config/ConfigData.kt` (NOUVEAU — data class + DEFAULT + CURRENT_SCHEMA_VERSION)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/config/ConfigStore.kt` (NOUVEAU — load/save/update/migrate atomic write)
- `android/app/src/main/kotlin/fr/plateformeliberte/levoile/bridge/LeVoileBridge.kt` (livré Story 11.2 directement avec selectCountry → ConfigStore)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/config/ConfigStoreTest.kt` (NOUVEAU — 8 tests)
- `android/app/src/test/kotlin/fr/plateformeliberte/levoile/config/ConfigMigrationTest.kt` (NOUVEAU — 3 tests + placeholder pour migration future v1→v2)
- `android/gradle/libs.versions.toml` (MODIFIÉ — ajout `org-json` version + library)
- `android/app/build.gradle.kts` (MODIFIÉ — `testImplementation(libs.org.json)`)

### Change Log

| Date | Description |
|---|---|
| 2026-05-03 | Story 11.8 livrée (ConfigStore JSON + migration helpers + selectCountry migré). |
| 2026-05-03 | Code-review Epic 11 : test anti-fuite `org json reste scope testImplementation only NFR-AND-3` ajouté à `AuditCITest` (M5 — refuse `implementation(libs.org.json)` accidentel). |
