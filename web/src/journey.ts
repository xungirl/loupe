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
    facts: ['Native JSON + OTLP/JSON ingest', 'Batched writes with backpressure (HTTP 429)', 'Trace tree assembly with orphans'],
    status: 'done',
  },
  {
    title: 'Claude Code importer + hook adapter',
    description: 'Turns Claude Code transcripts (incl. nested subagents) into traces.',
    facts: ['Imports ~/.claude/projects history', 'Live hook adapter', 'Subagent → agent tree'],
    status: 'done',
  },
  {
    title: 'Query API, SSE live tail, cost, retention, metrics',
    description: 'The read side: filters, stats, a live stream and a price table.',
    facts: ['Server-Sent Events live tail', 'Per-model price table', 'Prometheus /metrics, retention job'],
    status: 'done',
  },
  {
    title: 'Detectors + fault-injection corpus + P/R/F1',
    description: 'Loop, rework, error-streak, stuck and budget-burn detectors, measured on injected faults.',
    facts: ['120-trace synthetic corpus', 'Held-out precision / recall / F1', 'Tuned on train split only'],
    status: 'done',
  },
  {
    title: 'Web UI',
    description: 'This app: sessions, waterfall, agent tree, live tail, findings, insights.',
    facts: ['React + TypeScript, hand-rolled SVG', 'Vitest + Playwright', 'Served by the Go binary'],
    status: 'done',
  },
  {
    title: 'Python SDK + mini coding agent + providers/ledger',
    description: 'Instrument any agent in a few lines; a small coding agent to experiment with.',
    facts: ['Span context manager SDK', 'Provider abstraction', 'Spend ledger with a hard cap'],
    status: 'done',
  },
  {
    title: 'Preregistered experiment + fake-provider dry run',
    description: 'Single agent vs. restated task vs. planner/coder/reviewer, decided before running.',
    facts: ['Preregistered hypotheses', 'Paired CIs', 'Dry run exercises the pipeline for free'],
    status: 'done',
  },
  {
    title: 'Benchmarks',
    description: 'How fast ingest and queries are, measured rather than guessed.',
    facts: ['Ingest throughput', 'Query latency', 'Assembly time per trace'],
    status: 'done',
  },
  {
    title: 'Docs + one-command demo',
    description: 'Clone, run one command, see a populated UI.',
    facts: ['make demo', 'Architecture notes', 'Screenshots'],
    status: 'done',
  },
  {
    title: 'Final verification',
    description: 'Everything re-run from a clean checkout.',
    facts: ['All tests green', 'Numbers re-checked', 'README matches reality'],
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
