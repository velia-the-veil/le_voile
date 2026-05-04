'use strict';
/* Story 9.3+10.2+11.2+11.3+11.4 — frontend JS Android Le Voile (Option 2 :
 * Android-natif, pas de sync depuis windows/frontend/).
 *
 * NFR-AND-9 : aucun console.log direct (logcat tag chromium lisible par toute
 * app avec READ_LOGS). Opt-in `window.LeVoileDebug = true` à activer
 * manuellement via chrome://inspect en debug.
 */

(function () {
  function dlog(msg, payload) {
    if (typeof window.LeVoileDebug === 'boolean' && window.LeVoileDebug) {
      console.log(msg, payload);
    }
  }

  /* ====== Story 9.3 + 11.2 — polling getStatus enrichi ====== */
  var statusEl = document.getElementById('status-dot');
  var statusTextEl = document.getElementById('status-text');
  var statusIpEl = document.getElementById('status-ip');
  var connectBtn = document.getElementById('android-btn-connect');

  function poll() {
    try {
      if (typeof window.LeVoile === 'undefined' ||
          typeof window.LeVoile.getStatus !== 'function') {
        if (statusEl) statusEl.textContent = 'bridge-absent';
        return;
      }
      var raw = window.LeVoile.getStatus();
      var parsed = JSON.parse(raw);
      var state = parsed.state || 'unknown';
      if (statusEl) statusEl.textContent = state;
      if (statusTextEl) {
        var labels = {
          connected: 'Connecté',
          connecting: 'Connexion…',
          disconnected: 'Déconnecté',
          reconnecting: 'Reconnexion…',
          error: 'Erreur',
        };
        statusTextEl.textContent = labels[state] || state;
      }
      if (statusIpEl) statusIpEl.textContent = parsed.ip || '—';
      // Maj bouton connect/disconnect (Story 11.2)
      if (connectBtn) {
        if (state === 'connected' || state === 'connecting') {
          connectBtn.textContent = 'DÉCONNECTER';
          connectBtn.dataset.state = 'connected';
        } else {
          connectBtn.textContent = 'CONNECTER';
          connectBtn.dataset.state = 'disconnected';
        }
      }
      dlog('getStatus polled:', parsed);
    } catch (err) {
      if (statusEl) statusEl.textContent = 'erreur';
    }
  }

  poll();
  setInterval(poll, 2000);

  /* ====== Story 11.2 — window.LV bridge UI handlers ====== */
  function call(fn) {
    var args = Array.prototype.slice.call(arguments, 1);
    try {
      if (typeof window.LeVoile === 'undefined' ||
          typeof window.LeVoile[fn] !== 'function') {
        return { error: 'bridge_method_missing', method: fn };
      }
      var raw = window.LeVoile[fn].apply(window.LeVoile, args);
      // openKillSwitchTarget ne retourne rien (void) — pas de parsing.
      if (raw === undefined || raw === null || raw === '') return { ok: true };
      return JSON.parse(raw);
    } catch (e) {
      return { error: 'bridge_call_failed', method: fn };
    }
  }

  window.LV = window.LV || {};
  window.LV.connect = function (country) { return call('connect', country || null); };
  window.LV.disconnect = function () { return call('disconnect'); };
  window.LV.selectCountry = function (iso) { return call('selectCountry', iso); };
  window.LV.getStatus = function () { return call('getStatus'); };
  window.LV.openAppDetailsSettings = function () { return call('openAppDetailsSettings'); };
  window.LV.openKillSwitchTarget = function () { return call('openKillSwitchTarget'); };

  /* Bouton Connect/Disconnect Android */
  if (connectBtn) {
    connectBtn.addEventListener('click', function () {
      if (connectBtn.dataset.state === 'connected') {
        window.LV.disconnect();
      } else {
        window.LV.connect(null);
      }
    });
  }
})();

