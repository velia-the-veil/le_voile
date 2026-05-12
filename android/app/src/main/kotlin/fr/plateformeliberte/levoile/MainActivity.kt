package fr.plateformeliberte.levoile

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.util.Log
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.lifecycle.Observer
import androidx.webkit.WebViewAssetLoader
import fr.plateformeliberte.levoile.bridge.LeVoileBridge
import fr.plateformeliberte.levoile.conflict.VpnConflictDetector
import fr.plateformeliberte.levoile.kill.KillSwitchDetector
import fr.plateformeliberte.levoile.kill.KillSwitchStatus
import fr.plateformeliberte.levoile.log.LeVoileLog
import fr.plateformeliberte.levoile.update.UpdateScheduler
import fr.plateformeliberte.levoile.vpn.LeVoileVpnService
import fr.plateformeliberte.levoile.vpn.VpnConstants

/**
 * Activité unique hôte du WebView Le Voile.
 *
 * Story 9.3 livre :
 *   - WebView plein écran chargé via WebViewAssetLoader (https://appassets.androidplatform.net/
 *     pas de file://) — sécurité + portabilité (architecture.md l. 263, l. 1157).
 *   - Bridge JS↔Kotlin minimal (LeVoileBridge stub avec getStatus() placeholder uniquement).
 *   - body.platform-android injecté au onPageFinished (préparation Story 11.1 responsive).
 *   - configChanges déclaré dans le manifest pour préserver l'état WebView sur rotation.
 *
 * Story 9.4 (livré) : LeVoileVpnService — non consommé fonctionnellement par cette activity.
 * Story 9.5 enrichit (cette story) : helpers internes [requestVpnStart] / [requestVpnStop] +
 * ActivityResultLauncher pour le consent VpnService (UI ↔ Service via Intents
 * `ACTION_CONNECT`/`ACTION_DISCONNECT`). Ces helpers sont DORMANTS — aucun appelant côté
 * UI dans cette story. Story 11.2 wirera depuis `LeVoileBridge.connect(country)` /
 * `LeVoileBridge.disconnect()` en castant le Context en MainActivity puis en
 * appelant ces helpers.
 * Story 9.6 (livré) : NotificationHelper — l'action « Déconnecter » de la notification
 * persistante invoque directement `LeVoileVpnService` via `PendingIntent.getService(...)` ;
 * cette MainActivity n'est pas dans la chaîne de cette action.
 * Story 11.1 enrichira : sync des assets HTML/CSS/JS depuis frontend/ racine via sync-frontend.sh.
 */
class MainActivity : AppCompatActivity() {

    /**
     * Story 9.5 : launcher pour le popup système de consent VpnService
     * (`Intent` retourné par `VpnService.prepare(this)`). Enregistré dans
     * `onCreate` AVANT toute action — l'API exige que la registration ait
     * lieu pendant la phase CREATED, sinon `IllegalStateException`.
     *
     * Au retour : si `RESULT_OK`, le consent a été accordé → on démarre le
     * service en Foreground avec `ACTION_CONNECT`. Sinon, on log un avertissement
     * (le frontend Story 11.2 affichera un toast/UI feedback). On préserve
     * `pendingConnectCountry` au cas où l'utilisatrice voudrait re-tenter.
     */
    private lateinit var vpnConsentLauncher: ActivityResultLauncher<Intent>

    /**
     * Pays sélectionné pour le prochain Connect (Story 11.4 livrera le
     * sélecteur UI ; Story 11.2 wirera la valeur via le bridge JS). Conservé
     * en mémoire de l'Activity pendant le flow consent → start. Story 11.x
     * pourra persister via `SharedPreferences` (FR-AND-10 prd.md l. 618).
     */
    private var pendingConnectCountry: String? = null

