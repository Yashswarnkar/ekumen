# Ekumen development tasks. See docs/development.md.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/Yashswarnkar/ekumen/internal/buildinfo
LDFLAGS := -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)
ADDR    ?= 127.0.0.1:7477

.PHONY: dev build ui test lint clean

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	@touch web/node_modules

# Go server (placeholder UI, API on $(ADDR)) + Vite dev server with HMR on
# http://127.0.0.1:5173, which proxies /api to the Go server. Ctrl-C stops both.
dev: web/node_modules
	@trap 'kill 0' INT TERM EXIT; \
	go run ./cmd/ekumen serve --addr $(ADDR) & \
	(cd web && EKUMEN_API=http://$(ADDR) npm run dev) & \
	wait

ui: web/node_modules
	cd web && npm run build

build: ui
	go build -tags embedui -ldflags "$(LDFLAGS)" -o bin/ekumen ./cmd/ekumen

test: web/node_modules
	go test ./...
	cd web && npm run typecheck

lint: web/node_modules
	go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	cd web && npm run typecheck

clean:
	rm -r -f bin web/dist
