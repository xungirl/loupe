import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Finding } from '../api'
import { useApi } from '../useApi'
import { DETECTORS, DETECTOR_INFO, DetectorBadge, EmptyState, ErrorBox, Loading, PageHeader, SeverityBadge, TimeAgo } from '../components/ui'

/** Every finding from every trace, grouped by detector. */
export default function FindingsPage() {
  const [params, setParams] = useSearchParams()
  const detector = params.get('detector') ?? ''
  const criticalOnly = params.get('critical') === 'true'

  const findings = useApi(() => api.listFindings({ limit: 5000 }), [])
  // Findings only carry a trace id; load trace names so rows read nicely.
  const traces = useApi(() => api.listTraces({ has_finding: true, limit: 2000 }), [])
  const traceNames = useMemo(
    () => new Map((traces.data?.traces ?? []).map((t) => [t.trace_id, t.name])),
    [traces.data],
  )

  const all = findings.data?.findings ?? []
  const counts = useMemo(() => {
    const c = new Map<string, number>()
    for (const f of all) c.set(f.detector, (c.get(f.detector) ?? 0) + 1)
    return c
  }, [all])
  // Known detectors first, then any new ones the server might add later.
  const detectorNames = [...DETECTORS, ...Array.from(counts.keys()).filter((d) => !(DETECTORS as string[]).includes(d))]

  const shown = all.filter((f) => (!detector || f.detector === detector) && (!criticalOnly || f.severity === 'critical'))
  const groups = detectorNames
    .map((d) => ({ detector: d, items: shown.filter((f) => f.detector === d) }))
    .filter((g) => g.items.length > 0)

  function set(k: string, v: string) {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setParams(next, { replace: true })
  }

  return (
    <div className="page">
      <PageHeader
        title="Findings"
        subtitle="What the failure detectors flagged: loops, rework, error streaks, stuck agents and budget burn."
      />

      <div className="detector-cards">
        <button className={`det-card ${detector === '' ? 'is-active' : ''}`} onClick={() => set('detector', '')}>
          <div className="det-card-count">{all.length}</div>
          <div className="det-card-label">All findings</div>
        </button>
        {detectorNames.map((d) => (
          <button
            key={d}
            className={`det-card det-card-${d} ${detector === d ? 'is-active' : ''}`}
            onClick={() => set('detector', detector === d ? '' : d)}
            title={DETECTOR_INFO[d]?.help}
            data-testid={`det-card-${d}`}
          >
            <div className="det-card-count">{counts.get(d) ?? 0}</div>
            <div className="det-card-label">
              <DetectorBadge detector={d} />
            </div>
            <div className="det-card-help">{DETECTOR_INFO[d]?.help}</div>
          </button>
        ))}
      </div>

      <div className="toolbar">
        <label className="check">
          <input type="checkbox" checked={criticalOnly} onChange={(e) => set('critical', e.target.checked ? 'true' : '')} />
          Critical only
        </label>
        <div className="toolbar-spacer" />
        <span className="muted small">{shown.length} shown</span>
      </div>

      {findings.error && <ErrorBox error={findings.error} />}
      {findings.loading && !findings.data && <Loading />}
      {findings.data && shown.length === 0 && (
        <EmptyState title="No findings" body="Detectors run when a trace has been quiet for a few seconds." />
      )}

      {groups.map((g) => (
        <section key={g.detector} className="card finding-group" data-testid="finding-group">
          <div className="finding-group-head">
            <DetectorBadge detector={g.detector} />
            <span className="muted small">
              {g.items.length} finding{g.items.length === 1 ? '' : 's'} · {DETECTOR_INFO[g.detector]?.help}
            </span>
          </div>
          <table className="table">
            <thead>
              <tr>
                <th style={{ width: 90 }}>Severity</th>
                <th>Summary</th>
                <th>Trace</th>
                <th>Agent</th>
                <th className="num">Spans</th>
                <th className="num">Score</th>
                <th>Detected</th>
              </tr>
            </thead>
            <tbody>
              {g.items.map((f: Finding) => (
                <tr key={f.id}>
                  <td>
                    <SeverityBadge severity={f.severity} />
                  </td>
                  <td>{f.summary}</td>
                  <td>
                    {/* ?finding= makes the trace page highlight and scroll to these spans */}
                    <Link to={`/traces/${encodeURIComponent(f.trace_id)}?finding=${f.id}`} data-testid="finding-link">
                      {traceNames.get(f.trace_id) ?? f.trace_id}
                    </Link>
                    <div className="mono muted small">{f.trace_id}</div>
                  </td>
                  <td className="muted">{f.agent_id || '–'}</td>
                  <td className="num">{f.span_ids.length}</td>
                  <td className="num">{f.score}</td>
                  <td className="nowrap muted">
                    <TimeAgo ns={f.created_ns} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ))}
    </div>
  )
}