    /**
     * Story 10.1 — détecteur du kill switch OS-délégué (heuristique
     * `Settings.Global.always_on_vpn_app` + `always_on_vpn_lockdown`).
     * Ré-évalué à chaque [onResume] (Android ne broadcaste pas ces
     * changements). L'observation côté UI / push frontend JS appartient
     * à Story 10.2.
     */
    private lateinit var killSwitchDetector: KillSwitchDetector

    /**
     * Story 10.3 — détecteur de conflit VPN. Stateless, pas de LiveData :
     * `check()` est invoqué ponctuellement par le bridge JS au tap
     * « Connecter » (Story 11.2). Activity context (pas applicationContext)
     * en anticipation du `startActivityForResult` Story 11.5.
     */
    private lateinit var vpnConflictDetector: VpnConflictDetector

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Story 11.5 — vérifier l'onboarding au tout début. Si non complété,
        // déléguer à OnboardingActivity et finish() pour ne pas charger le WebView pour rien.
        val prefs = getSharedPreferences(
            fr.plateformeliberte.levoile.onboarding.OnboardingActivity.PREFS_NAME,
            MODE_PRIVATE
        )
        if (!prefs.getBoolean(
                fr.plateformeliberte.levoile.onboarding.OnboardingActivity.KEY_ONBOARDING_COMPLETED,
                false
            )
        ) {
            startActivity(
                Intent(
                    this,
                    fr.plateformeliberte.levoile.onboarding.OnboardingActivity::class.java
                )
            )
            finish()
            return
        }

        // Story 11.7 — handler extra openKillSwitchFlow (notification tap → flow C15).
        handleKillSwitchFlowExtra(intent)

        // Story 12.5 — schedule l'UpdateCheckWorker périodique 24h. Idempotent
        // (`KEEP` policy). Le worker court-circuit lui-même au runtime via
        // BuildConfig.AUTO_UPDATE_ENABLED — schedulé pour les 2 flavors mais
        // n'effectue le fetch GitHub que pour `apkDirect`.
        UpdateScheduler.scheduleIfNeeded(applicationContext)

        // M-2 (code-review 9.3) : WebContents debugging activé UNIQUEMENT en debug pour
        // permettre l'inspection via chrome://inspect (cf. README-android.md « Lancement de
        // l'app debug »). Guard impératif sur BuildConfig.DEBUG — laisser actif en release
        // exposerait une surface d'attaque côté APK signé.
        if (BuildConfig.DEBUG) {
            WebView.setWebContentsDebuggingEnabled(true)
        }

        // Story 9.5 : registration de l'ActivityResultLauncher AVANT setContentView
        // (l'API exige la phase CREATED — appel apres STARTED leve IllegalStateException).
        vpnConsentLauncher = registerForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { result ->
            if (result.resultCode == RESULT_OK) {
                Log.i(TAG, "Consent VpnService accorde — demarrage service ACTION_CONNECT")
                startVpnService(pendingConnectCountry)
            } else {
                // Story 11.2 affichera un toast/UI feedback. Pour l'instant, log only.
                Log.w(TAG, "Consent VpnService refuse par l'utilisatrice")
            }
        }

        setContentView(R.layout.activity_main)

        val webView = findViewById<WebView>(R.id.webView)
        configureWebView(webView)

        // Story 10.1 — instancié AVANT addJavascriptInterface car le bridge
        // (Story 10.2) consomme le détecteur via constructeur. Refresh
        // effectif au premier onResume (séquencement Android : onCreate →
        // onStart → onResume garanti). applicationContext évite de retenir
        // l'Activity depuis le détecteur (long-vivant si jamais recyclé
        // Story 11.x).
        killSwitchDetector = KillSwitchDetector(applicationContext)

        // Story 10.3 — Activity context (pas applicationContext) car
        // VpnService.prepare() est généralement invoqué côté Activity et
        // l'Intent retourné sera consommé via startActivityForResult par
        // Story 11.5. Anticipation.
        vpnConflictDetector = VpnConflictDetector(this)

