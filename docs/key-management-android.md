# Le Voile · Gestion des clés Android

Story 12.3 — runbook gestion des clés cryptographiques côté Android.

## Architecture des clés

Trois clés **distinctes** assurent la chaîne de confiance Le Voile :

| Clé | Rôle | Format | Rotation | Stockage |
|---|---|---|---|---|
| **Master key Le Voile (Ed25519)** | Signe les binaires desktop, paquets `.deb`/`.rpm`/`.apk`/AUR, registre relais, releases (Story 7.4) | Ed25519, 256 bits | 24 mois | YubiKey HSM / machine air-gapped permanente |
| **Clé APK signing (Android — cette story 12.3)** | Signe l'APK GitHub direct (canal `apkDirect`) v2 + v3 | RSA 4096 (cf. note ci-dessous) | 24 mois (lineage v3) | Keystore PKCS12 sur machine air-gapped, encodé base64 → GitHub Actions secret |
| **Clé F-Droid** | Signe l'APK distribué via le store F-Droid | Géré par F-Droid build server | F-Droid policy | F-Droid infra |

Les trois clés sont **indépendantes** et **ne se substituent pas l'une à l'autre**. Un APK GitHub direct signé avec la clé APK Le Voile et un APK F-Droid signé avec la clé F-Droid sont deux artefacts distincts. **Important** : `PackageManager` Android refuse d'installer un APK F-Droid par-dessus un APK direct (`INSTALL_FAILED_UPDATE_INCOMPATIBLE`) car les signatures diffèrent — l'utilisatrice qui change de canal doit désinstaller puis réinstaller.

### Note technique : RSA 4096 vs Ed25519 pour la clé APK

