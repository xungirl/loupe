# Benchmarks

Every number here comes from `scripts/bench.sh all`, run once on
**2026-11-22 02:49 PST** on an **Apple M2 (8 cores, 24 GB), macOS 27.2.1, Go 1.27.4**
(`results/bench/machine.txt`). Raw outputs are in `results/bench/`;
`results/bench/summary.md` is generated from them by `scripts/bench_summary.py`
(mean ± sample standard deviation over 3 repetitions, with min/max).

Caveats that apply to everything below:

* **Localhost, one laptop.** Client and server share the same 8 cores, so the
  load generator competes with the server for CPU. Real network latency is
  absent. Nothing else heavy was running, but this is a personal laptop, not
  an isolated benchmark machine; the per-span runs in particular varied a lot
  (min 42,930 vs max 66,023 spans/s).
* **SQLite via the pure-Go driver** (`modernc.org/sqlite`), WAL mode,
  `synchronous=NORMAL`. A cgo driver would likely be faster; not measured.
* Synthetic data throughout (generated traces and transcripts).

Reproduce: `make bench` (took 8 minutes here: 02:49→02:57), then `python3 scripts/bench_summary.py`.

## 1. Ingest throughput: batched vs per-span

`bin/loupe loadgen -mode {single|batched} -batch {1|50|500} -concurrency 8`,
fresh database and server per run, default server config (queue 50,000 spans,
writer batches up to 2,000 spans / 50 ms). Request bodies are pre-encoded so
client JSON cost is not measured. **"Committed" throughput is measured until
the server has written every accepted span** (polling
`loupe_ingest_written_spans_total`), not until the 202 responses arrive.

| spans per request | spans | committed spans/s (mean ± sd) | 429 responses | request p99 |
|---|---|---|---|---|
| 1 | 30,000 | 55,027 ± 11,585 | 0 | 0.59 ± 0.31 ms |
| 50 | 300,000 | 137,172 ± 4,166 | 1,348 ± 20 | 1.96 ± 0.58 ms |
| 500 | 300,000 | 135,515 ± 3,406 | 962 ± 23 | 6.72 ± 3.42 ms |

* Batching requests gives **2.49x** the committed throughput of one span per
  request (`summary.md`). The server already coalesces per-span requests into
  large transactions, so the gap is mostly per-request HTTP + JSON overhead.
* Batch 50 and 500 are the same within noise: the bottleneck is then the
  single SQLite writer. For reference, inserting 1M spans directly through the
  store (no HTTP) ran at 139,987 spans/s (`query_generate.txt`, single run) —
  close to the HTTP number, so HTTP adds little once requests are batched.
* The batched runs **hit backpressure during normal operation**: 8 clients
  send faster than the writer commits, the 50,000-span queue fills, the server
  answers 429, and loadgen retries with backoff. In every run
  `written_on_server == accepted_spans` (300,000 of 300,000).

## 2. Overload / backpressure

Server started with `-queue 5000`; 32 clients fire 600 requests × 500 spans
with **no retries** (`-retry=false`).

| rep | 429 of 600 | accepted spans | written by server | max queue depth seen | max server RSS |
|---|---|---|---|---|---|
| 1 | 574 | 13,000 | 13,000 | 5,000 | 61.3 MB |
| 2 | 578 | 11,000 | 11,000 | 5,000 | 58.1 MB |
| 3 | 578 | 11,000 | 11,000 | 5,000 | 62.2 MB |

* The queue never exceeds its configured bound (5,000), memory stays around
  60 MB, and **every accepted span was written** — rejected requests are
  rejected cleanly, accepted ones are not lost.
* The burst arrives in ~0.2 s, much faster than the writer can drain, so most
  requests are refused; that is the intended behaviour without client retries.
  RSS was sampled every 50 ms with `ps` (`overload_rss.txt`).

## 3. Query and assembly latency at ~1M spans

Database: 1,017,547 spans in 24,201 traces (synthetic corpus traces plus one
10,000-span trace), 206 MB on disk. Sequential HTTP requests (concurrency 1)
against `bin/loupe serve`, 20 warm-up requests per endpoint, 3 server runs.

