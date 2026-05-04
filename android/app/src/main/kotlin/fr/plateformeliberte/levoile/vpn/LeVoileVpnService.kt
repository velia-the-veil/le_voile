package fr.plateformeliberte.levoile.vpn

import android.app.Service
import android.content.Intent
import android.net.VpnService
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.util.Log
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.registry.RegistryLoader
import fr.plateformeliberte.levoile.registry.RelayPicker
import fr.plateformeliberte.levoile.ui.NotificationHelper
import fr.plateformeliberte.levoile.ui.VpnState
import fr.plateformeliberte.levoile.vpn.VpnConstants.ACTION_CONNECT
import fr.plateformeliberte.levoile.vpn.VpnConstants.ACTION_DISCONNECT
import fr.plateformeliberte.levoile.vpn.VpnConstants.MAX_IP_PACKET
import fr.plateformeliberte.levoile.vpn.VpnConstants.NOTIF_ID
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.ClosedReceiveChannelException
import kotlinx.coroutines.runBlocking
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Service VPN Le Voile — etend android.net.VpnService.
 *
 * Story 9.4 livre :
 *   - Creation TUN via VpnService.Builder (MTU 1420 + route 0.0.0.0/0 + ::/0
 *     + DNS 10.6.6.1).
 *   - Pumps Kotlin lecture (fd -> PacketRelay) et ecriture (sink -> fd) en
 *     threads daemon.
 *   - Foreground Service avec notification stub (channel "levoile_vpn_status_stub").
 *   - Lifecycle minimal : ACTION_CONNECT / ACTION_DISCONNECT / onRevoke / onDestroy.
 *
 * Story 9.6 livre (modif de cette classe) :
 *   - Channel "levoile_vpn_status_stub" supprime au profit de "levoile_vpn_status"
 *     (gere par NotificationHelper.ensureChannel() — cleanup legacy idempotent).
 *   - Notification stub remplacee par NotificationHelper.build(VpnState) avec
 *     titre dynamique « Le Voile · {Etat} », icone ic_levoile_status, action
 *     « Deconnecter » (PendingIntent.getService FLAG_IMMUTABLE), tap -> MainActivity.
 *   - Notification VpnState.DISCONNECTED postee juste avant stopForeground
 *     dans disconnectInternal (visible logcat + UI le temps d'un repaint).
 *
 * Story 9.5 livre (modif de cette classe) :
 *   - Reference singleton @Volatile internal var instance accessible cross-thread
 *     (utile a Story 11.2 si elle veut interroger l'etat depuis le bridge JS).
 *   - Delai 5 s avant stopForeground(STOP_FOREGROUND_REMOVE) + stopSelf via
 *     Handler(Looper.getMainLooper()).postDelayed — UX cherchee : laisser la
 *     notif DISCONNECTED visible le temps que l'utilisateur voie la fermeture
 *     (eviter clignotement notif present/absent sur reconnect rapide).
 *   - cleanupSync() extrait pour idempotence : appele depuis disconnectInternal
 *     (post delayed teardown) ET depuis onDestroy (teardown immediat synchrone,
 *     evite race "stopForeground sur Service detruit"). Appels multiples sont
 *     no-op apres le premier (vpnInterface = null, tunnelStartedFired = false).
 *   - MainActivity helpers requestVpnStart/Stop livres (dormants jusqu'a
 *     Story 11.2 qui cablera depuis LeVoileBridge.connect()/disconnect()).
 * Story 11.7-bis (livree) : bascule effective NoOpPacketRelay -> GoBackedPacketRelay
 * via le factory provideRelay(country) — handshake QUIC/HTTP3 via gomobile +
 * stream /tunnel + transitions d'etat reelles (RECONNECTING/ERROR) cablees
 * via le callback onStateChanged(state, visibleIp, effectiveCountry) qui
 * forwarde vers notificationHelper.notify(...) avec donnees fraiches.
 *
 * Dette residuelle Phase 2 (cohérent NFR9-IPv6-out-of-tunnel) :
 *   - addAddress IPv6 (fd00:6:6::2) + IPv6 routing dans le tunnel — actuellement
 *     v6 hors tunnel (cf. commentaire `addRoute("::", 0)` dans connectInternal).
 *   - setBlocking false + detachFd Go pour optimiser le pump (findings M-6/M-7).
 *
 * Hors scope definitif : split tunneling per-app (architecture.md l. 469, Phase 2).
 */
class LeVoileVpnService : VpnService() {

    // Etat interne — accede depuis le main thread Service (onCreate, onStartCommand,
    // onRevoke, onDestroy) et depuis les threads pumps.
    //
    // @Volatile : garantit la visibilite cross-thread sans verrou explicite (fix L-8
    // code-review post-9.4). Le sequencement happens-before reste fourni par
    // running.set(true/false) (AtomicBoolean = full memory barrier) avant/apres
    // chaque transition lifecycle.
    @Volatile
    private var vpnInterface: ParcelFileDescriptor? = null
    @Volatile
    private var outPumpThread: Thread? = null
    @Volatile
    private var inPumpThread: Thread? = null
    @Volatile
    private var tunnelStartedFired: Boolean = false

    private val running = AtomicBoolean(false)

    /**
     * Story 11.7-bis : Channel<ByteArray> remplace ConcurrentLinkedQueue
     * pour permettre back-pressure bornée + interruption propre via close().
     * Capacité 256 paquets ≈ 360 KB max in-flight (cohérent
     * GoBackedPacketRelay outboundCapacity). Recréé à chaque connectInternal
     * car Channel.close() est terminal — un Channel fermé refuse send/receive.
     */
    @Volatile
    private var packetSink: Channel<ByteArray> = Channel(capacity = 256)

    /**
     * Story 11.7-bis : `packetRelay` est désormais construit dans
     * [connectInternal] où `currentCountry` est connu (post-onStartCommand).
     *
     * Si le registry charge OK + un relais est sélectionné pour le pays
     * demandé → [GoBackedPacketRelay] (tunnel réel).
     * Sinon → [NoOpPacketRelay] fallback gracieux (UX dégradée mais pas de crash).
     *
     * `lateinit` (et non `val provideRelay()`) car la construction nécessite
     * un suspend `RegistryLoader.load()` qui doit être appelée après onCreate.
     */
    private lateinit var packetRelay: PacketRelay

    // Story 9.6 : orchestrateur unique notification persistante (channel +
    // builder + PendingIntents). Lifecycle Service = lateinit dans onCreate
    // (pas by lazy : onCreate est l'endroit canonique d'init Service Android).
    private lateinit var notificationHelper: NotificationHelper

    // Story 11.7 — état enrichi consommé par NotificationHelper.notify(state, country, ip, killStatus).
    // Lecture/écriture cross-thread (onStartCommand main, pumps daemon, callbacks Story 9.7).
    @Volatile
    private var currentCountry: String? = null
    /**
     * Story 11.7-bis : `currentIp` est mis à jour depuis le callback
     * `onStateChanged(state, visibleIp, effectiveCountry)` du
     * [GoBackedPacketRelay] — résolu via `net.LookupIP(relayDomain)` côté Go
     * facade au moment de la transition `connected`. Reste `null` tant que la
     * transition n'a pas eu lieu (pre-handshake, ou si DNS lookup échoue —
     * fallback "—" UX-side dans la notification).
     */
    @Volatile
    private var currentIp: String? = null
    @Volatile
    private var currentKillStatus: fr.plateformeliberte.levoile.kill.KillSwitchStatus? = null

    private lateinit var killSwitchDetector: fr.plateformeliberte.levoile.kill.KillSwitchDetector

    // Story 9.5 : handler main-thread pour le teardown differe 5 s.
    // lateinit + creation onCreate = seul site d'init valide pour un Service
    // Android (le constructeur Service ne doit pas allouer de ressources OS).
    // removeCallbacksAndMessages(null) appele dans onDestroy + en debut de
    // disconnectInternal pour eviter empilement de runnables orphelins.
    private lateinit var teardownHandler: Handler

    // ---------- Lifecycle Service ----------

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "onCreate")
        // Story 9.5 : reference singleton + handler teardown init avant tout
        // autre travail (notamment avant ensureChannel qui pourrait poster).
        instance = this
        teardownHandler = Handler(Looper.getMainLooper())
        // Story 9.6 : NotificationHelper.ensureChannel() cree le channel final
        // "levoile_vpn_status" ET supprime le channel stub legacy 9.4
        // "levoile_vpn_status_stub" (idempotent).
        notificationHelper = NotificationHelper(this)
        notificationHelper.ensureChannel()
        // Story 11.7 — KillSwitchDetector pour enrichir la notification
        // (alerte « ⚠️ Kill switch inactif · Activer » si détecté Inactive).
        killSwitchDetector = fr.plateformeliberte.levoile.kill.KillSwitchDetector(applicationContext)
        killSwitchDetector.refresh()
        currentKillStatus = killSwitchDetector.status.value
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Fix H-1 + H-2 + H-3 (code-review post-9.4) : startForeground IMMEDIATEMENT
        // (avant tout traitement) pour respecter la regle Android 8+ « < 5s apres
        // onStartCommand sinon ANR + kill ». Couvre les 3 chemins :
        //   a) ACTION_CONNECT  : startForeground avant builder.establish() qui peut
        //      bloquer plusieurs secondes (consent dialog, propagation route OEM lent).
        //   b) ACTION_DISCONNECT : si l'UI/notif declenche disconnect via
        //      startForegroundService(), il faut quand meme avoir startForeground
        //      avant le stopForeground qui suit (sinon crash).
        //   c) intent null ou action inconnue (redelivery post-crash via
        //      START_REDELIVER_INTENT sans dernier intent enregistre) : pareil.
        //
        // L'invocation est idempotente : startForeground(NOTIF_ID, ...) plusieurs fois
        // sur la meme Notification met simplement a jour la notif (pas de churn).
        //
        // Fix M-2 (code-review post-9.5) : selectionne l'etat initial selon
        // l'action. Sur ACTION_CONNECT : CONNECTED (intention utilisateur, le
        // wiring temps-reel RECONNECTING/ERROR appartient a Story 9.7). Sur
        // ACTION_DISCONNECT ou action inconnue : DISCONNECTED — evite un flash
        // visuel CONNECTED -> DISCONNECTED pendant les 5 s de teardown differe
        // (cleanupSync notifie ensuite DISCONNECTED, qui devient un no-op visuel
        // puisque deja affiche).
        val action = intent?.action
        // Always-On VPN : Android lance le service via SERVICE_INTERFACE
        // ("android.net.VpnService") quand l'utilisatrice a active "VPN
        // permanent" dans Settings. Le manifest declare deja le filtre
        // <action android:name="android.net.VpnService"/> sur ce Service ;
        // ne pas traiter l'action ici brisait le contrat -> Android refusait
        // d'enregistrer le pinning -> kill switch inconfigurable (FR-AND-2).
        // Coherent avec le contrat framework Android : un Always-On start
        // equivaut a une demande de connexion, EXTRA_COUNTRY absent
        // (intent systeme) -> fallback ConfigStore.preferredCountry.
        val isConnectStart = action == ACTION_CONNECT || action == VpnService.SERVICE_INTERFACE

        val initialState = if (isConnectStart) VpnState.CONNECTED else VpnState.DISCONNECTED
        // Story 11.7 — récupère le pays depuis l'Intent EXTRA_COUNTRY (transmis
        // par MainActivity.startVpnService Story 9.5). Fallback : ConfigStore
        // preferredCountry (Story 11.8) si EXTRA_COUNTRY absent (cas Always-On
        // ou ACTION_CONNECT sans extra).
        if (isConnectStart) {
            val extraCountry = intent?.getStringExtra(VpnConstants.EXTRA_COUNTRY)
            currentCountry = extraCountry ?: try {
                fr.plateformeliberte.levoile.config.ConfigStore(applicationContext)
                    .load().preferredCountry
            } catch (_: Throwable) {
                null
            }
            // Refresh kill switch status au connect (re-vérification heuristique).
            killSwitchDetector.refresh()
            currentKillStatus = killSwitchDetector.status.value
        }
        startForeground(
            NOTIF_ID,
            notificationHelper.build(initialState, currentCountry, currentIp, currentKillStatus)
        )
        Log.i(TAG, "onStartCommand action=$action startId=$startId")
        when {
            isConnectStart -> {
                if (action == VpnService.SERVICE_INTERFACE) {
                    Log.i(TAG, "onStartCommand: demarrage Always-On VPN (intent systeme) -> connectInternal()")
                }
                connectInternal()
            }
            action == ACTION_DISCONNECT -> disconnectInternal()
            else -> {
                Log.w(TAG, "onStartCommand action inconnue ou null — cleanup + stopSelf")
                // On a startForeground'e juste au-dessus (defensif) ; il faut donc
                // un cleanup symetrique avant de rendre la main au systeme.
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
            }
        }
        // Coherent architecture.md l. 1051 : crash -> relance avec dernier intent.
        return Service.START_REDELIVER_INTENT
    }

    override fun onRevoke() {
        Log.i(TAG, "onRevoke — utilisateur a revoque le consentement VpnService")
        disconnectInternal()
        super.onRevoke()
    }

    override fun onDestroy() {
        Log.i(TAG, "onDestroy — cleanup final (idempotent)")
        // Story 9.5 : ANNULE le runnable de teardown differe (s'il existe encore)
        // pour eviter qu'il s'execute apres super.onDestroy() — un appel a
        // stopForeground/stopSelf sur un Service detruit est UB (no-op au
        // mieux, IllegalStateException au pire selon API).
        if (::teardownHandler.isInitialized) {
            teardownHandler.removeCallbacksAndMessages(null)
        }
        // Cleanup synchrone immediat (PAS via disconnectInternal qui posterait
        // un nouveau runnable de teardown). Idempotent vs cleanup deja fait.
        cleanupSync()
        try {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } catch (t: Throwable) {
            Log.w(TAG, "stopForeground in onDestroy error (ignored)", t)
        }
        // Reset des etats Always-On capturees — sans Service vivant ces valeurs
        // ne sont plus credibles pour [KillSwitchDetector].
        lastKnownAlwaysOn = false
        lastKnownLockdown = false
        instance = null
        super.onDestroy()
    }

    /**
     * Capture l'etat Always-On + Lockdown via l'API publique officielle
     * Android (`VpnService.isAlwaysOn()` / `isLockdownEnabled()` — API 29+,
     * minSdk Le Voile = 29). Source de verite officielle — remplace
     * l'heuristique `Settings.Global` qui a ete restreinte aux apps tierces
     * sur Android 16+.
     *
     * `isAlwaysOn()` retourne `true` uniquement quand le Service tourne dans
     * un contexte Always-On (utilisatrice a coche "VPN permanent" dans
     * Settings -> VPN). Sur un demarrage classique via `ACTION_CONNECT`
     * depuis l'UI, `isAlwaysOn()` retourne `false` -> kill switch correctement
     * marque comme `Inactive`.
     *
     * Appele apres [Builder.establish] reussi (la doc Android precise que
     * `isAlwaysOn` retourne `false` jusqu'a ce que `establish()` reussisse)
     * ET re-appele explicitement par [fr.plateformeliberte.levoile.kill.KillSwitchDetector.refresh]
     * a chaque verification — necessaire car l'utilisatrice peut basculer
     * le toggle "Bloquer les connexions sans VPN" dans Settings sans que
     * le Service redemarre, laissant la capture statique stale.
     */
    internal fun captureAlwaysOnState() {
        try {
            lastKnownAlwaysOn = isAlwaysOn
            lastKnownLockdown = isLockdownEnabled
            Log.i(TAG, "captureAlwaysOnState: isAlwaysOn=$lastKnownAlwaysOn isLockdownEnabled=$lastKnownLockdown")
        } catch (t: Throwable) {
            // Defense en profondeur : meme si l'API est documentee API 29+,
            // une OEM pourrait theoriquement throw. Ne PAS bloquer le service.
            Log.w(TAG, "captureAlwaysOnState error (ignored)", t)
        }
    }

    // ---------- Connect / Disconnect ----------

    private fun connectInternal() {
        // Fix H-1 (code-review post-9.5) : annule tout runnable de teardown
        // differe encore pending. Sans ca, la sequence disconnect -> reconnect
        // rapide (< 5 s) laisse le runnable orphelin executer apres reconnect ;
        // stopForeground(STOP_FOREGROUND_REMOVE) demote alors le Service redevenu
        // actif en background -> exposition au kill OS en battery save.
        if (::teardownHandler.isInitialized) {
            teardownHandler.removeCallbacksAndMessages(null)
        }
        if (vpnInterface != null) {
            Log.w(TAG, "connectInternal appelee alors que vpnInterface != null — ignore")
            return
        }

        // Story 11.7-bis : (re)créer un Channel fresh — un précédent close()
        // l'aurait rendu inutilisable. Construit AVANT le packetRelay car ce
        // dernier consomme inboundSink en référence.
        packetSink = Channel(capacity = 256)

        // Story 11.7-bis : construire le packetRelay maintenant que
        // currentCountry est connu (post-onStartCommand).
        packetRelay = provideRelay(currentCountry)
        val builder = Builder()
            .setSession(getString(R.string.vpn_session_name))
            // addAddress AVANT addRoute : certains OEM (Xiaomi MIUI, Huawei EMUI)
            // retournent silencieusement null sur establish() si l'ordre est inverse.
            .addAddress("10.6.6.2", 32)
            .addRoute("0.0.0.0", 0)
            // Fix M-7 (code-review post-9.4) : addRoute("::", 0) sans addAddress
            // IPv6 correspondant signifie qu'aucun paquet IPv6 n'atteindra la TUN
            // (le kernel n'a pas de source v6 sur l'interface). C'est INTENTIONNEL
            // — comportement « v6 hors tunnel » equivalent NFR9-IPv6-out-of-tunnel
            // desktop : les paquets v6 utilisateur sortent par l'interface
            // physique normale (pas leak car le routing system v4 par defaut va
            // dans le tunnel).
            //
            // Pour activer IPv6 dans le tunnel (Phase 2) : ajouter
            // `.addAddress("fd00:6:6::2", 64)` (ULA Le Voile) AVANT cette route.
            // Necessitera coordination avec internal/tunnel pour que le pump
            // accepte les paquets v6 + relais qui resolvent dual-stack.
            .addRoute("::", 0)
            .addDnsServer("10.6.6.1")
            .setMtu(1420)
            .setBlocking(true)
            .setUnderlyingNetworks(null)

        val pfd = builder.establish()
        if (pfd == null) {
            Log.e(
                TAG,
                "VpnService.establish() returned null — consentement non accorde ou route invalide"
            )
            // tunnelStartedFired reste false : disconnectInternal (via onDestroy
            // suite a stopSelf) ne declenchera donc PAS onTunnelStopped (fix M-5).
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return
        }
        vpnInterface = pfd

        // Source de verite officielle Always-On (API 29+) : la doc Android
        // [VpnService.isAlwaysOn] precise que isAlwaysOn() / isLockdownEnabled()
        // retournent `false` jusqu'a ce que [Builder.establish] reussisse.
        // Capture donc IMPERATIVEMENT apres l'attribution `vpnInterface = pfd`.
        // Necessaire car Android 16+ a retire l'acces aux apps tierces a
        // Settings.{Global,Secure}.always_on_vpn_*. Lu par [KillSwitchDetector].
        captureAlwaysOnState()

        // Fix M-5 (code-review post-9.4) : on flag avant d'appeler onTunnelStarted
        // pour que disconnectInternal sache si le contrat lifecycle a ete declenche.
        tunnelStartedFired = true
        try {
            // packetRelay garanti initialisé en début de connectInternal (Story 11.7-bis).
            packetRelay.onTunnelStarted()
        } catch (t: Throwable) {
            Log.w(TAG, "packetRelay.onTunnelStarted error", t)
        }
        startPumpThreads(pfd)
        // startForeground deja invoque au top de onStartCommand (defense ANR).
        Log.i(TAG, "connectInternal: tunnel cree, pumps demarres, foreground actif")
    }

    private fun disconnectInternal() {
        // Story 9.5 : idempotence du teardown differe — annule tout runnable
        // pendant avant d'en re-poster un. Sinon double-DISCONNECT (ex.
        // utilisateur re-tape l'action dans la fenetre 5 s) empilerait deux
        // stopForeground+stopSelf qui ferait crash sur Service deja stoppe.
        if (::teardownHandler.isInitialized) {
            teardownHandler.removeCallbacksAndMessages(null)
        }

        // Cleanup synchrone immediat (pumps + fd + relay + notif DISCONNECTED).
        // Apres ca, le tunnel est ferme cote utilisateur — l'attente 5 s ne
        // concerne QUE le retrait visuel de la notif et l'arret du Service.
        cleanupSync()

        // Story 9.5 : delai STOP_FOREGROUND_DELAY_MS avant retrait notif + stopSelf.
        //   - UX : la notif DISCONNECTED reste visible 5 s, l'utilisatrice voit
        //     bien le changement d'etat avant disparition.
        //   - Anti-clignotement : si reconnect rapide pendant ces 5 s, le runnable
        //     est annule (par le prochain disconnectInternal/cleanupSync de
        //     connectInternal indirectement, ou explicitement par appel direct).
        //   - Cleanup garde-fou onDestroy : si Android detruit le Service avant
        //     les 5 s (ex. force-stop), onDestroy.removeCallbacksAndMessages(null)
        //     annule le runnable -> pas de stopForeground sur instance morte.
        teardownHandler.postDelayed({
            try {
                stopForeground(STOP_FOREGROUND_REMOVE)
            } catch (t: Throwable) {
                Log.w(TAG, "stopForeground in delayed teardown error (ignored)", t)
            }
            stopSelf()
            Log.i(TAG, "disconnectInternal: notif retiree + stopSelf (apres delai ${STOP_FOREGROUND_DELAY_MS}ms)")
        }, STOP_FOREGROUND_DELAY_MS)

        Log.i(TAG, "disconnectInternal: tunnel ferme, pumps arretes ; teardown notif/service prevu dans ${STOP_FOREGROUND_DELAY_MS}ms")
    }

    /**
     * Story 9.5 : cleanup synchrone extrait de l'ancien disconnectInternal.
     * Appele depuis disconnectInternal (avec teardown differe ensuite) ET
     * depuis onDestroy (sans teardown differe — le Service est deja en cours
     * de destruction).
     *
     * Sur appels multiples : la majorite des operations sont reellement no-op
     * apres le premier appel (vpnInterface deja null, tunnelStartedFired
     * deja false, threads deja interrupted, packetSink deja vide). Fix M-3 +
     * L-5 (code-review post-9.5) : `notify(DISCONNECTED)` n'est postee QUE si
     * le tunnel etait effectivement actif (`wasActive`) — sans ca, un appel
     * cleanupSync sur un Service qui n'a jamais connecte (ex. requestVpnStop
     * sur Service idle, AC #1 path action inconnue) re-poste inutilement la
     * notif puis la retire 5 s plus tard, ce qui clignote pour l'utilisatrice.
     */
    private fun cleanupSync() {
        // Capture l'etat AVANT cleanup pour decider si on notifie DISCONNECTED.
        val wasActive = vpnInterface != null || tunnelStartedFired
        running.set(false)
        outPumpThread?.interrupt()
        inPumpThread?.interrupt()
        outPumpThread = null
        inPumpThread = null
        // Story 11.7-bis : fermer le Channel proprement — le inPumpThread
        // levera ClosedReceiveChannelException et break out de sa boucle.
        try { packetSink.close() } catch (_: Throwable) {}
        // Fix M-5 (code-review post-9.4) : ne notifier que si onTunnelStarted a
        // effectivement ete appele — sinon GoBackedPacketRelay (Story 9.7) verrait
        // un "close before open" inconsistant.
        // Story 11.7-bis : packetRelay est lateinit, guard supplémentaire.
        //
        // Code-review post-11.7-bis (H-8) : onTunnelStopped() appelé AVANT
        // shutdown(). onTunnelStopped fait `runBlocking + NonCancellable` qui
        // appelle GoCoreAdapter.disconnect() — c'est OBLIGATOIRE pour fermer la
        // session côté Go. shutdown() seul cancel le scope mais NE FAIT PAS
        // disconnect — l'inverser laissait la session Go ouverte sur reconnect
        // rapide (ErrSessionAlreadyOpen).
        if (tunnelStartedFired && ::packetRelay.isInitialized) {
            try {
                packetRelay.onTunnelStopped()
            } catch (t: Throwable) {
                Log.w(TAG, "packetRelay.onTunnelStopped error", t)
            }
            tunnelStartedFired = false
        }
        // shutdown() après onTunnelStopped pour libérer le scope coroutine
        // résiduel (idempotent vs cancellations déjà faites).
        if (::packetRelay.isInitialized && packetRelay is GoBackedPacketRelay) {
            try { (packetRelay as GoBackedPacketRelay).shutdown() }
            catch (t: Throwable) { Log.w(TAG, "GoBackedPacketRelay.shutdown error", t) }
        }
        try {
            // Fix M-6 (code-review post-9.4) : on close ICI le ParcelFileDescriptor
            // qui possede le fd. Les FileInputStream/FileOutputStream du pump
            // partagent ce fd ; leur prochaine read()/write() leve IOException
            // (EBADF) qui est catched dans le pump (cleanup en cascade).
            // Si une race fd-reuse devient un probleme reel sous OOM kill,
            // basculer Story 9.7 sur pfd.detachFd() + ParcelFileDescriptor.adoptFd()
            // pour donner ownership exclusif au pump (architecture.md l. 604).
            vpnInterface?.close()
        } catch (e: IOException) {
            Log.w(TAG, "vpnInterface.close error", e)
        }
        vpnInterface = null
        // Story 9.6 : poste explicitement l'etat DISCONNECTED juste avant que
        // le retrait de la notif soit declenche (ici par le runnable differe
        // 5 s plus tard, ou immediatement dans onDestroy). Visible le temps
        // d'un repaint UI + 5 s + trace dans logcat — utile au debug pour
        // confirmer que la branche "happy path" a ete prise.
        // notificationHelper est lateinit et garanti initialise par onCreate
        // (appele par Android avant tout autre callback du Service).
        //
        // Fix M-3 + L-5 (code-review post-9.5) : guard `wasActive` — sans ca,
        // un cleanupSync sur Service idle (ex. onStartCommand action inconnue,
        // ou requestVpnStop sur Service jamais connecte) re-poste la notif
        // pour la retirer 5 s apres -> clignotement. Avec le guard, la notif
        // n'est postee QUE quand un changement d'etat reel a eu lieu.
        if (wasActive && ::notificationHelper.isInitialized) {
            try {
                notificationHelper.notify(VpnState.DISCONNECTED, currentCountry, currentIp, currentKillStatus)
            } catch (t: Throwable) {
                // Defense en profondeur : si le notify echoue (channel supprime
                // par le systeme, etc.), ne PAS bloquer le cleanup.
                Log.w(TAG, "notify(DISCONNECTED) error (ignored)", t)
            }
        }
    }

    // ---------- Pumps paquets IP ----------

    private fun startPumpThreads(pfd: ParcelFileDescriptor) {
        running.set(true)
        // Note M-6 : fis et fos partagent le fd kernel possede par pfd. Leur cleanup
        // est implicite via vpnInterface?.close() dans disconnectInternal (les reads
        // bloquants levent IOException, le pump exit proprement). On ne ferme PAS
        // fis/fos explicitement ici car cela double-fermerait le fd (le kernel
        // pourrait le reattribuer entre les deux close, race documentee en M-6).
        val fis = FileInputStream(pfd.fileDescriptor)
        val fos = FileOutputStream(pfd.fileDescriptor)

        outPumpThread = Thread({
            val buf = ByteArray(MAX_IP_PACKET)
            try {
                while (running.get()) {
                    val n = try {
                        fis.read(buf)
                    } catch (e: IOException) {
                        Log.d(TAG, "out-pump ferme via IOException (close attendu)")
                        break
                    }
                    if (n <= 0) break
                    try {
                        packetRelay.onOutboundPacket(buf, n)
                    } catch (t: Throwable) {
                        // Ne JAMAIS laisser remonter une exception de la pompe — ca tuerait le service.
                        Log.w(TAG, "out-pump packetRelay error (continuons)", t)
                    }
                }
            } finally {
                running.set(false)
            }
        }, "vpn-out-pump").apply {
            isDaemon = true
            start()
        }

        inPumpThread = Thread({
            try {
                while (running.get()) {
                    // Story 11.7-bis : Channel<ByteArray>.receive() blocking via
                    // runBlocking. Si le Channel est fermé (cleanupSync OR
                    // packetSink renouvelé sur reconnect), ClosedReceiveChannelException
                    // est levée — on break proprement.
                    //
                    // Code-review post-11.7-bis (H-3) : Dispatchers.IO explicite
                    // pour éviter d'allouer un EventLoop par paquet. Un refactor
                    // full-coroutine (scope.launch + for(pkt in channel)) reste
                    // un follow-up Phase 2 pour éliminer le coût runBlocking
                    // résiduel à haut throughput (8000 paquets/s à 100 Mbps).
                    val pkt = try {
                        runBlocking(Dispatchers.IO) { packetSink.receive() }
                    } catch (e: ClosedReceiveChannelException) {
                        Log.d(TAG, "in-pump ferme via ClosedReceiveChannelException (close attendu)")
                        break
                    } catch (e: InterruptedException) {
                        Thread.currentThread().interrupt()
                        break
                    }
                    try {
                        fos.write(pkt)
                    } catch (e: IOException) {
                        Log.d(TAG, "in-pump ferme via IOException (close attendu)")
                        break
                    }
                }
            } finally {
                running.set(false)
            }
        }, "vpn-in-pump").apply {
            isDaemon = true
            start()
        }
    }

    /**
     * Story 11.7-bis : factory PacketRelay basé sur le pays demandé.
     *
     * Pipeline :
     *  1. RegistryLoader.load() — cache ConfigStore (Story 11.8) OU bundle res/raw
     *  2. RelayPicker.pick(country) — round-robin intra-pays
     *  3. Si succès → GoBackedPacketRelay(domain, pubKey, packetSink, onStateChanged)
     *     où onStateChanged forwarde transitions + visibleIp + effectiveCountry à
     *     notificationHelper.notify(...)
     *  4. Si échec (registry indispo, pays inexistant) → NoOpPacketRelay fallback
     *
     * `runBlocking` sur le main thread Service est OK pour MVP : la suspend
     * fun `RegistryLoader.load()` est rapide (lecture cache RAM ou bundle
     * res/raw + verify Ed25519 ~5ms). Si Story future ajoute fetch online,
     * il faudra passer en background (CoroutineScope du Service).
     *
     * Code-review post-11.7-bis (H-2) : anti-pattern Android documenté —
     * acceptable car l'opération est < 50ms avec le bundle res/raw. Action
     * item Phase 2 si fetch online ajouté.
     */
    private fun provideRelay(country: String?): PacketRelay {
        if (country.isNullOrBlank()) {
            Log.w(TAG, "provideRelay : pays absent, fallback NoOp")
            return NoOpPacketRelay()
        }
        val registryData = try {
            runBlocking { RegistryLoader(applicationContext).load() }
        } catch (t: Throwable) {
            Log.w(TAG, "RegistryLoader.load echoue : ${t.javaClass.simpleName}, fallback NoOp")
            null
        } ?: return NoOpPacketRelay()

        val relayInfo = RelayPicker(registryData).pick(country)
            ?: run {
                Log.w(TAG, "RelayPicker.pick : pas de relais pour le pays demande, fallback NoOp")
                return NoOpPacketRelay()
            }

        // Code-review post-11.7-bis (H-1) : pas de log du domaine ni du pays
        // (NFR-AND-9 — un attaquant avec READ_LOGS ne doit pas savoir quel
        // relais utilise l'utilisateur). Log neutre confirmant juste l'état.
        Log.i(TAG, "provideRelay: GoBackedPacketRelay actif")

        return GoBackedPacketRelay(
            relayDomain = relayInfo.domain,
            pinnedKeyB64 = relayInfo.pinnedKeyB64,
            inboundSink = packetSink,
            onStateChanged = { state, visibleIp, effectiveCountry ->
                // Story 11.7-bis Task 6 : update currentIp + currentCountry depuis
                // le callback Go enrichi, puis re-notify avec données fraîches.
                if (visibleIp != null) currentIp = visibleIp
                if (effectiveCountry != null) currentCountry = effectiveCountry
                if (::notificationHelper.isInitialized) {
                    try {
                        notificationHelper.notify(state, currentCountry, currentIp, currentKillStatus)
                    } catch (t: Throwable) {
                        Log.w(TAG, "onStateChanged notify error (ignored): ${t.javaClass.simpleName}")
                    }
                }
            },
        )
    }

    // Story 9.6 : ensureNotificationChannel() + buildStubOngoingNotification()
    // supprimees — orchestration deplacee dans NotificationHelper (ui/).

    companion object {
        private const val TAG = "LeVoileVpnService"

        /**
         * Story 9.5 : delai applique entre la fin du cleanup et le
         * stopForeground(STOP_FOREGROUND_REMOVE) + stopSelf. UX : laisse la
         * notif DISCONNECTED visible 5 s + evite le clignotement notif sur
         * reconnect rapide. Coherent epic AC `epics.md l. 1611`.
         */
        const val STOP_FOREGROUND_DELAY_MS: Long = 5_000L

        /**
         * Story 9.5 : reference singleton du Service VPN actif (ou null si
         * aucun). @Volatile garantit la visibilite cross-thread sans verrou
         * (les threads pumps lisent vpnInterface mais l'instance ici sert
         * surtout aux consommateurs externes — Story 11.2 LeVoileBridge).
         *
         * `internal` : visible uniquement depuis le module :app Kotlin ; pas
         * d'API publique. Story 11.2 utilisera plutot Intent.action +
         * Context.startForegroundService pour declencher Connect/Disconnect
         * (architecture.md l. 134 — pas d'IPC interne, communication via
         * Intents). Cette reference reste utile pour les tests de cycle de
         * vie + les diagnostics introspectifs.
         *
         * Lifecycle : assignee dans onCreate, mise a null dans onDestroy.
         * Android garantit au plus une instance active de Service par classe
         * dans un meme process.
         */
        @Volatile
        internal var instance: LeVoileVpnService? = null
            private set

        /**
         * Etat Always-On capture par [captureAlwaysOnState] via l'API publique
         * officielle [android.net.VpnService.isAlwaysOn] (API 29+).
         *
         * Lu par [fr.plateformeliberte.levoile.kill.KillSwitchDetector] —
         * remplace l'heuristique `Settings.Global` qui a ete restreinte
         * aux apps tierces sur Android 16+.
         *
         * Reset a `false` dans [onDestroy] : si [instance] == null, ces
         * valeurs sont stales (un consommateur doit toujours verifier
         * `instance != null` avant de les lire).
         */
        @Volatile
        internal var lastKnownAlwaysOn: Boolean = false
            private set

        /**
         * Etat Lockdown capture par [captureAlwaysOnState] via l'API publique
         * officielle [android.net.VpnService.isLockdownEnabled] (API 29+).
         * Voir [lastKnownAlwaysOn] pour le contrat.
         */
        @Volatile
        internal var lastKnownLockdown: Boolean = false
            private set
    }
}
