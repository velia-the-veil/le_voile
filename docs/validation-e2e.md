# Validation E2E — Guide Opérationnel

## Exécution des tests E2E automatisés

### Commande principale

```bash
E2E=1 go test -tags e2e -run TestE2E ./... -v -timeout 5m
```

### Exécution par sous-chaîne

```bash
# DNS proxy + kill switch
E2E=1 go test -tags e2e -run TestE2E ./internal/dns/ -v

# Proxy CONNECT + mock relay
E2E=1 go test -tags e2e -run TestE2E ./internal/httpproxy/ -v

# IPC multi-client
E2E=1 go test -tags e2e -run TestE2E ./internal/ipc/ -v

# Failover relay
E2E=1 go test -tags e2e -run TestE2E ./internal/registry/ -v

# Shutdown + crash recovery (Windows, admin requis)
E2E=1 go test -tags e2e -run TestE2E ./internal/service/ -v

# Politiques navigateur (Chromium: Windows admin, Firefox: cross-platform)
E2E=1 go test -tags e2e -run TestE2E ./internal/browser/ -v
```

## Prérequis par plateforme

### Windows (principal)

| Prérequis | Tests concernés | Skip auto |
|-----------|----------------|-----------|
| Élévation admin | DNS restore, DNS port 53, Chromium policies, recovery | `t.Skip("requires admin")` |
| Port 53 libre | `TestE2E_DNSPort53Real` | `t.Skip("port 53 unavailable")` |
| Navigateur Chromium installé | `TestE2E_WebRTCPoliciesApplied_Chromium` | `t.Skip("no Chromium browser detected")` |
| IPv6 actif | `TestE2E_DNSIPv6Resolver` | `t.Skip("IPv6 not available")` |

### Linux / macOS

Les tests cross-platform fonctionnent :
- DNS proxy resolution et kill switch
- Proxy CONNECT + mock relay
- IPC multi-client
- Failover relay
- Firefox policies.json (structure JSON uniquement)

Les tests Windows-only (registre, netsh, WinINET) sont exclus par build tags.

## Variables d'environnement

| Variable | Usage | Défaut |
|----------|-------|--------|
| `E2E=1` | **Requis** — active les tests E2E | Tests skippés si absent |
| `RELAY_ADDR` | Adresse d'un relais réel pour tests avec tunnel complet | Non utilisé (mock local) |
| `RELAY_PUBKEY` | Clé publique Ed25519 du relais réel | Non utilisé |

## Limitations connues

### Tests DNS proxy sur port éphémère

Les tests E2E DNS utilisent un port éphémère (pas 53) pour éviter les conflits. Le test `TestE2E_DNSPort53Real` teste le port 53 réel mais nécessite admin et un port libre — il est conditionnel et skip automatiquement si non disponible.

### Tests browser policies Chromium = Windows-only

Les politiques Chromium sont appliquées via le registre Windows (`HKLM\SOFTWARE\Policies\...`). Pas de test automatisé sur Linux/macOS pour Chromium.

### Tests WinINET = Windows-only

Le proxy système WinINET (registre `Internet Settings`) est spécifique à Windows.

### Tests avec relais réel nécessitent `RELAY_ADDR`

Les tests E2E automatisés utilisent des mock relays locaux. Pour tester avec un relais réel (vérification IP visible, latence réseau), configurer `RELAY_ADDR` et `RELAY_PUBKEY`.

### Limitations de sécurité hors scope

1. **Crash DNS proxy → recovery via watchdog** : le cycle complet (crash → watchdog → relance → reprise) n'est pas automatisable sans kill du process interne. Testé en unitaire (watchdog), pas en E2E bout-en-bout.

2. **Politiques navigateur révoquées par un tiers** : Le Voile ne monitore pas en continu les clés registre/policies.json. Le leak checker STUN périodique (10 min) est la seule détection post-modification.

3. **Windows Update réinitialisant le resolver DNS** : le watchdog détecte et re-modifie au prochain cycle de vérification.

## Couverture AC ↔ Tests automatisés

| AC | Description | Tests E2E automatisés |
|----|-------------|----------------------|
| AC1 | Zéro fuite DNS | `TestE2E_DNSProxyResolution`, `TestE2E_KillSwitchActivation`, `TestE2E_DNSIPv6Resolver`, `TestE2E_DNSPort53Real` |
| AC2 | Zéro fuite IP | `TestE2E_IPCamouflage` |
| AC3 | Zéro fuite WebRTC | `TestE2E_WebRTCPoliciesApplied_Chromium`, `TestE2E_WebRTCPoliciesApplied_Firefox` |
| AC4 | Failover + UI sync | `TestE2E_FailoverSameCountry`, `TestE2E_FailoverIPConsistency`, `TestE2E_FailoverKillSwitchProtection`, `TestE2E_ReconnectInitiation` |
| AC5 | Changement pays cohérent | `TestE2E_IPCCountryChange`, `TestE2E_IPCConcurrentCountryChange` |
| AC6 | Post-shutdown fonctionnel | `TestE2E_CleanShutdown_DNSRestored`, `TestE2E_CleanShutdown_BrowserPoliciesRestored`, `TestE2E_DNSRestoredAfterShutdown` |
| AC7 | Crash recovery | `TestE2E_CrashRecovery_OrphanDNS`, `TestE2E_CrashRecovery_OrphanPolicies`, `TestE2E_WinINETRecovery` |
| AC8 | CONNECT résilient | `TestE2E_ProxyCONNECT_TunnelDown`, `TestE2E_SessionTokenRefresh`, `TestE2E_VolumeBypass` |
| AC9 | Cold start cohérent | `TestE2E_IPCMultiClient`, `TestE2E_IPCStatusDuringReconnect`, `TestE2E_IPCPipeBroken` |

## Checklist manuelle

La checklist de validation manuelle complète (tests visuels, UI tray/webview, sites de test externes) est documentée dans la story :

`_bmad-output/implementation-artifacts/12-2-validation-bout-en-bout-et-tests-dintegration.md` → Task 7 (subtasks 7.1 à 7.14)

### Résumé des points de vérification manuelle

| # | Test | AC |
|---|------|-----|
| 7.1 | Démarrage cold start (SCM → UI → tray → webview) | AC9 |
| 7.2 | Zéro fuite DNS (dnsleaktest.com) | AC1 |
| 7.3 | Zéro fuite IP (whatismyip.com) | AC2 |
| 7.4 | Zéro fuite WebRTC (browserleaks.com/webrtc) | AC3 |
| 7.5 | Changement de pays via webview | AC5 |
| 7.6 | Failover (relais down → bascule < 5s) | AC4 |
| 7.7 | Shutdown propre (DNS, SysProxy, processus) | AC6 |
| 7.8 | Crash recovery (taskkill → SCM restart) | AC7 |
| 7.9 | Fermeture webview indépendante du tray | AC6 |
| 7.10 | Proxy CONNECT résilient (pas de hang) | AC8 |
| 7.11 | WinINET recovery après crash UI | AC7 |
| 7.12 | Blocklist DNS (activation/désactivation) | AC1 |

## Architecture

Le Voile utilise une architecture **2 processus** :

- **`levoile-service.exe`** (service Windows SCM) : tunnel QUIC, DNS proxy, kill switch, HTTP proxy CONNECT, politiques navigateur, leak checker, STUN, blocklist
- **`levoile-ui.exe`** (UI unique) : fyne.io/systray + webview/webview + serveur HTTP local + WinINET proxy

La communication inter-processus utilise des named pipes Windows (`\\.\pipe\levoile`) avec un protocole JSON ligne par ligne.
