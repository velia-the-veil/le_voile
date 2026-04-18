# Story 5.7 — supervision UI : tests manuels

Ces deux scénarios valident les ACs en boîte noire sur les deux plateformes
cibles. Ils complètent les tests unitaires `internal/uiwatchdog/*_test.go`
qui couvrent la machine d'état (rate limit, backoff, sortie propre).

## Pré-requis communs

- `levoile-service` installé et démarré
- `levoile-ui` installé et démarré (icône tray visible)
- Une session graphique utilisateur active

## Linux — systemd user unit (AC1, AC2, AC3)

```bash
# 1) S'assurer que l'unit est active.
systemctl --user status levoile-ui.service
# attendu : Active: active (running)

# 2) Crash forcé.
pkill -9 levoile-ui

# 3) Suivre le journal — on doit voir le respawn en moins de 10 s.
journalctl --user -u levoile-ui.service -f

# 4) Vérifier que le tray est revenu (icône visible).

# 5) Crash répétés (rate limit).
for i in 1 2 3 4 5 6; do pkill -9 levoile-ui; sleep 1; done
systemctl --user status levoile-ui.service
# attendu après le 6e kill : Active: failed (start-limit-hit)
# Reset manuel :
systemctl --user reset-failed levoile-ui.service
systemctl --user start levoile-ui.service

# 6) Quitter proprement via le menu tray "Quitter".
# attendu : pas de respawn (Restart=on-failure ignore exit 0).
```

## Windows — watchdog SCM (AC4, AC5, AC6, AC7)

PowerShell admin (le service tourne en LocalSystem).

```powershell
# 1) Vérifier que le service tourne.
sc.exe query LeVoile
# attendu : STATE 4 (RUNNING)

# 2) Crash forcé.
taskkill /F /IM levoile-ui.exe

# 3) Attendre 5-10 s, vérifier que l'icône tray est revenue.
Get-Process levoile-ui -ErrorAction SilentlyContinue
# attendu : nouvelle instance avec un PID différent

# 4) État watchdog observable via IPC (depuis l'UI ou un client minimal).
#    GetStatus.UISupervision doit montrer :
#      Enabled            : true
#      LastRestartAt      : timestamp RFC3339 récent
#      RestartCountWindow : 1
#      BackoffUntil       : "" (pas de backoff)

# 5) Crashes répétés (rate limit).
for ($i=0; $i -lt 6; $i++) { taskkill /F /IM levoile-ui.exe; Start-Sleep 1 }
# Le 6e crash doit déclencher BACKOFF (pas de respawn pendant 5 min).
#    GetStatus.UISupervision doit montrer :
#      RestartCountWindow : 5
#      BackoffUntil       : timestamp RFC3339 ~5 min dans le futur

# 6) Stopper le service — pas de respawn pendant le shutdown.
sc.exe stop LeVoile
Get-Process levoile-ui -ErrorAction SilentlyContinue
# attendu : aucun respawn ; l'instance UI restée ouverte continue mais
# n'est plus supervisée (le watchdog n'est plus actif).
```

## Notes

- Le watchdog Windows utilise `WTSQueryUserToken` + `CreateProcessAsUser`
  pour traverser la barrière session 0 → session utilisateur. Sur un
  serveur sans utilisateur connecté (`WTSGetActiveConsoleSessionId` retourne
  `0xFFFFFFFF`), le watchdog reste en attente sans erreur — c'est le
  comportement attendu pour AC6.
- Le mutex nommé `Global\LeVoileUI` est libéré par le kernel à la mort du
  processus, donc le respawn n'attend pas — mais si une instance fantôme
  retient le mutex >10 s (rare), le watchdog réessaye au cycle suivant.
- NFR22a (zéro PII) : les logs `service: ui watchdog: ...` ne contiennent
  ni nom d'utilisateur, ni chemin complet, ni SID.
