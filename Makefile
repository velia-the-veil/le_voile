# Le Voile — build targets
#
# Principaux targets :
#   make wintun              — fetch + embed wintun.dll (requis avant build Windows)
#   make build               — go build ./...
#   make test                — go test -race -count=1 ./...
#   make tun-test            — tests package TUN uniquement
#
# Story 7.4 — release signing :
#   make release-snapshot    — build goreleaser --snapshot + signature (clé éphémère)
#   make release-sign        — release réelle + signature (clé mainteneur)
#   make release-verify-smoke — scripts/test-release-signing.sh (E2E sign/verify)
#   make check-signing-key   — valide LEVOILE_SIGNING_KEY_PATH mode 0600

.PHONY: wintun build test tun-test clean \
        release-snapshot release-sign release-verify-smoke check-signing-key

# Chemin par défaut clé signature — override via env sur machine mainteneur.
LEVOILE_SIGNING_KEY_PATH ?= $(HOME)/.levoile/signing.key

wintun:
	@bash scripts/fetch-wintun.sh

build:
	@go build ./...

test:
	@go test -race -count=1 ./...

tun-test:
	@go test -race -count=1 ./internal/tun/... ./internal/service/... ./internal/config/...

clean:
	@rm -f internal/tun/wintun/wintun.dll internal/tun/wintun_dll_windows.go

# ---- Story 7.4 : release signing ---------------------------------------------

check-signing-key:
	@test -f "$(LEVOILE_SIGNING_KEY_PATH)" || { \
		echo "error: signing key not found: $(LEVOILE_SIGNING_KEY_PATH)"; \
		echo "generate one with: go run ./cmd/genkey -out \"$$HOME/.levoile/signing\" -pem"; \
		exit 1; }
	@perm=$$(stat -c %a "$(LEVOILE_SIGNING_KEY_PATH)" 2>/dev/null || stat -f %Lp "$(LEVOILE_SIGNING_KEY_PATH)" 2>/dev/null || echo "unknown"); \
		case "$$perm" in \
			600|0600) ;; \
			unknown) echo "warning: cannot stat permissions (Windows?) — continuing. Verify ACLs manually." ;; \
			*) echo "error: signing key must be mode 0600 (got $$perm)"; exit 1 ;; \
		esac
	@echo "signing key OK: $(LEVOILE_SIGNING_KEY_PATH)"

release-snapshot:
	@bash scripts/test-release-signing.sh --snapshot

release-sign:
	@LEVOILE_SIGNING_KEY_PATH="$(LEVOILE_SIGNING_KEY_PATH)" bash scripts/release-sign.sh

release-verify-smoke:
	@bash scripts/test-release-signing.sh
