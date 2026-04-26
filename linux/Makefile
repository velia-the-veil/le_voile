# Le Voile — Linux tier build targets
#
# All targets run from this directory (linux/). Use `cd linux` first.
#
# Principaux targets :
#   make build               — go build ./...
#   make test                — go test -race -count=1 ./...
#   make snapshot            — goreleaser release --snapshot (no signing)
#   make deb                 — local snapshot deb only (.goreleaser-deb.yaml)
#   make release-sign        — release réelle (linux-vX.Y.Z tag + LEVOILE_SIGNING_KEY_PATH)
#   make clean               — wipe linux/dist/

.PHONY: build test snapshot deb release-sign clean

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
