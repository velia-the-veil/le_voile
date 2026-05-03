package fr.plateformeliberte.levoile.vpn

import android.util.Log
import fr.plateformeliberte.levoile.BuildConfig

/**
 * Pont entre la pompe paquets sortante du VpnService et le noyau de relayage.
 *
 * Story 9.4 livre cette interface + l'implementation par defaut [NoOpPacketRelay]
 * qui drope les paquets en silence (placeholder). Story 9.7 livrera une
 * implementation reelle qui pousse les paquets vers le noyau Go partage via
 * GoCoreAdapter.writePacket(...) (handshake QUIC/HTTP3 + stream /tunnel).
 *
 * Frontiere etroite : cette interface ne traverse JAMAIS la frontiere
 * OS-specifique (coherent ADR-08). Elle reste purement Kotlin/Android. Le
 * wrapper Go arrive Story 9.7 sous la forme d'une classe Kotlin
 * GoBackedPacketRelay : PacketRelay qui consomme la classe Java generee par
 * gomobile dans le .aar.
 */
interface PacketRelay {

    /**
     * Appelee pour chaque paquet IP brut lu depuis FileInputStream(fd VpnService).
     *
     * @param buf buffer rempli par fis.read() — NE PAS retenir la reference,
     *  le caller reutilise le meme buffer a chaque iteration de la pompe (allocation 0).
     * @param length nombre d'octets valides dans buf[0 until length]. Toujours > 0.
     */
    fun onOutboundPacket(buf: ByteArray, length: Int)

    /**
     * Lifecycle hook appele par LeVoileVpnService.connectInternal() apres establish().
     * Permet a l'implementation de preparer son state (ex : ouvrir la connexion
     * QUIC Story 9.7). Default : no-op.
     */
    fun onTunnelStarted() {}

    /**
     * Lifecycle hook appele par LeVoileVpnService.disconnectInternal() avant fermeture
     * du fd. Permet a l'implementation de drainer ses buffers / fermer la connexion
     * QUIC Story 9.7. Default : no-op.
     */
    fun onTunnelStopped() {}
}

/**
 * Implementation par defaut Story 9.4 — drope tous les paquets en silence.
 *
 * Utilisee jusqu'a Story 9.7 qui livrera GoBackedPacketRelay (qui consommera
 * le .aar gomobile via GoCoreAdapter.writePacket(buf, length)).
 *
 * Aucun payload paquet n'est jamais logge (coherent NFR-AND-9 / architecture l. 1034).
 * Seulement un compteur en BuildConfig.DEBUG, throttle a 1 ligne par 1000 paquets
 * pour eviter de saturer logcat. R8/ProGuard release strip ce branche mort
 * (NFR-AND-11, Story 9.1 minifyEnabled = true).
 */
class NoOpPacketRelay : PacketRelay {

    private var droppedCount = 0L

    override fun onOutboundPacket(buf: ByteArray, length: Int) {
        if (BuildConfig.DEBUG) {
            droppedCount++
            if (droppedCount % 1000L == 0L) {
                Log.d(
                    TAG,
                    "NoOpPacketRelay: $droppedCount paquets sortants dropes (placeholder Story 9.4)"
                )
            }
        }
    }

    companion object {
        private const val TAG = "NoOpPacketRelay"
    }
}
