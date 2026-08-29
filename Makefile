GO      ?= go
NPM     ?= npm
NPM_INSTALL ?= install
DIST    := dist
WEB     := web
ASSETS  := internal/webui/assets

# Two binaries: the CLI and the resident control plane (design.md §3.5).
BIN     := olr
PKG     := ./cmd/olr
DBIN    := olrd
DPKG    := ./cmd/olrd

# The version lives in one file, VERSION, and everything else is derived from
# it: what the binaries report, what dpkg and apk sort on, the tarball names,
# and what the release publishes. Bumping it is a reviewable commit, and the
# tag that follows only records which commit claims that number — so a clone
# with no tags fetched gives the same answer CI does. PKGVERSION is the bare
# form because dpkg and apk sort on it, and they want a number with no v.
PKGVERSION ?= $(shell tr -d '[:space:]' < VERSION)

# Set by the release workflow from the pushed tag; empty for a local check.
# Exported rather than pasted into version-check's recipe: a tag name is
# user-supplied text, and interpolating it is how that text becomes a command.
TAG ?=
export TAG

# Empty when this build *is* the release — HEAD carries the tag, and nothing has
# been edited since — and a marker otherwise, so a build from a working tree
# cannot report itself as the released artefact. That guarantee is the one thing
# the old `git describe --dirty` bought which a file on its own does not.
GITSUFFIX := $(shell \
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 0; \
  git tag --points-at HEAD 2>/dev/null | grep -Fqx 'v$(PKGVERSION)' || printf %s -dev; \
  git diff --quiet HEAD -- 2>/dev/null || printf %s -dirty)

VERSION ?= v$(PKGVERSION)$(GITSUFFIX)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

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
	  | awk -F':.*?## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: version
version: ## Print the version this build stamps in
	@echo '$(VERSION)'

.PHONY: pkgversion
pkgversion: ## Print the bare version, as dpkg, apk and the tarballs want it
	@echo '$(PKGVERSION)'

.PHONY: version-check
version-check: ## Check VERSION is well-formed, and matches $TAG when one is set
	@# The release workflow runs this before it runs anything else, so that a
	@# mistyped tag costs seconds rather than a published version number.
	@# A trailing -rc1 and the like is allowed: release.yml reads the dash as
	@# "prerelease", so the two agree on what an unfinished version looks like.
	@echo '$(PKGVERSION)' | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$$' || { \
	  echo "VERSION is not a version number: '$(PKGVERSION)'" >&2; exit 1; }
	@if [ -n "$$TAG" ] && [ "$$TAG" != "v$(PKGVERSION)" ]; then \
	  echo "tag $$TAG does not match VERSION, which says v$(PKGVERSION)" >&2; \
	  echo "the file is the source of truth: bump VERSION in a commit, then tag it" >&2; \
	  exit 1; \
	fi
	@echo "VERSION $(PKGVERSION) ok$${TAG:+, matching tag $$TAG}"

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
	@# `install` for a developer, who may be adding a dependency; CI overrides
	@# this with `ci`, which installs exactly the lockfile and fails rather than
	@# rewriting it. Same target either way, so there is one answer to "how are
	@# the SPA's dependencies installed".
	cd $(WEB) && $(NPM) $(NPM_INSTALL)

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
