// app.js — Le Voile desktop UI

const POLL_INTERVAL = 2000;
const REGISTRY_POLL_INTERVAL = 30000;

const dom = {};
let pollId = null;
let registryId = null;
// Story 5.6 — true while the "Service non démarré" fallback screen is shown;
// suppresses tab-switching and the normal status panel so the fallback owns
// the whole panel-area until IPC comes back.
let serviceDownShown = false;
let selectedCountryName = '';
// Code ISO du pays sélectionné — utilisé pour détecter le mismatch pays de façon
// robuste (voir H1 review). Comparer les codes évite les faux positifs si le
// backend change le format des noms (drapeau inline, localisation alternative…).
let selectedCountryCode = '';
let currentPanel = 'status';
// Dernière réponse /api/status reçue — permet à toggleConnect() de décider
// connect vs disconnect sans refetch et évite une race avec le polling 2s.
let lastStatus = null;
// Sémaphore anti-double-clic : tenu pendant toute la durée du fetch connect/
// disconnect, indépendant de l'état `disabled` du bouton (qui peut être réécrit
// par le polling updateUI pendant que le fetch est en vol).
let connectInflight = false;
// Story 6.3 — retained across polls so we can detect the true → false
// transition of anomaly_active and flash a transient "Reconnexion
// réussie" confirmation before the banner is hidden.
let wasAnomalyActive = false;
let anomalyFlashTimer = null;

function init() {
    dom.dot = document.getElementById('status-dot');
    dom.text = document.getElementById('status-text');
    dom.ipReal = document.getElementById('ip-real');
    dom.ipVisible = document.getElementById('ip-visible');
    dom.titlebarV = document.getElementById('titlebar-v');
    dom.countryList = document.getElementById('country-list');
    dom.countryName = document.getElementById('country-name');
    dom.relayInfo = document.getElementById('relay-info');
    dom.testLink = document.getElementById('test-link');
    dom.btnConnect = document.getElementById('btn-connect');
    dom.captiveBanner = document.getElementById('captive-banner');
    dom.btnCaptiveRetry = document.getElementById('btn-captive-retry');
    dom.failoverBanner = document.getElementById('failover-banner');
    dom.failoverBannerText = document.getElementById('failover-banner-text');
    // Story 6.3 — anomaly recovery banner, driven by /api/status
    // anomaly_active + anomaly_reason fields surfaced by the service.
    dom.anomalyBanner = document.getElementById('anomaly-banner');
    dom.anomalyBannerText = document.getElementById('anomaly-banner-text');
    dom.ipv6Badge = document.getElementById('ipv6-badge');
    dom.ipv6Warn = document.getElementById('ipv6-warn');
    // Story 5.6 — fallback screen elements.
    dom.panelServiceDown = document.getElementById('panel-service-down');
    dom.serviceDownMsg = document.getElementById('service-down-msg');
    dom.serviceDownCmd = document.getElementById('service-down-cmd');

    startPolling();
    startRegistryPolling();
    startUIEventsPolling();
    loadUIPrefs();

    // Retry if page failed to render (cold WebView2 runtime). Skip when the
    // service-down fallback is shown — in that state #panel-status is hidden
    // on purpose, so dom.dot.offsetParent is legitimately null and a reload
    // would just restart the cycle (Story 5.6 review finding H1).
    setTimeout(function() {
        if (serviceDownShown) return;
        if (!dom.dot || !dom.dot.offsetParent) location.reload();
    }, 1500);
}

// === Panels ===
function showPanel(name) {
    // Story 5.6 — while the fallback screen owns the panel-area, ignore tab
    // clicks. Record the intended panel so hideServiceDownScreen() restores it.
    if (serviceDownShown) {
        currentPanel = name;
        return;
    }
    currentPanel = name;
    document.querySelectorAll('.panel').forEach(function(p) { p.classList.remove('visible'); });
    document.querySelectorAll('.sidebar-tab').forEach(function(t) { t.classList.remove('active'); });

    var panel = document.getElementById('panel-' + name);
    var tab = document.getElementById('tab-' + name);
    if (panel) panel.classList.add('visible');
    if (tab) tab.classList.add('active');

    if (name === 'settings') loadSettings();
}

