// Le Voile — Extension WebExtension
// Routage automatique de tout le trafic navigateur via le proxy Le Voile
// Bypass intelligent des téléchargements volumineux (> 50 Mo)

// Détection navigateur
const isFirefox = typeof browser !== 'undefined' && browser.proxy && browser.proxy.onRequest;

const PROXY_HOST = '127.0.0.1';
const PROXY_PORT = 50113;

// Seuil de bypass : 50 Mo (52 428 800 octets)
const BYPASS_THRESHOLD = 52428800;

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
        api.downloads.download({ url: details.url, saveAs: false });
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
  const bypassChecks = Array.from(bypassUrls).map((hostname) => {
    return `if (host === '${hostname}') return 'DIRECT';`;
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

  // Gestion erreur silencieuse — le navigateur fait le fallback en direct automatiquement
  browser.proxy.onError.addListener(() => {
    // Silencieux — fallback DIRECT géré nativement par Firefox
  });
}

// --- Point d'entrée : configuration automatique au chargement ---

if (isFirefox) {
  setupFirefoxProxy();
} else {
  setupChromeProxy();
}

setupBypassDetection();

// Chrome : chrome.proxy.settings est automatiquement restauré par Chrome
// à la désinstallation/désactivation de l'extension. Rien à faire côté code.
