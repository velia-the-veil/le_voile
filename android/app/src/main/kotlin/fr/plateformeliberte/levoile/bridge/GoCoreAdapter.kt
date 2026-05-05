package fr.plateformeliberte.levoile.bridge

import fr.plateformeliberte.levoile.core.auth.Auth
import fr.plateformeliberte.levoile.core.protocol.Protocol
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext

/**
 * Adaptateur unique vers le noyau Go partagé exposé via .aar gomobile (ADR-09).
 *
 * **Frontière étroite — règle architecture l. 1059-1060** :
 *  - Aucune autre classe Kotlin du module ne doit importer
 *    `fr.plateformeliberte.levoile.core.*` directement. Cet adaptateur est
 *    le SEUL point de couplage. Vérification : `grep -r
 *    "fr.plateformeliberte.levoile.core" android/app/src/main/kotlin/` doit
 *    lister UNIQUEMENT ce fichier.
 *  - Toutes les méthodes JNI bloquantes (`Protocol.connect`,
 *    `Protocol.writePacket`, `Auth.issueSessionToken`) sont enveloppées en
 *    `suspend fun` + `Dispatchers.IO`. Gomobile ne supporte pas suspend
 *    nativement (cf. architecture l. 1213-1214).
 *  - Un [Mutex] sérialise les mutateurs (`connect`/`disconnect`/
 *    `writePacket`/`setCallbacks`) — gomobile n'est pas thread-safe sur les
 *    méthodes mutatrices d'une même session.
 *  - Les exceptions Java propagées par gomobile sont attrapées et
 *    re-encapsulées en [LeVoileCoreException] — aucun type gomobile généré
 *    ne fuit hors de cet objet.
 *
 * **⚠️ RÉENTRANCE INTERDITE (M-8 — code-review Story 9.7)** :
 *  Les callbacks Go → Kotlin ([PacketCallback], [StatusCallback]) sont
 *  invoqués depuis la goroutine de pump du noyau Go (thread JVM géré par
 *  gomobile). Le code utilisateur de ces callbacks **NE DOIT JAMAIS**
 *  appeler de méthode mutatrice du `GoCoreAdapter` (`connect`,
 *  `disconnect`, `writePacket`, `requestSessionToken`,
 *  `refreshSessionToken`). Une telle réentrance déclencherait un deadlock
 *  irréversible sur le [Mutex] interne (gomobile pump bloqué = pump in
 *  bloqué = paquets perdus = retransmissions = throttle).
 *
 *  Pattern correct : enqueue le travail dans un `Channel` ou un
 *  `actor` Kotlin et le drainer depuis une coroutine séparée. Voir
 *  `GoBackedPacketRelay` pour un exemple (le packetCallback enqueue dans
 *  un `ConcurrentLinkedQueue` du Service ; aucun appel re-entrant à
 *  `GoCoreAdapter`).
 *
 * **Pattern de consommation** :
 * ```
 * GoCoreAdapter.setCallbacks(
 *     packetCb = { pkt -> sink.offer(pkt) },        // enqueue, PAS d'appel adapter
 *     statusCb = { state, msg -> updateNotif(state) } // pas d'appel adapter
 * )
 *
 * lifecycleScope.launch {
 *     GoCoreAdapter.connect(relayDomain, pinnedKey)
 *         .onSuccess {
 *             while (running) {
 *                 val n = fis.read(buf)
 *                 if (n > 0) GoCoreAdapter.writePacket(buf.copyOf(n))
 *             }
 *         }
 *         .onFailure { Log.e(TAG, "connect", it) }
 * }
 * ```
 */
object GoCoreAdapter {

    /**
     * Sérialise les mutateurs. Lecture concurrente sans verrou (les
     * méthodes pure-data ci-dessous n'ont pas besoin du lock).
     *
     * **Couvre désormais aussi `setCallbacks` (M-7 code-review Story 9.7)** :
     * une race entre `setCallbacks` et un `connect` concurrent pouvait
     * laisser le packetCallback Go enregistré pointer sur une instance
     * Kotlin obsolète (callbacks half-set côté Go).
     */
    private val mutex = Mutex()

    /**
     * Établit la session QUIC/HTTP3 + handshake pinning Ed25519 + obtention
     * du session token via `/verify`. Démarre la goroutine de pump.
     *
     * Singleton : si une session est déjà active OU en cours d'établissement,
     * retourne `Result.failure(LeVoileCoreException("session already open"))`.
     *
     * Synchrone (dans la coroutine appelante) : retour après /verify
     * complète. Cible NFR-AND-2 < 3 sec sur LTE/Wi-Fi domestique.
     */
    suspend fun connect(
        relayDomain: String,
        pinnedKeyB64: String,
    ): Result<Unit> = mutex.withLock {
        withContext(Dispatchers.IO) {
            runCatching { Protocol.connect(relayDomain, pinnedKeyB64) }
                .recoverCatching { throw LeVoileCoreException("connect failed: ${it.message}", it) }
        }
    }

