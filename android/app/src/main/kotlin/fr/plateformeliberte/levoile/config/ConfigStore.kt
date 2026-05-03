package fr.plateformeliberte.levoile.config

import android.content.Context
import fr.plateformeliberte.levoile.log.LeVoileLog
import org.json.JSONException
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.nio.file.Files
import java.nio.file.StandardCopyOption

/**
 * Story 11.8 — Persistence JSON pour les préférences Le Voile Android.
 *
 * Single source of truth Kotlin pour la config app. Le flag onboarding_completed
 * reste dans SharedPreferences car trivial binaire (cf. coordination Story 11.5).
 *
 * Thread-safety : @Synchronized sur load + save + migrate. Le pattern
 * read-modify-write doit passer par update { it.copy(...) }.
 *
 * Pas de cache mémoire — chaque load relit le fichier (overhead I/O ~1ms,
 * négligeable hors hot path).
 *
 * Pas de méthode reset()/clear() exposée (cohérent feedback memory
 * `feedback_no_reset_endpoints.md`) — recovery hors-band uniquement via
 * Réglages > Apps > Effacer données.
 */
internal class ConfigStore(private val context: Context) {

    private val configFile: File
        get() = File(context.filesDir, CONFIG_FILENAME)

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
        if (raw.isBlank()) {
            return ConfigData.DEFAULT
        }
        val json = try {
            JSONObject(raw)
        } catch (t: JSONException) {
            LeVoileLog.w(TAG, "Config corrompue (JSON invalide) — fallback DEFAULT")
            return ConfigData.DEFAULT
        }
        val version = json.optInt("schemaVersion", 0)
        return when {
            version == ConfigData.CURRENT_SCHEMA_VERSION -> parseV1(json)
            version < ConfigData.CURRENT_SCHEMA_VERSION -> {
                LeVoileLog.i(TAG, "Migration config v$version → v${ConfigData.CURRENT_SCHEMA_VERSION}")
                migrate(version, ConfigData.CURRENT_SCHEMA_VERSION)
                load()
            }
            else -> {
                LeVoileLog.w(TAG, "Schema downgrade refuse (v$version > v${ConfigData.CURRENT_SCHEMA_VERSION})")
                ConfigData.DEFAULT
            }
        }
    }

    /**
     * Persiste la config en JSON. Écrit atomiquement via tmp + rename.
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
            // Atomic move avec REPLACE_EXISTING. Préféré à File.renameTo qui
            // échoue sur Windows si la destination existe (limitation JVM
            // connue, mais Android Linux supporte rename(2) atomique nativement).
            // ATOMIC_MOVE garantit la sémantique POSIX rename(2) sur Android
            // ext4/F2FS ; fallback non-atomique sur Windows si nécessaire.
            try {
                Files.move(
                    tmpFile.toPath(),
                    configFile.toPath(),
                    StandardCopyOption.ATOMIC_MOVE,
                    StandardCopyOption.REPLACE_EXISTING,
                )
            } catch (t: Throwable) {
                // Fallback non-atomique pour les systèmes où ATOMIC_MOVE refuse
                // ATOMIC_MOVE+REPLACE (rare — surtout Windows hors NTFS).
                Files.move(
                    tmpFile.toPath(),
                    configFile.toPath(),
                    StandardCopyOption.REPLACE_EXISTING,
                )
            }
        } catch (t: Throwable) {
            LeVoileLog.w(TAG, "Save config echoue: ${t.javaClass.simpleName}")
            try { tmpFile.delete() } catch (_: Throwable) {}
            throw t
        }
    }

    /**
     * Helper read-modify-write atomique : update { it.copy(preferredCountry = "ES") }.
     */
    @Synchronized
    fun update(block: (ConfigData) -> ConfigData) {
        save(block(load()))
    }

    /**
     * Migration de schema. Story 11.8 schema initial v1 → pas de migration prévue.
     * Story future ajoutant un champ devra incrémenter CURRENT_SCHEMA_VERSION
     * + ajouter une branche when ici.
     */
    @Synchronized
    fun migrate(oldVersion: Int, newVersion: Int) {
        when {
            oldVersion == 0 && newVersion == 1 -> {
                // Pas de v0 historique — rewrite avec schema v1 + valeurs par défaut.
                save(ConfigData.DEFAULT)
            }
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
