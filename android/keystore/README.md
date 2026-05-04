# `android/keystore/` — keystores APK Android

Ce dossier est **gitignoré strictement** (cf. [.gitignore](./.gitignore)). Aucun fichier `*.jks`, `*.p12`, `*.pfx`, `*.pem` ne doit être committé. Story 12.3 livre :

* Le runbook complet de gestion clés Android dans [`docs/key-management-android.md`](../../docs/key-management-android.md).
* Les scripts de génération du keystore master Le Voile : [`android/scripts/generate-master-keystore.sh`](../scripts/generate-master-keystore.sh) + variante PowerShell `generate-master-keystore.ps1`.

## Usage

1. **Génération initiale** (sur machine air-gapped / YubiKey HSM uniquement) :
   ```
   bash android/scripts/generate-master-keystore.sh
   # → produit android/keystore/levoile-release.jks (PKCS12)
   ```
2. **Encodage base64 + provisionnement secrets GitHub Actions** : suivre les instructions affichées par le script.
3. **Suppression locale après upload** : `shred -u android/keystore/levoile-release.jks` (efface définitivement, vs `rm` qui ne supprime que l'entrée FS).

Le keystore décodé n'apparaît **jamais** sur disque hors machine air-gapped — le job CI `sign-apk` le décode dans `$RUNNER_TEMP/` et le supprime en `if: always()` cleanup.
