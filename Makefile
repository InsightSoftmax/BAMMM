.PHONY: build test lint tidy check install clean help

BINARY     := bammm
CMD        := ./cmd/bammm
GOFLAGS    := -race
LDFLAGS    := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

## build: compile the binary into ./bin/bammm
build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

## install: install bammm into $GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

## test: run all tests with race detector and coverage
test:
	go test $(GOFLAGS) -coverprofile=coverage.out -covermode=atomic ./...

## test-short: run only unit tests (no integration tags)
test-short:
	go test $(GOFLAGS) -short ./...

## lint: run golangci-lint
lint:
	golangci-lint run --timeout=5m

## tidy: tidy and verify the module graph
tidy:
	go mod tidy
	go mod verify

## vuln: scan for known vulnerabilities
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## check: lint + test (what CI runs)
check: lint test

## clean: remove build artifacts
clean:
	rm -rf bin/ coverage.out dist/

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
