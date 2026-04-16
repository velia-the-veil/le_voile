# Le Voile — build targets
#
# Principaux targets :
#   make wintun      — fetch + embed wintun.dll (requis avant build Windows)
#   make build       — go build ./...
#   make test        — go test -race -count=1 ./...
#   make tun-test    — tests package TUN uniquement

.PHONY: wintun build test tun-test clean

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
