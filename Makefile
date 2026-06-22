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

test-console:
	cd console && npm run type-check

check-spine:
	bash scripts/ci/check_spine_catalog.sh

test: test-api test-console check-spine

build-api:
	cd api && go build -o bin/platform-api ./cmd/platform-api
