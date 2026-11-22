# Loupe — one entry point for building, testing, demoing and benchmarking.
SHELL := /bin/bash
BIN   := bin/loupe
GO    := cd server && go

.PHONY: build test test-go test-race
build:
	$(GO) build -o ../$(BIN) ./cmd/loupe

test-go:
	$(GO) test ./...

test-race:
	$(GO) test -race -count=5 ./...