/* Helper séquencement : la classe `platform-android` est ajoutée par
 * MainActivity.onPageFinished côté Kotlin, ce qui se produit APRÈS
 * l'exécution synchrone de ce script. Les IIFE plateformes-spécifiques
 * doivent donc différer leur logique jusqu'à ce que la classe soit posée.
 * Sinon le check `classList.contains('platform-android')` early-return
 * et les listeners ne sont jamais attachés. */
function whenPlatformAndroid(callback) {
  if (document.body.classList.contains('platform-android')) {
    callback();
    return;
  }
  var observer = new MutationObserver(function () {
    if (document.body.classList.contains('platform-android')) {
      observer.disconnect();
      callback();
    }
  });
  observer.observe(document.body, { attributes: true, attributeFilter: ['class'] });
}

/* ====== Story 10.2 — Bandeau C17 kill switch ====== */
whenPlatformAndroid(function () {
  'use strict';
  if (typeof window.LeVoile === 'undefined' ||
      typeof window.LeVoile.getKillSwitchStatus !== 'function') return;

  var banner = document.getElementById('android-c17-banner');
  if (!banner) return;

  function refreshFromBridge() {
    var status;
    try { status = window.LeVoile.getKillSwitchStatus(); } catch (e) { return; }
    var hide = (status === 'Active');
    if (hide) {
      banner.setAttribute('hidden', '');
      document.body.classList.remove('has-c17-banner');
    } else {
      banner.removeAttribute('hidden');
      document.body.classList.add('has-c17-banner');
    }
  }

  refreshFromBridge();
  window.__LV_killSwitchChanged = function () { refreshFromBridge(); };

  banner.addEventListener('click', function () {
    try { window.LeVoile.openKillSwitchTarget(); } catch (e) {}
  });
});

/* ====== Story 11.3 — AppBar + Drawer handlers ====== */
whenPlatformAndroid(function () {
  'use strict';

  var burger = document.querySelector('.android-appbar__burger');
  var drawer = document.getElementById('android-drawer');
  var backdrop = document.getElementById('android-drawer-backdrop');
  var closeBtn = document.querySelector('.android-drawer__close');
  var sysSettingsLink = document.getElementById('android-drawer-link-system-settings');

  if (!burger || !drawer || !backdrop) return;

  document.body.classList.add('has-android-appbar');

  function openDrawer() {
    drawer.setAttribute('aria-hidden', 'false');
    burger.setAttribute('aria-expanded', 'true');
    backdrop.removeAttribute('hidden');
    if (closeBtn) closeBtn.focus();
  }
  function closeDrawer() {
    drawer.setAttribute('aria-hidden', 'true');
    burger.setAttribute('aria-expanded', 'false');
    backdrop.setAttribute('hidden', '');
    burger.focus();
  }
  burger.addEventListener('click', openDrawer);
  if (closeBtn) closeBtn.addEventListener('click', closeDrawer);
  backdrop.addEventListener('click', closeDrawer);

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && drawer.getAttribute('aria-hidden') === 'false') {
      closeDrawer();
    }
  });

  if (sysSettingsLink) {
    sysSettingsLink.addEventListener('click', function (e) {
      e.preventDefault();
      if (window.LV && typeof window.LV.openAppDetailsSettings === 'function') {
        window.LV.openAppDetailsSettings();
      }
      closeDrawer();
    });
  }
});

