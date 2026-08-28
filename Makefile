GO      ?= go
NPM     ?= npm
DIST    := dist
WEB     := web
ASSETS  := internal/webui/assets

# Two binaries: the CLI and the resident control plane (design.md §3.5).
BIN     := olr
PKG     := ./cmd/olr
DBIN    := olrd
DPKG    := ./cmd/olrd

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X github.com/open-linux-router/open-linux-router/internal/buildinfo.Version=$(VERSION) \
  -X github.com/open-linux-router/open-linux-router/internal/buildinfo.Commit=$(COMMIT) \
  -X github.com/open-linux-router/open-linux-router/internal/buildinfo.Date=$(DATE)

# Static by default: the .deb has to run on any glibc/musl box (design.md §8).
export CGO_ENABLED := 0

# Where a development olrd keeps its files and listens. Nothing here touches
# the real /etc, so `make dev` needs no privileges.
DEV_ROOT   ?= /tmp/olr-dev
DEV_LISTEN ?= 127.0.0.1:8080
DEV_SOCKET ?= $(DEV_ROOT)/olrd.sock

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: deps
deps: ## Download modules
	@# proxy.golang.org is not reachable directly from the dev container; the
	@# SOCKS5 proxy lives in git config so no credential is committed here.
	@HTTPS_PROXY="$$(git config --global http.proxy)" \
	 HTTP_PROXY="$$(git config --global http.proxy)" \
	 $(GO) mod download

.PHONY: tidy
tidy: ## Tidy modules, keeping the go.mod floor at 1.23
	@# `go mod tidy` raises the go directive to whatever the newest dependency
	@# asks for. design.md §8 pins the floor at 1.23 — "nothing needs a newer
	@# language version" — so it is restored here rather than drifting upward
	@# every time a test-only dependency is refreshed.
	@HTTPS_PROXY="$$(git config --global http.proxy)" \
	 HTTP_PROXY="$$(git config --global http.proxy)" \
	 $(GO) mod tidy
	$(GO) mod edit -go=1.23

.PHONY: build
build: ## Build olr and olrd for the host (does not rebuild the web UI)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN) $(PKG)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(DBIN) $(DPKG)

.PHONY: cross
cross: ## Build both binaries for linux/amd64 and linux/arm64
	for arch in amd64 arm64; do \
	  GOOS=linux GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
	    -o $(DIST)/$(BIN)-linux-$$arch $(PKG) || exit 1; \
	  GOOS=linux GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
	    -o $(DIST)/$(DBIN)-linux-$$arch $(DPKG) || exit 1; \
	done

.PHONY: web-deps
web-deps: ## Install the web UI's node modules
	cd $(WEB) && $(NPM) install

.PHONY: web
web: ## Build the SPA into internal/webui/assets
	@# No proxy here on purpose: the npm registry is reachable directly, and
	@# the SOCKS5 proxy that Go modules need makes npm hang.
	cd $(WEB) && $(NPM) run build
	@# vite's emptyOutDir clears the directory, including the placeholder that
	@# lets `go build` work on a clone with no Node installed.
	@printf '# Placeholder so //go:embed has a directory on a fresh clone.\n# `make web` fills this with the built SPA; see .gitignore.\n' > $(ASSETS)/.gitkeep

.PHONY: types
types: ## Regenerate the SPA's config types from olrd's schema (needs olrd running)
	cd $(WEB) && node scripts/gen-types.mjs http://$(DEV_LISTEN)

.PHONY: all
all: web build ## Build the SPA and both binaries

.PHONY: dev
dev: build ## Run olrd against a scratch root, for `npm run dev` to proxy to
	@mkdir -p $(DEV_ROOT)
	$(DIST)/$(DBIN) \
	  --socket $(DEV_SOCKET) \
	  --listen $(DEV_LISTEN) \
	  --no-auth \
	  --root $(DEV_ROOT) \
	  --links $(WEB)/dev-links.json \
	  --log-level debug

.PHONY: test
test: ## Run tests
	$(GO) test ./...

.PHONY: vet
vet: ## Vet for the target OS
	GOOS=linux $(GO) vet ./...

.PHONY: check
check: vet test ## Vet and test

.PHONY: clean
clean: ## Remove build output
	rm -rf $(DIST)
	rm -rf $(WEB)/node_modules/.vite
	find $(ASSETS) -mindepth 1 ! -name .gitkeep -delete
