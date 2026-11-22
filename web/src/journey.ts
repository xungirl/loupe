// The build roadmap shown on /journey. Edit the text freely: the page renders
// whatever is in this array, top to bottom.

export type StageStatus = 'done' | 'todo'

export interface Stage {
  title: string
  description: string
  facts: string[]
  status: StageStatus
}

export const journey: Stage[] = [
  {
    title: 'Data model, ingest, storage, assembly',
    description: 'A span model for agent runs and a batched ingest pipeline into SQLite.',
    facts: ['Native JSON + OTLP/JSON (GenAI semconv) ingest', 'Span-bounded queue, one batching writer, HTTP 429', 'Idempotent by (trace_id, span_id); orphans + cycles handled'],
    status: 'done',
  },
  {
    title: 'Claude Code importer + hook adapter',
    description: 'Turns Claude Code transcripts (incl. nested subagents) into traces.',
    facts: ['Parse stats for every unknown shape', 'Subagents linked by agentId → prompt → time', 'Hook adapter never blocks Claude Code'],
    status: 'done',
  },
  {
    title: 'Query API, SSE live tail, cost, retention, metrics',
    description: 'The read side: filters, stats, a live stream and a price table.',
    facts: ['SSE live tail with slow-client isolation', 'Longest-prefix price table, unknown = unpriced', 'Graceful shutdown drains the queue (tested with SIGTERM)'],
    status: 'done',
  },
  {
    title: 'Detectors + fault-injection corpus + P/R/F1',
    description: 'Loop, rework, error-streak, stuck and budget-burn detectors, measured on injected faults.',
    facts: ['300-trace synthetic corpus, 5 fault types', 'Held-out macro-F1 0.803', 'Thresholds tuned on train only'],
    status: 'done',
  },
  {
    title: 'Web UI',
    description: 'This app: sessions, waterfall, agent tree, live tail, findings, insights.',
    facts: ['React + TypeScript, hand-rolled SVG', '50 Vitest + 10 Playwright tests', 'Served by the Go binary'],
    status: 'done',
  },
  {
    title: 'Python SDK + mini coding agent + providers/ledger',
    description: 'Instrument any agent in a few lines; a small coding agent to experiment with.',
    facts: ['contextvars SDK, OTLP/HTTP JSON exporter', 'DashScope provider + deterministic fake', 'Ledger refuses calls over the ¥4.50 cap'],
    status: 'done',
  },
  {
    title: 'Preregistered experiment + fake-provider dry run',
    description: 'Single agent vs. restated task vs. planner/coder/reviewer, decided before running.',
    facts: ['PREREG.md committed before any real run', '24 tasks, paired bootstrap CIs', '72-run fake-provider dry run (not results)'],
    status: 'done',
  },
  {
    title: 'Benchmarks',
    description: 'How fast ingest and queries are, measured rather than guessed.',
    facts: ['137k committed spans/s batched (2.49x per-span)', 'Trace open p50 0.38 ms at 1.02M spans', '/stats 2.3 s: known weak spot'],
    status: 'done',
  },
  {
    title: 'Docs + one-command demo',
    description: 'Clone, run one command, see a populated UI.',
    facts: ['make demo', 'README, BENCHMARK, INTERVIEW (中文)', 'Screenshots from Playwright'],
    status: 'done',
  },
  {
    title: 'Final verification',
    description: 'Everything re-run from a clean checkout.',
    facts: ['Fresh clone: make test + demo smoke', 'Git history scanned for key-like strings', 'No stray processes'],
    status: 'done',
  },
  {
    title: 'Run the real experiment (qwen3.8-flash, ≤¥4.50)',
    description: 'The real A/B/C run on a cheap model, within a hard spend cap.',
    facts: ['Spend cap enforced by the ledger', 'Report replaces the dry run'],
    status: 'todo',
  },
  {
    title: 'Import my own Claude Code history',
    description: 'Point Loupe at my real sessions and see what the detectors find.',
    facts: ['make import-claude', 'Stays local (gitignored)'],
    status: 'todo',
  },
  {
    title: 'OTLP protobuf + Postgres/ClickHouse backend',
    description: 'Standard OTLP/gRPC-protobuf ingest and a storage backend that scales out.',
    facts: ['Protobuf decoder', 'Pluggable store interface'],
    status: 'todo',
  },
]
