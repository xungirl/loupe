#!/usr/bin/env bash
# Reproduces every number in BENCHMARK.md. Raw outputs go to results/bench/.
# Usage: scripts/bench.sh [all|ingest|overload|query|import|assembly]
# Runs on localhost only; close other heavy apps first (shared-laptop caveat).
set -euo pipefail
cd "$(dirname "$0")/.."
BIN=bin/loupe
OUT=results/bench
TMP=data/bench
PORT=7380
URL=http://127.0.0.1:$PORT
mkdir -p "$OUT" "$TMP"
make -s build

start_server() { # $1 = db, rest = extra flags
  local db=$1; shift
  rm -f "$db" "$db-wal" "$db-shm"
  $BIN serve -db "$db" -addr 127.0.0.1:$PORT "$@" >"$TMP/server.log" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 100); do curl -sf $URL/healthz >/dev/null && return; sleep 0.05; done
  echo "server did not start"; cat "$TMP/server.log"; exit 1
}
stop_server() { kill -TERM "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; }
trap 'stop_server 2>/dev/null || true' EXIT

machine() {
  { echo "date: $(date '+%Y-%m-%d %H:%M %Z')"; echo "cpu: $(sysctl -n machdep.cpu.brand_string) ($(sysctl -n hw.ncpu) cores)";
    echo "mem: $(( $(sysctl -n hw.memsize) / 1073741824 )) GB"; echo "os: $(sw_vers -productName) $(sw_vers -productVersion)";
    echo "go: $(go version | awk '{print $3}')"; echo "git: $(git rev-parse --short HEAD)"; } > "$OUT/machine.txt"
}

bench_ingest() {
  : > "$OUT/ingest.jsonl"
  for rep in 1 2 3; do
    for cfg in "single 1 30000" "batched 50 300000" "batched 500 300000"; do
      set -- $cfg
      start_server "$TMP/ingest.db"
      $BIN loadgen -url $URL -mode "$1" -batch "$2" -spans "$3" -concurrency 8 -label "rep$rep batch=$2" -out "$OUT/ingest.jsonl" >/dev/null
      stop_server
    done
  done
  echo "ingest done"
}

bench_overload() {
  : > "$OUT/overload.jsonl"; : > "$OUT/overload_rss.txt"
  for rep in 1 2 3; do
    start_server "$TMP/overload.db" -queue 5000 -batch 2000
    ( while kill -0 "$SERVER_PID" 2>/dev/null; do ps -o rss= -p "$SERVER_PID" >> "$TMP/rss.$rep" || true; sleep 0.05; done ) &
    SAMPLER=$!
    $BIN loadgen -url $URL -batch 500 -spans 300000 -concurrency 32 -retry=false -label "rep$rep overload queue=5000" -out "$OUT/overload.jsonl" >/dev/null
    stop_server; wait "$SAMPLER" 2>/dev/null || true
    echo "rep$rep max_rss_kb=$(sort -n "$TMP/rss.$rep" | tail -1)" >> "$OUT/overload_rss.txt"; rm -f "$TMP/rss.$rep"
  done
  echo "overload done"
}

bench_query() {
  local db=$TMP/bench-1m.db
  $BIN bench-query -db "$db" -generate 1000000 2> "$OUT/query_generate.txt"
  { /usr/bin/time -p $BIN detect -db "$db"; } 2> "$OUT/query_detect_all.txt"
  : > "$OUT/query.jsonl"
  for rep in 1 2 3; do
    $BIN serve -db "$db" -addr 127.0.0.1:$PORT >"$TMP/server.log" 2>&1 &
    SERVER_PID=$!
    for _ in $(seq 100); do curl -sf $URL/healthz >/dev/null && break; sleep 0.05; done
    $BIN bench-query -db "$db" -url $URL -n 300 -label "rep$rep" -out "$OUT/query.jsonl" 2>> "$OUT/query_table.txt" >/dev/null
    stop_server
  done
  ls -l "$db" | awk '{print "db_bytes", $5}' > "$OUT/query_dbsize.txt"
  echo "query done"
}

bench_import() {
  local dir=$TMP/transcripts
  $BIN gen-transcripts -dir "$dir" -sessions 400 -turns 120 2> "$OUT/import_corpus.txt"
  : > "$OUT/import.jsonl"
  for rep in 1 2 3; do
    rm -f "$TMP/import.db"*
    $BIN import-claude -dir "$dir" -db "$TMP/import.db" 2>/dev/null | sed "s/^{/{\"label\":\"rep$rep write\",/" >> "$OUT/import.jsonl"
    $BIN import-claude -dir "$dir" -dry 2>/dev/null | sed "s/^{/{\"label\":\"rep$rep parse-only\",/" >> "$OUT/import.jsonl"
  done
  echo "import done"
}

bench_assembly() {
  (cd server && go test -run xxx -bench BenchmarkBuild10k -benchmem -count 3 ./internal/assemble/) > "$OUT/assembly_gobench.txt"
  echo "assembly done"
}

machine
case "${1:-all}" in
  ingest) bench_ingest ;; overload) bench_overload ;; query) bench_query ;; import) bench_import ;; assembly) bench_assembly ;;
  all) bench_ingest; bench_overload; bench_query; bench_import; bench_assembly ;;
esac
