package fr.plateformeliberte.levoile.vpn

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Tests unitaires audit fix R-T6-bis (2026-05-05) — fast-fail Happy Eyeballs.
 *
 * Couvre :
 *  - Détection IPv6 vs IPv4 vs malformé
 *  - Génération ICMPv6 admin-prohibited bien formé (header, addresses
 *    swappées, type/code, body tronqué)
 *  - Edge cases RFC 4443 §2.4 : multicast skip, ICMPv6 error skip
 *  - Checksum ICMPv6 conforme RFC 2460 §8.1 (vérifié par re-calcul = 0)
 *
 * JVM-only, sans Robolectric (cohérent LeVoileVpnServiceConfigTest). Le
 * comportement runtime (réinjection via packetSink, observation par le
 * stack v6 du device) est validé par le test instrumenté de Story 12.6
 * (espresso ping6 ipv6.google.com → reçoit ICMPv6 admin-prohibited).
 */
class Ipv6BlackholeFilterTest {

    // ---------- isIPv6Packet ----------

    @Test
    fun `isIPv6Packet returns false for IPv4 packet`() {
        val v4 = ByteArray(60).apply {
            this[0] = 0x45.toByte() // Version=4, IHL=5
        }
        assertFalse(Ipv6BlackholeFilter.isIPv6Packet(v4, 60))
    }

    @Test
    fun `isIPv6Packet returns true for valid IPv6 packet`() {
        val v6 = forgeIpv6TcpSyn()
        assertTrue(Ipv6BlackholeFilter.isIPv6Packet(v6, v6.size))
    }

    @Test
    fun `isIPv6Packet returns false when buffer shorter than 40 bytes`() {
        val short = ByteArray(39).apply { this[0] = 0x60.toByte() }
        assertFalse(Ipv6BlackholeFilter.isIPv6Packet(short, 39))
    }

    @Test
    fun `isIPv6Packet returns false when version is not 6`() {
        val weird = ByteArray(40).apply { this[0] = 0x70.toByte() } // version 7
        assertFalse(Ipv6BlackholeFilter.isIPv6Packet(weird, 40))
    }

    // ---------- buildIcmpv6AdminProhibited : null cases ----------

