# Wintun DLL

La DLL `wintun.dll` signée Microsoft (version 0.14.1) doit être placée dans
ce dossier avant un build Windows.

## Provenance

- Source officielle : https://www.wintun.net/
- Archive : `wintun-0.14.1.zip` (sha256 `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`)
- Fichier à copier : `wintun/bin/amd64/wintun.dll` (amd64) ou la variante arm64

## Vérification signature

Sur Windows :

```powershell
Get-AuthenticodeSignature .\wintun.dll
# Status doit être Valid et SignerCertificate émis par Microsoft
```

## Récupération automatisée

Depuis la racine du repo :

```bash
make wintun        # télécharge, vérifie SHA-256, extrait, génère wintun_dll_windows.go
# ou directement :
bash scripts/fetch-wintun.sh
```

Le script est idempotent : si `wintun.dll` existe déjà, il régénère juste
le fichier Go `wintun_dll_windows.go` contenant la directive `//go:embed`.

## Mise à jour

Lors d'un upgrade :

1. Télécharger la nouvelle version depuis wintun.net
2. Vérifier signature Authenticode
3. Remplacer `wintun.dll` dans ce dossier
4. Mettre à jour ce README (version + hash)
5. `go build` — l'embed récupère automatiquement la nouvelle DLL
6. Au premier runtime service, `ensureWintunDLL()` détecte le changement
   de SHA-256 et réécrit `%ProgramData%/LeVoile/wintun.dll`

## Build sans la DLL

Les builds non-Windows (Linux) n'incluent pas la DLL (build tag
`//go:build windows` sur `wintun_embed_windows.go`). Pour un build Windows
dev local sans Wintun installé, commenter la directive `//go:embed` dans
`wintun_embed_windows.go` — `ensureWintunDLL()` retournera `ErrUnavailable`
proprement.
