package fr.plateformeliberte.levoile.network

import android.annotation.SuppressLint
import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Handler
import android.os.HandlerThread
import android.os.ParcelFileDescriptor
import android.util.Log
import fr.plateformeliberte.levoile.bridge.GoCoreAdapter
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import java.net.DatagramSocket
import java.util.concurrent.atomic.AtomicLong

/**
 * R-T8 (2026-05-05) — QUIC Connection Migration côté Android.
 *
 * Détecte les changements d'underlying network (Wi-Fi <-> LTE, network
 * attach/detach) via [ConnectivityManager.NetworkCallback] et déclenche la
 * migration de la session QUIC vers le nouveau path sans coupure visible.
 *
 * **Workflow** (sur changement détecté) :
 *  1. `DatagramSocket()` ouvre un socket UDP éphémère.
 *  2. `network.bindSocket(socket)` le route via le nouveau réseau physique
 *     (et seulement celui-ci — pas de fallback sur le default).
 *  3. `vpnService.protect(socket)` l'exempte de la TUN (sinon le QUIC
 *     sortant serait aspiré par addRoute("0.0.0.0", 0) → boucle).
 *  4. `ParcelFileDescriptor.fromDatagramSocket().detachFd()` transfère
 *     l'ownership du fd à l'int qui sera consommé par Go.
 *  5. `GoCoreAdapter.migrate(fd)` invoque côté Go `MigrateGomobile` qui :
 *     - wrap fd dans `*quic.Transport`
 *     - `conn.AddPath(newTransport)` puis `path.Probe(ctx, 2s)` puis
 *       `path.Switch()` (RFC 9000 §9)
 *     - drain l'ancien transport sur 2s avant de le fermer
 *
 * **Sémantique de détection** :
 *  - `onAvailable(network)` : un nouveau réseau utilisable apparaît (Wi-Fi
 *    qui se connecte, LTE qui revient après tunnel souterrain). Migration
 *    immédiate.
 *  - `onLost(network)` : le réseau actuel disparaît. La migration via
 *    `onAvailable` du remplaçant gère le swap si un nouveau réseau est
 *    disponible. Si aucun, `onLost` n'est pas actionnable seul.
 *  - `onLinkPropertiesChanged(network, props)` : changement IP source ou
 *    routes (cell handoff partial avec changement DNS). Migration aussi —
 *    moins critique que onAvailable car cell handoff intra-LTE peut ne pas
 *    fire ici (CGNAT rotation reste invisible — le heartbeat /health côté
 *    Go est le filet de sécurité pour ces cas).
 *
 * **Threading** : NetworkCallback callbacks tournent par défaut sur le
 * thread interne du ConnectivityManager — on enregistre via une variante
 * avec `Handler` dédié pour avoir un thread connu et éviter les surprises
 * de synchronisation. Les appels `GoCoreAdapter.migrate` se font via
 * coroutine sur Dispatchers.IO pour ne pas bloquer le callback.
 *
 * **Faux positifs** : `onAvailable` peut être déclenché par l'OS Android
 * pour des raisons techniques (refresh capability, score change) sans
 * vraie nouvelle connectivité. Le path validation côté Go (PATH_CHALLENGE
 * avec timeout 2s) sert de garde-fou : si le "nouveau" path est en réalité
 * le même que l'ancien, le Probe réussit et on commute sans dommage. Si le
 * Probe timeout, la migration échoue et le tunnel continue sur l'ancien
 * path.
 *
 * **Cas non couverts ici (Phase 2)** :
 *  - Cell handoff intra-LTE sans changement de Network handle → invisible
 *    côté Android. Détecté par le heartbeat Go (/health 5s/2-fail) qui
 *    déclenche un reconnect classique.
 *  - CGNAT pool rotation pendant idle → idem.
 */
