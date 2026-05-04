# Le Voile · Release Hashes

Hashes SHA256 publiés pour permettre la vérification indépendante de la chaîne de confiance Le Voile Android (NFR-AND-6 / ADR-11 / Story 12.4).

| Version | Date | apk-content-archive.zip SHA256 | APK F-Droid SHA256 (signé F-Droid) | APK GitHub SHA256 (signé Le Voile) |
|---|---|---|---|---|
| v0.1.0 | TBD | _placeholder — à compléter par le mainteneur post-CI Story 12.4 + Story 12.3 secrets provisionnés_ | _placeholder F-Droid_ | _placeholder GitHub_ |

## Procédure de vérification (auditeur indépendant)

1. `git clone https://github.com/velia-the-veil/le_voile.git && cd le_voile`.
2. `git checkout v0.1.0` (ou la version à auditer).
3. `bash android/scripts/build-apk-release.sh` (cf. [`docs/reproducible-build-android.md`](reproducible-build-android.md) pour les pré-requis OS).
4. Comparer le `apk-content-archive.zip.sha256` produit avec la valeur listée ci-dessus.
5. **Identique** → chaîne de confiance validée.
6. **Différent** → ouvrir une [issue GitHub](https://github.com/velia-the-veil/le_voile/issues) avec le rapport `diffoscope` (commande dans [`docs/reproducible-build-android.md`](reproducible-build-android.md)).

## Notes importantes

* La signature de l'APK F-Droid est **différente** de l'APK GitHub direct (clés distinctes — voir [`docs/key-management-android.md`](key-management-android.md)). Les **bytes du contenu pré-signature** doivent être identiques (vérifié via `apk-content-archive.zip` qui exclut `META-INF/*.SF`/`*.RSA`/`*.DSA`/`*.EC`).
* Le `apk-content-archive.zip` est l'unique référence canonique de reproductibilité — comparer les APK signés directement n'a pas de sens (nonces signature non-reproductibles by design).

## Mise à jour

À chaque release :

1. Le mainteneur exécute le pipeline `release-android.yml` (push tag `v*`).
2. Le job `reproducibility-check` produit `content-sha-build{1,2}.txt` identiques.
3. Le job `sign-apk` produit l'APK GitHub direct + son SHA256.
4. F-Droid build server produit son APK + son SHA256 (process F-Droid externe).
5. Le mainteneur édite cette table avec :
   * `apk-content-archive.zip` SHA256 (depuis le job `reproducibility-check`).
   * APK GitHub direct SHA256 (depuis le job `sign-apk` — `apk-signature-info.txt`).
   * APK F-Droid SHA256 (depuis le store F-Droid une fois la version publiée).
6. PR avec ces hashes mergée dans le tag de release.
