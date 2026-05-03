package fr.plateformeliberte.levoile.onboarding

import android.app.AlertDialog
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.view.View
import android.widget.Button
import android.widget.FrameLayout
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import fr.plateformeliberte.levoile.R
import fr.plateformeliberte.levoile.kill.KillSwitchDetector
import fr.plateformeliberte.levoile.kill.KillSwitchStatus
import fr.plateformeliberte.levoile.log.LeVoileLog

/**
 * Story 11.5 + 11.6 — Onboarding obligatoire 3 écrans (cohérent FR-AND-3 + J6).
 *
 * Flow :
 *   1. MainActivity.onCreate vérifie SharedPreferences.onboarding_completed.
 *      Si false → startActivity(OnboardingActivity) + finish().
 *   2. OnboardingActivity affiche les 3 écrans séquentiellement (back désactivé).
 *   3. Au tap "Continuer" du dernier écran → onboarding_completed = true,
 *      finish(), MainActivity se relance.
 *
 * Le back Android est désactivé via OnBackPressedCallback enabled = true.
 *
 * Story 11.6 enrichit l'Écran 3 avec C15 (icône warning, lien skip avec
 * dialog de confirmation, KillSwitchDetector re-vérification au retour
 * Settings, fallback Inactive/Unverifiable).
 */
class OnboardingActivity : AppCompatActivity() {

    private var currentScreen = 1
    private lateinit var screenContainer: FrameLayout
    private lateinit var vpnConsentLauncher: ActivityResultLauncher<Intent>
    private lateinit var killSwitchDetector: KillSwitchDetector
    private var awaitingSettingsReturn = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_onboarding)
        screenContainer = findViewById(R.id.onboarding_container)

        killSwitchDetector = KillSwitchDetector(applicationContext)

        vpnConsentLauncher = registerForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { result ->
            if (result.resultCode == RESULT_OK) {
                LeVoileLog.i(TAG, "Consent VpnService accorde dans onboarding ecran 2")
            } else {
                LeVoileLog.w(TAG, "Consent VpnService refuse dans onboarding")
            }
            // Avance écran après le retour quoi qu'il arrive (l'écran 3 explique
            // que la protection sans consent VPN ne sera pas active).
            showScreen(3)
        }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                LeVoileLog.i(TAG, "Back ignore pendant l'onboarding")
            }
        })

        showScreen(1)
    }

    override fun onResume() {
        super.onResume()
        // Story 11.6 — re-vérifier kill switch au retour de Settings.
        if (currentScreen == 3 && awaitingSettingsReturn) {
            awaitingSettingsReturn = false
            triggerKillSwitchVerification()
        }
    }

    private fun showScreen(num: Int) {
        currentScreen = num
        screenContainer.removeAllViews()
        val layoutId = when (num) {
            1 -> R.layout.onboarding_screen_1
            2 -> R.layout.onboarding_screen_2
            3 -> R.layout.onboarding_screen_3
            else -> error("Invalid screen $num")
        }
        val view = layoutInflater.inflate(layoutId, screenContainer, false)
        screenContainer.addView(view)
        wireScreenButtons(view, num)
    }

    private fun wireScreenButtons(view: View, num: Int) {
        when (num) {
            1 -> view.findViewById<Button>(R.id.onboarding_btn_continue)
                .setOnClickListener { showScreen(2) }
            2 -> view.findViewById<Button>(R.id.onboarding_btn_continue)
                .setOnClickListener {
                    val intent = VpnService.prepare(this)
                    if (intent != null) vpnConsentLauncher.launch(intent)
                    else showScreen(3)
                }
            3 -> wireScreen3Enriched(view)
        }
    }

    private fun wireScreen3Enriched(view: View) {
        view.findViewById<Button>(R.id.onboarding_btn_open_settings)
            .setOnClickListener {
                awaitingSettingsReturn = true
                openVpnSettings()
            }
        view.findViewById<TextView>(R.id.onboarding_link_skip)
            .setOnClickListener { showSkipConfirmationDialog() }
        view.findViewById<Button>(R.id.onboarding_btn_retry)
            .setOnClickListener {
                view.findViewById<View>(R.id.onboarding_fallback_actions).visibility = View.GONE
                awaitingSettingsReturn = true
                openVpnSettings()
            }
        view.findViewById<Button>(R.id.onboarding_btn_manual_verified)
            .setOnClickListener {
                LeVoileLog.i(TAG, "Onboarding: manual_verified accepte par utilisatrice")
                completeOnboarding()
            }
    }

    private fun triggerKillSwitchVerification() {
        val overlay = findViewById<View>(R.id.onboarding_verifying_overlay) ?: return
        overlay.visibility = View.VISIBLE
        killSwitchDetector.refresh()
        Handler(Looper.getMainLooper()).postDelayed({
            overlay.visibility = View.GONE
            when (killSwitchDetector.status.value) {
                is KillSwitchStatus.Active -> {
                    LeVoileLog.i(TAG, "Onboarding: kill switch Active detecte — completion")
                    completeOnboarding()
                }
                else -> {
                    findViewById<View>(R.id.onboarding_fallback_actions)?.visibility = View.VISIBLE
                }
            }
        }, 1000L)
    }

    private fun showSkipConfirmationDialog() {
        val dialogView = layoutInflater.inflate(R.layout.dialog_skip_killswitch, null)
        val dialog = AlertDialog.Builder(this)
            .setView(dialogView)
            .setCancelable(true)
            .create()
        dialogView.findViewById<Button>(R.id.dialog_skip_cancel).setOnClickListener {
            dialog.dismiss()
        }
        dialogView.findViewById<Button>(R.id.dialog_skip_confirm).setOnClickListener {
            dialog.dismiss()
            LeVoileLog.i(TAG, "Onboarding: skip kill switch confirme par utilisatrice")
            completeOnboarding()
        }
        dialog.show()
    }

    private fun openVpnSettings() {
        try {
            startActivity(Intent(Settings.ACTION_VPN_SETTINGS))
        } catch (t: ActivityNotFoundException) {
            LeVoileLog.w(TAG, "Settings.ACTION_VPN_SETTINGS indisponible — ROM custom")
        }
    }

    private fun completeOnboarding() {
        getSharedPreferences(PREFS_NAME, MODE_PRIVATE)
            .edit()
            .putBoolean(KEY_ONBOARDING_COMPLETED, true)
            .apply()
        finish()
    }

    companion object {
        private const val TAG = "OnboardingActivity"
        const val PREFS_NAME = "levoile_prefs"
        const val KEY_ONBOARDING_COMPLETED = "onboarding_completed"
    }
}
