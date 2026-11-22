#!/usr/bin/env bash
# Starts a Loupe server for the Playwright tests:
#   - builds bin/loupe (make build)
#   - seeds a FRESH demo database (the previous run's DB is deleted first)
#   - serves the built UI (web/dist) on 127.0.0.1:7171 with a simulated live feed
# Playwright runs this via `webServer` in playwright.config.ts and stops it after the run.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [ ! -f web/dist/index.html ]; then
  echo "web/dist is missing: run 'npm run build' in web/ first" >&2
  exit 1
fi

make build >/dev/null

# Kept under node_modules/.cache (gitignored) so nothing leaks into /tmp:
# Playwright kills this script hard, so an exit trap would not get to clean up.
DB_DIR="$ROOT/web/node_modules/.cache/loupe-e2e"
DB="$DB_DIR/demo.db"
mkdir -p "$DB_DIR"
rm -f "$DB" "$DB-wal" "$DB-shm"

bin/loupe demo-seed -db "$DB"
exec bin/loupe serve -db "$DB" -addr 127.0.0.1:7171 -web web/dist -demo-live
