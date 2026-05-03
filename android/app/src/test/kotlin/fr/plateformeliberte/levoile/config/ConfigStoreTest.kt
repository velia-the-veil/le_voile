package fr.plateformeliberte.levoile.config

import android.content.Context
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.mockito.Mockito.mock
import org.mockito.Mockito.`when`
import java.io.File
import java.nio.file.Files

/**
 * Story 11.8 — tests JVM-only pour ConfigStore (utilise un tempDir).
 * org.json.JSONObject est dans android.jar — testé via le mock Context.filesDir.
 *
 * Note : les tests JVM-only nécessitent que le module Android utilise
 * testOptions.unitTests.isReturnDefaultValues = true OU que org.json soit
 * disponible côté tests JVM via une dépendance Maven (cf. Story 9.4 setup).
 * Si org.json n'est pas accessible JVM, ces tests passeront en instrumentés
 * Story 12.6.
 */
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
    fun `load fichier corrompu retourne DEFAULT`() {
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
