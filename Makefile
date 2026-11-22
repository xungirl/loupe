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

# --- Experiment (see PREREG.md / EXPERIMENT.md) -------------------------------
PY      := PYTHONPATH=sdk/python:agents:experiment uv run python
RUN     ?= real-qwen3.8-flash
REPS    ?= 3
CAP     ?= 4.50

.PHONY: experiment-verify experiment-dryrun experiment experiment-report
## experiment-verify: every reference solution passes, every stub fails
experiment-verify:
	@mkdir -p results/experiment
	$(PY) -m loupe_exp.tasks | tee results/experiment/task_verification.json

## experiment-dryrun: full pipeline with the fake deterministic provider (no key, no cost)
experiment-dryrun: build
	rm -rf results/experiment/dryrun-fake
	$(PY) -m loupe_exp.run --provider fake --out results/experiment/dryrun-fake --reps 1
	$(MAKE) experiment-report RUN=dryrun-fake

## experiment: the real run (needs DASHSCOPE_API_KEY or LOUPE_KEYFILE; resumable; stops at the ¥ cap)
experiment: build
	$(PY) -m loupe_exp.run --provider dashscope --out results/experiment/$(RUN) --reps $(REPS) --cap $(CAP)

## experiment-report: findings (Loupe detectors) + preregistered analysis for RUN
experiment-report: build
	rm -f results/experiment/$(RUN)/loupe.db*
	$(BIN) ingest-file -db results/experiment/$(RUN)/loupe.db results/experiment/$(RUN)/spans.jsonl
	$(BIN) detect -db results/experiment/$(RUN)/loupe.db
	$(PY) -m loupe_exp.analyze results/experiment/$(RUN) --db results/experiment/$(RUN)/loupe.db
