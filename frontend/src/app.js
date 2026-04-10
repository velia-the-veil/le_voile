// app.js — Le Voile desktop UI

const POLL_INTERVAL = 2000;
const REGISTRY_POLL_INTERVAL = 30000;

const dom = {};
let pollId = null;
let registryId = null;
let selectedCountryName = '';
let currentPanel = 'status';

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

    startPolling();
    startRegistryPolling();
}

// === Panels ===
function showPanel(name) {
    currentPanel = name;
    document.querySelectorAll('.panel').forEach(function(p) { p.classList.remove('visible'); });
    document.querySelectorAll('.sidebar-tab').forEach(function(t) { t.classList.remove('active'); });

    var panel = document.getElementById('panel-' + name);
    var tab = document.getElementById('tab-' + name);
    if (panel) panel.classList.add('visible');
    if (tab) tab.classList.add('active');

    if (name === 'settings') loadSettings();
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
        updateUI({ status: 'disconnected', message: 'Deconnecte' });
    }
}

function updateUI(s) {
    var st = s.status || 'disconnected';

    // Status dot
    dom.dot.className = 'status-dot ' + (st === 'connecting' ? 'connecting' : st === 'connected' ? 'connected' : 'disconnected');

    // Status text
    var stClass = st === 'connected' ? 'connected' : st === 'connecting' ? 'connecting' : 'disconnected';
    dom.text.className = 'status-text ' + stClass;
    dom.text.textContent = s.message || 'Deconnecte';

    // Titlebar V color
    dom.titlebarV.className = 'titlebar-v ' + stClass;

    // Country name
    dom.countryName.textContent = s.country ? s.country.toUpperCase() : '';

    // IPs
    if (s.real_ip) {
        dom.ipReal.innerHTML = '<span class="label">IP reelle : </span><span class="value">' + s.real_ip + '</span>';
    } else {
        dom.ipReal.textContent = '';
    }
    if (st === 'connected' && s.ip) {
        dom.ipVisible.innerHTML = '<span class="label">IP devoilee : </span><span class="value">' + s.ip + '</span>';
    } else {
        dom.ipVisible.textContent = '';
    }

    // Relay info — id + latency
    if (st === 'connected' && s.relay_id && s.relay_id !== 'default') {
        var info = s.relay_id;
        if (s.relay_latency) info += ' \u00b7 ' + s.relay_latency;
        dom.relayInfo.textContent = info;
    } else {
        dom.relayInfo.textContent = '';
    }

    // Connect button (visible only when disconnected)
    if (dom.btnConnect) {
        if (st === 'disconnected') {
            dom.btnConnect.className = 'btn btn-connect';
            dom.btnConnect.textContent = 'Connecter';
            dom.btnConnect.disabled = false;
        } else {
            dom.btnConnect.className = 'btn hidden';
        }
    }

    // Test link
    if (dom.testLink) dom.testLink.style.display = st === 'connected' ? '' : 'none';
}

// === Registry ===
function startRegistryPolling() {
    if (registryId) clearInterval(registryId);
    loadRegistry();
    registryId = setInterval(loadRegistry, REGISTRY_POLL_INTERVAL);
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
        if (countries[i].active) { selectedCountryName = countries[i].name; break; }
    }

    countries.forEach(function(c) {
        if (c.code === 'unknown') return;
        var btn = document.createElement('button');
        btn.className = 'sidebar-country' + (c.active ? ' active' : '');

        var name = document.createElement('span');
        name.className = 'name';
        name.textContent = c.name;

        var count = document.createElement('span');
        count.className = 'count';
        count.textContent = c.relay_count;

        btn.appendChild(name);
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
    try {
        await fetch('/api/country', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ code: code })
        });
        setTimeout(loadRegistry, 2000);
    } catch (e) {}
}

// === Connect ===
async function toggleConnect() {
    var btn = dom.btnConnect;
    btn.disabled = true;
    try {
        var resp = await fetch('/api/connect', { method: 'POST' });
        var data = await resp.json();
        if (data.error) dom.text.textContent = data.error;
    } catch (e) {}
}

// === Settings ===
async function loadSettings() {
    try {
        var resp = await fetch('/api/settings');
        var s = await resp.json();
        setToggle('toggle-autostart', s.auto_start);
        setToggle('toggle-blocklist', s.blocklist);
        setToggle('toggle-httpproxy', s.http_proxy);
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

document.addEventListener('DOMContentLoaded', init);
