// app.js — Polling status via local HTTP server and UI updates.

const POLL_INTERVAL = 2000;
const REGISTRY_POLL_INTERVAL = 30000;

const dom = {
    dot: null,
    text: null,
    ip: null,
    uptime: null,
    logo: null,
    countryList: null,
    countryFlag: null,
    countryName: null,
    relayInfo: null,
    testLink: null,
    btnConnect: null,
};

let pollIntervalId = null;
let registryIntervalId = null;
let selectedCountryName = ''; // tracks user-selected country for mismatch detection

function init() {
    dom.dot = document.getElementById('status-dot');
    dom.text = document.getElementById('status-text');
    dom.ip = document.getElementById('status-ip');
    dom.uptime = document.getElementById('status-uptime');
    dom.logo = document.getElementById('header-logo');
    dom.countryList = document.getElementById('country-list');
    dom.countryFlag = document.getElementById('country-flag');
    dom.countryName = document.getElementById('country-name');
    dom.relayInfo = document.getElementById('relay-info');
    dom.testLink = document.getElementById('test-link');
    dom.btnConnect = document.getElementById('btn-connect');

    startPolling();
    startRegistryPolling();
}

function startPolling() {
    if (pollIntervalId !== null) {
        clearInterval(pollIntervalId);
    }
    pollStatus();
    pollIntervalId = setInterval(pollStatus, POLL_INTERVAL);
}

async function pollStatus() {
    try {
        const resp = await fetch('/api/status');
        const status = await resp.json();
        updateUI(status);
    } catch (e) {
        updateUI({ status: 'disconnected', message: 'Déconnecté', ip: '', uptime: '' });
    }
}

function updateUI(status) {
    // Status dot classes
    dom.dot.className = 'status-dot';
    if (status.status === 'connected') {
        dom.dot.classList.add('connected');
    } else if (status.status === 'connecting') {
        dom.dot.classList.add('connecting');
    } else {
        dom.dot.classList.add('disconnected');
    }

    // Country display in main panel
    if (status.country) {
        dom.countryFlag.textContent = status.country_flag || '';
        dom.countryName.textContent = status.country;
    } else {
        dom.countryFlag.textContent = '';
        dom.countryName.textContent = '';
    }

    // Status text
    dom.text.textContent = status.message || 'Déconnecté';

    // IP
    if (status.status === 'connected' && status.ip) {
        dom.ip.textContent = 'IP : ' + status.ip;
    } else {
        dom.ip.textContent = '';
    }

    // Relay info
    if (status.status === 'connected' && status.relay_id) {
        const info = status.relay_latency ? status.relay_id + ' · ' + status.relay_latency : status.relay_id;
        dom.relayInfo.textContent = info;
    } else {
        dom.relayInfo.textContent = '';
    }

    // Uptime
    if (status.status === 'connected' && status.uptime) {
        dom.uptime.textContent = status.uptime;
    } else {
        dom.uptime.textContent = '';
    }

    // Connect/Disconnect button (AC5)
    if (dom.btnConnect) {
        const countryMismatch = status.status === 'connected'
            && selectedCountryName !== ''
            && status.country !== ''
            && selectedCountryName !== status.country;

        const countryLabel = selectedCountryName || status.country || '';
        if (countryMismatch) {
            // Selected country differs from connected country → show "Connecter"
            dom.btnConnect.className = 'btn-connect connect';
            dom.btnConnect.textContent = 'Connecter';
            dom.btnConnect.disabled = false;
            dom.btnConnect.setAttribute('aria-label', 'Se connecter à ' + countryLabel);
        } else if (status.status === 'connected') {
            dom.btnConnect.className = 'btn-connect disconnect';
            dom.btnConnect.textContent = 'Déconnecter';
            dom.btnConnect.disabled = false;
            dom.btnConnect.setAttribute('aria-label', 'Se déconnecter de ' + countryLabel);
        } else if (status.status === 'disconnected') {
            dom.btnConnect.className = 'btn-connect connect';
            dom.btnConnect.textContent = 'Connecter';
            dom.btnConnect.disabled = false;
            dom.btnConnect.setAttribute('aria-label', countryLabel ? 'Se connecter à ' + countryLabel : 'Se connecter');
        } else {
            // connecting, error, or unknown → hide button
            dom.btnConnect.className = 'btn-connect hidden';
            dom.btnConnect.removeAttribute('aria-label');
        }
    }

    // Test link — only visible when connected
    dom.testLink.style.display = status.status === 'connected' ? '' : 'none';

    // Header logo color
    if (dom.logo) {
        dom.logo.className = 'header-logo';
        if (status.status === 'connected') {
            dom.logo.classList.add('connected');
        } else if (status.status === 'connecting') {
            dom.logo.classList.add('connecting');
        } else {
            dom.logo.classList.add('disconnected');
        }
    }
}

function startRegistryPolling() {
    if (registryIntervalId !== null) {
        clearInterval(registryIntervalId);
    }
    loadRegistry();
    registryIntervalId = setInterval(loadRegistry, REGISTRY_POLL_INTERVAL);
}

async function loadRegistry() {
    try {
        const resp = await fetch('/api/registry');
        const reg = await resp.json();
        if (reg && reg.countries) {
            renderCountryList(reg.countries);
        }
    } catch (e) {
        // Sidebar unchanged on error
    }
}

function renderCountryList(countries) {
    const list = dom.countryList;
    while (list.firstChild) list.removeChild(list.firstChild);
    // Sync selectedCountryName from active country in registry.
    // This handles initial load and failover (service switched country).
    for (let i = 0; i < countries.length; i++) {
        if (countries[i].active) {
            selectedCountryName = countries[i].name;
            break;
        }
    }
    countries.forEach(function(c) {
        const item = document.createElement('div');
        item.className = 'country-item' + (c.active ? ' active' : '');

        const flagSpan = document.createElement('span');
        flagSpan.className = 'country-flag';
        flagSpan.textContent = c.flag;

        const nameSpan = document.createElement('span');
        nameSpan.className = 'country-name';
        nameSpan.textContent = c.name;

        const countSpan = document.createElement('span');
        countSpan.className = 'country-count';
        countSpan.textContent = c.relay_count;

        item.appendChild(flagSpan);
        item.appendChild(nameSpan);
        item.appendChild(countSpan);
        item.addEventListener('click', function() { selectCountry(c.code, c.name); });
        list.appendChild(item);
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
        // Refresh registry after reconnection delay
        setTimeout(loadRegistry, 2000);
    } catch (e) {
        // Polling will update status
    }
}

async function toggleConnect() {
    const btn = dom.btnConnect;
    btn.disabled = true;
    try {
        const action = btn.classList.contains('disconnect') ? 'disconnect' : 'connect';
        const resp = await fetch('/api/' + action, { method: 'POST' });
        const data = await resp.json();
        if (data.error) {
            dom.text.textContent = data.error;
        }
    } catch (e) {
        // Silent error — polling will update
    }
}

// Start when DOM is ready
document.addEventListener('DOMContentLoaded', init);