function shortRelayID(id) {
    if (!id) return '';
    const PREFIX = 'relay-';
    return id.indexOf(PREFIX) === 0 ? id.substring(PREFIX.length) : id;
}

// === Status polling ===
function startPolling() {
    if (pollId) clearInterval(pollId);
    pollStatus();
    pollId = setInterval(pollStatus, POLL_INTERVAL);
}

async function pollStatus() {
    try {
        var resp = await fetch('/api/status');
        var s = await resp.json();
        updateUI(s);
    } catch (e) {
        // Network error reaching the local HTTP server itself — treat as
        // "service unreachable" (the UI's own embedded server is down, or the
        // webview lost its origin). Render the same fallback screen with a
        // best-effort client-side hint (Story 5.6 review finding M1): without
        // this, the <pre> command block would be empty since no backend hint
        // was returned.
        updateUI({
            status: 'disconnected',
            service_reachable: false,
            service_start_hint: clientSideServiceHint(),
        });
    }
}

// integrityRecoveryHint builds the OS-specific recovery instructions shown
// under the integrity-failed banner (Story 7.5 / NFR9j). No in-process reset
// is offered by design, so the user needs the concrete paths to delete.
function integrityRecoveryHint() {
    var ua = (navigator.userAgent || '').toLowerCase();
    var platform = (navigator.platform || '').toLowerCase();
    var isWindows = platform.indexOf('win') === 0 || ua.indexOf('windows') !== -1;
    var isLinux = platform.indexOf('linux') === 0 || ua.indexOf('linux') !== -1;
    if (isWindows) {
        return "Recuperation : arretez le service 'levoile-service' (services.msc), supprimez %AppData%\\LeVoile\\config.toml et config.toml.hmac, puis redemarrez le service.";
    }
    if (isLinux) {
        return "Recuperation : sudo systemctl stop levoile.service && sudo rm /etc/levoile/config.toml /etc/levoile/config.toml.hmac && sudo systemctl start levoile.service";
    }
    return "Recuperation : arretez le service Le Voile, supprimez config.toml et config.toml.hmac dans le dossier de configuration, puis redemarrez le service.";
}

// clientSideServiceHint builds a degraded hint when the HTTP server is itself
// unreachable. Prefer the backend hint whenever available — this is only a
// fallback for the edge case where /api/status itself fails.
function clientSideServiceHint() {
    var ua = (navigator.userAgent || '').toLowerCase();
    var platform = (navigator.platform || '').toLowerCase();
    var isWindows = platform.indexOf('win') === 0 || ua.indexOf('windows') !== -1;
    var isLinux = platform.indexOf('linux') === 0 || ua.indexOf('linux') !== -1;
    if (isWindows) {
        return {
            os: 'windows',
            command: 'sc start levoile-service',
            human_message: "Le service Le Voile n'est pas démarré. Ouvrez Services.msc et démarrez « Le Voile Service », ou exécutez la commande ci-dessous dans une invite en tant qu'administrateur :",
        };
    }
    if (isLinux) {
        return {
            os: 'linux',
            command: 'sudo systemctl start levoile.service',
            human_message: "Le service Le Voile n'est pas démarré. Ouvrez un terminal et lancez la commande ci-dessous :",
        };
    }
    return {
        os: 'unknown',
        command: '',
        human_message: "Le service Le Voile n'est pas démarré. Démarrez-le avec l'outil de gestion des services de votre système.",
    };
}

