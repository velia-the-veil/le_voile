package fr.plateformeliberte.levoile.vpn

import android.util.Log
import androidx.annotation.VisibleForTesting
import fr.plateformeliberte.levoile.bridge.GoCoreAdapter
import fr.plateformeliberte.levoile.bridge.LeVoileCoreException
import fr.plateformeliberte.levoile.bridge.PacketCallback
import fr.plateformeliberte.levoile.bridge.StatusCallback
import fr.plateformeliberte.levoile.ui.VpnState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * Implémentation [PacketRelay] livrée Story 9.7 — branche
 * [LeVoileVpnService] sur le noyau Go partagé via [GoCoreAdapter].
 *
 * **Cycle de vie** :
 *  1. `onTunnelStarted()` — handshake QUIC/HTTP3 + démarrage de la
 *     coroutine consommatrice du `outboundChannel`.
 *  2. `onOutboundPacket(buf, length)` — copy + non-blocking offer dans le
 *     `outboundChannel` (Channel CONFLATED-like via trySend, drop sur
 *     saturation pour ne pas bloquer le thread `vpn-out-pump`).
 *  3. La coroutine consommatrice draine en série via `GoCoreAdapter.writePacket`
 *     (un seul JNI call à la fois, pas de churn de coroutines/Mutex/Dispatcher).
 *  4. Paquets entrants Go → `packetCallback` → `inboundSink.offer(packet)`
 *     du Service ; la pompe `vpn-in-pump` les écrit sur le `FileOutputStream(fd)`.
 *  5. `onTunnelStopped()` ferme la channel + cleanup via NonCancellable
 *     (cleanup garanti même si le scope appelant est cancellé).
 *
 * **Optimisation perf (M-1 code-review Story 9.7)** : à 8000 paquets/s
 * (100 Mbps), le pattern initial `scope.launch { writePacket(pkt) }` par
 * paquet créait 8000 coroutines/s + 8000 acquisitions Mutex/s. Refactor
 * vers Channel single-consumer : 1 coroutine drain qui prend N paquets,
 * une seule acquisition de mutex amortie.
 *
 * **Bounded inboundSink (M-3 code-review Story 9.7)** : le caller (le
 * Service) DOIT passer un `Channel<ByteArray>` borné (capacity ≤ 256 par
 * convention) plutôt qu'un `ConcurrentLinkedQueue` non borné. Si la pompe
 * `vpn-in-pump` ralentit, les paquets en surplus sont droppés silencieusement
 * (TCP/QUIC retransmettront).
 *
 * **PII / NFR-AND-9** : aucun log de payload paquet, IP destination ou URL.
 * Les transitions d'état reçues via le `StatusCallback` arrivent déjà
 * redactées par la facade Go (`network_error` plutôt que message brut —
 * cf. fix H-1 `redactErrorForStatus`).
 */
