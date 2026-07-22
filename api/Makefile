.PHONY: build run test tidy vet lint check

build:
	go build -o bin/platform-api ./cmd/platform-api

run:
	go run ./cmd/platform-api

test:
	go test ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed — run: make install-lint-api (or brew install golangci-lint)"; exit 1; }
	golangci-lint run ./...

tidy:
	go mod tidy

check: vet lint test