// Story 5.6 — show/hide the "Service non démarré" fallback screen. Hides every
// other panel and disables sidebar tabs while active so the fallback owns the
// whole panel-area. Returning to false restores the previously selected panel.
function showServiceDownScreen(hint) {
    if (dom.panelServiceDown) {
        if (hint && hint.human_message && dom.serviceDownMsg) {
            dom.serviceDownMsg.textContent = hint.human_message;
        }
        if (hint && hint.command && dom.serviceDownCmd) {
            dom.serviceDownCmd.textContent = hint.command;
        }
        dom.panelServiceDown.style.display = '';
    }
    // Hide every regular panel (status/settings/notifications).
    document.querySelectorAll('.panel').forEach(function(p) {
        if (p.id !== 'panel-service-down') p.classList.remove('visible');
    });
    // Mute sidebar tabs — they should look inert while the service is down.
    document.querySelectorAll('.sidebar-tab').forEach(function(t) { t.classList.add('disabled'); });
    // Keep the titlebar V in the "disconnected" color family for coherence.
    if (dom.titlebarV) dom.titlebarV.className = 'titlebar-v disconnected';
    serviceDownShown = true;
}

function hideServiceDownScreen() {
    if (dom.panelServiceDown) dom.panelServiceDown.style.display = 'none';
    document.querySelectorAll('.sidebar-tab').forEach(function(t) { t.classList.remove('disabled'); });
    serviceDownShown = false;
    // Restore whichever panel the user had selected before (defaults to status).
    showPanel(currentPanel || 'status');
}

