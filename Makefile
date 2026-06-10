# Local dev loop — mirrors .github/workflows/ci.yml so green here ≈ green CI.
# Tool versions are pinned once below; keep them in sync with ci.yml.

GOLANGCI_VERSION := v2.12.2
GOLANGCI         := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
GOVULNCHECK      := go run golang.org/x/vuln/cmd/govulncheck@v1.3.0
ACTIONLINT       := go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7

.PHONY: help test test-race lint fmt fmt-check vuln tidy-check shellcheck actionlint e2e contract build-all check

help: ## list targets
	@awk -F':.*## ' '/^[a-z0-9-]+:.*## / { printf "  %-12s %s\n", $$1, $$2 }' Makefile

test: ## unit + e2e tests (e2e builds the real binary against the mock API)
	go test ./...

test-race: ## what CI runs
	go test ./... -race

lint: ## golangci-lint, same version as CI
	$(GOLANGCI) run

fmt: ## apply gofmt/goimports via golangci-lint
	$(GOLANGCI) fmt

fmt-check: ## fail if formatting is needed
	$(GOLANGCI) fmt --diff

vuln: ## govulncheck against the Go vulnerability database
	$(GOVULNCHECK) ./...

tidy-check: ## fail on go.mod/go.sum drift, no writes
	go mod tidy -diff

shellcheck: ## lint install.sh (needs shellcheck: brew install shellcheck)
	@command -v shellcheck >/dev/null || { echo "shellcheck not installed (brew install shellcheck)"; exit 1; }
	shellcheck install.sh

actionlint: ## lint GitHub workflow files
	$(ACTIONLINT)

e2e: ## focused verbose run of the binary-level e2e tests
	go test ./cmd/positronick/ -v

contract: ## live-API contract tests (NOT part of check); override POSITRONICK_BASE_URL to retarget
	POSITRONICK_BASE_URL=$${POSITRONICK_BASE_URL:-https://positronick.com} \
		go test -tags contract ./internal/api/... -run Contract -v

build-all: ## cross-compile the same matrix as CI
	@for os in linux darwin windows; do for arch in amd64 arm64; do \
		echo "build $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -o /dev/null ./cmd/positronick || exit 1; \
	done; done

check: tidy-check fmt-check lint vuln shellcheck actionlint test-race build-all ## everything CI runs (fmt-check is local-only, stricter than CI)
