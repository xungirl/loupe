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

# --- Claude Code ------------------------------------------------------------
CLAUDE_DIR ?= $(HOME)/.claude/projects
LOCAL_DB   ?= data/local.db

.PHONY: import-claude serve-local
## import-claude: import your own Claude Code transcripts into data/local.db (gitignored)
import-claude: build
	$(BIN) import-claude -dir "$(CLAUDE_DIR)" -db $(LOCAL_DB)

## serve-local: browse data/local.db in the UI
serve-local: build web-build
	$(BIN) serve -db $(LOCAL_DB) -web web/dist