    @Test
    fun `buildIcmpv6AdminProhibited returns null for IPv4 packet`() {
        val v4 = ByteArray(60).apply { this[0] = 0x45.toByte() }
        assertNull(Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v4, 60))
    }

    @Test
    fun `buildIcmpv6AdminProhibited returns null for multicast destination`() {
        // RFC 4443 §2.4 e.2 : pas de réponse aux multicast. ff02::1 = all-nodes.
        val v6 = forgeIpv6TcpSyn(
            dst = byteArrayOf(
                0xff.toByte(), 0x02, 0, 0, 0, 0, 0, 0,
                0, 0, 0, 0, 0, 0, 0, 0x01,
            )
        )
        assertNull(Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size))
    }

    @Test
    fun `buildIcmpv6AdminProhibited returns null for ICMPv6 error message`() {
        // RFC 4443 §2.4 e.3 : pas de réponse à un autre ICMPv6 error (boucle).
        // Type 1 (Destination Unreachable) = error class.
        val v6 = forgeIpv6Icmpv6(icmpType = 1)
        assertNull(Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size))
    }

    @Test
    fun `buildIcmpv6AdminProhibited returns response for ICMPv6 info message (echo request)`() {
        // Type 128 = Echo Request (info, pas error). Doit recevoir réponse.
        val v6 = forgeIpv6Icmpv6(icmpType = 128)
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)
        assertNotNull(response)
    }

    // ---------- buildIcmpv6AdminProhibited : structure du paquet de réponse ----------

    @Test
    fun `response has IPv6 version 6 and ICMPv6 next header`() {
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        assertEquals(6, (response[0].toInt() ushr 4) and 0x0F)
        assertEquals(58, response[6].toInt() and 0xFF) // Next Header = ICMPv6
    }

    @Test
    fun `response payload length matches ICMPv6 message size`() {
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        val payloadLen = ((response[4].toInt() and 0xFF) shl 8) or (response[5].toInt() and 0xFF)
        // Payload = ICMPv6 header (8) + body (= original packet size, < 1232)
        assertEquals(8 + v6.size, payloadLen)
        // Total response size = IPv6 header (40) + payload
        assertEquals(40 + payloadLen, response.size)
    }

    @Test
    fun `response source equals original destination and vice versa`() {
        val src = byteArrayOf(
            0x20, 0x01, 0x0d, 0xb8.toByte(), 0, 0, 0, 0,
            0, 0, 0, 0, 0, 0, 0, 0x01,
        )
        val dst = byteArrayOf(
            0x26, 0x07, 0xf8.toByte(), 0xb0.toByte(), 0x40, 0x05, 0x08, 0x05,
            0, 0, 0, 0, 0, 0, 0x20, 0x0e,
        )
        val v6 = forgeIpv6TcpSyn(src = src, dst = dst)
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!

        // Response src (offset 8) = original dst
        assertArrayEquals(dst, response.copyOfRange(8, 24))
        // Response dst (offset 24) = original src
        assertArrayEquals(src, response.copyOfRange(24, 40))
    }

    @Test
    fun `response ICMPv6 type and code are 1 1 (admin prohibited)`() {
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        assertEquals(1, response[40].toInt() and 0xFF) // Type = Destination Unreachable
        assertEquals(1, response[41].toInt() and 0xFF) // Code = Admin Prohibited
    }

    @Test
    fun `response ICMPv6 unused field is zero`() {
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        // Bytes 44-47 = Unused (RFC 4443 §3.1)
        assertEquals(0, response[44].toInt())
        assertEquals(0, response[45].toInt())
        assertEquals(0, response[46].toInt())
        assertEquals(0, response[47].toInt())
    }

    @Test
    fun `response body contains the original packet`() {
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        // Body starts at offset 48 (40 IPv6 + 8 ICMPv6)
        val body = response.copyOfRange(48, response.size)
        assertArrayEquals(v6, body)
    }

    @Test
    fun `response body is truncated when original packet exceeds MTU minus headers`() {
        // Forge un gros paquet IPv6 (200 octets de data > limite réelle, mais
        // surtout > MAX_BODY_LEN si on créait un paquet jumbo). Pour tester la
        // troncature on simule un paquet très gros (taille > 1232).
        val big = ByteArray(2000)
        big[0] = 0x60.toByte() // version 6
        // Payload length 1960 (informatif uniquement)
        big[4] = ((1960 ushr 8) and 0xFF).toByte()
        big[5] = (1960 and 0xFF).toByte()
        big[6] = 6 // TCP
        // Source/dest globaux unicast
        big[8] = 0x20; big[9] = 0x01
        big[24] = 0x26; big[25] = 0x07

        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(big, big.size)!!
        // Total = 40 (IPv6) + 8 (ICMPv6) + 1232 (body max) = 1280 (= MTU min IPv6)
        assertEquals(1280, response.size)
    }

    // ---------- Checksum ----------

    @Test
    fun `response ICMPv6 checksum verifies (recompute equals zero)`() {
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        // Si le checksum est correct, recalculer la somme ones-complement sur
        // l'ensemble (pseudo-header + ICMPv6 incluant le checksum) doit donner 0.
        val sum = ipv6Checksum(response)
        assertEquals(
            "ICMPv6 checksum doit etre cohérent : recalcul ones-complement = 0xFFFF (sum) -> 0x0000 (one-comp). Got 0x${sum.toString(16)}",
            0,
            sum
        )
    }

    @Test
    fun `response checksum field is non-zero for typical packet`() {
        // Sanity check : on n'oublie pas d'écrire le checksum (il ne reste pas à 0).
        val v6 = forgeIpv6TcpSyn()
        val response = Ipv6BlackholeFilter.buildIcmpv6AdminProhibited(v6, v6.size)!!
        val checksum = ((response[42].toInt() and 0xFF) shl 8) or (response[43].toInt() and 0xFF)
        assertTrue(
            "Le checksum ne doit pas rester à 0 (cas où l'algorithme oublierait de l'écrire)",
            checksum != 0
        )
    }

    // ---------- Helpers ----------

    /**
     * Forge un paquet IPv6 TCP SYN minimal (40 IPv6 + 20 TCP = 60 octets) avec
     * src/dst configurables. Utilisé pour les cas standards (non-multicast,
     * non-ICMPv6 error).
     */
    private fun forgeIpv6TcpSyn(
        src: ByteArray = byteArrayOf(
            0xfd.toByte(), 0x00, 0x00, 0x06, 0x00, 0x06, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
        ),
        dst: ByteArray = byteArrayOf(
            0x26, 0x07, 0xf8.toByte(), 0xb0.toByte(), 0x40, 0x05, 0x08, 0x05,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x0e,
        ),
    ): ByteArray {
        require(src.size == 16) { "src doit faire 16 octets" }
        require(dst.size == 16) { "dst doit faire 16 octets" }
        val pkt = ByteArray(60)
        pkt[0] = 0x60.toByte() // Version=6
        pkt[4] = 0x00; pkt[5] = 0x14 // Payload length = 20 (TCP only, no options)
        pkt[6] = 6 // Next Header = TCP
        pkt[7] = 64 // Hop limit
        System.arraycopy(src, 0, pkt, 8, 16)
        System.arraycopy(dst, 0, pkt, 24, 16)
        // TCP minimal : src port 50000, dst port 443, SYN flag.
        pkt[40] = 0xc3.toByte(); pkt[41] = 0x50.toByte() // src port 50000
        pkt[42] = 0x01; pkt[43] = 0xbb.toByte() // dst port 443
        // SYN flag (offset 53 = byte 13 du TCP header)
        pkt[53] = 0x02
        return pkt
    }

    /**
     * Forge un paquet IPv6 contenant un message ICMPv6 du type donné.
     * Utilisé pour vérifier la sémantique error-skip (Type 1..127) vs
     * info-respond (Type 128..255).
     */
    private fun forgeIpv6Icmpv6(icmpType: Int): ByteArray {
        val pkt = ByteArray(48) // 40 IPv6 + 8 ICMPv6 minimum
        pkt[0] = 0x60.toByte() // Version=6
        pkt[4] = 0x00; pkt[5] = 0x08 // Payload length = 8
        pkt[6] = 58 // Next Header = ICMPv6
        pkt[7] = 64 // Hop limit
        // src/dst globaux unicast
        pkt[8] = 0xfd.toByte(); pkt[9] = 0x00 // src
        pkt[24] = 0x26; pkt[25] = 0x07 // dst
        pkt[40] = icmpType.toByte()
        return pkt
    }

    /**
     * Recalcul du checksum ICMPv6 sur le paquet COMPLET (incluant le champ
     * checksum déjà rempli). Si l'implémentation est correcte, le résultat
     * doit être 0 (proprieté du ones-complement checksum : sum + ~sum = 0xFFFF,
     * et le one-complement final = 0).
     *
     * Inputs : un paquet IPv6 complet où le next-header = 58 (ICMPv6) et le
     * payload length du header IPv6 reflète la taille ICMPv6.
     */
    private fun ipv6Checksum(packet: ByteArray): Int {
        val payloadLen = ((packet[4].toInt() and 0xFF) shl 8) or (packet[5].toInt() and 0xFF)
        var sum = 0L
        // Pseudo-header : src + dst (32 octets / 16 mots de 16 bits)
        var i = 8
        while (i < 40) {
            sum += ((packet[i].toInt() and 0xFF) shl 8) or (packet[i + 1].toInt() and 0xFF)
            i += 2
        }
        sum += (payloadLen ushr 16) and 0xFFFF
        sum += payloadLen and 0xFFFF
        sum += 58 // Next Header
        // ICMPv6 message
        i = 40
        val end = 40 + payloadLen
        while (i + 1 < end) {
            sum += ((packet[i].toInt() and 0xFF) shl 8) or (packet[i + 1].toInt() and 0xFF)
            i += 2
        }
        if (i < end) {
            sum += (packet[i].toInt() and 0xFF) shl 8
        }
        while ((sum ushr 16) != 0L) {
            sum = (sum and 0xFFFF) + (sum ushr 16)
        }
        return (sum.inv().toInt() and 0xFFFF)
    }
}
