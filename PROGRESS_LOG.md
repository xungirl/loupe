# Progress log

Resume recipe: `git status` + `git log --oneline` + the last entry below.
Times are PST (America/Los_Angeles).

- 2026-11-22 01:58 — started. Repo initialised at /Users/xunxun/loupe (branch main). Go module lives in `server/` (single `loupe` binary with subcommands). SQLite driver: modernc.org/sqlite (pure Go, no cgo — `go test -race` and cross-builds stay simple; trade-off: slower than mattn/go-sqlite3).