function updateUI(s) {
    lastStatus = s;

    // Story 5.6 — route to the fallback screen BEFORE touching regular panels.
    // service_reachable is always emitted by the backend; absence → treat as
    // true (defensive against older cached backend responses during upgrade).
    if (s && s.service_reachable === false) {
        showServiceDownScreen(s.service_start_hint);
        return;
    }
    if (serviceDownShown) {
        hideServiceDownScreen();
    }

    var st = s.status || 'disconnected';

    // Status dot
    dom.dot.className = 'status-dot ' + (st === 'connecting' ? 'connecting' : st === 'connected' ? 'connected' : 'disconnected');

    // Status text
    var stClass = st === 'connected' ? 'connected' : st === 'connecting' ? 'connecting' : 'disconnected';
    dom.text.className = 'status-text ' + stClass;
    dom.text.textContent = s.message || 'Deconnecte';

    // Titlebar V color
    dom.titlebarV.className = 'titlebar-v ' + stClass;

    // Country name — flag emoji + French name when connected (Story 5.2 AC1).
    // Fallback (M4): if Country metadata is missing (unknown relay ID format,
    // registry drift), still show the relay ID uppercased so the user isn't
    // left with an empty field while connected.
    if (st === 'connected' && s.country) {
        var flag = s.country_flag ? s.country_flag + ' ' : '';
        dom.countryName.textContent = flag + s.country.toUpperCase();
    } else if (st === 'connected' && s.relay_id) {
        dom.countryName.textContent = shortRelayID(s.relay_id).toUpperCase();
    } else {
        dom.countryName.textContent = '';
    }

    // IPs
    if (s.real_ip) {
        dom.ipReal.textContent = 'IP réelle : ' + s.real_ip;
    } else {
        dom.ipReal.textContent = '';
    }
    // AC3 + M5: when connected, show IP immediately if known, otherwise a
    // "detection in progress" placeholder so the user never sees a blank row
    // during the brief window while DetectVisibleIP is still running.
    if (st === 'connected' && s.ip) {
        dom.ipVisible.textContent = 'IP dévoilée : ' + s.ip;
    } else if (st === 'connected') {
        dom.ipVisible.textContent = 'IP dévoilée : détection en cours…';
    } else {
        dom.ipVisible.textContent = '';
    }

    // Relay info — short id + latency (Story 5.2 AC2).
    // Discoverer produces "relay-de-001"; AC demands the short "de-001" form.
    if (st === 'connected' && s.relay_id && s.relay_id !== 'default') {
        var info = shortRelayID(s.relay_id);
        if (s.relay_latency) info += ' \u00b7 ' + s.relay_latency;
        dom.relayInfo.textContent = info;
    } else {
        dom.relayInfo.textContent = '';
    }

    // Captive portal banner
    if (dom.captiveBanner) {
        if (s.captive_portal) {
            dom.captiveBanner.style.display = '';
            dom.dot.className = 'status-dot captive';
            dom.titlebarV.className = 'titlebar-v captive';
        } else {
            dom.captiveBanner.style.display = 'none';
        }
    }

    // Failover alert banner (Story 4.4) — inter-country switch message.
    if (dom.failoverBanner) {
        if (s.failover_alert) {
            dom.failoverBannerText.textContent = s.failover_alert;
            dom.failoverBanner.style.display = '';
        } else {
            dom.failoverBanner.style.display = 'none';
        }
    }

    // Anomaly recovery banner (Story 6.3) — auto-reconnect triggered by
    // the leak scheduler (STUN mismatch) or the TUN watchdog. Orange
    // while the recovery runs, flashes green for 3s on the true → false
    // transition to confirm success, then hides.
    renderAnomalyBanner(s);

    // Connect/Disconnect button — Story 5.4 AC3 + AC5.
    // Visibilité selon ux-design-specification.md §Feedback Patterns :
    //   connected  → "Déconnecter" (rouge transparent)
    //   disconnected (non captif, non erreur) → "Connecter" (vert)
    //   connecting / error / captive portal → caché
    // Mismatch pays : si un pays ≠ pays connecté est sélectionné, on affiche
    // "Connecter" pour permettre la re-connexion au nouveau pays.
    if (dom.btnConnect) {
        // Mismatch via codes ISO (robuste aux variations de libellé backend).
        // Fallback aux noms si les codes ne sont pas encore connus (bootstrap,
        // registre pas encore chargé).
        const currentCode = s.current_country_code || '';
        const countryMismatchByCode = selectedCountryCode && currentCode
            && selectedCountryCode !== currentCode;
        const countryMismatchByName = !currentCode
            && selectedCountryName && s.country
            && selectedCountryName !== s.country;
        const countryMismatch = st === 'connected'
            && (countryMismatchByCode || countryMismatchByName);
        const showConnect = (st === 'disconnected' && !s.captive_portal)
            || countryMismatch;
        const showDisconnect = st === 'connected' && !countryMismatch;

        if (showConnect) {
            dom.btnConnect.className = 'btn btn-connect';
            dom.btnConnect.textContent = 'Connecter';
            const target = selectedCountryName || s.country || '';
            dom.btnConnect.setAttribute('aria-label', target ? 'Se connecter à ' + target : 'Se connecter');
            dom.btnConnect.disabled = false;
        } else if (showDisconnect) {
            dom.btnConnect.className = 'btn btn-disconnect';
            dom.btnConnect.textContent = 'Déconnecter';
            dom.btnConnect.setAttribute('aria-label', 'Se déconnecter');
            dom.btnConnect.disabled = false;
        } else {
            dom.btnConnect.className = 'btn hidden';
        }
    }

    // IPv6 leak badge (permanent when allow_ipv6_leak is true)
    if (dom.ipv6Badge) {
        dom.ipv6Badge.style.display = s.allow_ipv6_leak ? '' : 'none';
    }
    if (dom.ipv6Warn) {
        dom.ipv6Warn.style.display = s.allow_ipv6_leak ? '' : 'none';
    }

    // Story 5.9 — permanent banner driven by killswitch_mode. The HTTP server
    // normalizes empty values to "normal" so we never show a phantom banner
    // while the service is unreachable.
    var ksBanner = document.getElementById('killswitch-banner');
    if (ksBanner) {
        ksBanner.style.display = (s.killswitch_mode === 'degraded') ? '' : 'none';
    }

    // Story 7.5 / NFR9j — config integrity banner (highest priority).
    // No reset button by design: recovery is hors-band only.
    var intBanner = document.getElementById('integrity-banner');
    if (intBanner) {
        intBanner.style.display = s.integrity_failed ? '' : 'none';
        if (s.integrity_failed) {
            var hint = document.getElementById('integrity-recovery-hint');
            if (hint && !hint.textContent) {
                hint.textContent = integrityRecoveryHint();
            }
            if (dom.btnConnect) {
                dom.btnConnect.className = 'btn hidden';
            }
        }
    }

    // Test link
    if (dom.testLink) dom.testLink.style.display = st === 'connected' ? '' : 'none';
}

