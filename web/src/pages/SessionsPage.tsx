import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, type Finding } from '../api'
import { useApi } from '../useApi'
import { formatCompact, formatCost, formatDuration, formatNumber } from '../format'
import {
  DETECTORS,
  DETECTOR_INFO,
  DetectorBadge,
  EmptyState,
  ErrorBox,
  Loading,
  PageHeader,
  SourceBadge,
  StatCard,
  TimeAgo,
} from '../components/ui'

const PAGE_SIZE = 50
const KNOWN_SOURCES = ['claude-code', 'corpus', 'demo-live', 'sdk', 'otlp']
const SORTS = [
  { value: 'start', label: 'Newest first' },
  { value: 'cost', label: 'Most expensive' },
  { value: 'tokens', label: 'Most tokens' },
  { value: 'errors', label: 'Most errors' },
  { value: 'spans', label: 'Most spans' },
]

/**
 * The trace list. All filters live in the URL (?q=…&source=…) so a filtered
 * view can be bookmarked and the back button restores it.
 */
export default function SessionsPage() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()

  const filters = {
    q: params.get('q') ?? '',
    source: params.get('source') ?? '',
    has_error: params.get('has_error') === 'true',
    has_finding: params.get('has_finding') === 'true',
    detector: params.get('detector') ?? '',
    tool: params.get('tool') ?? '',
    sort: params.get('sort') ?? 'start',
  }
  const page = Number(params.get('page') ?? '0') || 0

  /** Update one or more URL params; any filter change resets to page 0. */
  function setFilter(changes: Record<string, string | boolean>) {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries(changes)) {
      if (v === '' || v === false) next.delete(k)
      else next.set(k, String(v))
    }
    if (!('page' in changes)) next.delete('page')
    setParams(next, { replace: true })
  }

  // The search box updates the URL 250 ms after the user stops typing.
  const [text, setText] = useState(filters.q)
  useEffect(() => {
    if (text === filters.q) return
    const t = setTimeout(() => setFilter({ q: text }), 250)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text])

  const key = params.toString()
  const list = useApi(
    () => api.listTraces({ ...filters, limit: PAGE_SIZE, offset: page * PAGE_SIZE }),
    [key],
  )
  const stats = useApi(() => api.stats(), [])
  // Trace summaries only carry finding_count, so we load the findings once to
  // show which detectors fired on each trace.
  const findings = useApi(() => api.listFindings({ limit: 5000 }), [])
  const detectorsByTrace = useMemo(() => groupDetectors(findings.data?.findings ?? []), [findings.data])

  const traces = list.data?.traces ?? []
  const total = list.data?.total ?? 0
  const sources = useMemo(
    () => Array.from(new Set([...KNOWN_SOURCES, ...traces.map((t) => t.source)])).filter(Boolean).sort(),
    [traces],
  )
  const tools = (stats.data?.by_tool ?? []).map((t) => t.tool)
  const t = stats.data?.totals

  return (
    <div className="page">
      <PageHeader title="Sessions" subtitle="Every agent run Loupe has recorded. Click a row to open its waterfall." />

      {t && (
        <div className="stat-strip">
          <StatCard label="Traces" value={formatNumber(t.traces)} />
          <StatCard label="Spans" value={formatCompact(t.spans)} />
          <StatCard label="Tokens" value={formatCompact(t.input_tokens + t.output_tokens + t.cache_tokens)} sub={`${formatCompact(t.cache_tokens)} cached`} />
          <StatCard label="Cost" value={formatCost(t.cost_usd)} />
          <StatCard label="Errors" value={formatNumber(t.errors)} tone={t.errors ? 'red' : undefined} />
          <StatCard label="Findings" value={formatNumber(t.findings)} tone={t.findings ? 'amber' : undefined} />
        </div>
      )}

      <div className="toolbar">
        <input
          className="input search"
          type="search"
          placeholder="Search name or trace id…"
          value={text}
          onChange={(e) => setText(e.target.value)}
          aria-label="Search traces"
        />
        <select className="input" value={filters.source} onChange={(e) => setFilter({ source: e.target.value })} aria-label="Source">
          <option value="">All sources</option>
          {sources.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <select className="input" value={filters.detector} onChange={(e) => setFilter({ detector: e.target.value })} aria-label="Detector">
          <option value="">Any detector</option>
          {DETECTORS.map((d) => (
            <option key={d} value={d}>
              {DETECTOR_INFO[d].label}
            </option>
          ))}
        </select>
        <select className="input" value={filters.tool} onChange={(e) => setFilter({ tool: e.target.value })} aria-label="Tool">
          <option value="">Any tool</option>
          {tools.map((tool) => (
            <option key={tool} value={tool}>
              {tool}
            </option>
          ))}
        </select>
        <label className="check">
          <input type="checkbox" checked={filters.has_error} onChange={(e) => setFilter({ has_error: e.target.checked })} />
          Has errors
        </label>
        <label className="check">
          <input
            type="checkbox"
            checked={filters.has_finding}
            onChange={(e) => setFilter({ has_finding: e.target.checked })}
            data-testid="filter-has-finding"
          />
          Has findings
        </label>
        <div className="toolbar-spacer" />
        <select className="input" value={filters.sort} onChange={(e) => setFilter({ sort: e.target.value })} aria-label="Sort">
          {SORTS.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </div>

      {list.error && <ErrorBox error={list.error} />}
      {list.loading && !list.data && <Loading />}
      {list.data && traces.length === 0 && (
        <EmptyState title="No traces match" body="Try clearing a filter, or send spans to POST /api/v1/spans." />
      )}

      {traces.length > 0 && (
        <div className={`card table-wrap ${list.loading ? 'is-stale' : ''}`}>
          <table className="table table-hover" data-testid="traces-table">
            <thead>
              <tr>
                <th>Trace</th>
                <th>Source</th>
                <th>Started</th>
                <th className="num">Duration</th>
                <th className="num">Spans</th>
                <th className="num">Agents</th>
                <th className="num">Tool calls</th>
                <th className="num">Errors</th>
                <th className="num">Tokens</th>
                <th className="num">Cost</th>
                <th>Findings</th>
              </tr>
            </thead>
            <tbody>
              {traces.map((tr) => {
                const dets = detectorsByTrace.get(tr.trace_id)
                const tokens = tr.input_tokens + tr.output_tokens + tr.cache_read_tokens + tr.cache_write_tokens
                const open = () => navigate(`/traces/${encodeURIComponent(tr.trace_id)}`)
                return (
                  <tr
                    key={tr.trace_id}
                    onClick={open}
                    onKeyDown={(e) => e.key === 'Enter' && open()}
                    tabIndex={0}
                    data-testid="trace-row"
                  >
                    <td className="cell-name">
                      <div className="trace-name">{tr.name || '(unnamed)'}</div>
                      <div className="mono muted small">{tr.trace_id}</div>
                    </td>
                    <td>
                      <SourceBadge source={tr.source} />
                    </td>
                    <td className="nowrap">
                      <TimeAgo ns={tr.start_ns} />
                    </td>
                    <td className="num">{formatDuration(tr.end_ns - tr.start_ns)}</td>
                    <td className="num">{formatNumber(tr.span_count)}</td>
                    <td className="num">{tr.agent_count}</td>
                    <td className="num">{formatNumber(tr.tool_calls)}</td>
                    <td className={`num ${tr.error_count ? 'text-red' : 'muted'}`}>{tr.error_count}</td>
                    <td className="num" title={`${formatNumber(tokens)} tokens`}>
                      {formatCompact(tokens)}
                    </td>
                    <td className="num">{formatCost(tr.cost_usd)}</td>
                    <td>
                      <div className="badge-row" data-testid="finding-badges">
                        {dets
                          ? Array.from(dets.entries()).map(([d, n]) => <DetectorBadge key={d} detector={d} count={n} />)
                          : tr.finding_count > 0 && <span className="badge badge-amber">{tr.finding_count}</span>}
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {total > 0 && (
        <div className="pager">
          <span className="muted" data-testid="pager-label">
            {page * PAGE_SIZE + 1}–{Math.min(total, (page + 1) * PAGE_SIZE)} of {formatNumber(total)}
          </span>
          <button className="btn btn-sm" disabled={page === 0} onClick={() => setFilter({ page: String(page - 1) })}>
            ← Prev
          </button>
          <button
            className="btn btn-sm"
            disabled={(page + 1) * PAGE_SIZE >= total}
            onClick={() => setFilter({ page: String(page + 1) })}
          >
            Next →
          </button>
        </div>
      )}
    </div>
  )
}

/** trace_id → (detector → number of findings) */
function groupDetectors(findings: Finding[]): Map<string, Map<string, number>> {
  const out = new Map<string, Map<string, number>>()
  for (const f of findings) {
    const m = out.get(f.trace_id) ?? new Map<string, number>()
    m.set(f.detector, (m.get(f.detector) ?? 0) + 1)
    out.set(f.trace_id, m)
  }
  return out
}