| endpoint | p50 | p99 |
|---|---|---|
| `GET /api/v1/traces/{id}` (load + assemble a random trace; mean 41.6 spans) | 0.38 ms | 0.74 ms |
| `GET /api/v1/traces/{id}` (10,000-span trace) | 40.37 ms | 47.46 ms |
| `GET /api/v1/traces?limit=50&offset=…` (page + total count) | 8.92 ms | 19.48 ms |
| `GET /api/v1/traces?has_error=true&sort=cost` | 4.62 ms | 6.61 ms |
| `GET /api/v1/traces?tool=Bash` (subquery over spans) | 272.77 ms | 302.99 ms |
| `GET /api/v1/spans?tool=Bash&error=true` | 127.40 ms | 145.81 ms |
| `GET /api/v1/findings` | 0.66 ms | 0.92 ms |
| `GET /api/v1/stats` (all aggregates, full scans) | 2,283.87 ms | 2,348.26 ms |

(Means over the 3 runs; standard deviations are in `summary.md` and are small.)

* Trace views — the thing the UI opens most — are sub-millisecond for normal
  traces because a trace is one primary-key range scan in a `WITHOUT ROWID`
  table. In-process, building the tree for 10,000 spans takes 2.67 ± 0.20 ms
  (`go test -bench`, `assembly_gobench.txt`); the rest of the 40 ms is SQLite
  reads and JSON encoding.
* The weak spot is **`/stats`: 2.3 s at 1M spans**, because every aggregate is
  a full scan over `spans` on each request. Fixes, not implemented: maintain
  per-tool/per-model rollups at write time (as the `traces` table already does
  per trace), cache the response, or move aggregates to a columnar store.
* `tool=Bash` filtering walks every Bash span (6.5% of rows here, measured
  with sqlite3) through the partial index and de-duplicates trace ids before
  joining traces; a per-trace tool set maintained at write time would fix it.
* Running all detectors over the 24,201 traces took 7.50 s wall time
  (`query_detect_all.txt`, single run) ≈ 3,200 traces/s.

## 4. Claude Code importer

`bin/loupe gen-transcripts -sessions 400 -turns 120` writes 86.3 MB / 172,819
lines / 620 files in the documented transcript shape (split assistant lines,
tool results, errors, subagent files). Imported 3 times into a fresh DB and 3
times with `-dry` (parse + convert, no writes).

| mode | seconds | MB/s | lines/s |
|---|---|---|---|
| parse + convert only | 0.764 ± 0.023 | 113.0 ± 3.4 | 226,239 ± 6,815 |
| parse + convert + write SQLite | 1.782 ± 0.106 | 48.6 ± 2.8 | 97,201 ± 5,575 |

115,460 spans per import. Writing to SQLite more than doubles the time; the
parser itself is not the bottleneck. These transcripts are synthetic; real
ones have longer tool outputs (bigger lines, more bytes per span).

## 5. Detectors (precision / recall / F1)

Synthetic labelled corpus: 300 traces (`bin/loupe corpus -n 300 -seed 42`),
split by trace-id hash into 150 train / 150 test. Thresholds were tuned on
train only (two rounds, described in `results/detectors/README.md`); test was
evaluated once. Trace-level: a trace is predicted positive for a detector if
it emits at least one finding of that type.

Held-out **test** split (`results/detectors/test.txt`, 56 clean traces):

| detector | TP | FP | FN | precision | recall | F1 |
|---|---|---|---|---|---|---|
| loop | 20 | 15 | 0 | 0.571 | 1.000 | 0.727 |
| rework | 20 | 1 | 6 | 0.952 | 0.769 | 0.851 |
| error_streak | 18 | 0 | 0 | 1.000 | 1.000 | 1.000 |
| stuck | 11 | 11 | 6 | 0.500 | 0.647 | 0.564 |
| budget_burn | 21 | 4 | 2 | 0.840 | 0.913 | 0.875 |
| **macro-F1** | | | | | | **0.803** |

Train split for comparison: macro-F1 0.822 (`results/detectors/train.txt`).

Where the errors come from (from the evaluation's error analysis):

* **loop FPs** (15): accidental repeats inside other faults — e.g. the
  error-streak generator re-runs the same failing command, or a long
  exploration reads the same file three times with the same content. Arguably
  real loops; the labels say otherwise.
* **stuck**: all 6 misses are the "writes NOTES.md every 7 reads" variant —
  a write breaks the read-only streak, which a purely syntactic rule cannot see.
  FPs come mostly from loop traces (repeated reads extend the read streak).
* **rework**: all 6 misses are the "churn spread over two files" variant
  (each file stays below both thresholds).
* error_streak is perfect because the generator's streaks and the rule
  match exactly — a sign of how synthetic this is, not of real-world accuracy.

**Caveat:** the corpus generator and the detectors were written by the same
person; these numbers show the detectors catch the failure patterns *as
modelled*. Measuring them on real traces (hand-labelled Claude Code sessions)
is the next step.
