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
