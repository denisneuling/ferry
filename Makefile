# Tool versions — the single source of truth for a local run and CI: the
# Forgejo workflows grep them out of this file, so the two cannot drift apart.
# A full release tag, not a minor: the install script the lint job uses fetches
# a GitHub release, and there is no release called v2.14.
GOLANGCI_LINT_VERSION ?= v2.14.0
GITLEAKS_VERSION ?= v8.30.1
HELM_DOCS_VERSION ?= v1.14.2

BINARIES := ferry

# The test Samba (hack/samba): fictional users and shares, including the
# `map to guest = Bad User` setting ferry has to refuse. podman by default, as
# in the sibling projects; CONTAINER=docker works too.
CONTAINER ?= podman
DEV_SAMBA_ALPINE ?= docker.io/library/alpine:3.22
DEV_SAMBA_PORT ?= 4445
FERRY_TEST_SMB_SERVER ?= 127.0.0.1:$(DEV_SAMBA_PORT)

.PHONY: build test test-samba lint clean build-all changelog release install-tools ci \
	dev-samba dev-samba-stop secret-scan secret-scan-history $(addprefix build-,$(BINARIES))

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	go build ./...

# The SMB tests skip without FERRY_TEST_SMB_SERVER; test-samba runs them
# against the Samba from `make dev-samba`.
test:
	go test -v -count=1 ./...

test-samba:
	FERRY_TEST_SMB_SERVER=$(FERRY_TEST_SMB_SERVER) go test -race -count=1 ./internal/smb/...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

# What CI checks on a pull request, in the order it checks it.
ci: secret-scan lint test

# Samba on 127.0.0.1:$(DEV_SAMBA_PORT); its state goes with the container,
# which `make dev-samba-stop` stops and removes.
dev-samba:
	$(CONTAINER) build -q --build-arg ALPINE_IMAGE=$(DEV_SAMBA_ALPINE) -t ferry-dev-samba hack/samba
	$(CONTAINER) run -d --rm --name ferry-dev-samba -p 127.0.0.1:$(DEV_SAMBA_PORT):445 ferry-dev-samba
	@i=0; until $(CONTAINER) logs ferry-dev-samba 2>&1 | grep -q 'smbd version'; do \
		i=$$((i + 1)); [ $$i -lt 60 ] || { echo "Samba did not start"; exit 1; }; sleep 1; \
	done
	@echo "FERRY_TEST_SMB_SERVER=$(FERRY_TEST_SMB_SERVER)"

dev-samba-stop:
	$(CONTAINER) stop ferry-dev-samba

# ── Binaries ───────────────────────────────────────────────────────────────

# One binary for the current platform, into bin/.
$(addprefix build-,$(BINARIES)): build-%:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$* ./cmd/$*

# Every binary for the release platforms.
build-all:
	@mkdir -p bin
	@for bin in $(BINARIES); do \
		for arch in amd64 arm64; do \
			echo "Building $$bin linux/$$arch..."; \
			CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath \
				-ldflags="$(LDFLAGS)" \
				-o bin/$$bin-linux-$$arch ./cmd/$$bin || exit 1; \
		done; \
	done

# Tools a local run needs on PATH.
install-tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/norwoodj/helm-docs/cmd/helm-docs@$(HELM_DOCS_VERSION)

# ── Releases ───────────────────────────────────────────────────────────────

# Generate CHANGELOG.md from conventional commits.
changelog:
	git-cliff --config cliff.toml -o CHANGELOG.md

# Releases are automated: semantic-release decides the version on every push to
# main, once releases are unlocked. See .forgejo/README.md.
release:
	@echo "ERROR: manual releases are not made. See .forgejo/README.md, Releases are locked." && exit 1

# ── Secret scanning ────────────────────────────────────────────────────────

# gitleaks, downloaded rather than compiled: it is a static binary and the
# release tarball is seconds where `go install` is a minute of dependencies.
bin/gitleaks:
	@mkdir -p bin
	curl -sSfL "https://github.com/gitleaks/gitleaks/releases/download/$(GITLEAKS_VERSION)/gitleaks_$(GITLEAKS_VERSION:v%=%)_linux_x64.tar.gz" \
		| tar xz -C bin gitleaks
	@chmod +x bin/gitleaks

# What ci.yml runs. The working tree, so the result depends only on the commit
# under test and not on how much history the checkout happened to fetch.
secret-scan: bin/gitleaks
	bin/gitleaks dir . --no-banner --redact

# The whole history, for an audit. Deliberately not in CI: a leak in an old
# commit would turn every run red until the history is rewritten, which is a
# decision to take once and not a gate on unrelated work.
secret-scan-history: bin/gitleaks
	bin/gitleaks git . --no-banner --redact
