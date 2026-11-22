// Typed client for the Loupe Go server.
//
// Every URL is relative ("/api/v1/..."): in dev, Vite proxies /api to the Go
// server; in production the Go server serves this UI itself.
// All timestamps are Unix NANOSECONDS; all costs are USD unless noted.

export type SpanKind = 'agent' | 'llm' | 'tool' | 'step' | 'event'
export type DetectorName = 'loop' | 'rework' | 'error_streak' | 'stuck' | 'budget_burn'
export type Severity = 'warn' | 'critical'

export interface TraceSummary {
  trace_id: string
  name: string
  source: string
  start_ns: number
  end_ns: number
  span_count: number
  error_count: number
  llm_calls: number
  tool_calls: number
  agent_count: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
  last_ingest_ns: number
  detected_ns: number
  finding_count: number
}

export interface Span {
  trace_id: string
  span_id: string
  parent_span_id?: string
  kind: SpanKind
  name: string
  source?: string
  agent_id?: string
  agent_name?: string
  start_ns: number
  end_ns: number
  status?: 'ok' | 'error' // the server omits it when "ok"
  error_msg?: string
  model?: string
  input_tokens?: number
  output_tokens?: number
  cache_read_tokens?: number
  cache_write_tokens?: number
  cost_usd?: number
  tool_name?: string
  target?: string
  tool_input?: string
  tool_output?: string
  attributes?: string // a JSON string
}

/** One row of the waterfall: a span plus where it sits in the tree. */
export interface SpanNode {
  span: Span
  depth: number
  orphan?: boolean // parent span has not been received (yet)
  agent_key: string
}

export interface Agent {
  key: string
  name: string
  parent_key?: string
  span_id: string
  start_ns: number
  end_ns: number
  llm_calls: number
  tool_calls: number
  errors: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  cost_usd: number
  children?: string[]
}

export interface Finding {
  id: number
  trace_id: string
  detector: DetectorName
  severity: Severity
  agent_id: string
  summary: string
  span_ids: string[]
  score: number
  created_ns: number
}

export interface TraceDetail {
  summary: TraceSummary
  spans: SpanNode[] // depth-first order = waterfall order
  agents: Agent[] | null
  orphans: number
  max_depth: number
  findings: Finding[] | null
}

export interface TraceList {
  traces: TraceSummary[] | null
  total: number
  limit: number
  offset: number
}

export interface Stats {
  totals: {
    traces: number
    spans: number
    errors: number
    llm_calls: number
    tool_calls: number
    input_tokens: number
    output_tokens: number
    cache_tokens: number
    cost_usd: number
    findings: number
  }
  // Any of these arrays can be null when there is no data.
  by_tool: { tool: string; calls: number; errors: number; error_rate: number; avg_ms: number }[] | null
  by_model:
    | {
        model: string
        calls: number
        input_tokens: number
        output_tokens: number
        cache_tokens: number
        cost_usd: number
        unpriced: boolean
      }[]
    | null
  by_agent:
    | { agent: string; runs: number; llm_calls: number; tool_calls: number; errors: number; tokens: number; cost_usd: number }[]
    | null
  by_day: { day: string; traces: number; tokens: number; cost_usd: number }[] | null
  by_detector: { detector: string; findings: number; traces: number }[] | null
  hotspots: { target: string; tool: string; calls: number; traces: number; errors: number }[] | null
}

export interface PriceTable {
  usd_per_cny: number
  source: string
  prices: { prefix: string; currency: string; input: number; output: number; cache_read: number; cache_write: number }[]
}

// Experiment reports are written by the Python harness. Every field is
// optional on purpose: the UI must render whatever subset is present.
export type ArmId = string
export interface ArmSummary {
  arm?: ArmId
  label?: string
  n?: number
  pass_rate?: number
  pass_ci95?: [number, number]
  mean_tokens?: number
  mean_cost_cny?: number
  mean_steps?: number
  mean_rework?: number
  mean_wall_s?: number
  loop_findings?: number
}
export interface PairedComparison {
  comparison?: string
  primary?: boolean // the preregistered primary comparison
  metric?: string
  diff?: number
  ci95?: [number, number]
  n_tasks?: number
  wins?: number
  losses?: number
  ties?: number
  mcnemar_p?: number
  token_ratio?: number
  token_ratio_ci95?: [number, number]
}
export interface TaskArmResult {
  passed?: boolean
  tokens?: number
  steps?: number
  rework?: number
  trace_id?: string
}
export interface ExperimentReport {
  run_id?: string
  provider?: string
  model?: string
  is_dry_run?: boolean
  created_at?: string
  tasks?: number
  arms?: ArmId[]
  arm_labels?: Record<ArmId, string>
  runs_completed?: number
  runs_planned?: number
  spend?: { currency?: string; spent?: number; cap?: number }
  summary_by_arm?: ArmSummary[]
  paired?: PairedComparison[]
  // per_task rows look like {task: "t01", A: {...}, B: {...}}
  per_task?: Array<{ task?: string } & Record<string, TaskArmResult | string | undefined>>
  planning_estimate?: {
    label?: string
    // either a plain token count or an {input, output} split per arm
    tokens_per_run_by_arm?: Record<ArmId, number | { input?: number; output?: number }>
    projected_cost_cny?: number
  }
  notes?: string[]
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (!res.ok) {
    // The server answers errors as {"error": "..."}; fall back to status text.
    let msg = res.statusText
    try {
      const body = await res.json()
      if (body && typeof body.error === 'string') msg = body.error
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg || `HTTP ${res.status}`)
  }
  return (await res.json()) as T
}

/** Builds "?a=1&b=2", skipping empty values so URLs stay tidy. */
export function queryString(params: Record<string, string | number | boolean | undefined | null>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '' || v === false) continue
    q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

export interface TraceQuery {
  q?: string
  source?: string
  has_error?: boolean
  has_finding?: boolean
  detector?: string
  tool?: string
  agent?: string
  min_cost?: number
  since?: string
  sort?: string
  limit?: number
  offset?: number
}

export const api = {
  listTraces: (p: TraceQuery) => request<TraceList>(`/api/v1/traces${queryString({ ...p })}`),
  getTrace: (id: string) => request<TraceDetail>(`/api/v1/traces/${encodeURIComponent(id)}`),
  detect: (id: string) =>
    request<{ findings: Finding[] | null }>(`/api/v1/traces/${encodeURIComponent(id)}/detect`, { method: 'POST' }),
  listFindings: (p: { detector?: string; limit?: number } = {}) =>
    request<{ findings: Finding[] | null }>(`/api/v1/findings${queryString(p)}`),
  stats: (p: { since?: string; source?: string } = {}) => request<Stats>(`/api/v1/stats${queryString(p)}`),
  prices: () => request<PriceTable>('/api/v1/prices'),
  experiments: () => request<{ reports: ExperimentReport[] | null; dir?: string }>('/api/v1/experiments'),
  health: () => request<{ ok: boolean; queue_depth?: number }>('/healthz'),
}

/** URL for the Server-Sent Events live tail (used with EventSource). */
export function liveUrl(traceId?: string): string {
  return `/api/v1/live${queryString({ trace_id: traceId })}`
}
