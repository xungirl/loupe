import { useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, ApiError, type Finding } from '../api'
import { useApi } from '../useApi'
import { formatCompact, formatCost, formatDuration, formatNumber } from '../format'
import { DetectorBadge, EmptyState, ErrorBox, Loading, SourceBadge, StatCard, TimeAgo } from '../components/ui'
import Waterfall from '../trace/Waterfall'
import SpanPanel from '../trace/SpanPanel'
import AgentTree from '../trace/AgentTree'
import FindingsList from '../trace/FindingsList'
import { traceWindow } from '../trace/waterfallLayout'

type Tab = 'waterfall' | 'agents' | 'findings'

/**
 * One trace. URL state:
 *   ?tab=waterfall|agents|findings
 *   ?finding=<id>   highlight that finding's spans
 *   ?agent=<key>    show only that agent's spans
 */
export default function TracePage() {
  const { id = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as Tab) || 'waterfall'
  const findingId = params.get('finding') ? Number(params.get('finding')) : undefined
  const agentFilter = params.get('agent') ?? ''

  const trace = useApi(() => api.getTrace(id), [id])
  const [selectedSpan, setSelectedSpan] = useState<string>()
  // After "Re-run detectors" we show the fresh findings instead of the loaded ones.
  const [rerun, setRerun] = useState<{ findings: Finding[]; busy: boolean; error?: string }>()

  function setParam(changes: Record<string, string | undefined>) {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries(changes)) {
      if (v) next.set(k, v)
      else next.delete(k)
    }
    setParams(next, { replace: true })
  }

  const data = trace.data
  const nodes = data?.spans ?? []
  const agents = data?.agents ?? []
  const findings = rerun?.findings ?? data?.findings ?? []

  const win = useMemo(
    () => traceWindow(nodes, data?.summary.start_ns ?? 0, data?.summary.end_ns ?? 0),
    [nodes, data],
  )
  const shownNodes = useMemo(
    () => (agentFilter ? nodes.filter((n) => n.agent_key === agentFilter) : nodes),
    [nodes, agentFilter],
  )
  const activeFinding = findings.find((f) => f.id === findingId)
  const highlighted = useMemo(() => new Set(activeFinding?.span_ids ?? []), [activeFinding])
  const flagged = useMemo(() => new Set(findings.flatMap((f) => f.span_ids)), [findings])
  const selectedNode = nodes.find((n) => n.span.span_id === selectedSpan)

  async function runDetectors() {
    setRerun((r) => ({ findings: r?.findings ?? findings, busy: true }))
    try {
      const res = await api.detect(id)
      setRerun({ findings: res.findings ?? [], busy: false })
    } catch (e) {
      setRerun({ findings, busy: false, error: e instanceof Error ? e.message : String(e) })
    }
  }

  function pickFinding(f: Finding) {
    // Clicking the active finding again clears the highlight.
    const same = f.id === findingId
    setParam({ finding: same ? undefined : String(f.id), tab: 'waterfall', agent: undefined })
    if (!same && f.span_ids[0]) setSelectedSpan(f.span_ids[0])
  }

  if (trace.error) {
    if (trace.error instanceof ApiError && trace.error.status === 404) {
      return (
        <div className="page">
          <EmptyState title="Trace not found" body={<>No trace with id <code>{id}</code>. It may have been deleted by retention.</>}>
            <Link to="/" className="btn">
              Back to sessions
            </Link>
          </EmptyState>
        </div>
      )
    }
    return (
      <div className="page">
        <ErrorBox error={trace.error} />
      </div>
    )
  }
  if (!data) return <div className="page"><Loading label="Loading trace…" /></div>

  const s = data.summary
  const agentName = agents.find((a) => a.key === agentFilter)?.name

  return (
    <div className="page page-trace">
      <div className="crumbs">
        <Link to="/">← Sessions</Link>
      </div>
      <header className="trace-header">
        <div className="trace-title-row">
          <h1>{s.name || '(unnamed trace)'}</h1>
          <SourceBadge source={s.source} />
          <button className="btn btn-sm btn-ghost" onClick={trace.reload} title="Fetch the trace again">
            ↻ Refresh
          </button>
        </div>
        <div className="muted small">
          <code>{s.trace_id}</code> · started <TimeAgo ns={s.start_ns} />
        </div>
      </header>

      <div className="stat-strip stat-strip-tight">
        <StatCard label="Duration" value={formatDuration(s.end_ns - s.start_ns)} />
        <StatCard label="Spans" value={formatNumber(s.span_count)} sub={data.orphans ? `${data.orphans} orphaned` : `depth ${data.max_depth}`} />
        <StatCard label="Agents" value={s.agent_count} />
        <StatCard label="LLM calls" value={formatNumber(s.llm_calls)} />
        <StatCard label="Tool calls" value={formatNumber(s.tool_calls)} />
        <StatCard label="Errors" value={s.error_count} tone={s.error_count ? 'red' : undefined} />
        <StatCard
          label="Tokens"
          value={formatCompact(s.input_tokens + s.output_tokens + s.cache_read_tokens + s.cache_write_tokens)}
          sub={`${formatCompact(s.input_tokens)} in · ${formatCompact(s.output_tokens)} out · ${formatCompact(s.cache_read_tokens + s.cache_write_tokens)} cache`}
        />
        <StatCard label="Cost" value={formatCost(s.cost_usd)} />
        <StatCard label="Findings" value={findings.length} tone={findings.length ? 'amber' : undefined} />
      </div>

      {data.orphans > 0 && (
        <div className="notice notice-amber">
          {data.orphans} span{data.orphans === 1 ? '' : 's'} arrived before {data.orphans === 1 ? 'its' : 'their'} parent — marked
          “parent not received”.
        </div>
      )}

      <div className="tabs" role="tablist">
        {(
          [
            ['waterfall', 'Waterfall'],
            ['agents', `Agent tree (${agents.length})`],
            ['findings', `Findings (${findings.length})`],
          ] as [Tab, string][]
        ).map(([key, label]) => (
          <button
            key={key}
            role="tab"
            aria-selected={tab === key}
            className={`tab ${tab === key ? 'is-active' : ''}`}
            onClick={() => setParam({ tab: key === 'waterfall' ? undefined : key })}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'waterfall' && (
        <>
          {(findings.length > 0 || agentFilter) && (
            <div className="chip-row">
              {agentFilter && (
                <button className="chip chip-active" onClick={() => setParam({ agent: undefined })} title="Show all agents">
                  agent: {agentName ?? agentFilter} ✕
                </button>
              )}
              {findings.map((f) => (
                <button
                  key={f.id}
                  className={`chip ${f.id === findingId ? 'chip-active' : ''}`}
                  onClick={() => pickFinding(f)}
                  title={f.summary}
                  data-testid="finding-chip"
                >
                  <DetectorBadge detector={f.detector} />
                  <span className="chip-text">{f.summary}</span>
                </button>
              ))}
            </div>
          )}
          <div className={`split ${selectedNode ? 'has-panel' : ''}`}>
            <div className="card split-main">
              <Waterfall
                nodes={shownNodes}
                win={win}
                selectedId={selectedSpan}
                onSelect={setSelectedSpan}
                highlighted={highlighted}
                flagged={flagged}
              />
            </div>
            {selectedNode && (
              <SpanPanel
                node={selectedNode}
                findings={findings.filter((f) => f.span_ids.includes(selectedNode.span.span_id))}
                onClose={() => setSelectedSpan(undefined)}
              />
            )}
          </div>
        </>
      )}

      {tab === 'agents' && (
        <div className="card card-pad">
          <AgentTree
            agents={agents}
            selectedKey={agentFilter}
            onSelect={(key) => setParam({ agent: key, tab: undefined, finding: undefined })}
          />
        </div>
      )}

      {tab === 'findings' && (
        <div className="card card-pad">
          <div className="section-head">
            <p className="muted small">
              Detectors run automatically once a trace has been quiet for a few seconds. Re-run them after changing
              thresholds or when spans arrived late.
            </p>
            <button className="btn btn-primary" onClick={runDetectors} disabled={rerun?.busy}>
              {rerun?.busy ? 'Running…' : 'Re-run detectors'}
            </button>
          </div>
          {rerun?.error && <div className="notice notice-red">Re-run failed: {rerun.error}</div>}
          {rerun && !rerun.busy && !rerun.error && (
            <div className="notice notice-green">Detectors re-ran: {rerun.findings.length} finding(s).</div>
          )}
          <FindingsList findings={findings} activeId={findingId} onPick={pickFinding} />
        </div>
      )}
    </div>
  )
}
