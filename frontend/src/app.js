// app.js — Polling status via Wails bindings and UI updates.

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
    modalOverlay: null,
    modalSkipCheckbox: null,
};

let pollIntervalId = null;
let registryIntervalId = null;

function init() {
    dom.dot = document.getElementById('status-dot');
    dom.text = document.getElementById('status-text');
    dom.ip = document.getElementById('status-ip');
    dom.uptime = document.getElementById('status-uptime');
    dom.logo = document.getElementById('titlebar-logo');
    dom.countryList = document.getElementById('country-list');
    dom.countryFlag = document.getElementById('country-flag');
    dom.countryName = document.getElementById('country-name');
    dom.relayInfo = document.getElementById('relay-info');
    dom.testLink = document.getElementById('test-link');
    dom.btnConnect = document.getElementById('btn-connect');
    dom.modalOverlay = document.getElementById('modal-overlay');
    dom.modalSkipCheckbox = document.getElementById('modal-skip-checkbox');

    startPolling();
    loadRegistry();
    startRegistryPolling();
}

function startPolling() {
    if (pollIntervalId !== null) {
        clearInterval(pollIntervalId);
    }
    pollStatus();
    pollIntervalId = setInterval(pollStatus, POLL_INTERVAL);
}

function startRegistryPolling() {
    if (registryIntervalId !== null) {
        clearInterval(registryIntervalId);
    }
    registryIntervalId = setInterval(loadRegistry, REGISTRY_POLL_INTERVAL);
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

async function loadRegistry() {
    try {
        const reg = await window.go.desktop.App.GetRegistry();
        if (reg && reg.countries) {
            renderCountryList(reg.countries);
        }
    } catch (e) {
        // Sidebar remains as-is — no error displayed
    }
}

function renderCountryList(countries) {
    const list = dom.countryList;
    list.innerHTML = '';
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
        item.addEventListener('click', function() { selectCountry(c.code); });
        list.appendChild(item);
    });
}

async function selectCountry(code) {
    try {
        await window.go.desktop.App.SelectCountry(code);
        // Delay registry refresh to let tunnel reconnect so the active
        // country flag reflects the new relay, not the old one.
        setTimeout(loadRegistry, 2000);
    } catch (e) {
        // Silent error — polling will update the status
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
        dom.countryFlag.textContent = status.flag || '';
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
        const info = status.latency ? status.relay_id + ' · ' + status.latency : status.relay_id;
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

    // Test link — only visible when connected (clicking while disconnected reveals real IP)
    dom.testLink.style.display = status.status === 'connected' ? '' : 'none';

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

// Open test link via Wails runtime to ensure it goes through the system browser.
function openTestLink(event) {
    event.preventDefault();
    if (window.runtime && window.runtime.BrowserOpenURL) {
        window.runtime.BrowserOpenURL('https://plateformeliberte.fr/test-protection.html');
    }
}

// Titlebar controls
function minimizeWindow() {
    if (window.runtime && window.runtime.WindowMinimise) {
        window.runtime.WindowMinimise();
    }
}

async function closeWindow() {
    try {
        var skip = await window.go.desktop.App.GetSkipQuitModal();
        if (skip) {
            await confirmQuit();
            return;
        }
    } catch (e) {
        // If error, show modal by default
    }
    dom.modalOverlay.classList.remove('hidden');
    document.getElementById('btn-modal-cancel').focus();
}

function closeQuitModal() {
    dom.modalOverlay.classList.add('hidden');
}

async function confirmQuit() {
    if (dom.modalSkipCheckbox && dom.modalSkipCheckbox.checked) {
        try {
            await window.go.desktop.App.SetSkipQuitModal(true);
        } catch (e) { /* best effort */ }
    }
    try {
        await window.go.desktop.App.Quit();
    } catch (e) {
        if (window.runtime && window.runtime.Quit) {
            window.runtime.Quit();
        }
    }
}

async function toggleConnect() {
    var btn = dom.btnConnect;
    btn.disabled = true;
    try {
        if (btn.classList.contains('disconnect')) {
            await window.go.desktop.App.Disconnect();
        } else {
            await window.go.desktop.App.Connect();
        }
    } catch (e) {
        // Silent error — polling will update
    }
    // Don't re-enable here — polling updateUI manages the state
}

document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape' && dom.modalOverlay && !dom.modalOverlay.classList.contains('hidden')) {
        closeQuitModal();
    }
});

// Start when DOM is ready
document.addEventListener('DOMContentLoaded', init);
