# Le Voile — Relay (server-side) build targets
#
# All targets run from this directory (relay/). Use `cd relay` first.
#
# Principaux targets :
#   make build               — go build ./...
#   make test                — go test -race -count=1 ./...
#   make snapshot            — goreleaser release --snapshot (no signing)
#   make release-sign        — release réelle (requires LEVOILE_SIGNING_KEY_PATH + git tag)
#   make clean               — wipe relay/dist/

.PHONY: build test snapshot release-sign clean

# Chemin par défaut clé signature — override via env sur machine mainteneur.
LEVOILE_SIGNING_KEY_PATH ?= $(HOME)/.levoile/signing.key
export LEVOILE_SIGNING_KEY_PATH

build:
	@go build ./...

test:
	@go test -race -count=1 ./...

snapshot:
	@goreleaser release --snapshot --clean --config .goreleaser.yaml --skip=publish

release-sign:
	@bash scripts/release-sign.sh

clean:
	@rm -rf dist/
