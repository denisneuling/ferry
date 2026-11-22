# Tool versions — the single source of truth for a local run and CI: the
# Forgejo workflows grep them out of this file, so the two cannot drift apart.
# A full release tag, not a minor: the install script the lint job uses fetches
# a GitHub release, and there is no release called v2.14.
GOLANGCI_LINT_VERSION ?= v2.14.0
GITLEAKS_VERSION ?= v8.30.1
HELM_DOCS_VERSION ?= v1.14.2
# Lints the OpenAPI spec; run through npx, so it needs Node but no install.
REDOCLY_CLI_VERSION ?= 2.54.2

BINARIES := ferry
CHARTS := charts/ferry

# The test Samba (hack/samba): fictional users and shares, including the
# `map to guest = Bad User` setting ferry has to refuse. podman by default, as
# in the sibling projects; CONTAINER=docker works too.
CONTAINER ?= podman
DEV_SAMBA_ALPINE ?= docker.io/library/alpine:3.22
DEV_SAMBA_PORT ?= 4445
DEV_SAMBA_BAD_PASSWORD_PORT ?= 4446
FERRY_TEST_SMB_SERVER ?= 127.0.0.1:$(DEV_SAMBA_PORT)
FERRY_TEST_SMB_SERVER_BAD_PASSWORD ?= 127.0.0.1:$(DEV_SAMBA_BAD_PASSWORD_PORT)

# The mock NAS (hack/mock-nas): a household's shares with sample files, for
# development and demonstrations. Switch access-based share enumeration with
# DEV_NAS_ABSE=yes.
DEV_NAS_PORT ?= 4455
DEV_NAS_ABSE ?= no
DEV_NAS_GO_IMAGE ?= docker.io/library/golang:1.27
DEV_NAS_UBUNTU_IMAGE ?= docker.io/library/ubuntu:22.04

# Where `make run` and `make run-nas` listen: not 8080, which other
# development servers tend to hold.
RUN_LISTEN ?= :8095
RUN_OPS_LISTEN ?= :8096

.PHONY: build test test-samba lint clean build-all changelog release install-tools ci \
	ui-install ui-build ui-lint ui-test ui-i18n ui-start screenshots helm-lint helm-docs helm-package \
	run run-nas generate-api lint-api dev-samba dev-samba-stop dev-nas dev-nas-stop dev-nas-check secret-scan secret-scan-history $(addprefix build-,$(BINARIES))

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
	FERRY_TEST_SMB_SERVER=$(FERRY_TEST_SMB_SERVER) \
	FERRY_TEST_SMB_SERVER_BAD_PASSWORD=$(FERRY_TEST_SMB_SERVER_BAD_PASSWORD) \
		go test -race -count=1 ./...

lint: lint-api
	golangci-lint run ./...

clean:
	rm -rf bin/

# What CI checks on a pull request for the Go side, in the order it checks
# it. The interface has its own job (ui-lint ui-i18n ui-test ui-build).
ci: secret-scan lint test helm-lint

# ferry on http://localhost:8095 against the Samba from `make dev-samba`
# (log in as alex / alex-test-password), health on :8096. The interface is
# there after `make ui-build`; for work on it, `make ui-start`.
run:
	FERRY_SMB_SERVER=$${FERRY_SMB_SERVER:-$(FERRY_TEST_SMB_SERVER)} \
	FERRY_SERVER_NAME=$${FERRY_SERVER_NAME:-nas.example.lan} \
	FERRY_LISTEN=$${FERRY_LISTEN:-$(RUN_LISTEN)} \
	FERRY_OPS_LISTEN=$${FERRY_OPS_LISTEN:-$(RUN_OPS_LISTEN)} \
	FERRY_LOG_FORMAT=$${FERRY_LOG_FORMAT:-text} \
	FERRY_LOG_LEVEL=$${FERRY_LOG_LEVEL:-debug} \
		go run ./cmd/ferry serve

# The same against the mock NAS from `make dev-nas`: log in as alex or sam
# (hack/mock-nas/README.md has the passwords).
run-nas:
	$(MAKE) run FERRY_SMB_SERVER=127.0.0.1:$(DEV_NAS_PORT)

# The mock NAS on 127.0.0.1:$(DEV_NAS_PORT). The first build generates the
# sample files (~220 MB) and takes a minute; `make dev-nas-stop` stops and
# removes it, and its state with it.
dev-nas:
	$(CONTAINER) build -q -f hack/mock-nas/Dockerfile \
		--build-arg GO_IMAGE=$(DEV_NAS_GO_IMAGE) --build-arg UBUNTU_IMAGE=$(DEV_NAS_UBUNTU_IMAGE) \
		-t ferry-mock-nas .
	$(CONTAINER) run -d --rm --name ferry-mock-nas -p 127.0.0.1:$(DEV_NAS_PORT):445 \
		-e MOCK_NAS_ACCESS_BASED_SHARE_ENUM=$(DEV_NAS_ABSE) ferry-mock-nas
	@i=0; until $(CONTAINER) logs ferry-mock-nas 2>&1 | grep -q 'smbd version'; do \
		i=$$((i + 1)); [ $$i -lt 60 ] || { echo "the mock NAS did not start"; exit 1; }; sleep 1; \
	done
	@echo "mock NAS on 127.0.0.1:$(DEV_NAS_PORT): make run-nas, then log in as alex / alex-nas-demo"

dev-nas-stop:
	$(CONTAINER) stop ferry-mock-nas

