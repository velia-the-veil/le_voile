'use strict';
// Story 9.3 — placeholder polling toutes les 2 s appelant le bridge JS stub.
// Story 11.1 remplacera ce fichier par le résultat de sync-frontend.sh
// (logique frontend partagée desktop). Story 11.2 ajoutera connect/disconnect.
//
// M-6 (code-review 9.3) : aucun `console.log` direct — les console.* JS atterrissent
// dans logcat (tag `chromium`) et seraient lisibles par toute app avec READ_LOGS,
// frontière NFR-AND-8 (zéro télémétrie). Le polling est observable via le DOM
// (#status-dot) en chrome://inspect (debug WebView activé en BuildConfig.DEBUG)
// et via l'opt-in explicite `window.LeVoileDebug = true` (à ne définir qu'en debug
// manuel). Story 10.5 étendra la stratégie de filtrage logs Android par buildType.
(function () {
  var statusEl = document.getElementById('status-dot');

  function dlog(msg, payload) {
    if (typeof window.LeVoileDebug === 'boolean' && window.LeVoileDebug) {
      // Opt-in explicite seulement — invoqué manuellement en debug via
      // chrome://inspect console : `window.LeVoileDebug = true`.
      console.log(msg, payload);
    }
  }

  function poll() {
    try {
      if (typeof window.LeVoile === 'undefined' ||
          typeof window.LeVoile.getStatus !== 'function') {
        statusEl.textContent = 'bridge-absent';
        return;
      }
      var raw = window.LeVoile.getStatus();
      var parsed = JSON.parse(raw);
      statusEl.textContent = parsed.state || 'unknown';
      dlog('getStatus polled:', parsed);
    } catch (err) {
      // Erreur de parsing ou bridge cassé : on signale dans le DOM (visible
      // utilisatrice) sans écrire dans logcat. Le fail-fast est observable
      // via #status-dot.textContent === 'erreur'.
      statusEl.textContent = 'erreur';
    }
  }

  poll();                       // premier appel immédiat
  setInterval(poll, 2000);      // puis toutes les 2 s
})();