// renderAnomalyBanner drives the #anomaly-banner element in response to
// the two status fields surfaced by /api/status (Story 6.3). It is
// intentionally self-contained: no external state beyond the
// wasAnomalyActive / anomalyFlashTimer module-level flags.
//
// - active=true          → orange banner, reason-aware text.
// - true → false flip    → green "Reconnexion réussie" flash for 3s
//                           then hide.
// - idle (false → false) → banner hidden, no work.
function renderAnomalyBanner(s) {
    if (!dom.anomalyBanner || !dom.anomalyBannerText) {
        return;
    }
    var active = !!s.anomaly_active;

    if (active) {
        if (anomalyFlashTimer) {
            clearTimeout(anomalyFlashTimer);
            anomalyFlashTimer = null;
        }
        dom.anomalyBanner.classList.remove('anomaly-success');
        dom.anomalyBanner.style.display = '';
        dom.anomalyBannerText.textContent = anomalyText(s.anomaly_reason);
    } else if (wasAnomalyActive) {
        // Transition: a recovery just completed. Show success flash.
        dom.anomalyBanner.classList.add('anomaly-success');
        dom.anomalyBanner.style.display = '';
        dom.anomalyBannerText.textContent = 'Reconnexion réussie';
        if (anomalyFlashTimer) { clearTimeout(anomalyFlashTimer); }
        anomalyFlashTimer = setTimeout(function () {
            if (dom.anomalyBanner) {
                dom.anomalyBanner.style.display = 'none';
                dom.anomalyBanner.classList.remove('anomaly-success');
            }
            anomalyFlashTimer = null;
        }, 3000);
    } else if (!anomalyFlashTimer) {
        // No transition, no active recovery, no lingering flash timer
        // — safe to hide outright. When a flash is still running we
        // leave the banner visible so it can play out.
        dom.anomalyBanner.style.display = 'none';
        dom.anomalyBanner.classList.remove('anomaly-success');
    }

    wasAnomalyActive = active;
}

function anomalyText(reason) {
    switch (reason) {
        case 'tun_altered':
            return 'Interface VPN altérée — reconnexion en cours';
        case 'manual':
            return 'Reconnexion manuelle en cours';
        default:
            return 'Anomalie détectée — reconnexion en cours';
    }
}

// === Registry ===
function startRegistryPolling() {
    if (registryId) clearInterval(registryId);
    loadRegistry();
    // Retry quickly until countries appear, then slow down to normal interval.
    var fastId = setInterval(function() {
        if (dom.countryList && dom.countryList.children.length > 0) {
            clearInterval(fastId);
            registryId = setInterval(loadRegistry, REGISTRY_POLL_INTERVAL);
            return;
        }
        loadRegistry();
    }, 2000);
}

async function loadRegistry() {
    try {
        var resp = await fetch('/api/registry');
        var reg = await resp.json();
        if (reg && reg.countries) renderCountryList(reg.countries);
    } catch (e) {}
}

