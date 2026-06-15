.PHONY: dev dev-api dev-console test test-api test-console build-api start check-spine docs docs-build

ROOT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Preferred: frees :8780 / :5180 if busy, then starts both
start dev:
	python3 scripts/run_platform.py

dev-api:
	cd api && go run ./cmd/platform-api

dev-console:
	cd console && npm run dev

test-api:
	cd api && go test ./...

test-console:
	cd console && npm run type-check

check-spine:
	bash scripts/check_spine_catalog.sh

test: test-api test-console check-spine

build-api:
	cd api && go build -o bin/platform-api ./cmd/platform-api

# Docs staging site (draft notes — governance in Console Architecture) — http://127.0.0.1:8060
docs:
	./scripts/start_docs.sh

docs-build:
	python3 -c "from scripts.run_mkdocs import _ensure_doc_symlinks; _ensure_doc_symlinks()"
	python3 -m mkdocs build
