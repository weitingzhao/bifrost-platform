.PHONY: dev dev-api dev-console test test-api test-console build-api start

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

test: test-api test-console

build-api:
	cd api && go build -o bin/platform-api ./cmd/platform-api