# That the running mock NAS is what its README promises.
dev-nas-check:
	FERRY_TEST_MOCK_NAS=127.0.0.1:$(DEV_NAS_PORT) go test -count=1 ./hack/mock-nas/check/

# Samba on 127.0.0.1:$(DEV_SAMBA_PORT), and its `map to guest = Bad Password`
# instance on $(DEV_SAMBA_BAD_PASSWORD_PORT) (hack/samba/setup.sh). Their state
# goes with the container, which `make dev-samba-stop` stops and removes.
dev-samba:
	$(CONTAINER) build -q --build-arg ALPINE_IMAGE=$(DEV_SAMBA_ALPINE) -t ferry-dev-samba hack/samba
	$(CONTAINER) run -d --rm --name ferry-dev-samba -p 127.0.0.1:$(DEV_SAMBA_PORT):445 \
		-p 127.0.0.1:$(DEV_SAMBA_BAD_PASSWORD_PORT):446 ferry-dev-samba
	@i=0; until $(CONTAINER) logs ferry-dev-samba 2>&1 | grep -q 'smbd version'; do \
		i=$$((i + 1)); [ $$i -lt 60 ] || { echo "Samba did not start"; exit 1; }; sleep 1; \
	done
	@echo "FERRY_TEST_SMB_SERVER=$(FERRY_TEST_SMB_SERVER)"
	@echo "FERRY_TEST_SMB_SERVER_BAD_PASSWORD=$(FERRY_TEST_SMB_SERVER_BAD_PASSWORD)"

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

# ── The interface (Angular, ui/) ─────────────────────────────────────────
# Needs Node.js >= 22.22.3 (ui/.nvmrc: 24). No private registry and no token:
# every package is public.

ui-install:
	cd ui && npm ci --no-audit --no-fund

# Builds the interface into internal/ui/dist/app — once per language — which
# is embedded into the binary. Build ferry afterwards.
ui-build: ui-install
	cd ui && npm run build

ui-lint:
	cd ui && npm run lint

ui-test:
	cd ui && npm test

# The texts to translate, as extracted, must be committed as they are; a
# German one missing fails ui-build.
ui-i18n:
	cd ui && npm run extract-i18n
	cd ui && git diff --exit-code -- src/locale/messages.json \
		|| { echo "ERROR: src/locale/messages.json is out of date: run make ui-i18n, add the German to messages.de.json"; exit 1; }

# The interface with live reload on http://localhost:4200, against the ferry
# of `make run` or `make run-nas`.
ui-start:
	cd ui && npm start -- --port 4200

# The screenshots in docs/images, from the mock NAS (`make dev-nas` first):
# hack/screenshots/shoot.py starts ferry and drives Chromium, with the data,
# the clock and the viewport fixed. Needs Python with playwright and pillow.
screenshots: ui-build
	python3 hack/screenshots/shoot.py

# ── Generated code ─────────────────────────────────────────────────────────

# The API's types, server interface and client, from its OpenAPI spec. The spec
# is the source of truth; TestGeneratedCodeIsCurrent fails when the generated
# code does not match it. oapi-codegen is a tool in go.mod.
generate-api:
	go generate ./api/v1/

lint-api:
	npx -y @redocly/cli@$(REDOCLY_CLI_VERSION) lint api/openapi/ferry-v1.yaml

# Tools a local run needs on PATH. oapi-codegen is not among them: the target
# above runs it at the version go.mod pins.
install-tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/norwoodj/helm-docs/cmd/helm-docs@$(HELM_DOCS_VERSION)

# ── Helm chart ─────────────────────────────────────────────────────────────

# The chart with each of its ci/*-values.yaml. It must also refuse to render
# without an SMB server: a ferry with nothing to log in to is a mistake that
# should fail at install time, not at the first login.
helm-lint:
	@for chart in $(CHARTS); do \
		for f in $$chart/ci/*-values.yaml; do \
			helm lint $$chart -f $$f && helm template $$chart -f $$f > /dev/null || exit 1; \
		done; \
	done
	@if helm template charts/ferry > /dev/null 2>&1; then \
		echo "ERROR: charts/ferry rendered without smb.server"; exit 1; \
	fi

# The chart README from values.yaml.
helm-docs:
	@for chart in $(CHARTS); do helm-docs --chart-search-root $$chart --output-file README.md --log-level warning; done

helm-package:
	@mkdir -p bin
	@for chart in $(CHARTS); do helm package $$chart --destination bin/ || exit 1; done

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
# under test and not on how much history the checkout happened to fetch — and
# only the files git knows (tracked, or new and not ignored), copied into
# bin/secret-scan-tree, so that a local run sees what CI's fresh checkout sees
# and not the caches and builds .gitignore keeps out (ui/.angular,
# node_modules, internal/ui/dist).
secret-scan: bin/gitleaks
	@rm -rf bin/secret-scan-tree && mkdir -p bin/secret-scan-tree
	@git ls-files -z --cached --others --exclude-standard \
		| tar --null --ignore-failed-read -T - -cf - 2>/dev/null | tar -xf - -C bin/secret-scan-tree
	bin/gitleaks dir bin/secret-scan-tree --no-banner --redact

# The whole history, for an audit. Deliberately not in CI: a leak in an old
# commit would turn every run red until the history is rewritten, which is a
# decision to take once and not a gate on unrelated work.
secret-scan-history: bin/gitleaks
	bin/gitleaks git . --no-banner --redact
