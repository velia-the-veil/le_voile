// Le Voile — Extension WebExtension
// Routage automatique de tout le trafic navigateur via le proxy Le Voile
// Bypass intelligent des téléchargements volumineux (> 50 Mo)

// Détection navigateur
const isFirefox = typeof browser !== 'undefined' && browser.proxy && browser.proxy.onRequest;

const PROXY_HOST = '127.0.0.1';
const PROXY_PORT = 50113;

// Seuil de bypass : 50 Mo (52 428 800 octets)
const BYPASS_THRESHOLD = 52428800;

// Proxy health check — disable routing when proxy is down
let proxyAlive = true;
const HEALTH_CHECK_INTERVAL = 5000; // 5s

// Set des URLs/hostnames à bypasser temporairement
// Firefox : URL exacte (proxy.onRequest reçoit l'URL complète)
// Chrome : hostname uniquement (PAC ne reçoit que host en HTTPS)
const bypassUrls = new Set();

// --- Bypass : détection Content-Length et cancel+retry ---

function getContentLength(responseHeaders) {
  if (!responseHeaders) return -1;
  for (let i = 0; i < responseHeaders.length; i++) {
    if (responseHeaders[i].name.toLowerCase() === 'content-length') {
      const len = parseInt(responseHeaders[i].value, 10);
      if (isNaN(len) || len < 0) return -1;
      return len;
    }
  }
  return -1;
}

function isAlreadyBypassed(url) {
  if (isFirefox) return bypassUrls.has(url);
  try {
    return bypassUrls.has(new URL(url).hostname);
  } catch (e) {
    return false;
  }
}

function addBypassEntry(url) {
  if (isFirefox) {
    if (bypassUrls.has(url)) return;
    bypassUrls.add(url);
    setTimeout(() => { bypassUrls.delete(url); }, 120000);
  } else {
    try {
      const hostname = new URL(url).hostname;
      if (bypassUrls.has(hostname)) return;
      bypassUrls.add(hostname);
      setTimeout(() => {
        bypassUrls.delete(hostname);
        applyChromeProxy();
      }, 120000);
    } catch (e) {
      // URL malformée — pas de bypass
    }
  }
}

// NOTE: Chrome MV3 — 'blocking' in webRequest requires policy-installed extension.
// When loaded unpacked (dev mode), 'blocking' is silently ignored:
// return { cancel: true } becomes a no-op, bypass detection won't function.
// This is expected — bypass is fully functional only in production (policy install).
function setupBypassDetection() {
  const api = isFirefox ? browser : chrome;
  api.webRequest.onHeadersReceived.addListener(
    (details) => {
      if (isAlreadyBypassed(details.url)) return {};
      const contentLength = getContentLength(details.responseHeaders);
      if (contentLength > BYPASS_THRESHOLD) {
        addBypassEntry(details.url);
        if (!isFirefox) {
          applyChromeProxy();
        }
        api.downloads.download({ url: details.url, saveAs: false }, () => {
          if (isFirefox ? browser.runtime.lastError : chrome.runtime.lastError) {
            // Download retry failed — original request already cancelled, nothing to recover
          }
        });
        return { cancel: true };
      }
      return {};
    },
    { urls: ['<all_urls>'] },
    ['responseHeaders', 'blocking']
  );
}

// --- Chrome MV3 : routage via chrome.proxy.settings avec PAC script dynamique ---

function generatePacScript() {
  // If proxy is down, route everything direct
  if (!proxyAlive) {
    return `function FindProxyForURL(url, host) { return 'DIRECT'; }`;
  }

  const bypassChecks = Array.from(bypassUrls).map((hostname) => {
    const safe = hostname.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
    return `if (host === '${safe}') return 'DIRECT';`;
  }).join('\n      ');

  return `function FindProxyForURL(url, host) {
      if (host === '${PROXY_HOST}' || host === 'localhost' || host === '::1') return 'DIRECT';
      ${bypassChecks}
      return 'PROXY ${PROXY_HOST}:${PROXY_PORT}; DIRECT';
    }`;
}

function applyChromeProxy() {
  chrome.proxy.settings.set({
    value: {
      mode: 'pac_script',
      pacScript: {
        data: generatePacScript()
      }
    },
    scope: 'regular'
  });
}

function setupChromeProxy() {
  applyChromeProxy();
}

// --- Firefox MV2 : routage via browser.proxy.onRequest listener ---

function setupFirefoxProxy() {
  browser.proxy.onRequest.addListener(
    (details) => {
      // If proxy is down, route everything direct
      if (!proxyAlive) {
        return { type: 'direct' };
      }
      // Bypass pour les téléchargements volumineux détectés
      if (bypassUrls.has(details.url)) {
        return { type: 'direct' };
      }
      // Exclure les requêtes loopback (éviter boucle infinie)
      try {
        const url = new URL(details.url);
        if (url.hostname === PROXY_HOST || url.hostname === 'localhost' || url.hostname === '::1') {
          return { type: 'direct' };
        }
      } catch (e) {
        // URL malformée — router via proxy par défaut
      }
      return { type: 'http', host: PROXY_HOST, port: PROXY_PORT, failoverTimeout: 3 };
    },
    { urls: ['<all_urls>'] }
  );

  browser.proxy.onError.addListener(() => {
    // Proxy unreachable — switch to direct mode
    proxyAlive = false;
  });
}

// --- Point d'entrée : configuration automatique au chargement ---

if (isFirefox) {
  setupFirefoxProxy();
} else {
  setupChromeProxy();
}

setupBypassDetection();

// Periodic health check: try to connect to the proxy port.
// If the proxy comes back up, re-enable routing. If it goes down, switch to DIRECT.
// Manual test: stop service → verify navigation continues (DIRECT). Restart → verify routing resumes within 5s.
function checkProxyHealth() {
  fetch(`http://${PROXY_HOST}:${PROXY_PORT}/`, { mode: 'no-cors' }).then(() => {
    if (!proxyAlive) {
      proxyAlive = true;
      if (!isFirefox) applyChromeProxy();
    }
  }).catch(() => {
    if (proxyAlive) {
      proxyAlive = false;
      if (!isFirefox) applyChromeProxy();
    }
  });
}

setInterval(checkProxyHealth, HEALTH_CHECK_INTERVAL);

// Chrome : chrome.proxy.settings est automatiquement restauré par Chrome
// à la désinstallation/désactivation de l'extension. Rien à faire côté code.
