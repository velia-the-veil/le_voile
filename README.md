# le_voile

## Mode dégradé du kill switch (Story 5.9)

Le kill switch firewall (nftables Linux / WFP Windows) bloque tout trafic
sortant sauf le tunnel et l'IP du relais. Sur un Wi-Fi public instable, si le
tunnel ne se rétablit pas, vous pouvez le **désactiver temporairement** pour
récupérer un accès Internet en clair, en assumant le risque.

### Activer le mode dégradé

**Depuis la fenêtre / tray :**

1. Clic droit sur l'icône système → « Mode dégradé ».
2. La fenêtre s'ouvre sur une modale de confirmation destructive avec le
   texte exact : *« Voulez-vous désactiver la protection temporairement ?
   Votre trafic ne sera PAS chiffré. L'icône tray deviendra rouge jusqu'à
   rétablissement du tunnel. »*
3. Cliquez sur **Continuer** (rouge).

L'icône tray devient rouge en permanence et un bandeau rouge s'affiche dans
la fenêtre tant que vous êtes en mode dégradé.

**En CLI (root / Administrateur) :**

```bash
sudo levoile-ctl killswitch off    # désactive
sudo levoile-ctl killswitch on     # réactive immédiatement
sudo levoile-ctl status            # affiche tunnel + killswitch
```

Le binaire `levoile-ctl` lit le token d'authentification machine-local situé
dans :

- Linux : `/etc/levoile/ctl.token` (perms 0600)
- Windows : `%ProgramData%\LeVoile\ctl.token`

Le token est généré automatiquement au premier démarrage du service Le Voile.

### Auto-restauration

Le mode dégradé est **transitoire**. Dès qu'une nouvelle connexion tunnel
réussit (reconnexion automatique, manuelle, ou changement de pays), le kill
switch est automatiquement réactivé, l'icône tray retrouve sa couleur
correspondant à l'état du tunnel et le bandeau rouge disparaît.

### Refus en portail captif

Si un portail Wi-Fi captif est actif, la commande échoue avec
`captive_portal_active`. Authentifiez-vous d'abord sur le portail
(« Activer la protection » dans l'UI), puis le mode dégradé redevient
disponible si nécessaire.

## Validation anti-fuite STUN (Story 6.1)

Le client émet périodiquement (défaut : toutes les 10 minutes) des requêtes
**STUN Binding** (RFC 5389) vers trois serveurs publics (Google x2 +
Cloudflare). Le paquet UDP est émis via `net.DialUDP` — le noyau le route
par la TUN `levoile0` (route par défaut, Story 2.4), il est encapsulé dans
le tunnel HTTP/3, NAT-forwardé par le relais vers le serveur STUN, et la
réponse revient par le même chemin.

C'est un **check de validation**, pas une défense active : la capture L3
(Epic 2) rend les fuites structurellement impossibles. Si la STUN IP
retournée ≠ IP du relais attendue, cela signale une TUN down, une mauvaise
configuration ou un bug — pas une fuite « produit ».

**Failover** : si un serveur STUN ne répond pas, les suivants sont essayés
dans l'ordre (timeout 5 s par serveur). Échec des 3 serveurs → erreur
loggée, prochain check 10 min plus tard.

**Configuration** (section `[stun]` du `config.toml`) :

- `leakcheck_interval = "10m"` — intervalle entre deux checks
- `default_server = "stun.l.google.com:19302"` — override du premier serveur
- `servers = [...]` — override complet de la liste

Aucune IP détectée par STUN n'est loggée (NFR22a). Seules les erreurs
opérationnelles apparaissent dans journald / Event Log.

## Reconnexion automatique sur anomalie (Story 6.3)

Deux déclencheurs relancent une séquence de reconnexion complète
**kill-switch-préservée** :

1. Le scheduler leakcheck détecte `leak_detected` (STUN IP ≠ IP relais
   attendue — la capture L3 est censée rendre ce cas impossible,
   l'observer signale donc TUN down, mauvais routing, ou bug).
2. Le watchdog TUN (Story 2.2) détecte que `levoile0` a disparu ou a été
   altéré.

La séquence elle-même (close TUN → recreate → routing teardown+setup
→ `firewall.Activate` idempotent **sans `Deactivate`** → `tunnel.Connect`)
est celle déjà utilisée pour la recovery watchdog. Le kill switch
`nftables`/`WFP` ne retombe jamais à OFF pendant la procédure : le flush
et le chargement atomiques garantissent qu'aucun paquet ne passe en clair.

Côté utilisateur :

- L'icône tray passe à **orange `IconAlert`** avec le tooltip
  « Anomalie détectée — reconnexion en cours ».
- Un **bandeau orange** apparaît dans la fenêtre webview (`#anomaly-banner`).
  Sur reconnexion réussie il flashe en vert (« Reconnexion réussie »)
  pendant 3 s avant de disparaître.
- Un évènement WARNING est écrit dans le journal système, **sans aucune
  donnée utilisateur** (NFR22a) — seulement une catégorie d'erreur courte
  (`tun_create_failed`, `routing_setup_failed`, `firewall_activate_failed`,
  `tunnel_connect_failed`, `unknown`).

**Consulter les logs** :

- Windows : `Get-WinEvent -LogName Application -Source LeVoile` ou
  Event Viewer → Applications.
- Linux : `journalctl -t levoile` ou `journalctl -u levoile`.

**Trigger manuel (opérationnel)** : `sudo levoile-ctl trigger-recovery`
(ou l'alias `recover`). Authentifié par le token `ctl.token` machine-local,
réponse IPC immédiate ; le suivi se fait via `levoile-ctl status` ou le
journal.

**Concurrence** : un mutex dédié sérialise les reconnexions. Si le
watchdog TUN et le scheduler leakcheck se déclenchent dans la même
fenêtre, une seule séquence s'exécute — la seconde invocation est
silencieusement ignorée.
