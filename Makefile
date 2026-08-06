# Aperture build tooling.
# Note: if `go` is not on PATH, install puts it at ~/.local/go/bin.
GO ?= go
PKG := github.com/TheOriUHD/aperture
VERSION ?= 0.0.0-dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT)

.PHONY: all build build-go build-web tidy run dev web-dev test lint up down logs clean fmt install-cli

all: build

## Build everything (web bundle embedded into the Go binaries)
build: build-web build-go

build-web:
	cd web && npm ci && npm run build
	rm -rf internal/webassets/dist
	cp -r web/dist internal/webassets/dist

build-go:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/nvrd ./cmd/nvrd
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/nvrctl ./cmd/nvrctl

## Install the nvrctl admin console natively to ~/.local/bin (on PATH)
install-cli:
	@mkdir -p $(HOME)/.local/bin
	$(GO) build -ldflags "$(LDFLAGS)" -o $(HOME)/.local/bin/nvrctl ./cmd/nvrctl
	@echo "Installed 'nvrctl' to ~/.local/bin (defaults to http://localhost:8971)."
	@echo "Run it with:  nvrctl        (override with --url or APERTURE_URL)"

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt ./...

## Run the daemon (serves embedded UI if built; otherwise APERTURE_WEB_DIR for dev)
run:
	$(GO) run ./cmd/nvrd

## Local dev: backend on :8080, vite dev server on :5173 (run in two shells, or `make dev`)
dev:
	@echo "Run 'make web-dev' in one terminal and 'make run' in another."

web-dev:
	cd web && npm install && npm run dev

test:
	$(GO) test ./...
	cd web && npm test --silent || true

lint:
	$(GO) vet ./...

## Docker Compose stack
up:
	cd deploy && docker compose up -d --build
	$(MAKE) prune

# Reclaim what each --build leaves behind (~2 GiB/deploy: a superseded image
# layer + build cache). Dangling images only, so base images (golang/node) and
# the running image are kept; `-` ignores errors so a prune hiccup never fails
# a deploy. Run automatically by `up`; also safe to run standalone any time.
prune:
	-docker builder prune -f
	-docker image prune -f

down:
	cd deploy && docker compose down

logs:
	cd deploy && docker compose logs -f

clean:
	rm -rf bin dist internal/webassets/dist