    /**
     * Ferme proprement la session QUIC + arrête la pompe. Idempotent : peut
     * être appelée même sans session active (renvoie `Result.success`).
     */
    suspend fun disconnect(): Result<Unit> = mutex.withLock {
        withContext(Dispatchers.IO) {
            runCatching { Protocol.close() }
                .recoverCatching { throw LeVoileCoreException("disconnect failed: ${it.message}", it) }
        }
    }

    /**
     * Pousse un paquet IP brut dans la file d'envoi du pump. Comportement
     * back-pressure : si la file interne est pleine, le paquet est
     * silencieusement DROPPÉ par la facade Go (TCP/QUIC retransmettront).
     *
     * Story 9.4 `LeVoileVpnService` appellera cette méthode depuis la pompe
     * `vpn-out-pump`. Pour des raisons de performance (8000 paquets/s à
     * 100 Mbps), `GoBackedPacketRelay` utilise un `Channel<ByteArray>` +
     * coroutine consommatrice unique pour amortir le coût Mutex/Dispatchers
     * (cf. M-1 code-review Story 9.7).
     */
    suspend fun writePacket(packet: ByteArray): Result<Unit> = mutex.withLock {
        // Pas de withContext(Dispatchers.IO) ici : le seul caller hot-path
        // est [GoBackedPacketRelay.drainOutboundLoop] lancé via
        // `scope.launch { ... }` avec scope = CoroutineScope(Dispatchers.IO).
        // Donc on est déjà sur IO. Le re-dispatch coûte un nouveau Job +
        // cancellation check + suspension à 5000 paquets/s = overhead
        // significatif sans bénéfice (le current dispatcher est déjà IO).
        runCatching { Protocol.writePacket(packet) }
            .recoverCatching { throw LeVoileCoreException("writePacket failed: ${it.message}", it) }
    }

    /**
     * Demande un session token Ed25519 (TTL 4h) auprès du relais. N'ouvre
     * PAS de pump — utile pour valider la joignabilité d'un relais sans
     * démarrer le tunnel complet.
     *
     * Si une session globale est déjà ouverte sur **le même domaine ET la
     * même pubkey pinnée**, retourne son token courant (économie RTT —
     * cf. facade `RequestSessionTokenGomobile`, fix L-6 code-review).
     */
    suspend fun requestSessionToken(
        relayDomain: String,
        relayPubKeyB64: String,
    ): Result<String> = withContext(Dispatchers.IO) {
        runCatching { Auth.issueSessionToken(relayDomain, relayPubKeyB64) }
            .recoverCatching { throw LeVoileCoreException("issueSessionToken failed: ${it.message}", it) }
    }

    /**
     * Force un refresh proactif du session token de la session active.
     *
     * Sans session active : `Result.failure(LeVoileCoreException("not connected"))`.
     */
    suspend fun refreshSessionToken(): Result<String> = withContext(Dispatchers.IO) {
        runCatching { Auth.refreshSessionToken() }
            .recoverCatching { throw LeVoileCoreException("refreshSessionToken failed: ${it.message}", it) }
    }

    /**
     * Vérifie qu'un token est non-expiré et correspond à la session active.
     *
     * **Pas une preuve cryptographique** : la validation Ed25519 + IP hash
     * réelle est faite par le relais à chaque requête /tunnel. Ce check est
     * un guard rapide ("vaut le coup d'essayer ce token").
     */
    fun isSessionTokenValid(token: String): Boolean = Auth.validateSessionToken(token)

