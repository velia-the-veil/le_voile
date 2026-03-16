// app.js — Polling status via Wails bindings and UI updates.

const POLL_INTERVAL = 2000;

const dom = {
    dot: null,
    text: null,
    ip: null,
    uptime: null,
    logo: null,
};

let pollIntervalId = null;

function init() {
    dom.dot = document.getElementById('status-dot');
    dom.text = document.getElementById('status-text');
    dom.ip = document.getElementById('status-ip');
    dom.uptime = document.getElementById('status-uptime');
    dom.logo = document.getElementById('titlebar-logo');

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
        // Wails auto-generates bindings under window.go.desktop.App
        const status = await window.go.desktop.App.GetStatus();
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

    // Status text
    dom.text.textContent = status.message || 'Déconnecté';

    // IP
    if (status.status === 'connected' && status.ip && status.ip !== 'unknown') {
        dom.ip.textContent = 'IP : ' + status.ip;
    } else {
        dom.ip.textContent = '';
    }

    // Uptime
    if (status.status === 'connected' && status.uptime) {
        dom.uptime.textContent = status.uptime;
    } else {
        dom.uptime.textContent = '';
    }

    // Titlebar logo color
    dom.logo.className = 'titlebar-logo';
    if (status.status === 'connected') {
        dom.logo.classList.add('connected');
    } else if (status.status === 'connecting') {
        dom.logo.classList.add('connecting');
    } else {
        dom.logo.classList.add('disconnected');
    }
}

// Titlebar controls
function minimizeWindow() {
    if (window.runtime && window.runtime.WindowMinimise) {
        window.runtime.WindowMinimise();
    }
}

function closeWindow() {
    if (window.runtime && window.runtime.WindowHide) {
        window.runtime.WindowHide();
    }
}

// Start when DOM is ready
document.addEventListener('DOMContentLoaded', init);
