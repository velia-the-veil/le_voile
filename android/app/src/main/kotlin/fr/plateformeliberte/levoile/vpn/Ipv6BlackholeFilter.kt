package fr.plateformeliberte.levoile.vpn

/**
 * Audit fix R-T6-bis (2026-05-05) — IPv6 fast-fail Happy Eyeballs.
 *
 * Contexte : le fix R-T6 (2026-05-04) ferme la fuite v6 en posant
 * `addAddress(fd00:6:6::2/64)` + `addRoute(::/0)` sur la TUN, et le pump-out
 * forwarde tout au relais. Côté relais le NAT est v4-only → les paquets v6
 * sont **droppés silencieusement** sans ICMPv6 unreachable. Conséquence :
 * Chrome/Happy Eyeballs (RFC 8305) tente AAAA en premier, le SYN v6 part
 * dans la TUN, ne reçoit aucune réponse → timeout 250 ms → fallback v4.
 *
 * Sur les sites multi-CDN (Netflix, Facebook, 9gag) la cascade de
 * sub-resources v6 timeout → erreurs `ERR_NETWORK_CHANGED` /
 * `DNS_PROBE_STARTED`. Sites v4-prio (Instagram, TikTok) immunes.
 *
 * Fix : intercepter les paquets v6 SORTANTS dans le pump-out Android, ne
 * PAS les forwarder au relais, et **fabriquer une réponse ICMPv6
 * Destination Unreachable Code 1 (administratively prohibited)** qu'on
 * réinjecte dans le sink TUN. Du point de vue Chrome, le réseau a refusé
 * le paquet → Happy Eyeballs bascule v4 instantanément (pas de timeout).
 *
 * Defense in depth maintenue : le drop côté relais reste actif. Aucune
 * fuite v6 (le paquet ne sort jamais de l'appareil).
 *
 * Pourquoi pas v4 NoOp + retirer la route v6 ? Parce qu'on perdrait le
 * blackhole défensif côté relais. Un Phase 2 propre = NAT v6 côté relais.
 */
internal object Ipv6BlackholeFilter {

    private const val IPV6_HEADER_LEN = 40
    private const val ICMPV6_HEADER_LEN = 8

    /**
     * RFC 8200 §5 — IPv6 minimum MTU. Un nœud émettant ICMPv6 doit garantir
     * que le paquet ICMPv6 résultant tient dans 1280 octets pour traverser
     * sans fragmentation tout réseau IPv6 conforme.
     */
    private const val IPV6_MIN_MTU = 1280

    /**
     * Body max = MTU min - IPv6 header (40) - ICMPv6 header (8) = 1232.
     * RFC 4443 §3.1 le dit explicitement : « As much of invoking packet as
     * possible without the ICMPv6 packet exceeding the minimum IPv6 MTU. »
     */
    private const val MAX_BODY_LEN = IPV6_MIN_MTU - IPV6_HEADER_LEN - ICMPV6_HEADER_LEN

    private const val NEXT_HEADER_ICMPV6 = 58
    private const val ICMPV6_TYPE_DEST_UNREACHABLE = 1
    private const val ICMPV6_CODE_ADMIN_PROHIBITED = 1

    /**
     * Hop limit du paquet de réponse. 64 = défaut Linux/Android. La valeur
     * exacte importe peu : le paquet ne quitte jamais l'appareil (réinjecté
     * dans la TUN), elle ne sert qu'à passer le sanity check du stack v6.
     */
    private const val DEFAULT_HOP_LIMIT = 64

    /**
     * Détecte un paquet IPv6 par le champ Version (4 bits supérieurs du
     * premier octet, RFC 8200 §3). Strictement plus rigoureux que
     * `length >= 40` car un paquet v4 fragmenté peut faire 40 octets.
     */
    fun isIPv6Packet(buf: ByteArray, length: Int): Boolean {
        if (length < IPV6_HEADER_LEN) return false
        return ((buf[0].toInt() ushr 4) and 0x0F) == 6
    }

