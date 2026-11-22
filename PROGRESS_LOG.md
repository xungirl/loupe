# Progress log

Resume recipe: `git status` + `git log --oneline` + the last entry below.
Times are PST (America/Los_Angeles).

- 2026-11-22 01:58 — started. Repo initialised at /Users/xunxun/loupe (branch main). Go module lives in `server/` (single `loupe` binary with subcommands). SQLite driver: modernc.org/sqlite (pure Go, no cgo — `go test -race` and cross-builds stay simple; trade-off: slower than mattn/go-sqlite3).
- 2026-11-22 02:14 — stage 1 done (tag stage-1): model/store/ingest/otlp/assemble + HTTP ingest. `go test -race -count=3 ./...` green. Query API/SSE/stats handlers were written alongside (tests in api_test.go) and get finished in stage 3.
- 2026-11-22 02:17 — stage 2 done (tag stage-2): importer + hook adapter + docs/hooks.md. Python pinned to 3.15 via .python-version (uv had defaulted to 3.13).
- 2026-11-22 02:18 — stage 3 done (tag stage-3): cost table + config/prices.json, /metrics, retention, shutdown integration test.
- 2026-11-22 02:23 — stage 4 done (tag stage-4): detectors + corpus. Held-out test macro-F1 0.803 (results/detectors/test.txt). Two tuning rounds on train only, documented in results/detectors/README.md.
