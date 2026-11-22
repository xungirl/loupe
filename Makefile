# Loupe — one entry point for building, testing, demoing and benchmarking.
SHELL := /bin/bash
BIN   := bin/loupe
GO    := cd server && go

PORT  ?= 7070

.PHONY: help build test test-go test-race test-py test-web e2e web-deps web-build demo demo-seed screenshots bench clean
help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | sort

build: ## build the Go binary (bin/loupe)
	$(GO) build -o ../$(BIN) ./cmd/loupe

test: test-race test-py test-web ## Go (race, 5x) + pytest + vitest

test-go: ## Go tests once
	$(GO) test ./...

test-race: ## Go tests with the race detector, 5 times
	$(GO) test -race -count=5 ./...

test-py: ## Python tests (SDK, hook, agent, ledger, experiment)
	uv run pytest

web-deps:
	@test -d web/node_modules || (cd web && npm ci --no-audit --no-fund)

test-web: web-deps ## vitest unit tests
	cd web && npm test

web-build: web-deps ## build the UI into web/dist
	cd web && npm run build

e2e: build web-build ## Playwright end-to-end tests (starts its own server)
	cd web && npm run e2e

screenshots: build web-build ## regenerate docs/screenshots
	cd web && npm run screenshots

demo-seed: build ## (re)create data/demo.db with synthetic traces + the fake-provider experiment runs
	@test -f results/experiment/dryrun-fake/spans.jsonl || $(MAKE) experiment-dryrun
	$(BIN) demo-seed -db data/demo.db -spans results/experiment/dryrun-fake/spans.jsonl

demo: build web-build demo-seed ## one command: seed demo data, serve API + UI on http://127.0.0.1:$(PORT)
	@echo "Loupe demo → http://127.0.0.1:$(PORT)  (Ctrl-C to stop)"
	$(BIN) serve -db data/demo.db -addr 127.0.0.1:$(PORT) -web web/dist -demo-live

bench: ## run every benchmark (see BENCHMARK.md); takes several minutes
	scripts/bench.sh all

clean:
	rm -rf bin web/dist data/demo.db* data/bench

# --- Claude Code ------------------------------------------------------------
CLAUDE_DIR ?= $(HOME)/.claude/projects
LOCAL_DB   ?= data/local.db

.PHONY: import-claude serve-local
import-claude: build ## import YOUR Claude Code transcripts into data/local.db (gitignored)
	$(BIN) import-claude -dir "$(CLAUDE_DIR)" -db $(LOCAL_DB)

serve-local: build web-build ## browse data/local.db in the UI
	$(BIN) serve -db $(LOCAL_DB) -web web/dist

# --- Experiment (see PREREG.md / EXPERIMENT.md) -------------------------------
PY      := PYTHONPATH=sdk/python:agents:experiment uv run python
RUN     ?= real-qwen3.8-flash
REPS    ?= 3
CAP     ?= 4.50

.PHONY: experiment-verify experiment-dryrun experiment experiment-report
experiment-verify: ## every task: reference passes, stub fails
	@mkdir -p results/experiment
	$(PY) -m loupe_exp.tasks | tee results/experiment/task_verification.json

experiment-dryrun: build ## full experiment pipeline with the fake provider (no key, no cost)
	rm -rf results/experiment/dryrun-fake
	$(PY) -m loupe_exp.run --provider fake --out results/experiment/dryrun-fake --reps 1
	$(MAKE) experiment-report RUN=dryrun-fake

experiment: build ## REAL run: needs DASHSCOPE_API_KEY or LOUPE_KEYFILE; resumable; stops at the ¥ cap
	$(PY) -m loupe_exp.run --provider dashscope --out results/experiment/$(RUN) --reps $(REPS) --cap $(CAP)

experiment-report: build ## detectors + preregistered analysis for RUN=<dir>
	rm -f results/experiment/$(RUN)/loupe.db*
	$(BIN) ingest-file -db results/experiment/$(RUN)/loupe.db results/experiment/$(RUN)/spans.jsonl
	$(BIN) detect -db results/experiment/$(RUN)/loupe.db
	$(PY) -m loupe_exp.analyze results/experiment/$(RUN) --db results/experiment/$(RUN)/loupe.db