class GoBackedPacketRelay(
    private val relayDomain: String,
    private val pinnedKeyB64: String,
    /**
     * Sink fourni par le Service — chaque paquet inbound (Go → Kotlin) y est
     * `trySend`'é (non-blocking). Si la file est saturée (capacity dépassée),
     * le paquet est silencieusement DROPPÉ.
     *
     * **M-3** : le caller DOIT fournir un Channel BORNÉ (typique
     * `Channel(capacity = 256)`). Un Channel.UNLIMITED ouvre la porte à un
     * DoS mémoire si le pump-in du Service traîne.
     */
    private val inboundSink: Channel<ByteArray>,
    /**
     * Capacité du Channel outbound interne (Kotlin → Go). 8192 paquets ≈ 11 MB
     * max in-flight (MTU 1420). Drop silencieux au-delà.
     *
     * Bumped 256 -> 2048 (2026-05-04) puis 2048 -> 8192 (R-T8 BISECT round 4,
     * 2026-05-10) : sous charge Facebook (50+ connexions parallèles,
     * gros JS+images), 2048 saturait en quelques secondes → drops massifs →
     * SYN/ACK perdus → "tunnel zombie" apparent (Twitch + tout autre site
     * KO en même temps que FB) sans aucune erreur visible côté pump Go
     * (pas de heartbeat trip car la connexion répond aux /health). Le
     * vrai fix est de batcher les writes côté Kotlin↔Go (Phase 2), mais
     * augmenter la capacité absorbe les bursts en attendant. 11 MB
     * d'in-flight est acceptable côté mémoire (vs 2.9 MB avant).
     */
    private val outboundCapacity: Int = 8192,
    /**
     * Code-review post-Epic 11 (M4) + Story 11.7-bis : callback de transition
     * d'état tunnel enrichi. Reçoit aussi l'IP visible et le pays effectif
     * extraits du callback Go au moment de `connected`.
     *
     * Invoqué à chaque `StatusCallback` Go → Kotlin :
     *  - `state` : [VpnState] mappé depuis le string Go via [goStateToVpnState]
     *  - `visibleIp` : IP du relais résolue via DNS (vide pour autres états que
     *    CONNECTED, ou si DNS lookup a échoué — caller fallback "—")
     *  - `effectiveCountry` : code ISO 3166-1 alpha-2 (« DE », « ES ») extrait
     *    du domaine relais (vide pour autres états que CONNECTED)
     *
     * Le caller (typiquement [LeVoileVpnService]) passera
     * `{ state, ip, country -> notificationHelper.notify(state, country ?: currentCountry, ip ?: currentIp, currentKillStatus) }`.
     *
     * Reste null par défaut pour ne pas casser les tests existants. Quand
     * `LeVoileVpnService` bascule de `NoOpPacketRelay` vers
     * `GoBackedPacketRelay`, ce callback sera fourni.
     *
     * Invoqué depuis le thread JVM gomobile — le caller ne doit PAS appeler
     * de méthode mutatrice du `GoCoreAdapter` ici (cf. avertissement
     * réentrance). Pour `notificationHelper.notify` c'est OK (pas de réentrance).
     */
    private val onStateChanged: ((state: VpnState, visibleIp: String?, effectiveCountry: String?) -> Unit)? = null,
) : PacketRelay {

    private val running = AtomicBoolean(false)
    private val tunnelStarted = AtomicBoolean(false)

    /**
     * Scope principal des opérations relais (connect + drain pump).
     * SupervisorJob → un échec d'enfant ne fait pas tomber les autres.
     * Cancel via [shutdown].
     */
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /**
     * Channel outbound : producer = `vpn-out-pump` thread du Service via
     * `onOutboundPacket`, consumer = drain coroutine.
     * Capacité bornée + `BufferOverflow.DROP_OLDEST` n'est pas dispo sur
     * `Channel(capacity)` standard — on utilise `trySend` qui retourne
     * `false` quand la file est pleine (drop silencieux).
     */
    private val outboundChannel: Channel<ByteArray> = Channel(capacity = outboundCapacity)

    /**
     * **L-5 (code-review Story 9.7)** : counter atomique pour éviter la race
     * sur l'incrément cross-thread (out-pump du Service + drain coroutine).
     * Lecture seule pour log debug — pas critique mais correct.
     */
    private val droppedNotConnectedCount = AtomicLong(0)
    private val droppedBackPressureCount = AtomicLong(0)

    /**
     * Compteurs Round 7 (2026-05-11) — quantifier le pump-in drop.
     * Hypothèse : si `inboundSink` (Channel capacity 256 côté Service) sature,
     * les paquets retour Go→Kotlin sont droppés ici silencieusement → côté
     * Go on incrémente rxBytesTotal (avant l'appel inbound) → le watchdog ne
     * voit pas le drop. Mais le user voit son tunnel zombie. Instrumentons.
     */
    private val droppedInboundSinkCount = AtomicLong(0)

    /**
     * Callback Go → Kotlin invoqué pour chaque paquet IP reçu du relais.
     * Enqueue dans le sink du Service via `trySend` (non-blocking + drop
     * silencieux si saturé — M-3).
     */
    private val packetCallback = PacketCallback { packet ->
        if (running.get()) {
            val sent = inboundSink.trySend(packet).isSuccess
            if (!sent) {
                val total = droppedInboundSinkCount.incrementAndGet()
                if (total % 100L == 0L) {
                    Log.i(TAG, "drop $total (inbound sink saturé — pump-in service trop lent)")
                }
            }
        }
        // Si !running, on drop (le Service a déjà appelé onTunnelStopped
        // mais la goroutine Go pourrait encore livrer un dernier paquet).
    }

    /**
     * Callback Go → Kotlin pour les transitions d'état. Le paramètre
     * `message` arrive déjà redacté par la facade Go (cf. fix H-1
     * `redactErrorForStatus` — classes canoniques type "pinning_failed"
     * plutôt que message brut avec URL/IP).
     *
     * Story 11.7-bis : `visibleIp` et `effectiveCountry` sont remplis
     * uniquement pour `state == "connected"`, vides pour autres transitions.
     * Forward au caller via `onStateChanged(state, visibleIp, effectiveCountry)`
     * pour permettre `notificationHelper.notify(state, country, ip, killStatus)`
     * côté Service avec données fraîches.
     */
    private val statusCallback = StatusCallback { state, message, visibleIp, effectiveCountry ->
        Log.i(TAG, "tunnel state: $state${if (message.isNotEmpty()) " — $message" else ""}")
        val vpnState = mapGoStateToVpnState(state)
        try {
            onStateChanged?.invoke(
                vpnState,
                visibleIp.takeIf { it.isNotEmpty() },
                effectiveCountry.takeIf { it.isNotEmpty() },
            )
        } catch (t: Throwable) {
            // Anti-réentrance : le callback ne doit JAMAIS lever d'exception
            // qui remonterait dans la goroutine pump (M-8 GoCoreAdapter).
            Log.w(TAG, "onStateChanged callback error (ignored): ${t.javaClass.simpleName}")
        }
    }

    private fun mapGoStateToVpnState(goState: String): VpnState =
        goStateToVpnState(goState)

    /**
     * Job de la coroutine de connect — permet d'attendre la complétion
     * (ou de cancel) si onTunnelStopped est appelé pendant le handshake.
     */
    @Volatile
    private var connectJob: Job? = null

    /**
     * Job de la coroutine drainant le outboundChannel vers
     * GoCoreAdapter.writePacket. Démarrée après connect réussi.
     */
    @Volatile
    private var drainJob: Job? = null

    override fun onTunnelStarted() {
        if (!tunnelStarted.compareAndSet(false, true)) {
            Log.w(TAG, "onTunnelStarted appelé alors que tunnelStarted=true — ignore")
            return
        }
        running.set(true)

        connectJob = scope.launch {
            // M-7 : setCallbacks est désormais suspend → mutex-protected.
            // Enregistrement AVANT le connect pour ne pas perdre les premiers
            // paquets ou transitions d'état.
            GoCoreAdapter.setCallbacks(packetCb = packetCallback, statusCb = statusCallback)

            val result = GoCoreAdapter.connect(relayDomain, pinnedKeyB64)
            result.onSuccess {
                // M-1 : démarre la coroutine drain (single-consumer du
                // outboundChannel). Drain en série → 1 acquisition mutex
                // amortie sur N paquets, vs 1 par paquet en pattern initial.
                drainJob = scope.launch { drainOutboundLoop() }
            }.onFailure { err ->
                // Connect a échoué — log la classe (release) et le message
                // complet en DEBUG uniquement (NFR-AND-9 : zéro PII en release,
                // mais le message brut côté Go est essentiel au debug local).
                Log.e(TAG, "GoCoreAdapter.connect failed: ${err.javaClass.simpleName}")
                if (fr.plateformeliberte.levoile.BuildConfig.DEBUG) {
                    Log.e(TAG, "GoCoreAdapter.connect debug message: ${err.message}", err)
                }
                running.set(false)
                GoCoreAdapter.setCallbacks(null, null)
            }
        }
    }

    /**
     * Boucle de drain : consomme outboundChannel + writePacket en série.
     * Termine quand le channel est fermé (close → exception ClosedReceiveChannelException).
     */
    @VisibleForTesting
    internal suspend fun drainOutboundLoop() {
        try {
            for (packet in outboundChannel) {
                if (!running.get()) break
                val res = GoCoreAdapter.writePacket(packet)
                res.onFailure { err ->
                    if (err is LeVoileCoreException) {
                        // ErrNotConnected attendu si session fermée entre poll et write.
                        droppedNotConnectedCount.incrementAndGet()
                        if (droppedNotConnectedCount.get() % 1000L == 0L) {
                            Log.d(TAG, "writePacket dropped (session fermée), total=${droppedNotConnectedCount.get()}")
                        }
                    } else {
                        Log.w(TAG, "writePacket erreur inattendue: ${err.javaClass.simpleName}")
                    }
                }
            }
        } catch (e: kotlinx.coroutines.channels.ClosedReceiveChannelException) {
            // Channel fermé → normal au shutdown.
        }
    }

    override fun onOutboundPacket(buf: ByteArray, length: Int) {
        if (!running.get()) {
            droppedNotConnectedCount.incrementAndGet()
            // Log.i pour visibilité debug (Nothing OS filtre Verbose+Debug).
            if (droppedNotConnectedCount.get() % 1000L == 0L) {
                Log.i(TAG, "drop ${droppedNotConnectedCount.get()} (session non ouverte)")
            }
            return
        }
        // Copy défensive : LeVoileVpnService réutilise `buf` à chaque
        // itération (allocation 0 dans la pompe). Sans copie, le ByteArray
        // passé au channel serait écrasé avant que le drain ne le consomme.
        val packet = buf.copyOf(length)
        val sent = outboundChannel.trySend(packet).isSuccess
        if (!sent) {
            // Channel plein → drop silencieux (TCP/QUIC retransmettront).
            //
            // R-T8 BISECT round 4 (2026-05-10) : seuil 100 + Log.i pour
            // diagnostiquer le bug "Facebook tue Twitch" sur 4G LTE. Sous
            // charge (50+ connexions parallèles, gros JS, images), le
            // drainOutboundLoop ne suit pas → drop massifs → SYN/ACK perdus
            // → tunnel apparaît zombie. Si on voit ces logs, augmenter
            // outboundCapacity (2048→8192) absorbe les bursts en attendant
            // un fix structurel (batch JNI writes, Phase 2).
            val total = droppedBackPressureCount.incrementAndGet()
            if (total % 100L == 0L) {
                Log.i(TAG, "drop $total (back-pressure outbound) — augmenter outboundCapacity ?")
            }
        }
    }

    override fun onTunnelStopped() {
        if (!tunnelStarted.compareAndSet(true, false)) {
            Log.w(TAG, "onTunnelStopped appelé alors que tunnelStarted=false — ignore")
            return
        }
        running.set(false)
        // Cancel le connect en cours s'il n'a pas fini.
        connectJob?.cancel()
        connectJob = null
        // Fermer outboundChannel pour que drainJob termine sa boucle for.
        outboundChannel.close()

        // M-5 : cleanup via NonCancellable + runBlocking pour GARANTIR la
        // séquence (disconnect + setCallbacks(null,null)) même si le scope
        // appelant est en train de se faire cancel (Service.onDestroy → scope
        // est dans NonCancellable side mais le caller pourrait être lui-même
        // mid-cancel). Coût : bloque le thread quelques ms le temps que Go
        // ferme la session — acceptable côté Service (lifecycle asynchrone géré
        // par Android, pas un thread UI).
        runBlocking {
            withContext(NonCancellable) {
                drainJob?.cancel()
                drainJob = null
                GoCoreAdapter.disconnect().onFailure { err ->
                    Log.w(TAG, "GoCoreAdapter.disconnect cleanup error: ${err.javaClass.simpleName}")
                }
                GoCoreAdapter.setCallbacks(null, null)
            }
        }
    }

    /**
     * À appeler depuis `LeVoileVpnService.onDestroy` (ou équivalent) pour
     * libérer définitivement les ressources de cette instance. Après
     * appel, ne plus utiliser cette instance.
     *
     * Idempotent — peut être appelé même après onTunnelStopped.
     */
    fun shutdown() {
        running.set(false)
        connectJob?.cancel()
        drainJob?.cancel()
        // Force-close du channel — idempotent même s'il a déjà été fermé.
        runCatching { outboundChannel.close() }
        scope.cancel()
    }

    /**
     * Métriques exposées pour monitoring/test (M-6 + L-5).
     */
    @VisibleForTesting
    internal fun metrics(): Metrics = Metrics(
        droppedNotConnected = droppedNotConnectedCount.get(),
        droppedBackPressure = droppedBackPressureCount.get(),
        running = running.get(),
        tunnelStarted = tunnelStarted.get(),
    )

    @VisibleForTesting
    internal data class Metrics(
        val droppedNotConnected: Long,
        val droppedBackPressure: Long,
        val running: Boolean,
        val tunnelStarted: Boolean,
    )

    companion object {
        private const val TAG = "GoBackedPacketRelay"
    }
}

/**
 * Code-review post-Epic 11 (M4) : mapping String → VpnState exposé `internal`
 * pour testabilité JVM-only. États Go canoniques émis par
 * `internal/tunnel/gomobile_facade.go.emitStatus` :
 *  - `"connecting"` → [VpnState.RECONNECTING] (handshake QUIC + /verify)
 *  - `"connected"`  → [VpnState.CONNECTED]
 *  - `"disconnected"` → [VpnState.DISCONNECTED]
 *  - `"error"`     → [VpnState.ERROR]
 *
 * Tout autre state → DISCONNECTED par défense (fail-safe).
 */
internal fun goStateToVpnState(goState: String): VpnState = when (goState) {
    "connected" -> VpnState.CONNECTED
    "connecting" -> VpnState.RECONNECTING
    "error" -> VpnState.ERROR
    "disconnected" -> VpnState.DISCONNECTED
    else -> VpnState.DISCONNECTED
}
