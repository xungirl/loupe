import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { liveUrl, type Span } from '../api'
import { formatClock, formatCompact, formatCost, formatDuration } from '../format'
import { EmptyState, KindBadge, PageHeader } from '../components/ui'
import Sparkline from '../components/Sparkline'
import { isError, spanDetail, spanTitle, spanTokens } from '../trace/spanLabel'

const MAX_ROWS = 500 // keep the DOM small: older spans fall off the bottom
const RATE_WINDOW_S = 5 // spans/sec is averaged over this many seconds
const HISTORY_S = 60 // seconds of history in the sparkline

type Status = 'connecting' | 'open' | 'reconnecting' | 'closed'
interface Row {
  seq: number // unique key: span ids repeat across traces
  span: Span
}

/**
 * Live tail over Server-Sent Events.
 *
 * The browser's EventSource keeps one HTTP connection open to /api/v1/live;
 * the server pushes `event: span` messages as spans are committed. EventSource
 * reconnects by itself after network errors; we only show the state.
 */
export default function LivePage() {
  const navigate = useNavigate()
  const [rows, setRows] = useState<Row[]>([])
  const [status, setStatus] = useState<Status>('connecting')
  const [paused, setPaused] = useState(false)
  const [queued, setQueued] = useState(0)
  const [rate, setRate] = useState(0)
  const [history, setHistory] = useState<number[]>([])
  const [filterText, setFilterText] = useState('')
  const [traceFilter, setTraceFilter] = useState('') // the filter actually sent to the server

  // Refs hold values the SSE handler needs without re-subscribing:
  // the handler is created once per connection, but must see the latest `paused`.
  const pausedRef = useRef(false)
  const bufferRef = useRef<Span[]>([]) // spans that arrived while paused
  const arrivalsRef = useRef<number[]>([]) // arrival timestamps (ms) for spans/sec
  const seqRef = useRef(0)

  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  function addSpans(spans: Span[]) {
    const newRows = spans.map((span) => ({ seq: ++seqRef.current, span })).reverse() // newest on top
    setRows((prev) => [...newRows, ...prev].slice(0, MAX_ROWS))
  }

  // One EventSource per trace filter. The cleanup closes it when the filter
  // changes or the page unmounts — otherwise the connection would leak.
  useEffect(() => {
    setStatus('connecting')
    const es = new EventSource(liveUrl(traceFilter || undefined))
    es.addEventListener('hello', () => setStatus('open'))
    es.addEventListener('span', (ev) => {
      let span: Span
      try {
        span = JSON.parse((ev as MessageEvent<string>).data) as Span
      } catch {
        return // ignore a malformed message rather than crash the page
      }
      arrivalsRef.current.push(Date.now())
      if (pausedRef.current) {
        bufferRef.current.push(span)
        setQueued((n) => n + 1)
      } else {
        addSpans([span])
      }
    })
    es.onopen = () => setStatus('open')
    es.onerror = () => setStatus(es.readyState === EventSource.CLOSED ? 'closed' : 'reconnecting')
    return () => es.close()
  }, [traceFilter])

  // Once a second: compute spans/sec over the last few seconds and push the
  // count of the last second onto the sparkline history.
  useEffect(() => {
    const timer = setInterval(() => {
      const now = Date.now()
      const arrivals = arrivalsRef.current.filter((t) => now - t < HISTORY_S * 1000)
      arrivalsRef.current = arrivals
      setRate(arrivals.filter((t) => now - t < RATE_WINDOW_S * 1000).length / RATE_WINDOW_S)
      const lastSecond = arrivals.filter((t) => now - t < 1000).length
      setHistory((h) => [...h, lastSecond].slice(-HISTORY_S))
    }, 1000)
    return () => clearInterval(timer)
  }, [])

  function togglePause() {
    if (paused) {
      // Resume: show everything that arrived while paused.
      addSpans(bufferRef.current)
      bufferRef.current = []
      setQueued(0)
    }
    setPaused(!paused)
  }

  // Recently active traces, newest first, derived from the rows we have.
  const traces = new Map<string, { count: number; last: number; errors: number }>()
  for (const r of rows) {
    const t = traces.get(r.span.trace_id) ?? { count: 0, last: 0, errors: 0 }
    t.count++
    t.last = Math.max(t.last, r.span.end_ns)
    if (isError(r.span)) t.errors++
    traces.set(r.span.trace_id, t)
  }
  const activeTraces = Array.from(traces.entries()).sort((a, b) => b[1].last - a[1].last).slice(0, 8)

  return (
    <div className="page">
      <PageHeader
        title="Live"
        subtitle="Spans streaming in over Server-Sent Events as agents run."
        actions={
          <>
            <StatusPill status={status} />
            <span className="rate" data-testid="rate">
              <strong>{rate.toFixed(1)}</strong> spans/s
            </span>
            <button className={`btn ${paused ? 'btn-primary' : ''}`} onClick={togglePause} data-testid="pause">
              {paused ? `▶ Resume${queued ? ` (${queued} new)` : ''}` : '❚❚ Pause'}
            </button>
            <button className="btn btn-ghost" onClick={() => setRows([])}>
              Clear
            </button>
          </>
        }
      />

      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault()
          setTraceFilter(filterText.trim())
        }}
      >
        <input
          className="input search"
          placeholder="Filter by trace id (exact)…"
          value={filterText}
          onChange={(e) => setFilterText(e.target.value)}
          aria-label="Trace id filter"
        />
        <button className="btn" type="submit">
          Apply
        </button>
        {traceFilter && (
          <button
            className="chip chip-active"
            type="button"
            onClick={() => {
              setFilterText('')
              setTraceFilter('')
            }}
          >
            trace: {traceFilter} ✕
          </button>
        )}
        <div className="toolbar-spacer" />
        <span className="muted small">
          showing {rows.length} / {MAX_ROWS} max
        </span>
      </form>

      <div className="live-layout">
        <div className="card live-stream">
          {rows.length === 0 ? (
            <EmptyState
              title={status === 'open' ? 'Waiting for spans…' : 'Connecting…'}
              body={
                <>
                  Run an agent with the Loupe SDK or hook, or start the server with <code>-demo-live</code> to stream
                  simulated runs.
                </>
              }
            />
          ) : (
            <ul className="live-list" data-testid="live-list">
              {rows.map(({ seq, span: s }) => (
                <li
                  key={seq}
                  className={`live-row ${isError(s) ? 'is-error' : ''}`}
                  onClick={() => navigate(`/traces/${encodeURIComponent(s.trace_id)}`)}
                  data-testid="live-row"
                >
                  <span className="mono muted live-time">{formatClock(s.start_ns)}</span>
                  <KindBadge kind={s.kind} />
                  <span className="live-main">
                    <span className="live-title">{spanTitle(s)}</span>
                    {spanDetail(s) && <span className="mono muted live-detail">{spanDetail(s)}</span>}
                    {isError(s) && s.error_msg && <span className="text-red small live-detail">{s.error_msg}</span>}
                  </span>
                  <span className="muted small live-agent">{s.agent_name ?? s.agent_id ?? ''}</span>
                  <span className="mono small live-trace">{s.trace_id}</span>
                  <span className="num small">{formatDuration(s.end_ns - s.start_ns)}</span>
                  <span className="num small muted">{spanTokens(s) ? `${formatCompact(spanTokens(s))} tok` : ''}</span>
                  <span className="num small">{s.cost_usd ? formatCost(s.cost_usd) : ''}</span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <aside className="live-side">
          <div className="card card-pad">
            <div className="panel-label">Spans per second · last {HISTORY_S}s</div>
            <Sparkline values={history} slots={HISTORY_S} width={260} height={64} />
          </div>
          <div className="card card-pad">
            <div className="panel-label">Active traces</div>
            {activeTraces.length === 0 && <div className="muted small">None yet.</div>}
            <ul className="active-traces">
              {activeTraces.map(([id, t]) => (
                <li key={id}>
                  <Link to={`/traces/${encodeURIComponent(id)}`} className="mono small">
                    {id}
                  </Link>
                  <span className="muted small">
                    {t.count} spans{t.errors ? ` · ${t.errors} err` : ''}
                  </span>
                  <button
                    className="btn btn-ghost btn-xs"
                    title="Only show this trace"
                    onClick={() => {
                      setFilterText(id)
                      setTraceFilter(id)
                    }}
                  >
                    filter
                  </button>
                </li>
              ))}
            </ul>
          </div>
        </aside>
      </div>
    </div>
  )
}

function StatusPill({ status }: { status: Status }) {
  const tone = status === 'open' ? 'green' : status === 'closed' ? 'red' : 'amber'
  const label = { connecting: 'connecting', open: 'connected', reconnecting: 'reconnecting', closed: 'disconnected' }[status]
  return (
    <span className={`status-pill status-${tone}`} data-testid="live-status">
      <span className={`dot dot-${tone} ${status === 'open' ? 'dot-pulse' : ''}`} />
      {label}
    </span>
  )
}
