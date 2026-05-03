package fr.plateformeliberte.levoile.bridge

/**
 * Callback Go → Kotlin : invoqué par la goroutine de pump du noyau Go partagé
 * (cf. internal/tunnel/gomobile_facade.go.runGomobilePump) chaque fois qu'un
 * paquet IP arrive du relais via le stream HTTP/3 /tunnel.
 *
 * Le caller branché en Story 9.7 est `GoCoreAdapter.setCallbacks(...)` qui
 * adapte cette interface SAM Kotlin vers l'interface gomobile-générée
 * `fr.plateformeliberte.levoile.core.protocol.PacketCallback`.
 *
 * **Story 9.4 LeVoileVpnService** consommera cette interface via
 * [GoBackedPacketRelay] qui injecte les paquets reçus dans le packetSink du
 * VpnService — la pompe `vpn-in-pump` les écrit ensuite sur le
 * `FileOutputStream(fd.fileDescriptor)` du TUN.
 *
 * IMPORTANT — la méthode est appelée DEPUIS LA GOROUTINE Go (thread JVM
 * géré par gomobile) :
 *  - **Idempotente** — gomobile peut redélivrer le même paquet en cas de
 *    timeout sur le stream (rare).
 *  - **Non-bloquante** — bloquer ce thread bloque tout le pump → starvation
 *    des paquets suivants.
 *  - **Ne PAS lever d'exception** — gomobile JNI crashe la JVM si une
 *    exception non-attrapée remonte.
 *  - Le `ByteArray` reçu est une COPIE défensive (cf. facade Go), donc le
 *    callee peut le retenir / l'enqueuer sans risque de réécriture.
 */
fun interface PacketCallback {
    fun onPacketReceived(packet: ByteArray)
}
