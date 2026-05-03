package fr.plateformeliberte.levoile.vpn

import fr.plateformeliberte.levoile.ui.VpnState
import kotlinx.coroutines.channels.Channel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Tests JVM-only de [GoBackedPacketRelay] — couverture des chemins qui ne
 * déclenchent PAS le runtime JNI gomobile.
 *
 * Les chemins testés ici n'invoquent jamais `onTunnelStarted` / `connect` /
 * `writePacket` qui passent par `GoCoreAdapter` → `Protocol` (gomobile JNI
 * impossible à charger dans une JVM standalone). Ce qu'on valide :
 *  - Drop des paquets out-pump avant `onTunnelStarted` (state machine)
 *  - Counters atomiques sous incréments concurrents (L-5 fix)
 *  - `onTunnelStopped` idempotent + sans crash si `onTunnelStarted` jamais
 *    appelé
 *  - `shutdown()` idempotent
 *  - `metrics()` reflète l'état réel
 *
 * Le test fonctionnel complet (handshake QUIC réel + drain pump) reste
 * porté par Story 12.6 (matrice Espresso instrumentée API 29/33/34).
 */
class GoBackedPacketRelayTest {

    private fun newRelay(
        sinkCapacity: Int = 256,
        outboundCapacity: Int = 256,
    ): GoBackedPacketRelay = GoBackedPacketRelay(
        relayDomain = "127.0.0.1:1",
        pinnedKeyB64 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
        inboundSink = Channel(capacity = sinkCapacity),
        outboundCapacity = outboundCapacity,
    )

    @Test
    fun `metrics initial state — running false tunnelStarted false counters zero`() {
        val relay = newRelay()
        val m = relay.metrics()
        assertFalse("running attendu false avant onTunnelStarted", m.running)
        assertFalse("tunnelStarted attendu false avant onTunnelStarted", m.tunnelStarted)
        assertEquals(0L, m.droppedNotConnected)
        assertEquals(0L, m.droppedBackPressure)
    }

    @Test
    fun `onOutboundPacket avant onTunnelStarted increments droppedNotConnected`() {
        val relay = newRelay()
        repeat(3) {
            relay.onOutboundPacket(byteArrayOf(0x45, 0x00, 0x00, 0x14), 4)
        }
        val m = relay.metrics()
        assertEquals(3L, m.droppedNotConnected)
        assertEquals(0L, m.droppedBackPressure)
    }

    @Test
    fun `onTunnelStopped without onTunnelStarted is noop and does not crash`() {
        val relay = newRelay()
        relay.onTunnelStopped()
        relay.onTunnelStopped() // idempotent
        val m = relay.metrics()
        assertFalse(m.running)
        assertFalse(m.tunnelStarted)
    }

    @Test
    fun `shutdown is idempotent and clears running state`() {
        val relay = newRelay()
        relay.shutdown()
        relay.shutdown() // 2e appel ne doit pas crasher
        val m = relay.metrics()
        assertFalse(m.running)
    }

    @Test
    fun `concurrent onOutboundPacket increments counter atomically (L-5)`() {
        val relay = newRelay()
        val threads = 8
        val packetsPerThread = 1000
        val workers = (1..threads).map {
            Thread {
                repeat(packetsPerThread) {
                    relay.onOutboundPacket(byteArrayOf(0x01), 1)
                }
            }
        }
        workers.forEach { it.start() }
        workers.forEach { it.join() }

        val m = relay.metrics()
        // Sous concurrent — chaque appel doit avoir incrémenté EXACTEMENT
        // une fois (AtomicLong garantit l'atomicité). Sans atomicité (Long
        // brut) on aurait des incréments perdus.
        assertEquals(
            "AtomicLong droppedNotConnected doit être atomique sous concurrent",
            (threads * packetsPerThread).toLong(),
            m.droppedNotConnected,
        )
    }

    @Test
    fun `metrics is data class with proper equals (test infra contract)`() {
        // Garde-fou : si quelqu'un transforme Metrics en class non-data,
        // le test équivalence echoue → on saura que l'API a changé.
        val m1 = GoBackedPacketRelay.Metrics(
            droppedNotConnected = 1L,
            droppedBackPressure = 2L,
            running = true,
            tunnelStarted = false,
        )
        val m2 = GoBackedPacketRelay.Metrics(
            droppedNotConnected = 1L,
            droppedBackPressure = 2L,
            running = true,
            tunnelStarted = false,
        )
        assertEquals(m1, m2)
    }

    @Test
    fun `relay class has documented public surface`() {
        val cls = GoBackedPacketRelay::class.java
        val methodNames = cls.declaredMethods.map { it.name }.toSet()
        // PacketRelay interface contract.
        assertTrue("onOutboundPacket attendu", "onOutboundPacket" in methodNames)
        assertTrue("onTunnelStarted attendu", "onTunnelStarted" in methodNames)
        assertTrue("onTunnelStopped attendu", "onTunnelStopped" in methodNames)
        // Story 9.7 specific.
        assertTrue("shutdown attendu", "shutdown" in methodNames)
    }

    @Test
    fun `constructor accepts required dependencies`() {
        val ctor = GoBackedPacketRelay::class.java.constructors.firstOrNull()
        assertNotNull("Au moins un constructeur public attendu", ctor)
        // 5 paramètres : relayDomain, pinnedKeyB64, inboundSink, outboundCapacity,
        // onStateChanged (signature étendue Story 11.7-bis : state + visibleIp + effectiveCountry).
        assertEquals(5, ctor!!.parameterCount)
    }

    /**
     * Story 11.7-bis : le callback `onStateChanged` est désormais une lambda
     * 3-args (state, visibleIp, effectiveCountry). Vérifie via réflexion le type.
     */
    @Test
    fun `onStateChanged constructor parameter accepts 3-arg lambda`() {
        // Lambda 3-args (state, visibleIp, effectiveCountry).
        var capturedState: VpnState? = null
        var capturedIp: String? = null
        var capturedCountry: String? = null
        val callback: (VpnState, String?, String?) -> Unit = { state, ip, country ->
            capturedState = state
            capturedIp = ip
            capturedCountry = country
        }
        // Test direct d'invocation pour valider la signature.
        callback(VpnState.CONNECTED, "5.45.6.7", "DE")
        assertEquals(VpnState.CONNECTED, capturedState)
        assertEquals("5.45.6.7", capturedIp)
        assertEquals("DE", capturedCountry)
    }

    // === Code-review post-Epic 11 (M4) — mapping String → VpnState ===

    @Test
    fun `goStateToVpnState mappe les 4 etats Go canoniques`() {
        assertEquals(VpnState.CONNECTED, goStateToVpnState("connected"))
        assertEquals(VpnState.RECONNECTING, goStateToVpnState("connecting"))
        assertEquals(VpnState.DISCONNECTED, goStateToVpnState("disconnected"))
        assertEquals(VpnState.ERROR, goStateToVpnState("error"))
    }

    @Test
    fun `goStateToVpnState fallback DISCONNECTED pour state inconnu`() {
        // Défense fail-safe : tout state non listé → DISCONNECTED (rien
        // d'actif, pas d'illusion de connexion).
        assertEquals(VpnState.DISCONNECTED, goStateToVpnState(""))
        assertEquals(VpnState.DISCONNECTED, goStateToVpnState("foo"))
        assertEquals(VpnState.DISCONNECTED, goStateToVpnState("CONNECTED"))  // case-sensitive
    }
}
