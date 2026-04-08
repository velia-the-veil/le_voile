// app.js — Polling status via local HTTP server and UI updates.

const POLL_INTERVAL = 2000;

const dom = {
    dot: null,
    text: null,
    ip: null,
    uptime: null,
    logo: null,
    countryFlag: null,
    countryName: null,
    relayInfo: null,
    testLink: null,
    btnConnect: null,
};

let pollIntervalId = null;

function init() {
    dom.dot = document.getElementById('status-dot');
    dom.text = document.getElementById('status-text');
    dom.ip = document.getElementById('status-ip');
    dom.uptime = document.getElementById('status-uptime');
    dom.logo = document.getElementById('header-logo');
    dom.countryFlag = document.getElementById('country-flag');
    dom.countryName = document.getElementById('country-name');
    dom.relayInfo = document.getElementById('relay-info');
    dom.testLink = document.getElementById('test-link');
    dom.btnConnect = document.getElementById('btn-connect');

    startPolling();
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

    // Connect/Disconnect button
    if (dom.btnConnect) {
        if (status.status === 'connected') {
            dom.btnConnect.className = 'btn-connect disconnect';
            dom.btnConnect.textContent = 'Déconnecter';
            dom.btnConnect.disabled = false;
        } else if (status.status === 'disconnected' || status.status === 'error') {
            dom.btnConnect.className = 'btn-connect connect';
            dom.btnConnect.textContent = 'Connecter';
            dom.btnConnect.disabled = false;
        } else {
            dom.btnConnect.className = 'btn-connect hidden';
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

async function toggleConnect() {
    var btn = dom.btnConnect;
    btn.disabled = true;
    try {
        if (btn.classList.contains('disconnect')) {
            await fetch('/api/disconnect', { method: 'POST' });
        } else {
            await fetch('/api/connect', { method: 'POST' });
        }
    } catch (e) {
        // Silent error — polling will update
    }
}

// Start when DOM is ready
document.addEventListener('DOMContentLoaded', init);