function renderCountryList(countries) {
    var list = dom.countryList;
    while (list.firstChild) list.removeChild(list.firstChild);

    for (var i = 0; i < countries.length; i++) {
        if (countries[i].active) {
            selectedCountryName = countries[i].name;
            selectedCountryCode = countries[i].code || '';
            break;
        }
    }

    countries.forEach(function(c) {
        if (c.code === 'unknown') return;
        var btn = document.createElement('button');
        btn.className = 'sidebar-country' + (c.active ? ' active' : '');

        if (c.flag) {
            var flag = document.createElement('span');
            flag.className = 'flag';
            flag.textContent = c.flag;
            btn.appendChild(flag);
        }

        var name = document.createElement('span');
        name.className = 'name';
        name.textContent = c.name;
        btn.appendChild(name);

        var count = document.createElement('span');
        count.className = 'count';
        count.textContent = String(c.relay_count);
        btn.appendChild(count);

        btn.addEventListener('click', function() {
            selectCountry(c.code, c.name);
            showPanel('status');
        });
        list.appendChild(btn);
    });
}

async function selectCountry(code, name) {
    selectedCountryName = name || '';
    selectedCountryCode = code || '';
    try {
        await fetch('/api/country', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code: code })
        });
        setTimeout(loadRegistry, 2000);
    } catch (e) {}
}

// === Connect / Disconnect ===
// Branche sur l'état courant (lastStatus) : si connecté et pays aligné avec la
// sélection → /api/disconnect, sinon → /api/connect. Couvre aussi le cas
// "mismatch pays" (connecté mais utilisateur a sélectionné un autre pays) qui
// doit déclencher une re-connexion, pas une déconnexion.
async function toggleConnect() {
    // Garde anti-double-clic niveau module : `disabled` sur le bouton peut être
    // réinitialisé par le prochain updateUI(≤2s) pendant que fetch est en vol.
    // Ce flag couvre toute la durée de la requête, indépendamment du DOM.
    if (connectInflight) return;
    connectInflight = true;
    const btn = dom.btnConnect;
    btn.disabled = true;
    const st = (lastStatus && lastStatus.status) || 'disconnected';
    const currentCode = (lastStatus && lastStatus.current_country_code) || '';
    const mismatchByCode = selectedCountryCode && currentCode
        && selectedCountryCode !== currentCode;
    const mismatchByName = !currentCode
        && selectedCountryName && lastStatus && lastStatus.country
        && selectedCountryName !== lastStatus.country;
    const countryMismatch = st === 'connected' && (mismatchByCode || mismatchByName);
    const endpoint = (st === 'connected' && !countryMismatch) ? '/api/disconnect' : '/api/connect';
    try {
        const resp = await fetch(endpoint, { method: 'POST' });
        const data = await resp.json();
        if (data.error) dom.text.textContent = data.error;
    } catch (e) {}
    // Libération du flag en fin de fetch. Le bouton lui-même sera re-rendu par
    // le prochain updateUI(≤2s) dans l'état correspondant au nouveau status ;
    // on ne force PAS btn.disabled = false ici pour éviter la race avec ce
    // re-render.
    connectInflight = false;
}

// === Captive Portal ===
async function retryCaptive() {
    if (dom.btnCaptiveRetry) dom.btnCaptiveRetry.disabled = true;
    try {
        await fetch('/api/captive/retry', { method: 'POST' });
    } catch (e) {}
    // Re-enable after a delay to prevent spam clicks.
    setTimeout(function() { if (dom.btnCaptiveRetry) dom.btnCaptiveRetry.disabled = false; }, 3000);
}

// === Settings ===
async function loadSettings() {
    try {
        var resp = await fetch('/api/settings');
        var s = await resp.json();
        setToggle('toggle-autostart', s.auto_start);
        setToggle('toggle-blocklist', s.blocklist);
        setToggle('toggle-httpproxy', s.http_proxy);
        setToggle('toggle-ipv6leak', s.allow_ipv6_leak);
    } catch (e) {}
}

function setToggle(id, on) {
    var el = document.getElementById(id);
    if (!el) return;
    if (on) { el.classList.add('on'); } else { el.classList.remove('on'); }
}

async function toggleSetting(name) {
    var map = { autostart: 'toggle-autostart', blocklist: 'toggle-blocklist', httpproxy: 'toggle-httpproxy' };
    var el = document.getElementById(map[name]);
    if (!el) return;
    var nowOn = !el.classList.contains('on');
    setToggle(map[name], nowOn);
    try {
        await fetch('/api/settings/' + name, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled: nowOn })
        });
    } catch (e) {}
}

