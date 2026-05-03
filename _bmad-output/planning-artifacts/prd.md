---
stepsCompleted: ['step-01-init', 'step-02-discovery', 'step-02b-vision', 'step-02c-executive-summary', 'step-03-success', 'step-04-journeys', 'step-05-domain', 'step-06-innovation', 'step-07-project-type', 'step-08-scoping', 'step-09-functional', 'step-10-nonfunctional', 'step-11-polish', 'step-12-complete', 'step-e-01-discovery', 'step-e-02-review', 'step-e-03-edit', 'step-e-01-discovery-2', 'step-e-02-review-2', 'step-e-03-edit-2', 'step-e-01-discovery-3', 'step-e-02-review-3', 'step-e-03-edit-3']
inputDocuments: ['../brainstorming/brainstorming-session-2026-03-08-1530.md', 'architecture.md', 'implementation-readiness-report-2026-04-29-android.md']
workflowType: 'prd'
documentCounts:
  briefs: 0
  research: 0
  brainstorming: 1
  projectDocs: 0
classification:
  projectType: 'desktop_app + mobile_app + network_server'
  domain: 'cybersecurity_privacy'
  complexity: 'high'
  projectContext: 'greenfield'
lastEdited: '2026-04-30'
editHistory:
  - date: '2026-04-30'
    changes: 'Polish post-validation 2026-04-30 (Top 3 Improvements identifiés au rapport `validation-report-2026-04-30.md`) : (1) Mapping §4 enrichi de 4 lignes Léa #9 (FR-AND-5 JS Bridge, FR-AND-6 détection autre VPN, FR-AND-8 zéro télémétrie, FR-AND-10 config getFilesDir) — parallélisme complet avec le pattern Léa #9 ; (2) FR-AND-9 précisé : périodicité auto-update au lancement + WorkManager 24h (cohérence FR35 desktop) ; (3) NFR-AND-2 condition réseau ajoutée : LTE/4G+ ou Wi-Fi domestique, RTT < 80ms vers VPS relais (parallèle NFR11 desktop ADSL/fibre RTT < 50ms).'
  - date: '2026-04-30'
    changes: 'Prise en compte Phase Android (Phase 2) — alignement PRD avec architecture (révision 2026-04-29, ADR-08 à ADR-15) suite au rapport d''implementation-readiness 2026-04-29 identifiant 3 gaps critiques. Étendu §1 (Executive Summary), §2 (Classification : type desktop+mobile, distribution F-Droid+APK, complexité gomobile/JNI), §3 (Success Criteria : sous-section Phase 2 avec démarrage VpnService < 3s, RAM < 60 MB, APK < 25 MB, taux activation "VPN permanent" > 95%, build F-Droid reproductible). Ajouté Parcours 9 (Léa, utilisatrice Android grand public) + 6 lignes Journey→Capabilities Mapping. Étendu §5 (TalkBack, sous-sections Distribution Android + Permissions Android Minimales, 4 risques Android au tableau). Étendu §6 (innovation frontend partagé desktop↔Android + noyau Go via gomobile, compromis kill switch délégué OS + pas autostart boot, validation reproductibilité F-Droid + TalkBack). Renommé §7 "Desktop App Requirements" → "Client App Requirements" avec sous-sections Desktop (Windows+Linux) + Android Phase 2 (architecture mono-processus MainActivity+WebView + LeVoileVpnService Foreground+VpnService, gomobile .aar, lifecycle, auto-update F-Droid/APK direct, Platform Support étendu). Étendu §8 Phase 2 avec support Android explicite + hors-scope iOS/Wear/TV/Auto. §9 : ajouté FR-AND-1..10 dans sous-section "Phase 2 — Android" (VpnService Builder, Foreground Service, onboarding "VPN permanent" + deeplink, MainActivity+WebView+JS Bridge, détection autre VPN via VpnService.prepare, distribution F-Droid+APK signé v2/v3, zéro télémétrie, auto-update différencié, config JSON getFilesDir). Updates ciblés FR8/14/15/15b/16/16b/20/22 pour clarifier scope OS. §10 : ajouté NFR-AND-1..11 dans sous-section "Phase 2 — Android" (RAM, démarrage, APK size, minSdk 29 targetSdk 34, signature v2+v3, build reproductible, permissions minimales auditables, zéro télémétrie auditée, logs filtrés, matrice tests Espresso API 29/33/34, R8/ProGuard release). Updates NFR9j (scoped storage Android), NFR22 (matrice étendue Android), NFR22a (Logcat), NFR22d (pipeline CI Android : gradle lint, audit dépendances, reproductibilité), NFR26 (vérif intégrité APK déléguée PackageManager). TOC §7 mis à jour. Classification projectType étendu : desktop_app + mobile_app + network_server.'
  - date: '2026-04-15'
    changes: 'Polish post-validation (Top 3 Improvements): (1) 8 corrections mesurabilité (FR13c texte exact, FR15 leakage fyne retiré, FR19b quantif chiffré, NFR2 libs Go nommées, NFR3 TTL chiffrés, NFR11 condition précisée, NFR15 méthode mesure, NFR22 matrice tests). (2) Ajout TOC + numérotation H2 1-10. (3) 2 User Journeys edge: Théo #7 (IPv6 opt-in), Camille #8 (mode dégradé). Journey→Capabilities Mapping étendu.'
  - date: '2026-04-15'
    changes: 'Ajout support Linux (Debian/Ubuntu, Fedora/RHEL, Arch, Alpine). Bascule capture L3 unifiée (TUN Linux / Wintun Windows) remplaçant proxy HTTP CONNECT + proxy DNS local. Kill switch firewall OS-level (nftables / WFP). Suppression extension navigateur (FR37-40) + bypass > 50 Mo. FR5-8 et FR27-30 reformulés. DNS et blocklist déplacés côté relais. Classification: suppression browser_extension. Snapshot préservation: git tag windows-stable-2026-04-15. Renforcement sécurité via audit adversarial (Red Team, What If, Security Audit) : NFR9c-j, NFR22a-i, NFR25-26, FR5b/c, FR7b, FR8c/d, FR13c, FR15b, FR16b, FR20b. Conformité RGPD/RGAA/Disclosure. Warrant canary advancé au MVP.'
  - date: '2026-04-08'
    changes: 'Suppression mode portable, remplacement Wails v2 par webview/webview + fyne.io/systray (binaire UI unique). Architecture 2 processus (service + UI). FR14-16, FR20-22 reformulés. Desktop App Architecture réécrite. Risques et compromis portable supprimés.'
  - date: '2026-04-02'
    changes: 'Alignement PRD avec code réel — multi-processus (service kardianos/service + tray fyne.io/systray + desktop Wails v2), IPC named pipes, mode portable alternatif, installeur NSIS Windows. FR14-16 et FR20-22 reformulés, architecture desktop réécrite.'
  - date: '2026-03-30'
    changes: 'Alignement PRD avec architecture mono-processus portable Wails v3 — suppression service OS/IPC, Wails v2→v3 + systray natif, binaire portable unique (suppression installateur), FR14-16 et FR20-22 reformulés, démarrage auto→raccourci Startup optionnel.'
  - date: '2026-03-16'
    changes: 'Alignement PRD avec architecture révisée — multi-relais, UI Wails v2, registre distribué, IP camouflage, anti-fuite, extension navigateur, auto-update. 22→40 FRs, 16→20 NFRs.'
---

# Product Requirements Document - Le Voile de Vélia

