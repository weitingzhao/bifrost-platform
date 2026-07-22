.PHONY: dev dev-api dev-console dev-agent start-agent test test-api test-console lint-api vet-api build-api start check-spine check install-lint-api

ROOT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Preferred: frees :8780 / :5180 if busy, then starts both
start dev:
	python3 scripts/run_platform.py

dev-api:
	cd api && go run ./cmd/platform-api

dev-console:
	cd console && npm run dev

dev-agent start-agent:
	python3 scripts/run_agent.py start --install

nightly-agent:
	python3 scripts/run_agent.py nightly

deploy-mac-mini-agent:
	python3 scripts/run_agent.py deploy

drift-scan:
	python3 agent/drift/scan_layer1.py

drift-scan-api:
	python3 agent/drift/scan_layer2.py

drift-scan-semantic:
	python3 agent/drift/scan_layer3.py

drift-scan-all:
	python3 scripts/run_agent.py drift

test-api:
	cd api && go test ./...

install-lint-api:
	@command -v golangci-lint >/dev/null 2>&1 && { golangci-lint --version; exit 0; }; \
	if command -v brew >/dev/null 2>&1; then \
		echo "Installing golangci-lint via Homebrew…"; \
		brew install golangci-lint; \
	else \
		echo "Installing golangci-lint via go install…"; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest; \
	fi

lint-api:
	cd api && $(MAKE) lint

vet-api:
	cd api && $(MAKE) vet

test-console:
	cd console && npm run type-check && npm test

cluster-triage:
	cd console && npx --yes tsx scripts/cluster-failure-triage.ts

check-spine:
	bash scripts/ci/check_spine_catalog.sh

test: vet-api lint-api test-api test-console check-spine

check: test

build-api:
	cd api && go build -o bin/platform-api ./cmd/platform-api
