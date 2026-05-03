package fr.plateformeliberte.levoile.registry

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

/**
 * Story 11.7-bis — tests JVM-only pour [RelayPicker].
 *
 * **Refactor post-code-review (M-9)** : le pick est désormais pure Kotlin
 * sur la liste pré-extraite `RegistryData.relays`. Les tests peuvent
 * maintenant valider le **comportement réel** (round-robin, fallback) sans
 * dépendre de libgojni.so.
 */
class RelayPickerTest {

    private fun makeData(relays: List<RelayPicker.RelayInfo>): RegistryLoader.RegistryData =
        RegistryLoader.RegistryData(
            jsonBytes = ByteArray(0),  // pas utilisé par RelayPicker post-refactor
            numRelays = relays.size,
            relays = relays,
        )

    private val deAndEs = listOf(
        RelayPicker.RelayInfo(iso = "DE", domain = "de-001.relay.dev", pinnedKeyB64 = "k1"),
        RelayPicker.RelayInfo(iso = "DE", domain = "de-002.relay.dev", pinnedKeyB64 = "k2"),
        RelayPicker.RelayInfo(iso = "ES", domain = "es-001.relay.dev", pinnedKeyB64 = "k3"),
    )

    @Test
    fun `pick avec country valide retourne le premier relais matching`() {
        val picker = RelayPicker(makeData(deAndEs))
        val r = picker.pick("DE")
        assertNotNull(r)
        assertEquals("DE", r!!.iso)
        assertEquals("de-001.relay.dev", r.domain)
        assertEquals("k1", r.pinnedKeyB64)
    }

    @Test
    fun `pick fait round-robin intra-pays`() {
        val picker = RelayPicker(makeData(deAndEs))
        val r1 = picker.pick("DE")
        val r2 = picker.pick("DE")
        val r3 = picker.pick("DE")
        assertEquals("de-001.relay.dev", r1?.domain)
        assertEquals("de-002.relay.dev", r2?.domain)
        // Wrap-around après 2 entrées DE.
        assertEquals("de-001.relay.dev", r3?.domain)
    }

    @Test
    fun `pick par pays a son propre counter`() {
        val picker = RelayPicker(makeData(deAndEs))
        // 3 picks DE → counter DE = 3.
        repeat(3) { picker.pick("DE") }
        // 1 pick ES → counter ES = 1, premier relais ES.
        val es1 = picker.pick("ES")
        assertEquals("es-001.relay.dev", es1?.domain)
    }

    @Test
    fun `pick avec country inexistant retourne null`() {
        val picker = RelayPicker(makeData(deAndEs))
        assertNull("pays hors registry → null", picker.pick("FR"))
        assertNull("pays inconnu → null", picker.pick("XX"))
    }

    @Test
    fun `pick avec country vide retourne null`() {
        val picker = RelayPicker(makeData(deAndEs))
        assertNull("pick avec iso vide doit retourner null", picker.pick(""))
        assertNull("pick avec iso whitespace doit retourner null", picker.pick("   "))
    }

    @Test
    fun `pick est case-insensitive sur iso`() {
        val picker = RelayPicker(makeData(deAndEs))
        val rUpper = picker.pick("DE")
        val rLower = picker.pick("de")
        // Les 2 picks partagent le même counter (case-insensitive matching).
        assertNotNull(rUpper)
        assertNotNull(rLower)
    }

    @Test
    fun `resetCounter remet le round-robin a zero pour le pays`() {
        val picker = RelayPicker(makeData(deAndEs))
        picker.pick("DE")  // counter = 1
        picker.pick("DE")  // counter = 2
        picker.resetCounter("DE")
        val r = picker.pick("DE")
        assertEquals("Apres reset, premier pick doit retourner premier relais",
            "de-001.relay.dev", r?.domain)
    }

    @Test
    fun `RelayInfo data class equals fonctionne`() {
        val r1 = RelayPicker.RelayInfo(iso = "DE", domain = "de.example", pinnedKeyB64 = "abc")
        val r2 = RelayPicker.RelayInfo(iso = "DE", domain = "de.example", pinnedKeyB64 = "abc")
        val r3 = RelayPicker.RelayInfo(iso = "ES", domain = "de.example", pinnedKeyB64 = "abc")
        assertEquals(r1, r2)
        assertNotEquals(r1, r3)
    }

    @Test
    fun `pick sur registry vide retourne null`() {
        val picker = RelayPicker(makeData(emptyList()))
        assertNull(picker.pick("DE"))
    }

    @Test
    fun `pick stable sous appels concurrents simulés sans crash`() {
        // Smoke test thread-safety : ConcurrentHashMap + AtomicInteger doivent
        // survivre à des appels concurrents. Pas une vérification stricte du
        // round-robin (ordre non déterministe), juste l'absence de crash +
        // résultat valide.
        val picker = RelayPicker(makeData(deAndEs))
        val threads = (1..8).map {
            Thread {
                repeat(100) {
                    val r = picker.pick("DE")
                    assertNotNull("pick concurrent ne doit pas retourner null", r)
                    assertSame("DE", r!!.iso)
                }
            }
        }
        threads.forEach { it.start() }
        threads.forEach { it.join() }
    }
}