        // AC #4 (Story 9.3) : addJavascriptInterface AVANT loadUrl — le bridge doit être
        // disponible dès que la page commence à exécuter du JS.
        // Story 11.2 — passage de `this` (MainActivity) plutôt que applicationContext :
        // LeVoileBridge.connect/disconnect doit caster en MainActivity pour appeler
        // runOnUiThread + requestVpnStart/Stop. La rétention M-3 est mitigée par le
        // removeJavascriptInterface explicite dans onDestroy ci-dessous (détachement avant
        // destroy() = pas de fuite résiduelle).
        // Story 10.2 — passage du détecteur kill switch au bridge.
        // Story 10.3 — passage du détecteur conflit VPN au bridge pour
        // exposer checkVpnConflict() au JS.
        webView.addJavascriptInterface(
            LeVoileBridge(this, killSwitchDetector, vpnConflictDetector),
            JS_BRIDGE_NAME,
        )
        webView.loadUrl(ASSET_INDEX_URL)

        // Story 10.2 — observer LiveData : à chaque changement de statut, on
        // pousse un signal au JS qui re-query getKillSwitchStatus() via le
        // bridge (source de vérité unique côté Kotlin). Pas de sérialisation
        // de payload, pas de risque d'injection. observe(this, ...) lie au
        // lifecycle Activity — automatiquement désabonné en onDestroy.
        // runOnUiThread défensif : LiveData.postValue rejoue déjà sur main
        // thread, mais ceinture+bretelles si un futur appel arrive depuis
        // Dispatchers.IO.
        val killSwitchObserver = Observer<KillSwitchStatus> { _ ->
            runOnUiThread {
                webView.evaluateJavascript(
                    "window.__LV_killSwitchChanged && window.__LV_killSwitchChanged();",
                    null,
                )
            }
        }
        killSwitchDetector.status.observe(this, killSwitchObserver)
    }

    /**
     * Story 10.1 — ré-évalue le kill switch au retour au premier plan
     * (typiquement après un aller-retour Settings → VPN). Une seule source
     * d'invocation : ni `onStart`, ni `onCreate` directs (le séquencement
     * Android garantit que `onResume` est appelé immédiatement après
     * `onStart` au premier launch). L'observer qui pousse l'état au
     * frontend JS est livré Story 10.2.
     */
    override fun onResume() {
        super.onResume()
        killSwitchDetector.refresh()
    }

    /**
     * Story 11.7 — handler nouvel Intent (notification tap kill switch alerte).
     * launchMode singleTop garantit qu'onNewIntent est appelé sur l'instance
     * existante plutôt que de recréer l'Activity.
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleKillSwitchFlowExtra(intent)
    }

    /**
     * Story 11.7 — Si la notif a été tappée en mode kill switch alerte,
     * deeplink direct vers Settings VPN. Décision dev : pas de relance
     * OnboardingActivity (l'utilisateur a déjà complété l'onboarding ; on
     * cible directement la page Settings la plus pertinente).
     */
    private fun handleKillSwitchFlowExtra(intent: Intent?) {
        if (intent?.getBooleanExtra(EXTRA_OPEN_KILL_SWITCH_FLOW, false) == true) {
            LeVoileLog.i(TAG, "Notification tap → kill switch flow")
            try {
                startActivity(Intent(android.provider.Settings.ACTION_VPN_SETTINGS))
            } catch (t: android.content.ActivityNotFoundException) {
                LeVoileLog.w(TAG, "Settings.ACTION_VPN_SETTINGS indisponible — ROM custom")
            }
        }
    }

    /**
     * M-4 (code-review 9.3) : nettoyage explicite du WebView à la destruction de l'Activity.
     * Sans ça, Android WebView est connu pour leaker via le bridge JS (qui retient le contexte)
     * et la WebViewClassLoader. `removeJavascriptInterface` détache le bridge AVANT `destroy()`
     * pour éviter les appels JS in-flight pendant la destruction.
     */
    override fun onDestroy() {
        findViewById<WebView>(R.id.webView)?.apply {
            removeJavascriptInterface(JS_BRIDGE_NAME)
            stopLoading()
            destroy()
        }
        super.onDestroy()
    }

    private fun configureWebView(webView: WebView) {
        val assetLoader = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this))
            .build()

        webView.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(
                view: WebView,
                request: WebResourceRequest
            ): WebResourceResponse? {
                val resp = assetLoader.shouldInterceptRequest(request.url)
                if (BuildConfig.DEBUG) {
                    Log.i(TAG, "shouldInterceptRequest url=${request.url} matched=${resp != null}")
                }
                return resp
            }

            override fun onReceivedError(
                view: WebView,
                request: WebResourceRequest,
                error: android.webkit.WebResourceError
            ) {
                if (BuildConfig.DEBUG) {
                    Log.w(TAG, "onReceivedError url=${request.url} code=${error.errorCode} desc=${error.description}")
                }
                super.onReceivedError(view, request, error)
            }

            override fun onPageStarted(view: WebView, url: String, favicon: android.graphics.Bitmap?) {
                super.onPageStarted(view, url, favicon)
                // NFR-AND-9 : ne pas logger l'URL — marqueur d'événement seulement.
                if (BuildConfig.DEBUG) Log.i(TAG, "onPageStarted")
            }

            override fun onPageFinished(view: WebView, url: String) {
                super.onPageFinished(view, url)
                // NFR-AND-9 : ne pas logger l'URL — marqueur d'événement seulement.
                if (BuildConfig.DEBUG) Log.i(TAG, "onPageFinished")
                // AC #3 (Story 9.3) : marqueur responsive injecté APRÈS chargement DOM.
                // Si Story 11.1 a besoin que la classe soit là AVANT certaines instanciations
                // de composants C13-C17, elle utilisera un MutationObserver côté JS.
                // L-1 (code-review 9.3) : callback non-null pour logger en debug si l'injection
                // produit un résultat inattendu (DOM corrompu, body manquant). En release, le
                // callback est appelé mais muet (guard BuildConfig.DEBUG sur Log.d).
                view.evaluateJavascript(
                    "document.body.classList.add('platform-android'); void(0);"
                ) { result ->
                    if (BuildConfig.DEBUG && result != null && result != "null") {
                        Log.d(TAG, "platform-android injection result=$result")
                    }
                }
            }
        }

        webView.settings.apply {
            javaScriptEnabled = true                       // requis pour @JavascriptInterface
            domStorageEnabled = true                       // permet localStorage si Story 11.x en a besoin
            allowFileAccess = false                        // AC #2 — défense contre file://
            allowContentAccess = false
            @Suppress("DEPRECATION")
            allowFileAccessFromFileURLs = false            // déprécié API 30+ mais explicite
            @Suppress("DEPRECATION")
            allowUniversalAccessFromFileURLs = false
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
        }
    }

    // ---------- Story 9.5 — Helpers UI ↔ Service (dormants jusqu'à Story 11.2) ----------

    /**
     * Story 9.5 — Demande le consent système VpnService au premier lancement
     * puis démarre `LeVoileVpnService` avec `ACTION_CONNECT`. Si le consent
     * a déjà été accordé sur ce device pour cette app (cas suivants connects),
     * `VpnService.prepare(this)` retourne `null` et on démarre directement.
     *
     * **Story 11.2** wirera ce helper depuis `LeVoileBridge.connect(country)`
     * en castant le Context du bridge en MainActivity. Aucun appelant ne
     * cible ce helper dans le scope Story 9.5 → utile à des tests de
     * vérification de structure uniquement, et au DebugConnectActivity
     * optionnel (cf. README-android.md).
     *
     * @param country code ISO du pays préféré (ex. "DE", "GB"). `null`
     *   = laisse le noyau Go choisir (round-robin). Sera transmis comme
     *   `EXTRA_COUNTRY` au service (consommé par Story 9.7 pour la sélection
     *   du relais).
     */
    // Story 11.2 — wired par LeVoileBridge.connect(country).
    internal fun requestVpnStart(country: String? = null) {
        pendingConnectCountry = country
        val prepareIntent = VpnService.prepare(this)
        if (prepareIntent != null) {
            Log.i(TAG, "VpnService.prepare retourne un Intent — lancement popup consent")
            vpnConsentLauncher.launch(prepareIntent)
        } else {
            Log.i(TAG, "Consent deja accorde — startVpnService direct")
            startVpnService(country)
        }
    }

    /**
     * Story 9.5 — Envoie un Intent `ACTION_DISCONNECT` au service VPN actif.
     *
     * Fix M-1 (code-review post-9.5) : guard `LeVoileVpnService.instance == null`
     * — sans ca, un tap "Deconnecter" alors qu'aucun tunnel n'est actif
     * declenche l'instanciation breve d'un nouveau Service (overhead +
     * onCreate + onStartCommand + cleanup) qui affiche fugacement la
     * notification (« Le Voile · Deconnecte » apres fix M-2) avant de la
     * retirer 5 s plus tard. UX confondante. Le guard rend l'appel un vrai
     * no-op sur Service idle.
     *
     * Fix L-1 (code-review post-9.5) : utilisation de `ContextCompat.
     * startForegroundService` (et non `startService`) — coherent avec
     * `startVpnService`. L'ancien commentaire qui pretendait l'inverse etait
     * faux (le code utilisait deja `startForegroundService`). Le contrat
     * < 5 s pour `startForeground` est garanti par `onStartCommand` du Service
     * (fix H-1/H-2/H-3 code-review post-9.4 + Story 9.5).
     *
     * **Story 11.2** wirera ce helper depuis `LeVoileBridge.disconnect()`.
     */
    // Story 11.2 — wired par LeVoileBridge.disconnect().
    internal fun requestVpnStop() {
        if (LeVoileVpnService.instance == null) {
            Log.i(TAG, "requestVpnStop ignore — aucun service actif")
            return
        }
        val intent = Intent(this, LeVoileVpnService::class.java).apply {
            action = VpnConstants.ACTION_DISCONNECT
        }
        ContextCompat.startForegroundService(this, intent)
    }

    /**
     * Story 9.5 — Helper privé : démarre le service avec `ACTION_CONNECT`
     * + `EXTRA_COUNTRY` optionnel. Appelé par `requestVpnStart` après
     * obtention du consent (ou immédiatement si déjà accordé).
     */
    private fun startVpnService(country: String?) {
        val intent = Intent(this, LeVoileVpnService::class.java).apply {
            action = VpnConstants.ACTION_CONNECT
            country?.let { putExtra(VpnConstants.EXTRA_COUNTRY, it) }
        }
        ContextCompat.startForegroundService(this, intent)
    }

    companion object {
        private const val TAG = "MainActivity"

        // Nom exposé au DOM via window.LeVoile — figé Story 9.3, consommé tel quel
        // par le frontend desktop partagé (Story 11.1) et l'enrichissement bridge (Story 11.2).
        const val JS_BRIDGE_NAME = "LeVoile"

        /** Story 11.7 — extra pour le tap notif kill switch alerte. */
        const val EXTRA_OPEN_KILL_SWITCH_FLOW = "openKillSwitchFlow"
        // URL virtuelle servie par WebViewAssetLoader (host appassets.androidplatform.net
        // est l'authority réservée Google pour les assets locaux — pas de DNS, pas de réseau).
        // Story 11.1 : repointé vers le sous-arbre web/ (séparation assets sync vs natifs).
        private const val ASSET_INDEX_URL = "https://appassets.androidplatform.net/assets/web/index.html"
    }
}