NFR-AND-5 (`prd.md` l. 701) demande « APK signé v2 + v3 par la master key Ed25519 ». **Lecture pragmatique retenue** : la clé APK Le Voile suit la même rigueur que la master key (algorithmie forte, stockage HSM, rotation 24 mois, dual-signing 6 mois en cas d'incident) **mais peut être un keypair distinct** selon les contraintes Android :

* **`apksigner`** (build SDK 33+) supporte Ed25519 v3 sans problème.
* **`PackageManager`** Android API 29-30 (Android 10/11) **n'a pas le code de validation Ed25519** — un APK signé Ed25519 fail à l'installation avec `INSTALL_PARSE_FAILED_NO_CERTIFICATES`.

Notre minSdk = 29. **Décision conservative MVP** : RSA 4096 (cohérent avec la pratique courante : Mullvad, ProtonVPN, Calyx, Orbot signent tous en RSA). La master key Ed25519 reste le point d'ancrage pour les paquets desktop et le registre relais ; la clé APK est un cas particulier Android documenté ici.

Phase 2 si l'écosystème évolue (et si le marché minSdk passe à 30+) : envisager le passage à Ed25519 via une rotation lineage v3 documentée.

## Procédure setup initial (mainteneur)

À effectuer **une seule fois** au démarrage du projet (ou en rotation 24 mois), sur une machine air-gapped (ou au minimum déconnectée d'Internet pendant l'opération).

```bash
# 1. Génération keystore PKCS12 (interactif — saisie password ≥ 16 chars).
bash android/scripts/generate-master-keystore.sh
# Output : android/keystore/levoile-release.jks (gitignored).

# 2. Test local (smoke check) :
cd android
export LEVOILE_KEYSTORE_PATH="$(pwd)/keystore/levoile-release.jks"
export LEVOILE_KEYSTORE_PASSWORD='<password saisi>'
export LEVOILE_KEY_ALIAS='levoile-master-2026'
export LEVOILE_KEY_PASSWORD="$LEVOILE_KEYSTORE_PASSWORD"
# Story 12.5 a introduit les productFlavors apkDirect/fdroid → tasks
# spécifiques au flavor (assembleApkDirectRelease vs assembleFdroidRelease).
./gradlew :app:assembleApkDirectRelease --no-daemon
apksigner verify --verbose --print-certs --min-sdk-version 29 \
    app/build/outputs/apk/apkDirect/release/app-apkDirect-release.apk
# Doit afficher :
#   Verifies (APK Signature Scheme v2): true
#   Verifies (APK Signature Scheme v3): true
#   Number of signers: 1
#   Signer #1 SHA-256 digest: <fingerprint>

# 3. Récupérer le SHA256 fingerprint (pour publication post-release).
keytool -list -v -keystore android/keystore/levoile-release.jks \
    -alias levoile-master-2026 -storepass '<password>' | grep "SHA256:"

# 4. Encoder le keystore en base64 + provisionner les secrets GitHub.
base64 -w 0 < android/keystore/levoile-release.jks | xclip -selection clipboard
# Coller dans :
#   https://github.com/velia-the-veil/le_voile/settings/secrets/actions/new
#   Name: LEVOILE_KEYSTORE_BASE64
#   Value: <coller le base64>
# Puis ajouter les 3 autres secrets : LEVOILE_KEYSTORE_PASSWORD,
# LEVOILE_KEY_ALIAS, LEVOILE_KEY_PASSWORD.

# 5. Suppression locale après upload (machine non-permanente).
shred -u android/keystore/levoile-release.jks
unset LEVOILE_KEYSTORE_PATH LEVOILE_KEYSTORE_PASSWORD LEVOILE_KEY_ALIAS LEVOILE_KEY_PASSWORD
```

Sur une **machine air-gapped permanente** avec stockage hardware (YubiKey HSM, smartcard PKCS#11), le keystore reste sur le hardware ; seul le base64 transite vers GitHub.

## Procédure rotation 24 mois (NFR22g/h)

Tous les 24 mois (ou plus tôt en cas d'incident), rotation contrôlée :

1. **Générer la nouvelle clé** sur machine air-gapped :
   ```bash
   LEVOILE_KEY_YEAR=2028 bash android/scripts/generate-master-keystore.sh
   # Alias : levoile-master-2028
   ```
2. **Période transitoire 6 mois — dual signing v3 lineage** :
   ```bash
   # Sur machine air-gapped, depuis l'ancien keystore (2026) :
   apksigner rotate \
       --in original-app-release.apk \
       --out app-release-rotated.apk \
       --old-signer --ks levoile-release-2026.jks --ks-key-alias levoile-master-2026 \
       --new-signer --ks levoile-release-2028.jks --ks-key-alias levoile-master-2028
   ```
   Le lineage v3 est cryptographiquement lié à la clé précédente. `PackageManager` Android valide la transition automatiquement — les utilisateurs n'ont rien à faire.
3. **Mise à jour des secrets GitHub Actions** : `LEVOILE_KEYSTORE_BASE64` (nouveau .jks 2028), `LEVOILE_KEY_ALIAS=levoile-master-2028`. **Conserver l'ancien secret pendant 6 mois** sous un nom différent (`LEVOILE_KEYSTORE_BASE64_2026_ARCHIVED`) pour permettre des hotfix d'urgence sur l'ancienne version.
4. **Communication** : warrant canary mensuel + announcement post-rotation sur https://plateformeliberte.fr/le-voile/keys.
5. **Après 6 mois** : retirer l'ancien secret archivé. Toute release post-2028+6mois utilise uniquement la nouvelle clé v3 (le lineage reste valide pour les utilisateurs anciens).

## Procédure incident — clé compromise (NFR22h)

Si la clé APK est compromise (vol matériel, fuite secret GitHub, dump mémoire HSM) :

1. **Rotation immédiate** : suivre la procédure ci-dessus en accéléré (jour J → J+24h).
2. **Dual-signing 6 mois imposé** : `apksigner rotate --old-signer ... --new-signer ...` sur tout APK publié pendant 6 mois. Les utilisateurs existants peuvent encore valider (lineage v3 = chaîne valide).
3. **Communication post-mortem** : warrant canary spécial + email aux utilisateurs F-Droid (canal F-Droid n'utilise pas notre clé donc non affecté ; mention pour transparence).
4. **Re-publication APK direct** + notification F-Droid (qui re-build avec leur clé — pas affecté par notre incident, mais utile à signaler).
5. **Audit** : analyser cause racine (HSM, secrets, machines compromises), publier un post-mortem sous 30 jours.

**Anti-pattern critique** : ne JAMAIS révoquer la clé en supprimant simplement le secret CI sans rotation lineage v3 → les utilisateurs existants ne peuvent plus updater (signature mismatch sur tout futur APK = `INSTALL_FAILED_UPDATE_INCOMPATIBLE`).

## Vérification utilisateur (publication)

Après chaque release, publier dans `docs/release-hashes.md` (Story 12.4) le SHA256 fingerprint du certificat. Toute personne peut vérifier en local :

```bash
apksigner verify --verbose --print-certs --min-sdk-version 29 le-voile.apk
# Comparer le "Signer #1 SHA-256 digest" affiche au fingerprint publie.
# Si different : NE PAS installer — l'APK provient d'une autre chaine que Le Voile.
```

Le fingerprint canonique est aussi publié sur `https://plateformeliberte.fr/le-voile/keys` (page chargée en HTTPS, durcie HSTS, monitor warrant canary).

## Sécurité — anti-patterns à refuser systématiquement

* **Committer un keystore** dans le repo (`.jks`, `.p12`, `.pfx`, `.pem`) → fuite immédiate, rotation incident d'urgence. Le `.gitignore` strict refuse tout sauf `README.md` (cf. `android/keystore/.gitignore`).
* **Passer le password keystore en argument CLI** (`-storepass MyPass123`) → visible dans `ps aux`, dans les logs CI. Toujours via env var ou prompt interactif.
* **Logger le contenu du keystore décodé** (`cat $RUNNER_TEMP/keystore.jks`) → fuite via les logs Actions. Le job CI fait `set +x` autour de la décode + `::add-mask::`.
* **Stocker la clé sur le runner GitHub Actions entre 2 builds** → secrets éphémères par design ; toute persistance violerait NFR22g.
* **Utiliser un password < 16 chars** → brute-forçable au GPU. 16 chars aléatoires (entropy ~96 bits) = robuste 2026+.
* **Activer `signingReport` dans un job CI ouvert au public** → révèle le SHA256 fingerprint avant publication. Reste activé localement (debug only).
* **Re-signer en debug par accident sur tag release** → le bloc conditionnel `release` Gradle exige les 4 env vars `LEVOILE_*`. Si absentes, fallback debug + suffix `-unsigned-LOCAL-DEV` qui rend l'APK explicitement non-release.

## Références

* [Story 12.3](../_bmad-output/implementation-artifacts/12-3-signature-apk-v2-v3-key-rotation-par-master-key-ed25519.md) — story file complète.
* [APK Signature Scheme v2](https://source.android.com/docs/security/features/apksigning/v2) — Google docs.
* [APK Signature Scheme v3 (key rotation)](https://source.android.com/docs/security/features/apksigning/v3).
* [`android/scripts/generate-master-keystore.sh`](../android/scripts/generate-master-keystore.sh) — script génération.
* [`android/scripts/generate-master-keystore.ps1`](../android/scripts/generate-master-keystore.ps1) — variante PowerShell.
* [`android/keystore/.gitignore`](../android/keystore/.gitignore) — refus catégorique versionnement.
* [`docs/fdroid-build-recipe.md`](fdroid-build-recipe.md) — Story 12.1 (signature F-Droid distincte).
* [`docs/reproducible-build-android.md`](reproducible-build-android.md) — Story 12.4 (orthogonale à signature).
