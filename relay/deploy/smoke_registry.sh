#!/usr/bin/env bash
# Le Voile - smoke test du registre relais sur tous les relais de prod.
#
# Pour chaque relais : curl /.well-known/relay-registry.json, vérifie HTTP 200,
# et compare le SHA256 vs la première réponse réussie (tous les relais doivent
# servir exactement le même fichier signé).
#
# Usage :
#   deploy/smoke_registry.sh
#   deploy/smoke_registry.sh --domains de-001 de-002 ...   # override defaults
#   deploy/smoke_registry.sh --verify                      # also run go verifier
#
# Prereqs : curl, sha256sum.
# Master key de prod : rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk=
#
# Le script n'affiche jamais d'IP client ; il lit le registre public signé.

set -euo pipefail

DEFAULT_DOMAINS=(
    de-001.levoile.dev de-002.levoile.dev
    es-001.levoile.dev es-002.levoile.dev
    gb-001.levoile.dev gb-002.levoile.dev
    us-001.levoile.dev us-002.levoile.dev
)

MASTER_PUB_KEY="${LEVOILE_MASTER_PUB_KEY:-rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk=}"
DOMAINS=()
RUN_VERIFY=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --domains)
            shift
            while [[ $# -gt 0 && ! "$1" =~ ^-- ]]; do
                DOMAINS+=("$1")
                shift
            done
            ;;
        --verify)
            RUN_VERIFY=1
            shift
            ;;
        -h|--help)
            sed -n '2,/^set -e/p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "unknown flag: $1" >&2
            exit 2
            ;;
    esac
done

if [[ ${#DOMAINS[@]} -eq 0 ]]; then
    DOMAINS=("${DEFAULT_DOMAINS[@]}")
fi

OUT_DIR="$(mktemp -d -t levoile-smoke-XXXXXX)"
trap 'rm -rf "$OUT_DIR"' EXIT

FAIL=()
REF_SHA=""
REF_DOMAIN=""

for dom in "${DOMAINS[@]}"; do
    out="$OUT_DIR/$dom.json"
    if curl -fsS --max-time 10 \
        "https://${dom}/.well-known/relay-registry.json" -o "$out"; then
        sha="$(sha256sum "$out" | awk '{print $1}')"
        if [[ -z "$REF_SHA" ]]; then
            REF_SHA="$sha"
            REF_DOMAIN="$dom"
        fi
        if [[ "$sha" == "$REF_SHA" ]]; then
            printf '  OK %-28s sha256=%s\n' "$dom" "${sha:0:12}..."
        else
            printf '  DRIFT %-25s sha256=%s (ref %s via %s)\n' \
                "$dom" "${sha:0:12}..." "${REF_SHA:0:12}..." "$REF_DOMAIN"
            FAIL+=("$dom:drift")
        fi
    else
        printf '  FAIL %-26s (curl error)\n' "$dom"
        FAIL+=("$dom:curl")
    fi
done

echo
echo "Reference sha256  : ${REF_SHA:-<none>}"
echo "Reference from    : ${REF_DOMAIN:-<none>}"
echo "Relays attempted  : ${#DOMAINS[@]}"
echo "Relays OK         : $(( ${#DOMAINS[@]} - ${#FAIL[@]} ))"
echo "Relays FAIL       : ${#FAIL[@]}"

if [[ $RUN_VERIFY -eq 1 ]]; then
    if [[ -z "$REF_SHA" ]]; then
        echo "skip verify: no successful fetch"
    else
        SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
        # SCRIPT_DIR is relay/deploy/ ; REPO_ROOT is two levels up (relay/.. = repo root).
        REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
        echo
        echo "Parse+VerifyAll (master key $MASTER_PUB_KEY):"
        (cd "$REPO_ROOT" && go run ./relay/cmd/verify-registry \
            "$OUT_DIR/$REF_DOMAIN.json" "$MASTER_PUB_KEY")
    fi
fi

if [[ ${#FAIL[@]} -gt 0 ]]; then
    echo
    echo "Failures: ${FAIL[*]}"
    exit 1
fi
