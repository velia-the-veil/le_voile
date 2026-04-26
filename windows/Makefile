# Le Voile — Windows tier build targets
#
# All targets run from this directory (windows/). Use `cd windows` first.
#
# Principaux targets :
#   make wintun              — fetch + embed wintun.dll (requis avant build)
#   make build               — go build ./...
#   make test                — go test -race -count=1 ./...
#   make snapshot            — goreleaser release --snapshot (no signing)
#   make installer           — build NSIS installer (windows/installer/LeVoile-Setup.exe)
#   make release-sign        — release réelle (windows-vX.Y.Z tag + LEVOILE_SIGNING_KEY_PATH)
#   make clean               — wipe windows/dist/ + windows/installer/build/

.PHONY: wintun build test snapshot installer release-sign clean

# Chemin par défaut clé signature — override via env sur machine mainteneur.
LEVOILE_SIGNING_KEY_PATH ?= $(HOME)/.levoile/signing.key
export LEVOILE_SIGNING_KEY_PATH

wintun:
	@bash scripts/fetch-wintun.sh

build: wintun
	@go build ./...

test:
	@go test -race -count=1 ./...

snapshot:
	@goreleaser release --snapshot --clean --config .goreleaser.yaml --skip=publish

installer: wintun
	@bash installer/build.sh

release-sign:
	@bash scripts/release-sign.sh

clean:
	@rm -rf dist/ installer/build/