**Author:** Akerimus
**Date:** 2026-03-08
**Dernière révision :** 2026-04-30

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Project Classification](#2-project-classification)
3. [Success Criteria](#3-success-criteria)
4. [User Journeys](#4-user-journeys)
5. [Domain-Specific Requirements](#5-domain-specific-requirements)
6. [Innovation & Novel Patterns](#6-innovation--novel-patterns)
7. [Client App Requirements](#7-client-app-requirements)
8. [Project Scoping & Phased Development](#8-project-scoping--phased-development)
9. [Functional Requirements](#9-functional-requirements)
10. [Non-Functional Requirements](#10-non-functional-requirements)

## 1. Executive Summary

Le Voile est un VPN desktop qui garantit le zero-log par architecture — les relais VPS sont stateless, il n'y a physiquement rien à enregistrer. Contrairement aux VPN traditionnels qui promettent le zero-log par politique de confidentialité, Le Voile le prouve par le design et le code source ouvert.

Destiné au grand public francophone soucieux de sa vie privée, le produit cible un besoin urgent : la France s'apprête à bloquer les VPN traditionnels. Le Voile y survivra grâce à un trafic QUIC/HTTP/3 indistinguable du trafic web normal (TLS 1.3, ALPN h3, SNI standard), sans handshake VPN détectable au DPI.

Le client desktop 2 processus (service privilégié + UI unique combinant system tray et webview) est disponible sur **Windows et Linux** (Debian/Ubuntu, Fedora/RHEL, Arch, Alpine). Il capture tout le trafic IP de la machine via une interface virtuelle **TUN (Linux) / Wintun (Windows)**, l'encapsule dans HTTP/3 en connexion directe au VPS relais (DNS A record → origin, pas de CDN intermédiaire), et l'achemine vers un relais stateless qui agit comme gateway NAT. Le DNS est résolu côté relais (avec blocklist StevenBlack/hosts intégrée). Un **kill switch firewall OS-level** (nftables Linux / Windows Filtering Platform) survit aux crashes du service et rend les fuites structurellement impossibles.

**Android (Phase 2)** — Application mobile prévue pour Android 10+ (API 29+), distribuée via F-Droid + APK direct GitHub releases. Architecture mono-processus distincte du desktop : `MainActivity` (WebView plein écran avec assets HTML/CSS/JS partagés desktop) + `LeVoileVpnService` (Foreground Service héritant de `android.net.VpnService` — seule API non-rootée pour la capture L3). Le kill switch est délégué à l'OS via le réglage utilisateur "VPN permanent + bloquer connexions sans VPN" (kernel-level, inviolable côté app), guidé par un onboarding obligatoire au premier lancement. Le noyau Go (protocole, registre, auth, crypto, leak check) est partagé avec le desktop via gomobile (`.aar`), garantissant la cohérence de la couche réseau cross-OS.

Gratuit, financé par donations, distribué via plateformeliberte.fr.

### Ce qui rend Le Voile unique

- **Confiance par le design** — Relais stateless, zéro donnée à compromettre, code source ouvert (github.com/velia-the-veil/le_voile)
- **Indétectable** — Trafic QUIC/HTTP/3 avec TLS 1.3 + ALPN h3 + SNI standard, structurellement identique à une visite navigateur. Résistant au blocage des VPN traditionnels au DPI
- **Multi-relais géographiques** — Relais répartis par pays, failover automatique, registre distribué sans point de défaillance unique
- **Capture L3 machine-wide** — Interface TUN/Wintun capturant tout le trafic IP du système. Aucune configuration applicative requise : navigateurs, mail, jeux, clients BitTorrent — tout passe par le tunnel sans exception
- **Kill switch firewall OS-level** — Règles kernel (nftables / WFP) qui survivent aux crashes du service. Impossible à contourner
- **Anti-fuite structurelle** — DNS, WebRTC, IPv6 ne peuvent pas fuir par design : tout passe par la TUN ou est droppé par le firewall
- **Zero-config** — Le produit fonctionne dès le lancement

## 2. Project Classification

- **Type :** Application desktop + mobile (client) + serveurs réseau (relais VPS stateless multi-pays)
- **Domaine :** Cybersécurité / Vie privée
- **Complexité :** Élevée — QUIC/HTTP3, encapsulation IP (TUN/Wintun/VpnService), NAT côté relais, nftables/WFP, kill switch OS-delegated Android, Ed25519, registre distribué, packaging multi-distro Linux, gomobile + JNI bridge Android, build reproductible F-Droid
- **Contexte :** Greenfield
- **Distribution :**
  - Windows : Installeur NSIS (service SCM + UI + Wintun DLL) via GoReleaser
  - Linux : Paquets natifs .deb (Debian/Ubuntu), .rpm (Fedora/RHEL), AUR (Arch), .apk (Alpine) via GoReleaser + nfpm
  - Android (Phase 2) : F-Droid (catalogue officiel, build reproductible) + APK direct via GitHub releases (signé v2/v3 par master key Ed25519). Pas de Google Play en MVP/Phase 2
- **Ressources :** Développeur unique (Akerimus) + IA

## 3. Success Criteria

### User Success

- L'utilisateur lance Le Voile et est protégé immédiatement — zéro configuration
- L'UI affiche le pays sélectionné, le relais actif, l'IP visible et le statut de protection
- Tout le trafic IP de la machine passe par le tunnel (capture L3 machine-wide) sans action utilisateur
- Les requêtes DNS sont résolues côté relais (avec blocklist), masquant les noms consultés
- L'utilisateur peut choisir un pays parmi les relais disponibles
- Le failover entre relais d'un même pays est transparent — pas d'interruption perceptible, kill switch maintenu
- Aucune fuite possible par design : DNS, WebRTC, IPv6 ne peuvent structurellement pas contourner le tunnel
- Impact faible sur la navigation quotidienne (latence additionnelle acceptable pour la protection universelle)

### Business Success

- Le produit fonctionne de bout en bout (client → tunnel → relais multi-pays → internet)
- Des utilisateurs réels le téléchargent et l'utilisent au quotidien
- Projet open-source — le succès se mesure à l'adoption organique

### Measurable Outcomes

- Temps de lancement → protection active : < 30 secondes
- Zéro fuite DNS vérifiable via outils de test standard
- Zéro fuite IP WebRTC vérifiable via browserleaks.com, ipleak.net
- IP web masquée vérifiable via whatismyip.com
- Failover entre relais d'un même pays : < 5 secondes, transparent pour l'utilisateur
- Uptime par relais : ≥ 99.5% mensuel mesuré via endpoint /health

### Measurable Outcomes — Phase 2 (Android)

- Démarrage VpnService Android → tunnel actif : < 3 secondes sur Pixel 6+ ou équivalent (Snapdragon 7-gen 1+)
- Consommation RAM client Android : < 60 MB en fonctionnement normal (mesuré via `adb shell dumpsys meminfo`)
- Taille APK signé : < 25 MB (mesuré via `apkanalyzer apk file-size`)
- Taux d'activation "VPN permanent" via onboarding : > 95% des utilisateurs Android au premier lancement (mesuré via heuristique Settings.Global, agrégat anonyme local — non remonté)
- Build F-Droid reproductible : 2 builds successifs depuis le même tag git produisent un APK avec hash SHA256 identique

## 4. User Journeys

### Parcours 1 : Camille — Découverte et protection immédiate

**Qui :** Camille, 34 ans, journaliste freelance. Pas technique, cherche une solution simple face au blocage VPN annoncé.

**Scène d'ouverture :** Elle tombe sur plateformeliberte.fr via une recommandation et télécharge le binaire Le Voile.

**Action :** Elle lance l'installeur téléchargé. Installation rapide. L'icône Le Voile apparaît dans le tray — thème sombre aux couleurs de plateformeliberte.fr. Elle ouvre la fenêtre. Statut : "Connecté — Allemagne (de-01)". IP visible affichée.

**Moment clé :** Test de fuite DNS : zéro fuite. whatismyip.com affiche une IP allemande. Sans rien faire.

**Résolution :** Le Voile tourne en arrière-plan (icône tray), elle l'oublie. Exactement ce qu'elle voulait.

### Parcours 2 : Akerimus — Opérateur multi-relais

**Qui :** Akerimus, développeur et opérateur unique.

**Scène d'ouverture :** Deux VPS commandés — Allemagne et Espagne. Déployer les relais, configurer Cloudflare.

**Action :** Binaires déployés sur les VPS. Registre JSON créé avec les deux relais (de-01, es-01), déployé sur chaque instance. Sous-domaines Cloudflare configurés : de.levoile.dev, es.levoile.dev.

**Moment clé :** Premier test bout en bout — le client télécharge le registre, affiche les deux pays. Connexion à l'Allemagne, puis bascule vers l'Espagne. Failover testé en coupant de-01 — bascule automatique vers de-02.

**Résolution :** Ajout d'un relais = générer une clé Ed25519, ajouter au registre JSON, déployer. Pas de données à gérer.

### Parcours 3 : Camille — Tunnel coupé, failover transparent

**Qui :** Camille, quelques jours plus tard. Le Wi-Fi du café provoque une perte de connexion au relais.

**Action :** Le sélecteur bascule automatiquement vers un autre relais du même pays. Kill switch DNS actif pendant la bascule — aucune fuite.

**Moment clé :** Camille ne remarque rien. La fenêtre Le Voile affiche brièvement "Reconnexion..." puis "Connecté — Allemagne (de-02)".

**Résolution :** Changement de réseau. Tunnel reconnecté automatiquement. Rien fait manuellement.

### Parcours 4 : Camille — Choix du pays

**Qui :** Camille, veut apparaître depuis le Royaume-Uni pour un site géo-restreint.

**Action :** Elle ouvre la fenêtre Le Voile depuis l'icône tray. Le sélecteur de pays affiche les drapeaux avec le nombre de relais disponibles. Elle clique sur "Royaume-Uni (2 relais)".

**Moment clé :** Reconnexion automatique via un relais britannique. L'IP visible change. Le site géo-restreint fonctionne.

**Résolution :** Le pays préféré est sauvegardé — au prochain démarrage, Le Voile se connecte directement au Royaume-Uni.

### Parcours 5 : Camille — Navigation complètement protégée

**Qui :** Camille, navigue avec Le Voile actif.

**Action :** Toute son activité réseau (navigation web, mail client, streaming, jeu Steam, mise à jour Windows) passe par la TUN → relais → internet. Son IP réelle est masquée partout, pour toutes les applications, sans configuration.

**Moment clé :** Elle vérifie sur browserleaks.com — aucune fuite DNS, aucune fuite WebRTC, aucune fuite IPv6. L'IP affichée est celle du relais allemand. Elle n'a rien configuré.

**Résolution :** Protection universelle automatique. Chaque application est couverte sans action manuelle.

### Parcours 6 : Mathieu — Utilisateur Linux (Ubuntu)

**Qui :** Mathieu, développeur, utilise Ubuntu 24.04 au quotidien.

**Scène d'ouverture :** Il découvre Le Voile via un article sur plateformeliberte.fr.

**Action :** `sudo apt install ./levoile_1.0.0_amd64.deb`. Le paquet installe le service systemd, l'UI, configure les capabilities (setcap CAP_NET_ADMIN). L'UI se lance à la prochaine session via autostart XDG. Il clique "Connecter".

**Moment clé :** `ip addr` montre l'interface `levoile0` active. `curl ifconfig.me` retourne une IP allemande. Il coupe le service : `sudo systemctl stop levoile` — internet est instantanément coupé (kill switch nftables). Il relance : connexion rétablie en 2 secondes.

**Résolution :** Même expérience que sur Windows, dans l'écosystème Linux natif (systemd, apt, tray GNOME/KDE).

### Parcours 7 : Théo — Utilisateur technique activant IPv6

**Qui :** Théo, 28 ans, développeur réseau, utilise un FAI dual-stack IPv4/IPv6. Il est conscient que le MVP ne tunnelise pas IPv6.

**Scène d'ouverture :** Il installe Le Voile, se connecte. Vérifie sur `test-ipv6.com` → l'IPv6 est bloqué (kill switch drop tout IPv6 sortant).

**Action :** Il préfère laisser l'IPv6 fonctionner en direct (hors VPN) plutôt que d'être bloqué. Il ouvre la fenêtre Le Voile → Paramètres avancés → case "Autoriser IPv6 hors tunnel" (décochée par défaut).

**Moment clé :** Une modale d'avertissement s'affiche : "L'IPv6 ne sera PAS protégé par Le Voile et exposera votre IP réelle sur les services IPv6. Continuer ?" Il valide en connaissance de cause. `test-ipv6.com` fonctionne maintenant, avec son IPv6 publique visible ; l'IPv4 reste masqué par le relais allemand.

**Résolution :** Mode hybride assumé : IPv4 protégé, IPv6 direct. Paramètre persisté en config TOML (`[tunnel] allow_ipv6_leak = true`). Théo attend Phase 2 pour le tunneling IPv6 complet.

### Parcours 8 : Camille — Kill switch bloquant en mobilité

**Qui :** Camille, en déplacement dans une gare. Connexion Wi-Fi publique peu stable — Le Voile n'arrive pas à joindre un relais pendant 2 minutes. Kill switch firewall reste actif : pas d'internet.

**Action :** Elle doit absolument envoyer un email urgent via le webmail de son employeur. Elle clique sur l'icône tray → menu "Mode dégradé" → confirmation "Voulez-vous désactiver la protection temporairement ? Votre trafic ne sera PAS chiffré. L'icône tray deviendra rouge jusqu'à rétablissement du tunnel."

**Moment clé :** Kill switch désactivé, internet accessible en clair. Icône tray passe au rouge + bandeau rouge permanent dans la fenêtre webview. Elle envoie son email. 3 minutes plus tard, le tunnel se rétablit automatiquement → Le Voile détecte la connexion réussie → kill switch réactivé → icône retrouve sa couleur verte.

**Résolution :** Mode dégradé transitoire, automatiquement réversible. Camille n'a pas été coincée sans internet. L'indicateur visuel permanent l'a empêchée d'oublier qu'elle n'était pas protégée.

### Parcours 9 : Léa — Utilisatrice Android grand public (Phase 2)

**Qui :** Léa, 29 ans, communicante. Smartphone Pixel 7 (Android 14, API 34). Cherche un VPN simple sur mobile.

**Scène d'ouverture :** Elle découvre Le Voile via plateformeliberte.fr → page Android. Deux options proposées : F-Droid (recommandé, build reproductible) ou APK direct GitHub releases (signé v2/v3, vérifiable par PackageManager).

**Action :** Elle installe via F-Droid. Au premier lancement, popup système Android natif : "Le Voile demande l'autorisation de configurer un VPN" (consent VpnService). Elle accepte. L'app affiche un onboarding obligatoire : "Pour une protection maximale, activez 'VPN permanent + bloquer connexions sans VPN' dans les paramètres système. Sans cela, Le Voile peut être contourné par des apps qui démarrent avant lui." Bouton "Ouvrir les paramètres" → deeplink `Settings.ACTION_VPN_SETTINGS`. Elle active les deux toggles. Retour à l'app.

**Moment clé :** Statut "Connecté — Allemagne (de-01)". Notification persistante dans la barre de statut Android (icône Le Voile + "Connecté Allemagne — IP 5.x.x.x"). whatismyip.com via Chrome confirme l'IP allemande. Elle ferme l'app par swipe → la notification reste, le tunnel reste actif (Foreground Service).

**Résolution :** Usage normal du téléphone. Aucun trafic ne sort hors tunnel (kill switch OS). Le mode économie d'énergie agressif n'arrête pas le service (Foreground Service exempt). Au reboot, Léa relance l'app manuellement (pas d'autostart au boot — limitation Android), mais le réglage OS "VPN permanent" reconnecte automatiquement le dernier VPN actif avant qu'elle n'ouvre l'app.

### Journey → Capabilities Mapping

| Capacité requise | Parcours source |
|---|---|
| Installeur NSIS Windows | Camille #1 |
| Paquets Linux natifs (.deb/.rpm/.apk/AUR) | Mathieu #6 |
| UI webview/webview : fenêtre desktop, charte plateformeliberte.fr (Windows + Linux) | Camille #1, #4 ; Mathieu #6 |
| Sélecteur de pays (drapeaux, nombre relais) | Camille #4 |
| System tray fyne.io/systray : icône d'état + accès rapide fenêtre (même processus que webview) | Camille #1, #3 ; Mathieu #6 |
| Tunnel QUIC/HTTPS auto-connecté | Camille #1 |
| Capture L3 machine-wide (TUN Linux / Wintun Windows) | Camille #1, #5 ; Mathieu #6 |
| Kill switch firewall OS-level (nftables / WFP) | Camille #3 ; Mathieu #6 |
| Reconnexion automatique + failover multi-relais (firewall maintenu) | Camille #3 |
| DNS via relais (avec blocklist côté relais) | Camille #1 |
| Détection fuite WebRTC (validation TUN) | Camille #5 |
| Indicateur visuel d'état (connecté/en cours/déconnecté) | Camille #3, #4 |
| Relais multi-pays déployables sur VPS | Akerimus #2 |
| Registre distribué (chaque relais sert /.well-known/relay-registry.json) | Akerimus #2 |
| Relais stateless (zéro persistence) | Akerimus #2 |
| Service systemd Linux + SCM Windows via kardianos/service | Mathieu #6 |
| Option IPv6 hors tunnel (décochée par défaut) + avertissement explicite | Théo #7 |
| Mode dégradé (désactivation temporaire kill switch) + indicateur visuel permanent | Camille #8 |
| Réactivation automatique kill switch après rétablissement tunnel | Camille #8 |
| Capture L3 via `android.net.VpnService` (Builder + tun fd via establish) — Phase 2 | Léa #9 |
| Foreground Service avec notification persistante ongoing (rôle équivalent du tray Android) — Phase 2 | Léa #9 |
| Onboarding obligatoire "VPN permanent" + deeplink `Settings.ACTION_VPN_SETTINGS` — Phase 2 | Léa #9 |
| Kill switch délégué OS Android (réglage utilisateur "VPN permanent + bloquer connexions sans VPN") — Phase 2 | Léa #9 |
| MainActivity + WebView Android plein écran (assets HTML/CSS/JS partagés desktop, JS Bridge `@JavascriptInterface`) — Phase 2 | Léa #9 |
| Distribution F-Droid (build reproductible) + APK direct GitHub releases (signé v2/v3) — Phase 2 | Léa #9 |
| JS Bridge `@JavascriptInterface` exposant Connect/Disconnect/GetStatus/SelectCountry — Phase 2 | Léa #9 |
| Détection autre app VPN active via `VpnService.prepare()` (refus + message UI) — Phase 2 | Léa #9 |
| Zéro télémétrie / crash reporter / analytics côté Android (audit Gradle CI) — Phase 2 | Léa #9 |
| Config utilisateur Android persistée en JSON dans `getFilesDir()` (scoped storage) — Phase 2 | Léa #9 |

## 5. Domain-Specific Requirements

### Vie Privée & Réglementation

- **Zero-log architectural** — Relais stateless, aucune donnée persistée. Rien à fournir en cas de réquisition
- **Juridiction favorable** — Hébergement multi-pays (Allemagne, Espagne, Royaume-Uni, États-Unis), favorisant les juridictions respectueuses de la vie privée
- **RGPD** — Conformité par minimisation : aucune donnée personnelle collectée côté Le Voile. Mention explicite du rôle de Cloudflare (sous-traitant qui voit IP client + destination + timing) dans la politique de confidentialité publiée sur plateformeliberte.fr. DPA (Data Processing Agreement) Cloudflare signé et référencé
- **Mentions légales plateformeliberte.fr** — Page dédiée conforme RGPD Art. 12-14 : identité du responsable, base légale du traitement technique (intérêt légitime), durées de conservation (RAM uniquement), droits de l'utilisateur, contact DPO (Akerimus)
- **Code source ouvert** — Auditable publiquement dès le MVP
- **Confidentialité renforcée** — Aucun log IP client sur les relais. Hash IP uniquement dans les session tokens (TTL 4h)
- **Juridiction opérateur** — Akerimus est français, susceptible de réquisition L. 851-1 CPCE. Mitigation : architecture zero-log + relais hors France (rien à fournir techniquement). Warrant canary (déclaration publique mensuelle d'absence de réquisition) à mettre en place dès le MVP sur plateformeliberte.fr — advancé de Phase 3 à MVP
- **Pas d'export international** — Le Voile est destiné aux utilisateurs francophones. Aucune distribution ciblée hors France/Belgique/Suisse/Canada francophone. Pas de contrainte d'export de crypto

### Disclosure & Incident Response

- **SECURITY.md** publié à la racine du repo GitHub : canal de signalement (security@plateformeliberte.fr), PGP public key, SLA de triage (48h), délai de disclosure coordonné (90 jours par défaut, ajustable par sévérité)
- **security.txt** (RFC 9116) publié à la racine de plateformeliberte.fr
- **Bug bounty informel** — reconnaissance publique (hall of fame sur plateformeliberte.fr) pour les chercheurs, sans budget monétaire au MVP
- **SLA patch CVE critique** : 7 jours pour CVE avec CVSS ≥ 9 affectant Le Voile ou dépendance directe (quic-go, wireguard/tun, kernel). 30 jours pour CVSS 7-8
- **Registre d'incidents** — toute fuite/faille confirmée documentée publiquement (postmortem anonymisé) sur plateformeliberte.fr, même avant patch si transparence > risque

### Accessibilité

- **RGAA niveau AA** (exigence légale produits français) — cibles minimales MVP : contraste AA (4.5:1 texte), navigation clavier complète, aria-labels sur tous contrôles, taille police réglable, focus visible. Test avec lecteur d'écran NVDA (Windows) / Orca (Linux) / **TalkBack (Android — Phase 2)** avant release. Sur Android, cibles tactiles ≥ 48dp (Material guidelines), navigation séquentielle TalkBack sans piège (focus order cohérent dans la WebView)

### Distribution Android (Phase 2)

- **F-Droid** — Catalogue officiel logiciel libre. Build reproductible obligatoire (vérifié par fingerprinting APK hash entre 2 builds successifs depuis le même tag git). Métadonnées XML versionnées dans le repo. Pas de compte Google requis pour l'utilisateur
- **APK direct** — Disponible via GitHub releases, signé v2 (APK Signature Scheme v2) + v3 (key rotation) par la master key Ed25519 (cohérent NFR22g). Vérifié automatiquement à l'install par PackageManager Android
- **Pas de Google Play en MVP/Phase 2** — Justification : ToS Play Store sur les VPN apps trop restrictives, scrutin chronophage, contradiction avec posture OSS. Phase 3+ : publication Play Store si traction (AAB déjà buildable, Play App Signing à mettre en place)
- **Mise à jour** — F-Droid : géré par le client F-Droid de l'utilisateur. APK direct : notification UI + lien GitHub releases (pas d'auto-update embarqué — limitation Android non-rooté + posture sécurité)

### Permissions Android Minimales (Phase 2)

Permissions déclarées dans `AndroidManifest.xml` :

- `INTERNET` — connexion réseau
- `FOREGROUND_SERVICE` + `FOREGROUND_SERVICE_DATA_SYNC` (API 34+) — Foreground Service obligatoire pour héberger VpnService long-lived
- `POST_NOTIFICATIONS` (API 33+) — notification persistante du Foreground Service
- `BIND_VPN_SERVICE` — déclaration du service VpnService (consent utilisateur via popup système Android natif)

**Aucune permission dangereuse** : pas de `READ_PHONE_STATE`, pas de `READ_CONTACTS`, pas de localisation (`ACCESS_FINE_LOCATION`/`ACCESS_COARSE_LOCATION`), pas de stockage externe (`READ_EXTERNAL_STORAGE`/`WRITE_EXTERNAL_STORAGE`), pas de `CAMERA`, pas de `RECORD_AUDIO`. Auditable via `apkanalyzer manifest permissions`.

### Contraintes Techniques Domaine

- **Résistance DPI** — Trafic indiscernable du trafic web normal via QUIC/HTTPS Cloudflare
- **Cryptographie standard** — Ed25519 via bibliothèques cryptographiques standard uniquement. Pas de crypto maison. Une paire de clés par relais
- **Capture L3 propre** — Interface TUN/Wintun créée à l'activation, détruite à la désactivation. Routes système restaurées. Règles firewall retirées. Zéro résidu
- **Protection SSRF** — Le relais bloque les paquets IP sortants vers les réseaux privés (loopback, RFC 1918, link-local)
- **Validation source** — Le relais vérifie que les requêtes proviennent des plages IP Cloudflare
- **Privilèges minimum** — Service Linux avec capabilities CAP_NET_ADMIN + CAP_NET_RAW uniquement (pas root complet). Service Windows en LocalSystem (requis pour WFP)

### Risques & Mitigations

| Risque | Impact | Mitigation |
|---|---|---|
| Cloudflare bloque le domaine | Tunnel inaccessible | Phase 3 : connexion directe bypass Cloudflare |
| Saisie d'un VPS | Service interrompu sur ce pays | Stateless — rien à trouver. Failover automatique vers les relais restants |
| Compromission clé Ed25519 d'un relais | Usurpation d'identité du relais | Clé unique par relais — révocation granulaire, les autres relais non affectés |
| Antivirus/firewall bloque le client | Lancement échoué | Avertissement page de téléchargement. Instructions de mise en liste blanche. Wintun DLL signée Microsoft facilite la confiance |
| Fuite pendant reconnexion / failover | IP exposée | Kill switch firewall (nftables/WFP) reste actif pendant toute la durée — zéro fenêtre de fuite |
| Blocage VPN par la France | Produit inutile | Architecture anti-détection : QUIC/HTTPS standard, pas de signature VPN |
| Fuite WebRTC | IP réelle exposée via navigateur | Couverture structurelle par la capture L3 — les paquets STUN/ICE passent par la TUN obligatoirement |
| Crash du service → kill switch actif | Plus d'internet jusqu'au redémarrage | Service systemd/SCM redémarre automatiquement. Règles firewall persistent entre-temps (sécurité > disponibilité) |
| Le Voile fermé → navigation sans protection | L'utilisateur navigue sans s'en rendre compte | Service persiste au boot. Le tray UI reste actif même si la fenêtre est fermée. Indication claire d'état dans l'icône tray |
| Relais saturés par adoption organique | HTTP 503, failover épuise le pool, déconnexion en boucle | Monitoring /health (NFR18), seuil d'alerte opérationnel à 80% de capacité → ajouter un relais |
| Auto-update appliquée mais fonctionnellement cassée | Crash en boucle + internet bloqué (kill switch) pour tous les utilisateurs | Critère de santé post-update : tunnel établi dans les 30s, sinon rollback automatique (FR36) |
| WebView2 Runtime absent (Windows) | Le binaire crash ou affiche un message cryptique | Détection d'absence au lancement + message clair + lien de téléchargement WebView2 |
| WebKitGTK ou libayatana-appindicator3 absent (Linux) | L'UI ne se lance pas | Dépendance déclarée dans les paquets (.deb/.rpm/AUR/APK) — résolue automatiquement par le gestionnaire de paquets |
| nftables absent (Linux très ancien) | Kill switch impossible | Dépendance runtime déclarée. Message d'erreur clair si détecté absent au runtime : "nftables required, please install" |
| GNOME Wayland sans extension appindicator | Icône tray invisible | Documenter dans README : installer `gnome-shell-extension-appindicator` (présent sur 95%+ des installs Ubuntu/Fedora standard) |
| IPv6 mal géré par la TUN | Connectivité dégradée sur réseaux IPv6 | MVP : IPv6 bloqué par défaut. Option FR8d pour autoriser IPv6 hors tunnel (avec avertissement). Tunneling complet Phase 2 |
| Firewall tiers Windows (Comodo, ZoneAlarm, Norton) interfère avec WFP | Règles Le Voile supprimées ou bloquées, kill switch inopérant | Détection runtime via WfpEnumFilters. Alerte UI si règles altérées. Doc d'installation : instructions whitelisting pour firewalls tiers majeurs |
| Compromission de la master key Ed25519 | Effondrement de la chaîne de confiance (registre, releases, paquets) | Master key stockée air-gapped (machine dédiée) ou YubiKey HSM. Chaîne de confiance avec clé de rotation embarquée dans les clients permettant la bascule. Rotation planifiée tous les 24 mois (NFR22g/h) |
| Compromission du compte GitHub velia-the-veil | Publication de release malveillante signée par la clé privée dev compromise | Compte protégé 2FA hardware (YubiKey), signing commits GPG obligatoire, branch protection main. Clé privée de signature release stockée séparément (pas sur la machine GitHub) (NFR22i) |
| DDoS sur relais via multiplication de tunnels | Saturation NAT table, déni de service | Rate limits par IP (200 tunnels/IP FR30) + limite globale (150 tunnels/relais FR30b). Cloudflare bot fight mode + rate limiting CDN. Ajout de relais à la demande |
| DNS poisoning upstream (Cloudflare 1.1.1.1 compromis ou légalement contraint) | DNS dirigé vers serveurs malveillants | Validation DNSSEC côté relais (NFR9f). Fallback Quad9 9.9.9.9. Monitoring cohérence résultats entre upstreams |
| Modification config TOML par malware local | Activation furtive de `allow_ipv6_leak` ou désactivation kill switch | Config TOML permissions 0600 (Linux) / ACL user-only (Windows). HMAC signé au démarrage, écart détecté = refus de démarrer avec alerte UI (NFR9j) |
| Révocation de la signature Microsoft Wintun DLL | Wintun inutilisable sur Windows → produit cassé | Veille sur bulletins Microsoft. Fallback documenté : build custom driver signé via EV certificate (budget Phase 2). Probabilité faible (Wintun est stable depuis 2019) |
| Injection de paquets sur TUN par malware root | Paquets malveillants forwardés via le relais, IP relais grillée | Watchdog TUN détecte perturbations (NFR9g). Rate limiting par tunnel. Rotation IP relais si grillage détecté (opérationnel) |
| Session token volé via compromis mémoire client | Usurpation d'identité pendant TTL 4h | Validation IP hash côté relais (NFR9d) — token utilisable uniquement depuis IP d'origine. TTL court (4h) limite la fenêtre |
| **(Android Phase 2)** L'utilisateur n'active pas "VPN permanent" Android | Kill switch absent — fuites possibles au reconnect ou au boot avant lancement de l'app | Onboarding obligatoire au premier lancement avec deeplink `Settings.ACTION_VPN_SETTINGS` + warning persistant UI tant que non-activé. Détection heuristique via Settings.Global au lancement |
| **(Android Phase 2)** Autre app VPN active (Tailscale, Wireguard, OpenVPN) prend le slot VpnService | Le Voile ne peut pas établir le tunnel | Détection au lancement via `VpnService.prepare()` qui retourne un Intent. Message UI explicite : "Une autre application VPN est active. Désactivez-la pour utiliser Le Voile." |
| **(Android Phase 2)** OS Android tue le Foreground Service en battery save agressif (OEM Xiaomi/Huawei/Oppo) | Tunnel coupé sans avertissement utilisateur | Foreground Service avec notification ongoing résiste au battery save sur Android 8+. Documentation : whitelist battery conseillée pour OEM agressifs. Reconnect automatique au prochain réveil app + reprise du "VPN permanent" OS |
| **(Android Phase 2)** F-Droid maintainer compromise | Distribution APK F-Droid altérée | Build reproductible : tout utilisateur peut vérifier que `sha256(APK F-Droid)` == `sha256(APK GitHub releases)` (signature v2+v3 master key Ed25519 inchangée). Documentation procédure de vérification publiée |

## 6. Innovation & Novel Patterns

### Innovation Areas

- **Registre distribué pair-à-pair** — Chaque relais sert l'intégralité du registre via `/.well-known/relay-registry.json`. Pas de serveur de coordination central, pas de point de défaillance unique. Le client cache le registre localement pour un fonctionnement résilient au démarrage (cold start)
- **Capture L3 + encapsulation HTTP/3** — Paquets IP bruts capturés par TUN/Wintun, encapsulés dans un stream HTTP/3 vers le relais direct. Combine capture universelle (VPN traditionnel) + camouflage protocolaire (indiscernable d'HTTPS). Aucun produit grand public ne combine les deux
- **Modèle gateway NAT côté relais** — Le client n'embarque pas de stack TCP/IP userspace. Le relais désencapsule, applique NAT, forwarde. Simplifie massivement le client sans sacrifier les performances
- **Camouflage protocolaire** — QUIC/HTTP/3 + TLS 1.3 direct vers le relais (ALPN h3, SNI = domaine relais). Pour un observateur réseau, Le Voile ressemble à un site web ordinaire
- **Kill switch firewall kernel-level** — Règles nftables (Linux) / WFP (Windows) qui survivent aux crashes du service client. Impossible à contourner accidentellement ou par défaut d'arrêt du process
- **Frontend HTML/CSS/JS partagé desktop ↔ Android (Phase 2)** — Mêmes assets servis par WebView Android (`WebViewAssetLoader`) que par webview desktop, synchronisés au build via `android/scripts/sync-frontend.sh`. Cohérence visuelle 100% cross-OS (charte plateformeliberte.fr réutilisée), réutilisation maximale du frontend, pas de réécriture UI native (ni Compose, ni Flutter, ni RN). JS Bridge `@JavascriptInterface` exposant les commandes natives côté Android — équivalent fonctionnel du serveur HTTP local desktop, sans ouvrir de port localhost
- **Noyau Go partagé via gomobile (Phase 2)** — 5 shims gomobile-compatibles sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` compilés en `.aar` consommable par Gradle (cf. erratum architecture ADR-09 Story 9.2 — `protocol`/`auth` shims canoniques, les 3 autres consommant en lecture les packages racine `internal/{crypto,registry,leakcheck,tunnel}`). Réutilisation 100% de la logique protocole/crypto déjà testée desktop. Pattern éprouvé production (Tailscale Android, WireGuard Android)

### Compromis Documentés

- **Tunnel pour tout le trafic → latence additionnelle** — Tout le trafic IP (web, mail, jeux, mises à jour) transite par le relais. Impact : latence légèrement accrue même pour les flux légers. Compromis accepté — protection universelle > performance ciblée
- **Abandon du bypass > 50 Mo** — Les gros téléchargements (vidéos, jeux, ISO) consomment la bande passante des relais. Compromis accepté — cohérence de la protection, simplicité. Les relais sont dimensionnés pour absorber la charge (10 GiB/jour par IP)
- **Dépendance à WebKitGTK sur Linux** — UI HTML/CSS/JS nécessite une runtime WebView. Compromis accepté — réutilisation du frontend Windows, packaging moderne
- **DNS résolu côté relais** — Le relais voit les noms de domaine résolus en mémoire courte (le temps de la résolution, non persisté). Compromis accepté : blocklist StevenBlack centralisée, moins de dépendance au resolver système client. **Changement de modèle de confiance** : l'utilisateur déplace la confiance DNS de son ISP vers l'opérateur du relais — c'est le trade-off inhérent à tout VPN. Zero-log par architecture côté relais (aucune persistence, RAM effacée au restart)
- **Kill switch délégué OS Android (Phase 2)** — Sur Android non-rooté, impossible d'installer des règles iptables/eBPF côté app (restriction kernel). Le réglage OS "VPN permanent + bloquer connexions sans VPN" est kernel-level, inviolable côté app, persiste aux crashes/redémarrages — supérieur à toute simulation app-level (reconnect-loop, drop sockets). Compromis : dépendance à un toggle utilisateur explicite, mitigée par onboarding obligatoire + warning persistant UI tant que non-activé. Détection heuristique via `Settings.Global` (API non-publique, fragile — fallback "non vérifiable" si Android change l'API)
- **Pas d'autostart au boot Android (Phase 2)** — Limitation OS (Android 10+ restrictions sur les BootCompletedReceiver pour les apps en background). L'utilisateur doit ouvrir l'app après reboot pour relancer la connexion. Atténuation : le réglage OS "VPN permanent" reconnecte automatiquement le dernier VPN actif au reboot, donc le tunnel est rétabli avant que l'utilisateur ne lance l'app

### Validation

- Test fuite DNS : dnsleaktest.com, ipleak.net
- Test fuite WebRTC : browserleaks.com, ipleak.net
- Test IP masquée : whatismyip.com
- Test DPI : Wireshark — vérifier absence de signature protocolaire VPN (indiscernabilité par 0 pattern-match sur 100 échantillons de trafic)
- Tests terrain avec amis d'Akerimus
- **Tests Android (Phase 2)** : reproductibilité F-Droid (2 builds successifs → APK avec hash SHA256 identique), accessibilité TalkBack (navigation séquentielle complète sans piège), audit permissions (`apkanalyzer manifest permissions` ne révèle aucune permission dangereuse), audit dépendances Gradle (aucun module Firebase/Sentry/Bugsnag/Mixpanel/analytics)

## 7. Client App Requirements

Le client se décline en deux familles d'arbres de code complets et autonomes (cf. ADR-08 : isolation OS maximale, duplication assumée). Le strict noyau protocole/crypto/registre/session est mutualisé via packages Go (5 packages exposés cross-platform — natif sur desktop, gomobile `.aar` sur Android).

### Desktop (Windows + Linux)

#### Architecture

**Architecture 2 processus (Windows + Linux) :**
- **levoile-service** — Service privilégié cross-platform via kardianos/service. Orchestrateur principal : TUN/Wintun interface, firewall (nftables/WFP), routing, tunnel QUIC, registry discovery, failover, leak check, updater. Expose un serveur IPC.
  - Windows : service SCM, tourne en LocalSystem
  - Linux : service systemd, tourne en user `levoile` avec capabilities CAP_NET_ADMIN + CAP_NET_RAW
- **levoile-ui** — UI unique user-space combinant fyne.io/systray + webview/webview dans un seul processus. Icône tray avec menu contextuel, fenêtre webview 420×540px ouverte/fermée à la demande, serveur HTTP local embarqué servant les assets frontend et exposant une API REST JSON. Polling status via IPC. Singleton via mutex nommé (Windows) ou flock (Linux)
- **Communication** — Service ↔ UI : IPC via named pipes (Windows `\\.\pipe\levoile`) ou Unix sockets (Linux `/run/levoile/ipc.sock`). Protocole JSON ligne par ligne, max 4 Ko. Le service est l'autorité, l'UI est un client IPC. Frontend JS ↔ UI Go : serveur HTTP local embarqué (API REST JSON sur 127.0.0.1:{port})

**Installeur NSIS (Windows) :**
- Installation : service SCM, UI autostart (HKCU), shortcuts desktop/Start menu, Wintun DLL (signée Microsoft)
- Désinstallation propre : suppression filters WFP, suppression TUN adapter, suppression shortcuts

**Paquets natifs Linux :**
- .deb (Debian 11+, Ubuntu 22.04+), .rpm (Fedora 38+, RHEL 9+), .apk (Alpine 3.18+), AUR (Arch rolling)
- Post-install : `setcap cap_net_admin,cap_net_raw+ep /usr/bin/levoile-service`, `systemctl enable --now levoile.service`
- Fichiers installés : `/usr/bin/levoile-service`, `/usr/bin/levoile-ui`, `/usr/lib/systemd/system/levoile.service`, `/etc/levoile/config.toml`, `/usr/share/applications/levoile.desktop`, icônes XDG

#### System Integration

- **Capture trafic L3** — Interface virtuelle `levoile0` (TUN Linux `/dev/net/tun` / Wintun Windows DLL). Route par défaut pointe vers cette interface. Tout le trafic IP de la machine y transite
- **Firewall kill switch** — nftables (Linux) via ruleset `inet levoile` / Windows Filtering Platform (WFP) via provider + sublayer dédiés. Drop tout sauf interface TUN + paquets vers IP relais:443. Persiste aux crashes du service
- **Routing** — Route par défaut via `levoile0` (metric basse), route spécifique vers IP relais via gateway originale (metric haute). Linux : `ip rule` + table 51820. Windows : winipcfg metrics
- **Privilèges** — Linux : capabilities CAP_NET_ADMIN + CAP_NET_RAW via setcap au post-install (pas de sudo récurrent). Windows : service SCM en LocalSystem (pas d'UAC récurrent)
- **DNS** — Résolu côté relais avec blocklist StevenBlack/hosts intégrée. Plus de proxy DNS local côté client. Plus de gestion du resolver système (inutile : tout passe par la TUN)
- **Singleton UI** — Mutex nommé Windows / flock Linux empêchant les instances multiples de l'UI

#### Auto-Update Strategy

- Vérification périodique des releases GitHub
- Téléchargement en arrière-plan + vérification signature Ed25519
- Installation au prochain démarrage (Windows : service SCM restart / Linux : `systemctl restart levoile.service`)
- Rollback automatique si la nouvelle version échoue (tunnel pas établi dans les 30s)
- Notification UI : "Mise à jour prête — appliquée au prochain démarrage"

### Android (Phase 2)

#### Architecture

**Architecture mono-processus Android (1 processus, plusieurs composants) :**

- **`MainActivity`** — Activity Android hébergeant un `WebView` plein écran avec `WebViewAssetLoader`. Charge les assets HTML/CSS/JS partagés desktop (synchronisés au build via `android/scripts/sync-frontend.sh`). Layout responsive mobile (media queries, sélecteur pays vertical, boutons tactiles ≥ 48dp). JS Bridge `@JavascriptInterface` exposant les commandes natives Connect/Disconnect/GetStatus/SelectCountry, sérialisation JSON ligne par ligne, max 4 Ko par message
- **`LeVoileVpnService`** — Foreground Service héritant de `android.net.VpnService`. Crée l'interface TUN via Builder pattern (`mtu(1420)`, `addRoute("0.0.0.0", 0)`, `addAddress(tun_ip, 24)`), récupère le file descriptor via `establish()`, lit/écrit les paquets IP bruts via `FileInputStream`/`FileOutputStream` sur le fd. Tunnel QUIC/HTTP3 vers le relais via le noyau Go partagé (gomobile)
- **Notification persistante** — Channel `levoile_vpn_status` (importance LOW), notification ongoing non-dismissable avec statut + pays + IP visible + action "Déconnecter" (PendingIntent `FLAG_IMMUTABLE`). Joue le rôle équivalent du tray desktop
- **Noyau Go partagé** — Build via `android/scripts/build-aar.{sh,ps1}` (hors task Gradle directe pour frontière claire). Produit `android/levoile-core/libs/levoile-core.aar` consommable par Gradle, alimenté par 5 shims gomobile sous `android/shims/{protocol,auth,crypto,registry,leakcheck}/` (cf. erratum architecture ADR-09 Story 9.2)

**Distribution :**
- F-Droid (catalogue officiel, métadonnées XML, build reproductible)
- APK direct via GitHub releases (signé v2 + v3 par master key Ed25519, vérifié par PackageManager au install)

#### System Integration

- **Capture trafic L3** — `android.net.VpnService.Builder` (seule API non-rootée disponible). MTU 1420, route 0.0.0.0/0
- **Kill switch** — Délégué à l'OS via réglage utilisateur "VPN permanent + bloquer connexions sans VPN" (`Settings.ACTION_VPN_SETTINGS`). Onboarding obligatoire au premier lancement avec deeplink + warning persistant UI tant que non-activé. Détection d'état via heuristique `Settings.Global` (fragile, fallback "non vérifiable" documenté)
- **Routing** — Géré par VpnService (addRoute + addAddress)
- **DNS** — Résolu côté relais (cohérent desktop)
- **Lifecycle** — Activity destruction (swipe close) n'arrête pas le Foreground Service. Au reboot, pas d'autostart automatique (limitation Android 10+ sur les BootCompletedReceiver), mais le réglage OS "VPN permanent" reconnecte le dernier VPN actif
- **Singleton** — Implicite (Android impose 1 seule app VPN active via `VpnService.prepare()`)
- **Privilèges** — Aucun root requis. Permissions Android minimales (cf. §5 Permissions Android Minimales)

#### Auto-Update Strategy

- **F-Droid** : géré par le client F-Droid de l'utilisateur (notification de mise à jour automatique côté F-Droid)
- **APK direct** : notification UI "Mise à jour {version} disponible" + lien GitHub releases. Pas d'auto-update embarqué (limitation Android non-rooté + posture sécurité — pas d'élévation runtime)
- **Vérification signature** : APK signé v2 + v3 par master key Ed25519, vérifié automatiquement à l'install par PackageManager Android (mécanisme natif OS)

### Platform Support

| Plateforme | Statut | Notes |
|---|---|---|
| Windows 10/11 | MVP — Principal | Installeur NSIS. WebView2 Runtime requis (présent par défaut Windows 11, installé automatiquement Windows 10) |
| Debian 11+ / Ubuntu 22.04+ | MVP — Principal | Paquet .deb. Deps : libwebkit2gtk-6.0-0 \| libwebkit2gtk-4.1-0, libayatana-appindicator3-1, nftables, iproute2 |
| Fedora 38+ / RHEL 9+ | MVP — Principal | Paquet .rpm. Deps : webkit2gtk4.1, libayatana-appindicator-gtk3, nftables, iproute |
| Arch Linux (rolling) | MVP — Principal | AUR `levoile`. Deps : webkit2gtk-4.1, libayatana-appindicator, nftables, iproute2 |
| Alpine 3.18+ | MVP — Principal | Paquet .apk. Deps : webkit2gtk-4.1, libayatana-appindicator, nftables, iproute2 |
| Android 10+ (API 29+) | **Phase 2** | minSdk 29, targetSdk 34. F-Droid + APK direct GitHub releases. Pixel 6+ recommandé. Hors scope : iOS, Wear OS, Android TV, Auto |
| macOS | Différé Phase 3 | Support utun via wireguard/tun possible, mais non prioritaire |

## 8. Project Scoping & Phased Development

### MVP (Phase 1) — Valider le socle multi-relais + multi-OS

**Must-Have :**
- Client desktop 2 processus cross-platform : service privilégié (SCM Windows / systemd Linux via kardianos/service) + UI unique (fyne.io/systray + webview/webview), communication IPC named pipes / Unix sockets
- **Capture trafic L3 unifiée** : TUN (Linux `/dev/net/tun`) + Wintun (Windows DLL signée) via `wireguard/tun`. Interface `levoile0`, MTU 1420
- **Kill switch firewall OS-level** : nftables (Linux) + Windows Filtering Platform (Windows). Drop tout sauf TUN + IP relais
- **Routing automatique** : route par défaut via TUN + route spécifique vers IP relais via gateway originale
- Installeur NSIS Windows (service SCM, UI autostart, shortcuts, Wintun DLL)
- Paquets Linux natifs via GoReleaser + nfpm : .deb (Debian/Ubuntu), .rpm (Fedora/RHEL), AUR (Arch), .apk (Alpine). Post-install setcap + systemctl enable
- Tunnel QUIC/HTTP3 direct vers les relais (DNS A record → VPS origin) — stream bidirectionnel `/tunnel` transportant paquets IP bruts
- DNS résolu côté relais (upstream Cloudflare 1.1.1.1 / Quad9 9.9.9.9 avec failover) + blocklist StevenBlack/hosts intégrée côté relais
- Reconnexion automatique du tunnel (backoff exponentiel + circuit breaker) — kill switch firewall reste actif pendant toute la durée
- NAT côté relais : table 5-tuple → port NAT, TTL court, éviction automatique
- Multi-relais par pays + registre distribué signé Ed25519
- Sélecteur de pays dans l'UI + failover automatique + latency measurement (kill switch maintenu entre relais)
- Détection fuites WebRTC via STUN (validation que la TUN capture bien le trafic)
- Session tokens Ed25519 signés (TTL 4h) pour l'authentification /tunnel
- Auto-update via GitHub releases + vérification signature Ed25519 + rollback + rate limiting
- Relais VPS stateless multi-pays — binaires autonomes avec bandwidth limiting par IP (10 GiB/jour)
- Distribution : GoReleaser cross-platform (Windows NSIS + 4 familles paquets Linux + relais ELF)

**Hors MVP :**
- Registre dynamique avec API de gestion → Phase 2
- Support ICMP dans /tunnel (ping) — à valider selon complexité d'implémentation

### Phase 2 — Enrichissements + Support Android

**Priorité haute :**
- **Support Android (API 29+, Android 10+)** — Application Android Kotlin avec architecture mono-processus : `MainActivity` (WebView plein écran avec assets HTML/CSS/JS partagés desktop, JS Bridge `@JavascriptInterface`) + `LeVoileVpnService` (Foreground Service héritant de `android.net.VpnService`). Noyau Go partagé via gomobile (`.aar`). Distribution F-Droid (build reproductible obligatoire) + APK direct GitHub releases (signé v2/v3 par master key Ed25519). Onboarding obligatoire activant "VPN permanent + bloquer connexions sans VPN" (kill switch délégué OS). Aucune télémétrie / crash reporter / analytics. Hors scope explicite : iOS, Android TV, Wear OS, Android Auto, tablettes spécifiques (la WebView responsive couvre les phones et tablettes Android standard)

**Autres :**
- Support IPv6 end-to-end si non complet au MVP
- Support ICMP (ping) via /tunnel si différé
- Fallback DNS multi-résolveur supplémentaire côté relais
- Auto-test de fuite périodique (10 min) côté client
- Registre dynamique (API de gestion pour ajouter/retirer des relais sans redéployer)
- Certificate pinning renforcé
- CI/CD GitHub Actions complet (build multi-arch amd64 + arm64, publication AUR automatique, build `.aar` Android + APK signé)

### Phase 3 — Expansion

- Connexion directe bypass Cloudflare (dernier recours)
- Support macOS (utun via wireguard/tun)
- Diversification géographique des hébergeurs
- Page santé publique auto-générée
- Obfuscation binaire avancée (garble)
- Authentification client (tokens/clés client pour restriction d'accès)
- Split tunneling par domaine (si demande utilisateur)

## 9. Functional Requirements

### Tunnel & Connexion Réseau

- FR1: Le client peut établir un tunnel QUIC/HTTP3 direct vers le relais sélectionné au démarrage
- FR2: Le client peut se reconnecter automatiquement au relais après une perte de connexion (kill switch firewall maintenu pendant toute la durée)
- FR3: Le client peut authentifier chaque relais via sa clé publique Ed25519 unique (certificate pinning)
- FR4: Les relais peuvent accepter et relayer les connexions QUIC/HTTP3 entrantes des clients

### Capture Trafic L3 & Kill Switch

- FR5: Le client peut créer une interface virtuelle TUN (Linux) / Wintun (Windows) nommée `levoile0` avec MTU 1420 pour capturer tout le trafic IP de la machine
- FR5b: Le client peut détecter la disparition ou l'altération externe de l'interface TUN/Wintun (watchdog 3s) et déclencher une reconnexion complète avec kill switch firewall maintenu
- FR5c: Le client peut détecter au démarrage la présence d'autres interfaces VPN actives (scan des interfaces réseau pour TUN/TAP/utun/wireguard/openvpn/cisco). Si détecté, refus de connexion avec message explicite : "VPN concurrent détecté ({nom_interface}). Déconnectez-le pour utiliser Le Voile."
- FR6: Le client peut configurer le routage système pour router le trafic par défaut via `levoile0`, avec route spécifique vers l'IP du relais via la gateway originale
- FR7: Le client peut détruire l'interface TUN/Wintun et restaurer les routes d'origine à la désactivation ou au shutdown propre
- FR7b: Le client peut flusher le cache DNS système au disconnect : `ipconfig /flushdns` (Windows), `resolvectl flush-caches` ou équivalent selon le resolver détecté (Linux)
- FR8: **(Desktop : Windows + Linux)** Le client peut activer un kill switch firewall OS-level (nftables Linux / Windows Filtering Platform) droppant tout trafic sauf (a) sur l'interface TUN, (b) sortant vers l'IP du relais sur port 443. Le kill switch persiste même si le service crashe (règles kernel indépendantes du process). Sur Android (Phase 2), le kill switch est délégué à l'OS via le réglage utilisateur "VPN permanent" — voir FR-AND-3
- FR8b: Le relais peut filtrer les requêtes DNS via une blocklist de domaines malveillants (StevenBlack/hosts), téléchargée périodiquement et stockée en mémoire côté relais
- FR8c: Le client peut détecter un captive portal Wi-Fi au démarrage (probe HTTP RFC 7710 ou `http://captive.apple.com/hotspot-detect.html`). Si redirect détecté, mode "captive portal" activé : lockdown firewall relaxé autorisant uniquement trafic vers gateway réseau local. Bandeau UI : "Authentifiez-vous sur le portail Wi-Fi, puis cliquez 'Activer la protection'". Transition automatique vers kill switch plein + tunnel dès que le probe réussit
- FR8d: **IPv6 non tunnelisé (option)** — Au MVP, le tunnel transporte uniquement IPv4. Par défaut, IPv6 est **entièrement bloqué** par le firewall (aucune fuite possible). L'utilisateur peut cocher une option avancée dans l'UI (`[ ] Autoriser IPv6 hors tunnel`, décochée par défaut) qui autorise le trafic IPv6 natif en clair hors tunnel. L'activation affiche un avertissement clair : "L'IPv6 ne sera PAS protégé par Le Voile et exposera votre IP réelle sur les services IPv6". Setting persisté en config TOML (`[tunnel] allow_ipv6_leak = false`). Tunneling IPv6 complet prévu Phase 2

### Interface Utilisateur

- FR9: L'utilisateur peut voir l'état de protection via une fenêtre desktop (connecté/en cours/déconnecté) avec indicateur visuel
- FR10: L'utilisateur peut voir le pays sélectionné, le relais actif et l'IP visible dans la fenêtre
- FR11: L'utilisateur peut sélectionner un pays parmi les relais disponibles via un sélecteur avec drapeaux et nombre de relais
- FR12: L'utilisateur peut connecter/déconnecter Le Voile via la fenêtre ou le menu tray
- FR13: L'utilisateur peut accéder rapidement à la fenêtre via l'icône system tray (clic gauche = toggle fenêtre)
- FR13b: L'utilisateur peut quitter Le Voile via le menu clic droit du tray
- FR13c: Si l'UI ne peut pas joindre l'IPC du service (service non démarré, crash, container sans systemd), elle affiche un écran fixe avec titre "Service Le Voile non démarré" et bloc-texte affichant la commande shell selon l'OS détecté :
  - Linux : "Le service Le Voile n'est pas démarré. Ouvrez un terminal et lancez : `sudo systemctl start levoile.service`"
  - Windows : "Le service Le Voile n'est pas démarré. Ouvrez Services.msc et démarrez 'Le Voile Service', ou utilisez `sc start levoile-service` en admin"
  - Retry automatique de la connexion IPC toutes les 5 secondes en arrière-plan

### Démarrage & Lifecycle

- FR14: **(Desktop)** Le service démarre automatiquement au boot (SCM Windows / systemd Linux), l'UI démarre via autostart (HKCU Windows / XDG autostart Linux). Sur Android (Phase 2), pas d'autostart au boot (limitation OS — restrictions Android 10+ sur les BootCompletedReceiver) : l'utilisateur lance l'app manuellement, mais le réglage OS "VPN permanent" (FR-AND-3) reconnecte le dernier VPN actif au reboot
- FR15: **(Desktop)** L'icône système (system tray) persiste en arrière-plan après fermeture de la fenêtre webview (seule la fenêtre est détruite, le tray et le service continuent). Réouverture via le menu tray "Ouvrir". Sur Android (Phase 2), l'équivalent fonctionnel est la notification persistante du Foreground Service (FR-AND-2) qui reste affichée après swipe-close de l'Activity
- FR15b: **(Desktop uniquement)** Le processus UI est supervisé par un gestionnaire de redémarrage automatique. Linux : unit systemd user `levoile-ui.service` avec `Restart=on-failure` lancé via autostart XDG. Windows : détection crash via job object / Watchdog service SCM qui relance `levoile-ui.exe`. Couvre les cas de crash du desktop environment (GNOME Shell restart, KDE Plasma crash) qui tuent le processus UI. Sur Android, supervision déléguée à l'OS (Android relance les Foreground Services tués dans la mesure du possible — best-effort)
- FR16: **(Desktop)** L'utilisateur peut quitter l'UI via le menu tray. Le service continue de tourner (contrôlé par systemd/SCM). Pour un arrêt complet : désactivation via l'UI ou `systemctl stop levoile` / `sc stop levoile-service`. Sur Android (Phase 2), l'utilisateur arrête le tunnel via l'action "Déconnecter" de la notification persistante ou un bouton dans l'app
- FR16b: **(Desktop uniquement)** L'utilisateur peut désactiver temporairement le kill switch firewall via menu tray "Mode dégradé" ou CLI authentifiée (`levoile-ctl killswitch off`). État persisté en config. Restauration automatique à la prochaine connexion tunnel réussie. Indicateur visuel permanent dans l'UI tant que le mode dégradé est actif (icône tray rouge + bandeau webview). Sur Android, le mode dégradé équivaut à désactiver "VPN permanent" dans Settings — action utilisateur explicite hors-app (cohérent avec la délégation OS)

### Relais Multi-VPS

- FR17: Les relais peuvent recevoir et relayer les paquets IP bruts via un stream HTTP/3 `/tunnel`, appliquer NAT, résoudre le DNS en interne, et forwarder le trafic vers les destinations
- FR18: Les relais peuvent fonctionner sans aucune persistence de données (stateless — NAT table en RAM avec TTL court)
- FR19: Les relais peuvent être déployés comme binaires autonomes sur des VPS Linux (systemd)
- FR19b: Les relais peuvent être organisés par pays. Chaque pays dispose d'au moins 1 relais. Les pays prioritaires (DE, ES, GB, US) sont ciblés à 2 relais ou plus pour permettre le failover intra-pays

### Distribution & Lancement

- FR20: L'utilisateur peut installer Le Voile via :
  - Windows : installeur NSIS (service SCM, UI autostart, shortcuts, Wintun DLL)
  - Linux : paquets natifs `apt install levoile` (.deb) / `dnf install levoile` (.rpm) / `yay -S levoile` (AUR) / `apk add levoile` (Alpine). Post-install configure les capabilities et active le service systemd
  - **Android (Phase 2)** : F-Droid (recherche "Le Voile" dans le client F-Droid) ou APK direct via github.com/velia-the-veil/le_voile/releases (signé v2/v3, vérifié automatiquement par PackageManager Android au install)
- FR20b: Tous les paquets de distribution (.exe NSIS, .deb, .rpm, .apk) sont signés Ed25519 par la master key Le Voile. Le PKGBUILD AUR embarque un checksum SHA256 vérifié upstream + signatures GPG des commits du repo AUR. Les gestionnaires de paquets rejettent toute installation sans signature valide
- FR21: Le service persiste via SCM Windows / systemd Linux. Les règles firewall (nftables/WFP) et l'interface TUN persistent tant que le service tourne. Au shutdown propre, tout est nettoyé
- FR22: Configuration utilisateur (pays préféré, préférences UI, relay domain, clé publique Ed25519, TUN name, MTU) stockée en TOML dans `%AppData%/LeVoile/` (Windows) ou `~/.config/levoile/` (Linux). Config service Linux : `/etc/levoile/config.toml`. Sur Android (Phase 2), la config est stockée en JSON dans `getFilesDir()` (scoped storage natif AndroidX, accessible uniquement à l'UID de l'app) — voir FR-AND-10. Cache registre relais en JSON séparé

### Découverte & Sélection de Relais

- FR23: Chaque relais peut servir le registre complet de tous les relais via un endpoint dédié (`/.well-known/relay-registry.json`), signé Ed25519 par la master key
- FR23b: Le client peut se connecter à un relais bootstrap hardcodé au premier lancement pour obtenir le registre initial
- FR24: Le client peut sélectionner un relais par pays choisi par l'utilisateur
- FR25: Le client peut distribuer les connexions entre les relais d'un même pays via round-robin
- FR26: Le client peut basculer automatiquement vers un autre relais du même pays en cas d'échec (timeout 3s, erreur 503, perte connexion). Le kill switch firewall reste actif pendant le failover

### IP Camouflage & Tunnel IP

- FR27: Le client peut encapsuler les paquets IP bruts capturés par la TUN/Wintun dans un stream HTTP/3 `/tunnel` vers le relais (framing : 2 octets longueur + payload)
- FR28: Le relais peut désencapsuler les paquets IP, appliquer NAT (substitution IP source = relais + port NAT alloué), résoudre le DNS en interne avec blocklist, et forwarder le trafic via sockets système
- FR29: Le relais peut authentifier les connexions /tunnel via des session tokens Ed25519 signés (TTL 4h, IP hash dans le payload)
- FR30: Le relais peut limiter le nombre de tunnels par IP source (max 200 simultanés) et appliquer un bandwidth quota journalier (10 GiB/jour)
- FR30b: Le relais peut limiter le nombre total de tunnels simultanés (max 150), rejetant les connexions excédentaires avec HTTP 503

### Protection Anti-Fuite

- FR31: Le client peut émettre des requêtes STUN Binding (RFC 5389) via la TUN pour valider que le trafic UDP passe bien par le tunnel
- FR32: La capture L3 garantit structurellement qu'aucun paquet (DNS, WebRTC, IPv6, ICMP) ne peut sortir hors du tunnel. Le firewall kill switch drop tout le reste
- FR33: Le client peut comparer l'IP détectée via STUN (retournée par le serveur STUN) avec l'IP du tunnel attendue pour vérifier l'absence de fuite
- FR34: En cas d'anomalie détectée (TUN bypass, IP inattendue), le client peut :
  - déclencher une reconnexion automatique complète (fermeture tunnel + reset TUN + nouveau Connect)
  - afficher une alerte UI (icône tray orange + bandeau webview)
  - logger l'événement dans le journal système (Event Log Windows / journald Linux)
  - maintenir le kill switch firewall actif pendant toute la procédure

### Mise à Jour Automatique

- FR35: Le client peut vérifier périodiquement la disponibilité de nouvelles versions via les releases GitHub
- FR36: Le client peut télécharger, vérifier la signature Ed25519 et appliquer les mises à jour au prochain démarrage (service SCM restart / `systemctl restart levoile.service`), avec rollback automatique si le tunnel n'est pas établi dans les 30 secondes après le premier lancement post-update. Si le remplacement atomique du binaire échoue (disque plein, permissions, écriture bloquée), le service continue sur l'ancienne version sans interruption, l'échec est loggé (syslog/Event Log), l'UI notifie l'utilisateur, retry à la prochaine occasion périodique

<!-- FR37-FR40 (Extension Navigateur) : SUPPRIMÉS lors de la révision 2026-04-15 — la capture L3 machine-wide (TUN/Wintun) rend l'extension redondante. Le bypass > 50 Mo est abandonné. -->

### Phase 2 — Android

Les FRs ci-dessous formalisent les exigences Android pour la Phase 2 (livraison après stabilisation Windows + Linux). L'architecture est actée (révision 2026-04-29, ADR-08 à ADR-15) ; ces FRs servent de baseline pour le découpage en epics/stories Android.

- FR-AND-1: Le client Android peut établir un tunnel via `android.net.VpnService` (Builder pattern : `mtu(1420)`, `addRoute("0.0.0.0", 0)`, `addAddress(tun_ip, 24)`). L'interface TUN est créée par `establish()` et le file descriptor utilisé pour lire/écrire les paquets IP bruts via `FileInputStream`/`FileOutputStream`
- FR-AND-2: Le client Android peut héberger le tunnel dans un Foreground Service (`LeVoileVpnService` héritant de `VpnService`) avec notification persistante ongoing (channel `levoile_vpn_status`, importance LOW, non-dismissable, action "Déconnecter" via PendingIntent `FLAG_IMMUTABLE`). La notification affiche statut + pays + IP visible
- FR-AND-3: Le client Android peut afficher au premier lancement un onboarding obligatoire incitant l'utilisateur à activer "VPN permanent + bloquer connexions sans VPN" dans Settings via deeplink `Settings.ACTION_VPN_SETTINGS`. L'app détecte au lancement si le toggle est actif via heuristique `Settings.Global` et affiche un warning persistant UI (bandeau rouge non-dismissable) tant que non-activé
- FR-AND-4: Le client Android peut héberger l'UI dans une `MainActivity` avec `WebView` plein écran chargeant les mêmes assets HTML/CSS/JS que desktop via `WebViewAssetLoader` (synchronisés au build via `android/scripts/sync-frontend.sh`). Layout responsive mobile (media queries : sélecteur pays vertical, pas de sidebar, boutons tactiles ≥ 48dp)
- FR-AND-5: Le client Android peut exposer les commandes natives au frontend JS via `@JavascriptInterface` (méthodes `connect()`, `disconnect()`, `getStatus()`, `selectCountry(iso)`), sérialisation JSON ligne par ligne, max 4 Ko par message. Pas de serveur HTTP local (limitation Android non-rooté + posture sécurité)
- FR-AND-6: Le client Android peut détecter au lancement la présence d'une autre app VPN active via `VpnService.prepare()` retournant un `Intent` non-null. Si détecté, refus de démarrer le tunnel avec message UI explicite : "Une autre application VPN est active sur cet appareil. Désactivez-la pour utiliser Le Voile."
- FR-AND-7: Le client Android peut être distribué via F-Droid (catalogue officiel, métadonnées XML versionnées dans le repo) avec build reproductible obligatoire (vérifié par fingerprinting : 2 builds successifs depuis le même tag git produisent un APK avec hash SHA256 identique) et APK direct via GitHub releases signé v2 (APK Signature Scheme v2) + v3 (key rotation) par la master key Ed25519
- FR-AND-8: Le client Android n'embarque aucune télémétrie, aucun crash reporter (pas de Firebase Crashlytics, pas de Sentry, pas de Bugsnag, pas de Mixpanel/Adjust/Branch), aucune analytics maison. Audit dépendances Gradle vérifie l'absence de ces modules. Bug reports utilisateur via export texte manuel local (sans IP, sans identifiant, sans données utilisateur)
- FR-AND-9: Le client Android peut notifier l'utilisateur de la disponibilité d'une nouvelle version. Vérification au lancement de l'app + vérification périodique en arrière-plan toutes les 24h via `WorkManager` (cohérent FR35 desktop "vérification périodique"). Pour APK direct : notification UI "Mise à jour {version} disponible" + lien GitHub releases (pas d'auto-update embarqué — limitation Android non-rooté + posture sécurité). Pour F-Droid : la vérification embarquée est désactivée (la mise à jour est gérée nativement par le client F-Droid de l'utilisateur)
- FR-AND-10: La configuration utilisateur Android (pays préféré, préférences UI, registre relais cache, dernière clé publique Ed25519 vérifiée) est stockée en JSON dans `getFilesDir()` (scoped storage natif AndroidX, accessible uniquement à l'UID de l'app — équivalent sécurité aux permissions 0600 desktop). Pas de TOML sur Android (convention écosystème AndroidX)

## 10. Non-Functional Requirements

### Security

- NFR1: Communications client-relais chiffrées via QUIC/HTTPS (TLS 1.3 minimum)
- NFR2: Authentification client-relais exclusivement Ed25519 via les bibliothèques standards Go (`crypto/ed25519`) + TLS via quic-go (TLS 1.3 standard Go). Aucune implémentation cryptographique maison. Une paire de clés Ed25519 unique par relais
- NFR3: Les relais ne persistent aucune donnée au-delà de la durée d'une requête. NAT table en RAM uniquement, avec TTL ≤ 300s pour TCP et ≤ 120s pour UDP. Éviction automatique à l'expiration du TTL
- NFR4: Trafic tunnel indiscernable du trafic HTTPS standard par analyse DPI — vérifié par capture Wireshark (0 pattern-match VPN sur 100 échantillons de trafic)
- NFR5: Aucune fuite DNS pendant le fonctionnement normal, la reconnexion ou le failover — garanti structurellement par la capture L3 + kill switch firewall
- NFR6: Interface TUN/Wintun, routes système et règles firewall restaurées dans tous les scénarios (désactivation, crash, shutdown). Zéro résidu
- NFR7: Code source publiquement auditable sur GitHub
- NFR8: **Retiré** (pivot 2026-04-19). Ancien libellé imposait la validation des plages IP Cloudflare — obsolète, le relais accepte les connexions directes au domaine relais, la protection anti-abus se fait via rate-limit + bandwidth quota par IP client.
- NFR9: Les relais bloquent les paquets IP vers les réseaux privés (loopback, RFC 1918, link-local) — protection SSRF
- NFR9b: Kill switch firewall OS-level (nftables Linux / WFP Windows) survit aux crashes du process service — aucune fuite possible même en cas de défaillance applicative
- NFR9c: Toutes les comparaisons cryptographiques (pinning Ed25519, validation signature token, vérification hash binaire) utilisent `crypto/subtle.ConstantTimeCompare` — résistance aux timing attacks
- NFR9d: Le relais vérifie que l'IP hash (SHA256) du session token correspond à l'IP source du socket (`r.RemoteAddr`) — rejet immédiat si différent, empêche le replay depuis une autre IP
- NFR9e: TLS direct entre le client et le VPS relais, TLS 1.3 minimum, certificat Let's Encrypt servi depuis l'origin (pas de terminaison CDN intermédiaire)
- NFR9f: Le relais valide les signatures DNSSEC sur les réponses upstream (Cloudflare 1.1.1.1 + Quad9 9.9.9.9 supportent DNSSEC) — protection contre DNS poisoning amont
- NFR9g: Le client détecte l'injection de paquets externes sur l'interface TUN par comparaison de checksum et timestamp. Paquets non émis par le pump tunnel ignorés et loggés
- NFR9h: Binaires compilés avec `-ldflags="-s -w"` (strip symbols + DWARF debug info) — freine le reverse engineering basique. Obfuscation avancée (garble) différée Phase 2
- NFR9i: Résolution DNS du relais au bootstrap via DoH (Cloudflare DoH ou Quad9 DoH) — protège contre DNS poisoning du resolver système client lors de la première résolution
- NFR9j: Config TOML client stockée avec permissions 0600 (Linux) / ACL user-only (Windows). Sur Android (Phase 2), config JSON dans `getFilesDir()` — scoped storage natif AndroidX, accessible uniquement à l'UID de l'app, équivalent sécurité aux permissions 0600 desktop. Toute modification externe détectée au prochain démarrage (hash HMAC signé par clé machine-local — sur Android, clé dérivée via `Android Keystore`)

### Performance

- NFR10: Latence DNS additionnelle via tunnel : < 50ms — mesuré par tests automatisés (requêtes DNS avant/après tunnel)
- NFR11: Établissement tunnel initial (TUN création + firewall activation + QUIC handshake) : < 3 secondes sur connexion ADSL/fibre résidentielle (RTT < 50ms vers Cloudflare edge) — mesuré par chronométrage applicatif
- NFR12: Reconnexion automatique : initiation < 1 seconde après perte — mesuré par chronométrage applicatif
- NFR13: Consommation RAM client : < 25 MB en fonctionnement normal (hausse vs 20 MB acceptable — inclut buffers TUN + stack encapsulation) — mesuré via Task Manager / profiling mémoire
- NFR14: Impact CPU en état stable : < 2% d'utilisation CPU (hausse vs 1% acceptable — encapsulation L3) — mesuré sur 5 minutes

### Reliability

- NFR15: Kill switch firewall : actif dès l'activation du tunnel et maintenu pendant toutes les phases (connexion, reconnexion, failover). Activation initiale < 100ms — mesuré par chronométrage applicatif lors d'un scénario de coupure tunnel provoquée (kill QUIC conn) avec assertion sur l'état nftables/WFP dans les 100ms
- NFR16: Watchdog TUN : détection interface disparue < 5 secondes, reconnexion automatique avec maintien du kill switch
- NFR17: Crash-recovery : au redémarrage du service après crash, les règles firewall et l'interface TUN orphelines sont détectées et nettoyées proprement < 5 secondes avant réinitialisation
- NFR18: Uptime par relais : ≥ 99.5% mensuel mesuré via endpoint /health
- NFR19: Failover entre relais d'un même pays : bascule < 5 secondes, 0 paquet IP perdu au-delà de la fenêtre de bascule, kill switch firewall maintenu — mesuré par test de coupure relais sous charge

### Privacy

- NFR20: Aucun log IP client sur les relais — ni côté /tunnel, ni /verify, ni /.well-known/relay-registry.json
- NFR21: IP source hashée (SHA256) uniquement dans les session tokens Ed25519 (TTL 4h, non persisté)

### Platform Compatibility

- NFR22: Fonctionnement équivalent sur toutes les plateformes cibles, mesuré par une matrice de tests e2e (tunneling, kill switch, leak check, UI, connect/disconnect, failover, update) dont 100% doivent passer sur Windows 11, Ubuntu 24.04, Fedora 40, Arch rolling et Alpine 3.19 avant release. **Phase 2 :** matrice étendue à émulateur Android API 29 + 33 + 34 (cf. NFR-AND-10)
- NFR23: Dépendances runtime Linux résolues automatiquement par les gestionnaires de paquets natifs (apt/dnf/pacman/apk) — aucune installation manuelle requise pour libwebkit2gtk, libayatana-appindicator3, nftables, iproute2
- NFR24: Installation Linux configure les capabilities via le unit systemd (`AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW`, `User=levoile`) — pas de sudo récurrent pour l'utilisateur au runtime, capabilities persistent aux mises à jour binaire sans réapplication

### Logging & Observability (Client)

- NFR22a: Les logs client (syslog Linux / Event Log Windows / Logcat Android — Phase 2 via `android.util.Log`) contiennent uniquement des événements opérationnels : état tunnel (connected/disconnected), erreurs, alertes fuite, updates. **Aucune URL visitée, aucun nom de domaine résolu, aucune destination IP, aucun contenu utilisateur**
- NFR22b: Niveau de log par défaut : INFO (production). Niveau DEBUG activable uniquement via flag CLI `--debug` (utilisateur avancé). DEBUG n'active JAMAIS le log de données utilisateur
- NFR22c: Rotation logs automatique (systemd/journald ou rotation manuelle Windows) — taille max 10 Mo, conservation 7 jours

### Security Testing & Supply Chain

- NFR22d: Pipeline CI exécute au minimum : `go vet`, `gosec` (SAST), `govulncheck` (dépendances vulnérables), `go test -race ./...`. **Phase 2 (Android) :** `gradle lint`, audit dépendances Gradle (assertion absence de modules télémétrie : Firebase, Sentry, Bugsnag, Mixpanel, Adjust, Branch), `gradle testDebugUnitTest`, scan ProGuard rules, vérification reproductibilité APK (hash SHA256 stable entre 2 builds successifs). Build bloqué si l'un échoue avec severity ≥ medium
- NFR22e: Les dépendances Go sont épinglées (go.sum commit) et révisées à chaque mise à jour. Renovate bot ou équivalent pour automatisation. Scan hebdomadaire govulncheck sur `main`
- NFR22f: Fuzzing (go-fuzz / Go 1.18+ native fuzzing) sur les parsers critiques : packet IP, STUN, TOML config, registre JSON. Exécution hebdomadaire en CI

### Cryptographic Key Management

- NFR22g: Master key Ed25519 (signature registre + releases + paquets) stockée exclusivement sur une machine dédiée isolée (air-gapped ou HSM logiciel type YubiKey). Sauvegardes chiffrées hors ligne. Rotation tous les 24 mois ou sur incident
- NFR22h: Les clients embarquent une chaîne de confiance permettant la rotation de la master key : clé actuelle + clé de rotation future. La mise à jour de clé publique cliente passe par une release signée par la clé actuelle ET la nouvelle (dual-signature transitoire de 6 mois)
- NFR22i: Compte GitHub velia-the-veil protégé : 2FA hardware (YubiKey), pas de Personal Access Token long-terme, signing commits GPG obligatoire, branch protection sur main

### Runtime Integrity & Startup Safety

- NFR25: Le kill switch firewall est activé dans le même ordre que le reste de la séquence Connect (après TUN + routing + tunnel établi). **Risque accepté** : les apps auto-lancées au boot (clients cloud, AV, Windows Update) peuvent émettre du trafic pendant les premières secondes avant que le tunnel soit prêt. Le produit cible le grand public, pas des hackers : la protection commence quand l'utilisateur utilise activement ses services, pas pendant le boot. Une fenêtre de fuite de quelques secondes au démarrage système est acceptable face à la simplicité architecturale
- NFR26: Le service vérifie l'intégrité de son propre binaire au démarrage — hash SHA256 comparé à une valeur signée Ed25519 embarquée dans le binaire. Échec de vérification = refus de démarrer + log syslog/Event Log + refus d'activer le tunnel. Protège contre le remplacement du binaire post-installation par un malware. **Sur Android (Phase 2)**, l'intégrité APK est vérifiée par PackageManager Android au install (signature v2 + v3 par master key Ed25519) — équivalent fonctionnel natif OS, pas besoin de vérification applicative supplémentaire

### Phase 2 — Android

Les NFRs ci-dessous formalisent les exigences non-fonctionnelles Android pour la Phase 2.

- NFR-AND-1: Consommation RAM client Android < 60 MB en fonctionnement normal (mesuré via `adb shell dumpsys meminfo fr.plateformeliberte.levoile`, agrégé Java heap + native heap + system)
- NFR-AND-2: Établissement tunnel Android (consent VpnService + Builder + `establish()` + handshake QUIC) < 3 secondes sur Pixel 6+ ou équivalent (Snapdragon 7-gen 1+) sur réseau LTE/4G+ ou Wi-Fi domestique avec RTT < 80ms vers le VPS relais — mesuré par chronométrage applicatif (Trace API Android). Parallèle fonctionnel à NFR11 desktop (ADSL/fibre, RTT < 50ms)
- NFR-AND-3: Taille APK signé < 25 MB (mesuré via `apkanalyzer apk file-size`). Inclut le `.aar` gomobile + assets HTML/CSS/JS partagés desktop
- NFR-AND-4: `minSdk = 29` (Android 10+), `targetSdk = 34` (Android 14+). Couvre ~80% du parc actif fin 2026. Refus explicite Android 8/9 (corner cases sécu/VPN ne valant pas l'effort)
- NFR-AND-5: APK signé v2 (APK Signature Scheme v2) + v3 (key rotation) par la master key Ed25519 (cohérent NFR22g). Vérification automatique au install par PackageManager Android — refus d'install si signature invalide ou altérée
- NFR-AND-6: Build F-Droid reproductible — 2 builds successifs depuis le même tag git produisent un APK avec hash SHA256 identique. Vérifiable manuellement par tout utilisateur via `sha256sum apk-fdroid.apk apk-github.apk` (procédure documentée dans le repo)
- NFR-AND-7: Aucune permission Android dangereuse. Permissions déclarées dans `AndroidManifest.xml` : `INTERNET`, `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC` (API 34+), `POST_NOTIFICATIONS` (API 33+), `BIND_VPN_SERVICE`. Auditable via `apkanalyzer manifest permissions` (assertion CI). **Permission acceptée hors-liste (auto-injectée par AGP 8+) :** `<applicationId>.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` (donc `fr.plateformeliberte.levoile.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` en release et `fr.plateformeliberte.levoile.debug.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` en debug). Cette permission est custom (préfixée par notre `applicationId`, donc surface d'attaque nulle — non détenable par d'autres apps), invisible utilisateur (n'apparaît pas dans la liste UI à l'install) et bénigne (sécurité dynamique des `BroadcastReceiver` Android 13+ recommandée par Google). L'assertion CI doit l'autoriser explicitement (cf. https://developer.android.com/build/releases/gradle-plugin#dynamic-receiver-not-exported-permission)
- NFR-AND-8: Aucune télémétrie / analytics / crash reporter (cohérent ADR-15 et NFR20). Audit dépendances Gradle (`gradle dependencies`) bloqué en CI si présence de modules : Firebase, Sentry, Bugsnag, Crashlytics, Mixpanel, Adjust, Branch, Amplitude
- NFR-AND-9: Logs Android via `android.util.Log` filtrés par buildType — release : niveau WARN+ uniquement, debug : INFO+. Aucune URL, aucun nom de domaine, aucune destination IP, aucun contenu utilisateur loggué (cohérent NFR22a). Logcat sortant filtré côté code (pas de log par défaut "user-facing data")
- NFR-AND-10: Tests instrumentés Android via Espresso + AndroidX Test sur émulateur API 29 + 33 + 34 — matrice e2e (consent VpnService, démarrage VpnService, kill switch via heuristique "VPN permanent", UI flow, Connect/Disconnect, failover, notification persistante) à 100% de passing avant release (cohérent NFR22)
- NFR-AND-11: Code Android obfuscation release : R8/ProGuard activé (`minifyEnabled true` + `proguard-android-optimize.txt`) — équivalent fonctionnel du strip `-ldflags="-s -w"` côté Go natif desktop (cohérent NFR9h). Configuration ProGuard préservant les classes JNI exposées par gomobile (`.aar`)
