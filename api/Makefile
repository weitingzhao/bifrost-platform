.PHONY: build run test tidy

build:
	go build -o bin/platform-api ./cmd/platform-api

run:
	go run ./cmd/platform-api

test:
	go test ./...

tidy:
	go mod tidy
