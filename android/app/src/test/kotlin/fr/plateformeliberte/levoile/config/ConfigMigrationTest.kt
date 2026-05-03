package fr.plateformeliberte.levoile.config

import android.content.Context
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Before
import org.junit.Test
import org.mockito.Mockito.mock
import org.mockito.Mockito.`when`
import java.io.File
import java.nio.file.Files

/**
 * Story 11.8 — tests des migrations de schema. Cohérent epics.md l. 2034.
 *
 * Aujourd'hui Le Voile Android n'a qu'un seul schema (v1). Ce fichier sert
 * de placeholder pour les futures migrations (v1 → v2, etc.) — chaque story
 * future qui ajoute un champ persistance doit enrichir ce fichier.
 */
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
     * Anti-régression : si une story future ajoute v2, elle devra enrichir
     * cette suite avec un cas v1→v2.
     */
    @Test
    fun `placeholder pour migration future v1 vers v2`() {
        assertEquals(1, ConfigData.CURRENT_SCHEMA_VERSION)
    }
}