/* ====== Story 11.4 — Country Selector Bottom-Sheet handlers ====== */
whenPlatformAndroid(function () {
  'use strict';

  var pill = document.getElementById('android-country-pill');
  var sheet = document.getElementById('android-country-sheet');
  var backdrop = document.getElementById('android-country-sheet-backdrop');
  if (!pill || !sheet || !backdrop) return;

  // FLAGS sync avec LeVoileBridge.COUNTRIES_WHITELIST (Kotlin) +
  // CountryDisplay (Story 11.7). Toute extension nécessite mise à jour des 3.
  var FLAGS = { DE: '🇩🇪', ES: '🇪🇸', GB: '🇬🇧', US: '🇺🇸' };

  var currentIso = 'DE';

  function refreshActiveItem() {
    var items = sheet.querySelectorAll('.android-bottomsheet__item');
    items.forEach(function (item) {
      if (item.dataset.iso === currentIso) {
        item.setAttribute('aria-selected', 'true');
      } else {
        item.removeAttribute('aria-selected');
      }
    });
  }

  function openSheet() {
    sheet.removeAttribute('data-closing');
    sheet.setAttribute('aria-hidden', 'false');
    pill.setAttribute('aria-expanded', 'true');
    backdrop.removeAttribute('hidden');
    refreshActiveItem();
    // Code-review post-11.4 (M7) : back Android intercept consolidé ici plutôt
    // que dans un second handler click — un seul listener pour ouvrir + push
    // history évite la divergence si un futur dev modifie un des deux.
    try { history.pushState({ sheet: 'country' }, '', '#country'); } catch (e) {}
    var firstItem = sheet.querySelector('.android-bottomsheet__item');
    if (firstItem) firstItem.focus();
  }

  function closeSheet() {
    // Code-review post-11.4 (L4) : garde anti-double-fermeture. Si déjà fermé
    // ou en cours de fermeture, no-op — évite les races drag-down + back
    // simultanés qui posteraient deux setTimeout concurrents.
    if (sheet.getAttribute('aria-hidden') === 'true' ||
        sheet.getAttribute('data-closing') === 'true') {
      return;
    }
    sheet.setAttribute('data-closing', 'true');
    backdrop.setAttribute('hidden', '');
    pill.setAttribute('aria-expanded', 'false');
    setTimeout(function () {
      sheet.setAttribute('aria-hidden', 'true');
      sheet.removeAttribute('data-closing');
      pill.focus();
    }, 200);
  }

  function selectCountry(iso) {
    if (!FLAGS[iso]) return;
    if (iso === currentIso) {
      closeSheet();
      return;
    }
    var result = window.LV && window.LV.selectCountry ? window.LV.selectCountry(iso) : null;
    if (!result || result.error) {
      closeSheet();
      return;
    }
    currentIso = iso;
    var pillFlag = pill.querySelector('.android-country-pill__flag');
    if (pillFlag) pillFlag.textContent = FLAGS[iso];
    document.dispatchEvent(new CustomEvent('lv:country-changed', {
      detail: { iso: iso, flag: FLAGS[iso] }
    }));
    closeSheet();
  }

  pill.addEventListener('click', openSheet);
  backdrop.addEventListener('click', closeSheet);

  sheet.addEventListener('click', function (e) {
    var item = e.target.closest('.android-bottomsheet__item');
    if (item && item.dataset.iso) selectCountry(item.dataset.iso);
  });

  sheet.addEventListener('keydown', function (e) {
    var item = e.target.closest('.android-bottomsheet__item');
    if (item && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault();
      selectCountry(item.dataset.iso);
    }
    if (e.key === 'Escape') closeSheet();
  });

  // Drag-down dismiss minimaliste sur le handle
  var handle = sheet.querySelector('.android-bottomsheet__handle');
  var dragStartY = null;
  if (handle) {
    handle.addEventListener('touchstart', function (e) {
      dragStartY = e.touches[0].clientY;
    }, { passive: true });
    handle.addEventListener('touchmove', function (e) {
      if (dragStartY === null) return;
      var dy = e.touches[0].clientY - dragStartY;
      if (dy > 0) {
        sheet.style.transform = 'translateY(' + dy + 'px)';
      }
    }, { passive: true });
    handle.addEventListener('touchend', function (e) {
      if (dragStartY === null) return;
      var dy = e.changedTouches[0].clientY - dragStartY;
      dragStartY = null;
      sheet.style.transform = '';
      if (dy > 80) closeSheet();
    });
  }

  // Back Android intercept (history popstate). Code-review post-11.4 (M7) :
  // pushState consolidé dans openSheet() — ici seulement le listener popstate.
  window.addEventListener('popstate', function () {
    if (sheet.getAttribute('aria-hidden') === 'false') closeSheet();
  });
});