class NetworkMigrationCoordinator(
    private val vpnService: VpnService,
) {

    private val ctxApp: Context = vpnService.applicationContext
    private val cm: ConnectivityManager =
        ctxApp.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

    /**
     * HandlerThread dédié pour les callbacks NetworkCallback. Permet :
     *  - Threading déterministe (les callbacks ne tournent pas sur le main
     *    thread Service ni sur le thread interne ConnectivityManager).
     *  - Quitter proprement à `stop()` (`quitSafely()` draine les messages
     *    en attente).
     */
    private val callbackThread: HandlerThread = HandlerThread("levoile-netcb").apply { start() }
    private val callbackHandler: Handler = Handler(callbackThread.looper)

    /**
     * Scope coroutine pour les appels async à `GoCoreAdapter.migrate` —
     * sortis du thread NetworkCallback pour ne pas bloquer le dispatcher
     * système si la migration QUIC prend du temps (~1-2s typique).
     */
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /**
     * Compteur de migrations tentées (succès + échec). Exposé pour debug
     * et observabilité runtime via reflection (tests JVM-only).
     */
    private val migrationsAttempted = AtomicLong(0L)
    private val migrationsSucceeded = AtomicLong(0L)
    private val migrationsFailed = AtomicLong(0L)

    @Volatile
    private var registered: Boolean = false

    @Volatile
    private var lastMigrationJob: Job? = null

    /**
     * R-T8 fix-bis (2026-05-05) — `armed` empêche les migrations spurieuses
     * pendant les premières secondes après [start].
     *
     * Pourquoi : `ConnectivityManager.registerNetworkCallback` déclenche
     * IMMÉDIATEMENT `onAvailable` pour TOUS les networks existants qui
     * matchent (LTE actif, Wi-Fi actif, etc.). C'est by design — l'app
     * doit pouvoir "découvrir" l'état actuel. Mais sans garde, on tente
     * une migration QUIC sur un tunnel qui vient d'être établi → ça le
     * casse (test device : tunnel mort dès la première connexion).
     *
     * Solution : `armed=false` au start, post-delayed `armed=true` après
     * 2s. Pendant 2s, les events sont ignorés (l'état initial est connu).
     * Après 2s, les events sont de vrais changements → migration légitime.
     *
     * Trade-off : un VRAI changement réseau dans les 2 premières secondes
     * de la session est manqué — Phase 2 si critique. En pratique rarissime
     * (rare de cell-handover juste après ouverture VPN).
     */
    @Volatile
    private var armed: Boolean = false

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            // Évite les races : si on est en cours de teardown, ignore.
            if (!registered) return
            // Initial-event skip : le premier batch d'onAvailable post-register
            // décrit l'état COURANT, pas un changement → tunnel marche déjà
            // sur ce network, pas la peine de migrer.
            if (!armed) return
            // Cell handoff / Wi-Fi reconnect / network attach.
            scheduleMigration(network, reason = "onAvailable")
        }

        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
            // IP source ou routes changent — peut être un cell handoff
            // partial (l'OS garde le même Network handle mais l'IP change).
            // Migration utile si l'underlying socket est lié à l'ancienne IP.
            //
            // ATTENTION : cet event peut fire FRÉQUEMMENT (ajout/retrait
            // route, DNS changement). On y limite via debounce implicite :
            // chaque migration en cours est trackée via lastMigrationJob ;
            // si elle est encore active, on skip — sinon on lance.
            if (!registered) return
            if (!armed) return // initial-event skip (cf. onAvailable)
            if (lastMigrationJob?.isActive == true) {
                Log.d(TAG, "onLinkPropertiesChanged: migration en cours, skip")
                return
            }
            scheduleMigration(network, reason = "onLinkPropertiesChanged")
        }

        override fun onLost(network: Network) {
            // Pas d'action seule — onAvailable du remplaçant (s'il y en a un)
            // déclenchera la migration vers le nouveau path. Si pas de
            // remplaçant, le tunnel continuera à essayer sur l'ancien socket
            // et le heartbeat Go finira par tripper Disconnected.
            //
            // On ne migre PAS ici car on n'a pas de Network alternatif à
            // offrir à `network.bindSocket`.
            Log.i(TAG, "onLost: no actionable migration (waiting for replacement onAvailable)")
        }
    }

    /**
     * Démarre la coordination. Idempotent : appels multiples sans `stop()`
     * intermédiaire sont des no-op après le premier.
     *
     * Filtre `NetworkRequest` :
     *  - `TRANSPORT_CELLULAR` + `TRANSPORT_WIFI` : on observe les deux types
     *    physiques. Pas de TRANSPORT_VPN (sinon on observerait notre propre
     *    interface tun0 → boucle).
     *  - `NET_CAPABILITY_INTERNET` : on ne veut que les réseaux utilisables.
     *    Filtre les profils sans Internet (captive portal en cours de
     *    validation, etc.).
     *
     * `registerDefaultNetworkCallback` aurait été plus simple mais inclurait
     * TRANSPORT_VPN et déclencherait sur nos propres changements de tunnel.
     */
    fun start() {
        if (registered) return
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        try {
            cm.registerNetworkCallback(request, callback, callbackHandler)
            registered = true
            // Arm les migrations après 2s — le temps que l'OS livre tous
            // les `onAvailable` initiaux (snapshot de l'état courant qu'on
            // veut ignorer, pas migrer dessus).
            callbackHandler.postDelayed({
                armed = true
                Log.i(TAG, "NetworkMigrationCoordinator armed — migrations now active")
            }, ARM_DELAY_MS)
            Log.i(TAG, "NetworkCallback registered (transports=cellular+wifi)")
        } catch (t: Throwable) {
            Log.e(TAG, "registerNetworkCallback failed", t)
        }
    }

    /**
     * Arrête la coordination. Idempotent. Cancel toutes les coroutines
     * de migration en cours. Quit le HandlerThread.
     */
    fun stop() {
        if (!registered) {
            // HandlerThread peut quand même être actif (start() partial).
            try { callbackThread.quitSafely() } catch (_: Throwable) {}
            return
        }
        registered = false
        try {
            cm.unregisterNetworkCallback(callback)
            Log.i(TAG, "NetworkCallback unregistered")
        } catch (t: Throwable) {
            Log.w(TAG, "unregisterNetworkCallback failed (ignored)", t)
        }
        try { scope.cancel() } catch (_: Throwable) {}
        try { callbackThread.quitSafely() } catch (_: Throwable) {}
    }

    /**
     * Compteurs migrations — exposés pour observabilité tests JVM-only +
     * debug. Format : (attempted, succeeded, failed).
     */
    fun stats(): Triple<Long, Long, Long> = Triple(
        migrationsAttempted.get(),
        migrationsSucceeded.get(),
        migrationsFailed.get(),
    )

    /**
     * Construit le socket UDP protégé bindé au `network` cible et déclenche
     * `GoCoreAdapter.migrate(fd)` en coroutine. Bloque le NetworkCallback
     * uniquement le temps de la création + bind + protect (rapide ~1ms) ;
     * la migration QUIC elle-même tourne async sur Dispatchers.IO.
     */
    @SuppressLint("Recycle") // socket lifecycle géré par PFD.detachFd → Go
    private fun scheduleMigration(network: Network, reason: String) {
        migrationsAttempted.incrementAndGet()
        val socket: DatagramSocket = try {
            DatagramSocket()
        } catch (t: Throwable) {
            Log.e(TAG, "scheduleMigration: DatagramSocket() failed (reason=$reason)", t)
            migrationsFailed.incrementAndGet()
            return
        }

        try {
            // Route via le nouveau réseau physique. SANS ce bind, le socket
            // utiliserait la default route — qui peut être notre TUN active
            // → boucle infinie où QUIC tente de sortir via la TUN qui dépend
            // de QUIC.
            network.bindSocket(socket)
        } catch (t: Throwable) {
            Log.e(TAG, "scheduleMigration: bindSocket failed (reason=$reason)", t)
            try { socket.close() } catch (_: Throwable) {}
            migrationsFailed.incrementAndGet()
            return
        }

        // Exempte de la TUN. Sans ça, addRoute("0.0.0.0", 0) aspirerait le
        // QUIC sortant dans la TUN → drop (la TUN ne peut pas se servir
        // elle-même). protect() ajoute le socket à la liste blanche kernel.
        if (!vpnService.protect(socket)) {
            Log.e(TAG, "scheduleMigration: VpnService.protect failed (reason=$reason)")
            try { socket.close() } catch (_: Throwable) {}
            migrationsFailed.incrementAndGet()
            return
        }

        // Transfert d'ownership du fd vers Go. fromDatagramSocket dup le fd
        // interne du socket dans un PFD ; detachFd retire l'ownership du PFD.
        // Après detachFd, l'int retourné est la seule référence vivante au fd.
        val fd: Int = try {
            val pfd: ParcelFileDescriptor = ParcelFileDescriptor.fromDatagramSocket(socket)
            pfd.detachFd()
        } catch (t: Throwable) {
            Log.e(TAG, "scheduleMigration: fromDatagramSocket/detachFd failed (reason=$reason)", t)
            try { socket.close() } catch (_: Throwable) {}
            migrationsFailed.incrementAndGet()
            return
        }

        // Note : on close le socket Java après detachFd. Le fd interne du
        // socket est déjà invalide (transféré au PFD puis détaché), donc
        // socket.close() est essentiellement un cleanup d'état Java sans
        // toucher au fd. Si on omet ce close, le finalizer s'en charge
        // éventuellement — mais on reste explicit pour le GC pressure.
        try { socket.close() } catch (_: Throwable) {}

        // Lance la migration QUIC en async. Ownership du fd transféré à Go.
        // En cas d'échec, MigrateGomobile ferme le fd avant retour.
        lastMigrationJob = scope.launch {
            val result = GoCoreAdapter.migrate(fd)
            if (result.isSuccess) {
                migrationsSucceeded.incrementAndGet()
                Log.i(TAG, "QUIC migration ok (reason=$reason)")
            } else {
                migrationsFailed.incrementAndGet()
                // Note pas de log de l'erreur détaillée (peut contenir IP relais)
                Log.w(TAG, "QUIC migration failed (reason=$reason): ${result.exceptionOrNull()?.javaClass?.simpleName}")
            }
        }
    }

    companion object {
        private const val TAG = "NetMigrationCoord"

        /**
         * Délai avant que les migrations soient autorisées. Couvre la
         * livraison du snapshot d'état initial par `ConnectivityManager`
         * (un onAvailable par network actif au moment du register). 2s est
         * généreux : en pratique l'OS livre ces events en quelques ms.
         */
        private const val ARM_DELAY_MS = 2_000L
    }
}
