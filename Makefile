GO      ?= go
BIN     := olr
PKG     := ./cmd/olr
DIST    := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X github.com/open-linux-router/open-linux-router/internal/buildinfo.Version=$(VERSION) \
  -X github.com/open-linux-router/open-linux-router/internal/buildinfo.Commit=$(COMMIT) \
  -X github.com/open-linux-router/open-linux-router/internal/buildinfo.Date=$(DATE)

# Static by default: the .deb has to run on any glibc/musl box (design.md §8).
export CGO_ENABLED := 0

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

.PHONY: build
build: ## Build olr for the host
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN) $(PKG)

.PHONY: cross
cross: ## Build olr for linux/amd64 and linux/arm64
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN)-linux-amd64 $(PKG)
	GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BIN)-linux-arm64 $(PKG)

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