    /**
     * Construit un paquet ICMPv6 « Destination Unreachable / Communication
     * with destination administratively prohibited » (Type 1 Code 1) en
     * réponse au paquet v6 sortant fourni.
     *
     * Retourne `null` si aucune réponse ne doit être émise :
     *  - paquet non IPv6
     *  - destination multicast (RFC 4443 §2.4 e.2 — interdit pour éviter
     *    storms ICMPv6)
     *  - paquet original déjà un ICMPv6 *error* (RFC 4443 §2.4 e.3 —
     *    évite boucles d'erreur infinies)
     *
     * Le paquet retourné est un IPv6 packet complet (headers + payload)
     * prêt à être réinjecté dans la TUN via `fos.write` — Chrome/Linux
     * voient une réponse réseau valide et Happy Eyeballs bascule v4.
     */
    fun buildIcmpv6AdminProhibited(buf: ByteArray, length: Int): ByteArray? {
        if (!isIPv6Packet(buf, length)) return null

        // Source = original dst, Destination = original src (orientation réponse)
        val origSrcStart = 8
        val origDstStart = 24

        // RFC 4443 §2.4 (e.2) : ne PAS répondre aux destinations multicast.
        // Multicast IPv6 = `ff00::/8` → premier octet de l'address dst = 0xFF.
        if ((buf[origDstStart].toInt() and 0xFF) == 0xFF) return null

        // RFC 4443 §2.4 (e.3) : ne PAS répondre à un message ICMPv6 erreur.
        // Champ Next Header à offset 6 ; types ICMPv6 erreur = 1..127
        // (info messages = 128..255, eux peuvent recevoir réponse).
        val nextHeader = buf[6].toInt() and 0xFF
        if (nextHeader == NEXT_HEADER_ICMPV6 && length >= IPV6_HEADER_LEN + 1) {
            val icmpType = buf[IPV6_HEADER_LEN].toInt() and 0xFF
            if (icmpType in 1..127) return null
        }

        val bodyLen = minOf(length, MAX_BODY_LEN)
        val totalIcmpv6Len = ICMPV6_HEADER_LEN + bodyLen
        val totalIpv6Len = IPV6_HEADER_LEN + totalIcmpv6Len

        val out = ByteArray(totalIpv6Len)

        // ----- IPv6 header (40 octets) -----
        out[0] = 0x60.toByte() // Version=6, Traffic Class=0
        // out[1..3] = 0 (traffic class lower nibble + flow label)
        // Payload Length = ICMPv6 header + body (RFC 8200 §3 — exclut le header IPv6)
        out[4] = ((totalIcmpv6Len ushr 8) and 0xFF).toByte()
        out[5] = (totalIcmpv6Len and 0xFF).toByte()
        out[6] = NEXT_HEADER_ICMPV6.toByte()
        out[7] = DEFAULT_HOP_LIMIT.toByte()
        // Source = original destination (16 octets)
        System.arraycopy(buf, origDstStart, out, 8, 16)
        // Destination = original source (16 octets)
        System.arraycopy(buf, origSrcStart, out, 24, 16)

        // ----- ICMPv6 header (8 octets) -----
        out[40] = ICMPV6_TYPE_DEST_UNREACHABLE.toByte()
        out[41] = ICMPV6_CODE_ADMIN_PROHIBITED.toByte()
        // out[42..43] = checksum (calculé en bas)
        // out[44..47] = Unused (zéro pour Type 1 — RFC 4443 §3.1)

        // ----- Body : autant du paquet original que MTU le permet -----
        System.arraycopy(buf, 0, out, 48, bodyLen)

        // ----- Checksum ICMPv6 (RFC 2460 §8.1, RFC 4443 §2.3) -----
        val checksum = computeIcmpv6Checksum(out, totalIcmpv6Len)
        out[42] = ((checksum ushr 8) and 0xFF).toByte()
        out[43] = (checksum and 0xFF).toByte()

        return out
    }

    /**
     * Checksum ICMPv6 = ones-complement de la somme 16-bit de :
     *   1. Pseudo-header IPv6 :
     *      - Source Address (16 octets) — déjà dans `packet[8..23]`
     *      - Destination Address (16 octets) — déjà dans `packet[24..39]`
     *      - Upper-Layer Packet Length (4 octets, big-endian) = `icmpv6Len`
     *      - Zéros (3 octets) + Next Header (1 octet) = 58
     *   2. Message ICMPv6 entier (avec champ Checksum à 0 pendant calcul).
     *
     * Note : on lit les 16-bit groupes via `(hi shl 8) or lo` pour éviter
     * les surprises d'arithmétique signée Kotlin/Java sur les bytes.
     */
    private fun computeIcmpv6Checksum(packet: ByteArray, icmpv6Len: Int): Int {
        var sum = 0L

        // Pseudo-header : Source + Destination (32 octets / 16 mots)
        var i = 8
        while (i < 40) {
            sum += ((packet[i].toInt() and 0xFF) shl 8) or (packet[i + 1].toInt() and 0xFF)
            i += 2
        }
        // Upper-Layer Packet Length (4 octets BE) — split en 2 mots 16-bit
        sum += (icmpv6Len ushr 16) and 0xFFFF
        sum += icmpv6Len and 0xFFFF
        // 3 octets zéro + 1 octet Next Header = 0x0000 + 0x003A
        sum += NEXT_HEADER_ICMPV6

        // Message ICMPv6 (offset 40 dans le packet IPv6 complet).
        // Le champ Checksum (offset 42-43) est à 0, donc inclus tel quel.
        i = 40
        val end = 40 + icmpv6Len
        while (i + 1 < end) {
            sum += ((packet[i].toInt() and 0xFF) shl 8) or (packet[i + 1].toInt() and 0xFF)
            i += 2
        }
        if (i < end) {
            // Octet final isolé : padding implicite à droite
            sum += (packet[i].toInt() and 0xFF) shl 8
        }

        // Fold des carries jusqu'à n'avoir plus que 16 bits
        while ((sum ushr 16) != 0L) {
            sum = (sum and 0xFFFF) + (sum ushr 16)
        }
        return (sum.inv().toInt() and 0xFFFF)
    }
}