// === IPv6 Leak Toggle ===
function toggleIPv6Leak() {
    var el = document.getElementById('toggle-ipv6leak');
    if (!el) return;
    var isOn = el.classList.contains('on');
    if (isOn) {
        // Disabling (returning to safe state) — no confirmation needed (AC4).
        applyIPv6Leak(false);
    } else {
        // Enabling — show warning modal (AC2).
        document.getElementById('modal-ipv6').style.display = 'flex';
        document.getElementById('btn-ipv6-cancel').focus();
    }
}

function cancelIPv6Modal() {
    document.getElementById('modal-ipv6').style.display = 'none';
}

async function confirmIPv6Leak() {
    document.getElementById('modal-ipv6').style.display = 'none';
    await applyIPv6Leak(true);
}

// === Story 5.9 — Mode dégradé kill switch ===
//
// The destructive modal is opened either:
//   (a) by the systray "Mode dégradé" entry (Go side queues a one-shot UI
//       event consumed by pollUIEvents), or
//   (b) any future in-webview path (none today).
//
// The modal stays open until /api/settings/killswitch returns ok — no
// optimistic state. Errors are surfaced inline so the user can adjust
// (e.g. captive portal blocker) without losing the modal.

function openKillSwitchModal() {
    var modal = document.getElementById('modal-killswitch');
    if (!modal) return;
    var err = document.getElementById('modal-killswitch-error');
    if (err) { err.textContent = ''; err.style.display = 'none'; }
    modal.style.display = 'flex';
    var cancel = document.getElementById('btn-killswitch-cancel');
    if (cancel) cancel.focus();
}

function cancelKillSwitchModal() {
    var modal = document.getElementById('modal-killswitch');
    if (modal) modal.style.display = 'none';
}

// CSRF token cache — fetched lazily on first use, refreshed on 403.
let csrfTokenCache = '';
async function getCSRFToken(forceRefresh) {
    if (csrfTokenCache && !forceRefresh) return csrfTokenCache;
    try {
        var resp = await fetch('/api/csrf-token');
        if (!resp.ok) return '';
        var data = await resp.json();
        csrfTokenCache = data.token || '';
    } catch (e) { csrfTokenCache = ''; }
    return csrfTokenCache;
}

async function confirmKillSwitchDegraded() {
    var btn = document.getElementById('btn-killswitch-confirm');
    var err = document.getElementById('modal-killswitch-error');
    var banner = document.getElementById('killswitch-banner');
    if (btn) btn.disabled = true;
    try {
        var token = await getCSRFToken(false);
        var resp = await fetch('/api/settings/killswitch', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': token,
            },
            body: JSON.stringify({ mode: 'degraded' }),
        });
        // Retry once on 403 with a freshly-fetched token (handles UI restart
        // races where the cache is stale relative to a new server token).
        if (resp.status === 403) {
            token = await getCSRFToken(true);
            resp = await fetch('/api/settings/killswitch', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': token,
                },
                body: JSON.stringify({ mode: 'degraded' }),
            });
        }
        var data = {};
        try { data = await resp.json(); } catch (_) {}
        if (resp.ok && data.status === 'ok') {
            // Story 5.9 L2 fix — show the banner optimistically so there's no
            // visual gap between modal close and the next /api/status poll
            // (≤ 2 s). The poll will keep it in sync afterwards.
            if (banner) banner.style.display = '';
            cancelKillSwitchModal();
            return;
        }
        // Surface a user-friendly error inside the modal so the user can react.
        if (err) {
            err.textContent = humanizeKillSwitchError(data.error);
            err.style.display = '';
        }
    } catch (e) {
        if (err) {
            err.textContent = 'Connexion au service impossible. Réessayez.';
            err.style.display = '';
        }
    } finally {
        if (btn) btn.disabled = false;
    }
}