    /**
     * Enregistre les callbacks Go → Kotlin. À appeler **une seule fois** au
     * startup (Story 9.4 `LeVoileVpnService.onCreate`). Adapte les
     * interfaces Kotlin SAM ([PacketCallback], [StatusCallback]) vers les
     * interfaces gomobile-générées.
     *
     * Passer `null` pour désenregistrer (cleanup au shutdown).
     *
     * **M-7 (code-review Story 9.7)** : suspend + mutex.withLock pour
     * sérialiser avec les autres mutateurs (connect/disconnect/writePacket).
     * Une race entre setCallbacks et un connect concurrent pouvait laisser
     * les callbacks Go pointer vers une instance Kotlin obsolète. Le coût
     * suspend ici est négligeable (appel de configuration, pas hot path).
     */
    suspend fun setCallbacks(packetCb: PacketCallback?, statusCb: StatusCallback?) {
        mutex.withLock {
            withContext(Dispatchers.IO) {
                Protocol.setPacketCallback(
                    packetCb?.let {
                        object : fr.plateformeliberte.levoile.core.protocol.PacketCallback {
                            override fun onPacketReceived(packet: ByteArray?) {
                                // gomobile peut nuller le ByteArray (annotation @Nullable
                                // par défaut) — guard défensif.
                                packet?.let { it1 -> packetCb.onPacketReceived(it1) }
                            }
                        }
                    },
                )
                Protocol.setStatusCallback(
                    statusCb?.let {
                        object : fr.plateformeliberte.levoile.core.protocol.StatusCallback {
                            // Story 11.7-bis : 4 params côté shim Go (state, message,
                            // visibleIp, effectiveCountry). gomobile peut nuller chaque
                            // String individuellement — guard défensif.
                            override fun onStateChange(
                                state: String?,
                                message: String?,
                                visibleIP: String?,
                                effectiveCountry: String?,
                            ) {
                                statusCb.onStateChange(
                                    state ?: "unknown",
                                    message ?: "",
                                    visibleIP ?: "",
                                    effectiveCountry ?: "",
                                )
                            }
                        }
                    },
                )
            }
        }
    }

    /**
     * Indique si une session est actuellement ouverte côté Go. Utilitaire
     * de diagnostic — ne doit pas être utilisé pour synchroniser le
     * lifecycle (race conditions possibles entre check et action ; utiliser
     * directement `connect()` qui retourne une erreur si déjà ouverte).
     */
    fun isSessionOpen(): Boolean = Protocol.isSessionOpen()

    /**
     * R-T8 (2026-05-05) — QUIC Connection Migration RFC 9000 §9.
     *
     * Rebascule la session QUIC active vers le file descriptor UDP fourni.
     * Utilisé par [fr.plateformeliberte.levoile.network.NetworkMigrationCoordinator]
     * quand `ConnectivityManager.NetworkCallback` signale un changement
     * d'underlying network (Wi-Fi <-> LTE handoff, network attach/detach).
     *
     * **Ownership du fd** : Go prend ownership. Sur succès le socket est lié
     * au nouveau `*quic.Transport` côté Go ; sur échec le fd est fermé avant
     * retour. Kotlin NE DOIT PAS close le DatagramSocket après l'appel.
     *
     * **Synchrone bornée 5s** : `Protocol.migrate` bloque sur le PATH_CHALLENGE
     * /PATH_RESPONSE QUIC (timeout 2s) + AddPath/Switch internal scheduling.
     * Si la migration échoue (path validation timeout, peer-disabled-migration,
     * pas de session active), retourne un `Result.failure`.
     *
     * **Pourquoi pas de coupure visible utilisateur** : l'état applicatif
     * (HTTP/3 streams, session token, /tunnel stream) est préservé. Les
     * paquets en vol côté ancien socket sont drainés sur 2s avant que
     * l'ancien transport soit fermé côté Go.
     */
    suspend fun migrate(fd: Int): Result<Unit> = mutex.withLock {
        withContext(Dispatchers.IO) {
            runCatching { Protocol.migrate(fd.toLong()) }
                .recoverCatching { throw LeVoileCoreException("migrate failed: ${it.message}", it) }
        }
    }

    // ----- Constantes pure-data Story 9.2 (re-exposées pour confort) -----

    /** Version du protocole filaire — retour de `protocol.Version()`. */
    fun protocolVersion(): String = Protocol.version()

    /**
     * Taille du header de framing tunnel (2 octets BE).
     *
     * Retour `Long` (et non `Int`) : gomobile mappe Go `int` (64-bit sur les
     * plateformes 64-bit) → Java `long`. Caster `.toInt()` est sûr ici (la
     * valeur est 2) mais on préserve la sémantique gomobile pour clarté du
     * mapping de types.
     */
    fun framingHeaderSize(): Long = Protocol.framingHeaderSize()
}

/**
 * Exception unifiée encapsulant toute erreur remontée par le noyau Go via
 * gomobile (mappées en `java.lang.Exception` par le toolchain). Permet aux
 * consommateurs Kotlin de catcher un seul type sans dépendre des types
 * gomobile générés — cohérent ADR-09 frontière étroite.
 */
class LeVoileCoreException(message: String, cause: Throwable? = null) : Exception(message, cause)
