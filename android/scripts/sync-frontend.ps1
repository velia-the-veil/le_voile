# Story 11.1 — Variante PowerShell pour devs Windows. Voir sync-frontend.sh
# pour la décision dev (Option 2 — pas de sync réel depuis windows/frontend/).
#
# Code-review post-Epic 11 (L1) : Write-Error couplé à $ErrorActionPreference =
# 'Stop' provoquait un throw immédiat à la 1re absence (le `if ($missing -ne 0)`
# ne s'exécutait jamais). Switch vers Write-Warning + accumulation idempotente.

$ErrorActionPreference = 'Stop'

$RepoRoot = (git rev-parse --show-toplevel 2>$null)
if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
    $RepoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
}
$Dest = Join-Path $RepoRoot 'android\app\src\main\assets\web'

Write-Host "[sync-frontend] Repo root: $RepoRoot"
Write-Host "[sync-frontend] Destination: $Dest"

if (-not (Test-Path $Dest)) {
    New-Item -ItemType Directory -Force -Path $Dest | Out-Null
}

$required = @('index.html', 'style.css', 'app.js', 'style-android.css')
$missing = 0
foreach ($f in $required) {
    $p = Join-Path $Dest $f
    if (-not (Test-Path $p)) {
        # Write-Warning n'est PAS terminating sous ErrorActionPreference Stop —
        # on accumule tous les manquants avant de sortir avec un statut clair.
        Write-Warning "[sync-frontend] MANQUANT : web/$f"
        $missing = 1
    }
}

if ($missing -ne 0) {
    Write-Host "[sync-frontend] Au moins un fichier requis est absent — verifier le commit Story 11.1."
    exit 2
}

Write-Host "[sync-frontend] OK — $($required.Count) fichiers presents, idempotent re-run safe"
exit 0
