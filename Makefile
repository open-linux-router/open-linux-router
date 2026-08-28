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

# Package version, which is not the same string as VERSION: dpkg and apk want
# a number, and `git describe` on an untagged tree gives a bare commit hash.
# Falls back to 0.0.0 so `make package` works on a fresh clone.
PKGVERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
ifeq ($(strip $(PKGVERSION)),)
PKGVERSION := 0.0.0
endif

# Architectures both packaging targets build for.
ARCHES := amd64 arm64

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

.PHONY: package
package: web cross ## Build .deb and .apk for every architecture
	@# The dependency on dnsmasq is declared in the package rather than checked
	@# in Go: apt resolves it before any of our code runs.
	@command -v nfpm >/dev/null 2>&1 || { \
	  echo "nfpm not found. Install it with:"; \
	  echo "  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"; \
	  exit 1; }
	@mkdir -p $(DIST)
	for arch in $(ARCHES); do \
	  for packager in deb apk; do \
	    ARCH=$$arch VERSION=$(PKGVERSION) \
	      nfpm package -f packaging/nfpm.yaml -p $$packager -t $(DIST) || exit 1; \
	  done; \
	done
	@ls -1 $(DIST)/*.deb $(DIST)/*.apk 2>/dev/null || true

.PHONY: tarball
tarball: web cross ## Build the static tarballs, for distributions the .deb does not cover
	@# The fallback path, and it is a fallback: without a package manager
	@# nothing resolves the dnsmasq dependency, so install.sh has to look for it
	@# by hand and write a unit drop-in when the path is not Debian's.
	for arch in $(ARCHES); do \
	  stage=$(DIST)/olr-$(PKGVERSION)-linux-$$arch; \
	  rm -rf $$stage && mkdir -p $$stage/systemd; \
	  cp $(DIST)/$(BIN)-linux-$$arch $$stage/$(BIN); \
	  cp $(DIST)/$(DBIN)-linux-$$arch $$stage/$(DBIN); \
	  cp packaging/systemd/*.service $$stage/systemd/; \
	  cp packaging/tarball/install.sh $$stage/install.sh; \
	  chmod +x $$stage/install.sh $$stage/$(BIN) $$stage/$(DBIN); \
	  tar -C $(DIST) -czf $$stage.tar.gz $$(basename $$stage) || exit 1; \
	  rm -rf $$stage; \
	done
	@ls -1 $(DIST)/*.tar.gz 2>/dev/null || true

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
