import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { useApi } from '../useApi'
import { formatCompact, formatCost, formatDuration, formatNumber, formatPercent } from '../format'
import BarChart from '../components/BarChart'
import { DetectorBadge, ErrorBox, InlineBar, Loading, PageHeader, StatCard } from '../components/ui'

const RANGES = [
  { value: '', label: 'All time' },
  { value: '24h', label: 'Last 24 h' },
  { value: '168h', label: 'Last 7 days' },
  { value: '720h', label: 'Last 30 days' },
]
type Metric = 'cost' | 'tokens' | 'traces'

/** Aggregates over all traces: where tokens, money and errors go. */
export default function InsightsPage() {
  const [params, setParams] = useSearchParams()
  const since = params.get('since') ?? ''
  const [metric, setMetric] = useState<Metric>('cost')
  const stats = useApi(() => api.stats({ since }), [since])
  const prices = useApi(() => api.prices(), [])

  const st = stats.data
  const header = (
    <PageHeader
      title="Insights"
      subtitle="Where the tokens, dollars and failures go — across every recorded run."
      actions={
        <select
          className="input"
          value={since}
          aria-label="Time range"
          onChange={(e) => setParams(e.target.value ? { since: e.target.value } : {}, { replace: true })}
        >
          {RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      }
    />
  )
  if (stats.error) return <div className="page">{header}<ErrorBox error={stats.error} /></div>
  if (!st) return <div className="page">{header}<Loading /></div>

  const t = st.totals
  const models = st.by_model ?? []
  const tools = st.by_tool ?? []
  const agents = st.by_agent ?? []
  const detectors = st.by_detector ?? []
  const hotspots = st.hotspots ?? []
  // The API returns days newest-first; charts read left (old) to right (new).
  const days = [...(st.by_day ?? [])].sort((a, b) => a.day.localeCompare(b.day))
  const totalModelCost = models.reduce((s, m) => s + m.cost_usd, 0)
  const unpriced = models.filter((m) => m.unpriced)
  const maxDetector = Math.max(1, ...detectors.map((d) => d.findings))
  const maxAgentCost = Math.max(1e-9, ...agents.map((a) => a.cost_usd))

  const metricFormat: Record<Metric, (v: number) => string> = {
    // Axis labels read better as "$25" than "$25.00"; small values keep cents.
    cost: (v) => (v === 0 ? '$0' : v >= 10 ? `$${Math.round(v)}` : formatCost(v)),
    tokens: formatCompact,
    traces: (v) => formatNumber(v),
  }
  const bars = days.map((d) => ({
    label: shortDay(d.day),
    value: metric === 'cost' ? d.cost_usd : metric === 'tokens' ? d.tokens : d.traces,
    title: `${d.day}: ${formatCost(d.cost_usd)} · ${formatCompact(d.tokens)} tokens · ${d.traces} traces`,
  }))

  return (
    <div className="page">
      {header}

      <div className="stat-strip">
        <StatCard label="Traces" value={formatNumber(t.traces)} sub={`${formatCompact(t.spans)} spans`} />
        <StatCard label="LLM calls" value={formatCompact(t.llm_calls)} />
        <StatCard label="Tool calls" value={formatCompact(t.tool_calls)} />
        <StatCard
          label="Errors"
          value={formatNumber(t.errors)}
          sub={t.spans ? `${formatPercent(t.errors / t.spans)} of spans` : undefined}
          tone={t.errors ? 'red' : undefined}
        />
        <StatCard
          label="Tokens"
          value={formatCompact(t.input_tokens + t.output_tokens + t.cache_tokens)}
          sub={`${formatCompact(t.input_tokens)} in · ${formatCompact(t.output_tokens)} out · ${formatCompact(t.cache_tokens)} cache`}
        />
        <StatCard label="Cost" value={formatCost(t.cost_usd)} sub={t.traces ? `${formatCost(t.cost_usd / t.traces)} / trace` : undefined} />
        <StatCard label="Findings" value={formatNumber(t.findings)} tone={t.findings ? 'amber' : undefined} />
      </div>

      <section className="card card-pad">
        <div className="section-head">
          <h2>Per day</h2>
          <div className="seg">
            {(['cost', 'tokens', 'traces'] as Metric[]).map((m) => (
              <button key={m} className={`seg-btn ${metric === m ? 'is-active' : ''}`} onClick={() => setMetric(m)}>
                {m}
              </button>
            ))}
          </div>
        </div>
        {bars.length ? (
          <BarChart bars={bars} format={metricFormat[metric]} ariaLabel={`${metric} per day`} />
        ) : (
          <div className="muted">No data in this range.</div>
        )}
      </section>

      <div className="grid-2">
        <section className="card card-pad">
          <h2>Tokens &amp; cost by model</h2>
          <table className="table table-compact">
            <thead>
              <tr>
                <th>Model</th>
                <th className="num">Calls</th>
                <th className="num">In</th>
                <th className="num">Out</th>
                <th className="num">Cache</th>
                <th className="num">Cost</th>
                <th style={{ width: 90 }}>Share</th>
              </tr>
            </thead>
            <tbody>
              {models.map((m) => (
                <tr key={m.model || '(none)'}>
                  <td>
                    <code>{m.model || '(no model)'}</code>{' '}
                    {m.unpriced && (
                      <span className="badge badge-amber badge-xs" title="No price in the table: cost counted as $0">
                        unpriced
                      </span>
                    )}
                  </td>
                  <td className="num">{formatNumber(m.calls)}</td>
                  <td className="num">{formatCompact(m.input_tokens)}</td>
                  <td className="num">{formatCompact(m.output_tokens)}</td>
                  <td className="num">{formatCompact(m.cache_tokens)}</td>
                  <td className="num">{formatCost(m.cost_usd)}</td>
                  <td>
                    <InlineBar ratio={totalModelCost ? m.cost_usd / totalModelCost : 0} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {unpriced.length > 0 && (
            <p className="notice notice-amber small">
              {unpriced.length} model(s) have no price in the table, so their cost shows as $0:{' '}
              {unpriced.map((m) => m.model).join(', ')}. Add them to config/prices.json.
            </p>
          )}
          {prices.data && <p className="muted small footnote">Prices: {prices.data.source}</p>}
        </section>

        <section className="card card-pad">
          <h2>Findings by detector</h2>
          {detectors.length === 0 && <div className="muted">No findings.</div>}
          <div className="hbars" data-testid="detector-bars">
            {detectors.map((d) => (
              <Link key={d.detector} to={`/findings?detector=${d.detector}`} className="hbar-row">
                <span className="hbar-label">
                  <DetectorBadge detector={d.detector} />
                </span>
                <span className="hbar-track">
                  <span className={`hbar-fill det-fill-${d.detector}`} style={{ width: `${(d.findings / maxDetector) * 100}%` }} />
                </span>
                <span className="hbar-value">
                  {d.findings} <span className="muted small">in {d.traces} traces</span>
                </span>
              </Link>
            ))}
          </div>
        </section>
      </div>

      <section className="card card-pad">
        <h2>Tools</h2>
        <table className="table table-compact" data-testid="tools-table">
          <thead>
            <tr>
              <th>Tool</th>
              <th className="num">Calls</th>
              <th className="num">Errors</th>
              <th style={{ width: '30%' }}>Error rate</th>
              <th className="num">Avg duration</th>
            </tr>
          </thead>
          <tbody>
            {tools.map((tool) => (
              <tr key={tool.tool}>
                <td>
                  <Link to={`/?tool=${encodeURIComponent(tool.tool)}`}>
                    <code>{tool.tool}</code>
                  </Link>
                </td>
                <td className="num">{formatNumber(tool.calls)}</td>
                <td className={`num ${tool.errors ? 'text-red' : 'muted'}`}>{formatNumber(tool.errors)}</td>
                <td>
                  <div className="bar-cell">
                    <InlineBar ratio={tool.error_rate} tone={tool.error_rate > 0.2 ? 'red' : tool.error_rate > 0.05 ? 'amber' : 'green'} />
                    <span className="small">{formatPercent(tool.error_rate)}</span>
                  </div>
                </td>
                <td className="num">{formatDuration(tool.avg_ms * 1e6)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <div className="grid-2">
        <section className="card card-pad">
          <h2>By agent</h2>
          <table className="table table-compact">
            <thead>
              <tr>
                <th>Agent</th>
                <th className="num">Runs</th>
                <th className="num">LLM</th>
                <th className="num">Tools</th>
                <th className="num">Errors</th>
                <th className="num">Tokens</th>
                <th className="num">Cost</th>
                <th style={{ width: 70 }} />
              </tr>
            </thead>
            <tbody>
              {agents.map((a) => (
                <tr key={a.agent}>
                  <td>{a.agent}</td>
                  <td className="num">{formatNumber(a.runs)}</td>
                  <td className="num">{formatNumber(a.llm_calls)}</td>
                  <td className="num">{formatNumber(a.tool_calls)}</td>
                  <td className={`num ${a.errors ? 'text-red' : 'muted'}`}>{a.errors}</td>
                  <td className="num">{formatCompact(a.tokens)}</td>
                  <td className="num">{formatCost(a.cost_usd)}</td>
                  <td>
                    <InlineBar ratio={a.cost_usd / maxAgentCost} tone="violet" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>

        <section className="card card-pad">
          <h2>Rework hotspots</h2>
          <p className="muted small">Files edited most often, across traces.</p>
          <table className="table table-compact" data-testid="hotspots-table">
            <thead>
              <tr>
                <th>File</th>
                <th>Tool</th>
                <th className="num">Edits</th>
                <th className="num">Traces</th>
                <th className="num">Errors</th>
              </tr>
            </thead>
            <tbody>
              {hotspots.slice(0, 12).map((h) => (
                <tr key={`${h.target}-${h.tool}`}>
                  <td>
                    <code>{h.target}</code>
                  </td>
                  <td className="muted">{h.tool}</td>
                  <td className="num">{h.calls}</td>
                  <td className="num">{h.traces}</td>
                  <td className={`num ${h.errors ? 'text-red' : 'muted'}`}>{h.errors}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {hotspots.length === 0 && <div className="muted">No edits recorded.</div>}
        </section>
      </div>
    </div>
  )
}

/** "2026-11-08" → "Nov 8" (parsed as a plain date, no timezone shift). */
function shortDay(day: string): string {
  const [y, m, d] = day.split('-').map(Number)
  if (!y || !m || !d) return day
  return new Date(y, m - 1, d).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}
