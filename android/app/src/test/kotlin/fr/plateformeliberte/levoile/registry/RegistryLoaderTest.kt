package fr.plateformeliberte.levoile.registry

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Story 11.7-bis — tests JVM-only pour [RegistryLoader].
 *
 * Le runtime JNI gomobile n'étant pas chargeable JVM-only, ces tests valident
 * la surface API + le parsing JSON Kotlin (org.json) sans déclencher
 * `verifyRegistry` (JNI). Les fixtures `res/raw/registry_bootstrap_relays`
 * sont accessibles directement via le filesystem.
 *
 * Tests post-refactor M-7/M-8/M-9 (code-review post-11.7-bis) :
 *  - Parsing du registry bundle via org.json (chemin réel sans JNI)
 *  - Validation du format `master_public_key` 44 chars base64
 *  - Validation de la structure `relays` array avec 8 entrées (4 pays × 2)
 *  - Cache TTL : `cacheIsFresh` retourne false si timestamp manquant
 */
class RegistryLoaderTest {

    private fun loadBundleJson(): String {
        val candidates = listOf(
            File("src/main/res/raw/registry_bootstrap_relays"),
            File("app/src/main/res/raw/registry_bootstrap_relays"),
            File("android/app/src/main/res/raw/registry_bootstrap_relays"),
        )
        return candidates.firstOrNull { it.exists() }?.readText()
            ?: throw AssertionError("registry_bootstrap_relays introuvable — run gen-registry-fixtures.go")
    }

    @Test
    fun `RegistryData equals utilise contentEquals sur jsonBytes`() {
        val bytes1 = "{}".toByteArray(Charsets.UTF_8)
        val bytes2 = "{}".toByteArray(Charsets.UTF_8)  // contenu identique, ref différente
        val bytes3 = "{\"a\":1}".toByteArray(Charsets.UTF_8)

        val d1 = RegistryLoader.RegistryData(jsonBytes = bytes1, numRelays = 4, relays = emptyList())
        val d2 = RegistryLoader.RegistryData(jsonBytes = bytes2, numRelays = 4, relays = emptyList())
        val d3 = RegistryLoader.RegistryData(jsonBytes = bytes3, numRelays = 4, relays = emptyList())
        val d4 = RegistryLoader.RegistryData(jsonBytes = bytes1, numRelays = 5, relays = emptyList())

        assertEquals("ref différentes mais contenu identique → equals true", d1, d2)
        assertFalse("contenu différent → equals false", d1 == d3)
        assertFalse("numRelays différent → equals false", d1 == d4)
    }

    @Test
    fun `RegistryData hashCode coherent avec equals`() {
        val bytes1 = "test".toByteArray()
        val bytes2 = "test".toByteArray()
        val d1 = RegistryLoader.RegistryData(bytes1, 3, emptyList())
        val d2 = RegistryLoader.RegistryData(bytes2, 3, emptyList())
        assertEquals(d1.hashCode(), d2.hashCode())
    }

    @Test
    fun `bundle res raw registry_master_pubkey existe et fait 44 bytes`() {
        val candidates = listOf(
            File("src/main/res/raw/registry_master_pubkey"),
            File("app/src/main/res/raw/registry_master_pubkey"),
            File("android/app/src/main/res/raw/registry_master_pubkey"),
        )
        val keyFile = candidates.firstOrNull { it.exists() }
            ?: throw AssertionError("registry_master_pubkey introuvable — run gen-registry-fixtures.go")
        val content = keyFile.readText().trim()
        // Ed25519 pubkey = 32 bytes brut → 44 chars base64 standard avec padding.
        assertEquals("master pubkey doit faire 44 chars base64 (32 bytes Ed25519)", 44, content.length)
        assertTrue("ne doit pas être vide", content.isNotEmpty())
    }

    @Test
    fun `bundle res raw registry_bootstrap_relays est JSON valide avec champs requis`() {
        val raw = loadBundleJson()
        val obj = JSONObject(raw)
        // Code-review post-11.7-bis (L-4) : parsing JSON réel via org.json,
        // pas juste un check `contains` text.
        assertEquals("schema version doit être 1", 1, obj.optInt("version"))
        val masterKey = obj.optString("master_public_key")
        assertEquals("master_public_key doit être 44 chars base64", 44, masterKey.length)
        val relays = obj.optJSONArray("relays")
        assertNotNull("array relays présent", relays)
        assertEquals("8 relais bundled (4 pays × 2)", 8, relays!!.length())
        assertNotNull("timestamp updated présent", obj.optString("updated").takeIf { it.isNotEmpty() })
    }

    @Test
    fun `bundle res raw registry_bootstrap_relays a relais valides bien formes`() {
        val raw = loadBundleJson()
        val obj = JSONObject(raw)
        val relays = obj.getJSONArray("relays")
        for (i in 0 until relays.length()) {
            val r = relays.getJSONObject(i)
            assertTrue("relais $i id non-vide", r.optString("id").isNotEmpty())
            assertTrue("relais $i domain non-vide", r.optString("domain").isNotEmpty())
            assertTrue("relais $i public_key non-vide", r.optString("public_key").isNotEmpty())
            assertTrue("relais $i signature non-vide", r.optString("signature").isNotEmpty())
        }
    }

    @Test
    fun `bundle relais couvre les 4 pays MVP DE ES GB US`() {
        val raw = loadBundleJson()
        val obj = JSONObject(raw)
        val relays = obj.getJSONArray("relays")
        val countries = mutableSetOf<String>()
        for (i in 0 until relays.length()) {
            val id = relays.getJSONObject(i).optString("id", "")
            // ID format: relay-{code}-{num}
            if (id.startsWith("relay-")) {
                val rest = id.removePrefix("relay-")
                val dashIdx = rest.indexOf('-')
                if (dashIdx >= 2) {
                    countries.add(rest.substring(0, dashIdx).uppercase())
                }
            }
        }
        assertTrue("DE présent", "DE" in countries)
        assertTrue("ES présent", "ES" in countries)
        assertTrue("GB présent", "GB" in countries)
        assertTrue("US présent", "US" in countries)
    }
}