function humanizeKillSwitchError(code) {
    switch (code) {
        case 'captive_portal_active':
            return 'Indisponible : portail Wi-Fi captif actif. Authentifiez-vous d\'abord.';
        case 'auth_failed':
            return 'Authentification refusée par le service.';
        case 'tunnel_not_connected':
            return 'Aucun tunnel actif — impossible de basculer.';
        default:
            return code ? ('Erreur : ' + code) : 'Échec inconnu.';
    }
}

// pollUIEvents drains one-shot events queued by the Go tray side. Cheap GET,
// so we run on a 1 s cadence — independent from /api/status (2 s) so the modal
// pops up promptly after a tray click.
let uiEventsId = null;
function startUIEventsPolling() {
    if (uiEventsId) clearInterval(uiEventsId);
    uiEventsId = setInterval(pollUIEvents, 1000);
    pollUIEvents();
}

async function pollUIEvents() {
    try {
        var resp = await fetch('/api/ui-event');
        if (!resp.ok) return;
        var data = await resp.json();
        if (data && data.event === 'killswitch_modal') {
            openKillSwitchModal();
        }
    } catch (e) { /* server transient — try again next tick */ }
}

// --- Quit confirmation -----------------------------------------------------
//
// Preference is persisted server-side via /api/ui-prefs (NOT localStorage):
// the HTTP server binds to 127.0.0.1:0 (dynamic port) — localStorage is
// origin-scoped, so every relaunch would reset the preference. See Story 5.5
// review H3. The server writes to %APPDATA%\LeVoile\ui-prefs.json (Windows)
// or $XDG_CONFIG_HOME/levoile/ui-prefs.json (Linux).

// Cached prefs, refreshed on page init.
let uiPrefs = { quit_prompt_enabled: true };

async function loadUIPrefs() {
    try {
        const resp = await fetch('/api/ui-prefs');
        if (!resp.ok) return;
        const data = await resp.json();
        if (data && typeof data.quit_prompt_enabled === 'boolean') {
            uiPrefs.quit_prompt_enabled = data.quit_prompt_enabled;
        }
    } catch (e) { /* first run or service restarting — keep defaults */ }
}

async function saveUIPrefs(partial) {
    const next = Object.assign({}, uiPrefs, partial);
    try {
        const resp = await fetch('/api/ui-prefs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(next),
        });
        if (resp.ok) uiPrefs = next;
    } catch (e) { /* best effort — the user's choice in-session still holds */ }
}

function confirmQuit() {
    if (!uiPrefs.quit_prompt_enabled) {
        __close();
        return;
    }
    const modal = document.getElementById('modal-quit');
    const checkbox = document.getElementById('quit-dont-ask');
    if (checkbox) checkbox.checked = false;
    if (modal) {
        modal.style.display = 'flex';
        const cancel = document.getElementById('btn-quit-cancel');
        if (cancel) cancel.focus();
    }
}

function cancelQuitModal() {
    const modal = document.getElementById('modal-quit');
    if (modal) modal.style.display = 'none';
}

async function doQuit() {
    const checkbox = document.getElementById('quit-dont-ask');
    const modal = document.getElementById('modal-quit');
    if (modal) modal.style.display = 'none';
    if (checkbox && checkbox.checked) {
        // Await so the quit-prompt disable hits disk before the app exits;
        // otherwise a fast __close() could race the POST and drop the write.
        await saveUIPrefs({ quit_prompt_enabled: false });
    }
    __close();
}

// --- IPv6 leak toggle ------------------------------------------------------

async function applyIPv6Leak(enable) {
    try {
        var resp = await fetch('/api/settings/ipv6leak', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled: enable })
        });
        var data = await resp.json();
        if (data.status === 'ok') {
            setToggle('toggle-ipv6leak', enable);
        }
    } catch (e) {}
}

document.addEventListener('DOMContentLoaded', init);
