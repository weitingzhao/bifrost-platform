.PHONY: dev dev-api dev-console dev-agent start-agent test test-api test-console build-api start check-spine

ROOT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Preferred: frees :8780 / :5180 if busy, then starts both
start dev:
	python3 scripts/run_platform.py

dev-api:
	cd api && go run ./cmd/platform-api

dev-console:
	cd console && npm run dev

dev-agent start-agent:
	python3 scripts/run_remediation_agent.py --install

bootstrap-agent-host:
	bash scripts/bootstrap_agent_host.sh

nightly-agent:
	bash scripts/nightly_agent.sh

drift-scan:
	python3 agent/drift/scan_layer1.py

test-api:
	cd api && go test ./...

test-console:
	cd console && npm run type-check

check-spine:
	bash scripts/check_spine_catalog.sh

test: test-api test-console check-spine

build-api:
	cd api && go build -o bin/platform-api ./cmd/platform-api
