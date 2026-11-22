import { useEffect, useMemo, useRef, useState } from 'react'
import type { SpanNode } from '../api'
import { formatCompact, formatCost, formatDuration } from '../format'
import { barPosition, hasChildren, timeTicks, visibleRows, type TimeWindow } from './waterfallLayout'
import { isError, spanDetail, spanTitle, spanTokens } from './spanLabel'

interface Props {
  nodes: SpanNode[]
  win: TimeWindow
  selectedId?: string
  onSelect: (spanId: string) => void
  /** Spans of the finding the user clicked: strongly highlighted + scrolled to. */
  highlighted: ReadonlySet<string>
  /** Spans referenced by any finding: get a small marker. */
  flagged: ReadonlySet<string>
}

const INDENT_PX = 14

export default function Waterfall({ nodes, win, selectedId, onSelect, highlighted, flagged }: Props) {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const listRef = useRef<HTMLDivElement>(null)

  const rows = useMemo(() => visibleRows(nodes, collapsed), [nodes, collapsed])
  const ticks = useMemo(() => timeTicks(win.endNs - win.startNs), [win])
  // Rows are indented relative to the shallowest row shown (matters when the
  // list is filtered to one agent, whose spans may all start at depth 3).
  const minDepth = useMemo(() => nodes.reduce((m, n) => Math.min(m, n.depth), nodes[0]?.depth ?? 0), [nodes])

  function toggle(spanId: string) {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(spanId)) next.delete(spanId)
      else next.add(spanId)
      return next
    })
  }
  const collapseAll = () =>
    setCollapsed(new Set(nodes.filter((_, i) => hasChildren(nodes, i) && nodes[i].depth > minDepth).map((n) => n.span.span_id)))

  // When a finding is picked, expand everything (its spans might be hidden
  // inside a collapsed subtree) and scroll the first highlighted row into view.
  useEffect(() => {
    if (highlighted.size === 0) return
    setCollapsed(new Set())
    const first = nodes.find((n) => highlighted.has(n.span.span_id))
    if (!first) return
    // Wait one frame so the expanded rows exist in the DOM, then scroll only
    // the waterfall's own scroll box (scrollIntoView would scroll the whole
    // page too). offsetTop is relative to .wf-rows, the top of the content.
    const raf = requestAnimationFrame(() => {
      const box = listRef.current
      const el = box?.querySelector<HTMLElement>(`[data-span-id="${CSS.escape(first.span.span_id)}"]`)
      if (box && el) box.scrollTo?.({ top: el.offsetTop - box.clientHeight / 3, behavior: 'smooth' })
    })
    return () => cancelAnimationFrame(raf)
  }, [highlighted, nodes])

  return (
    <div className="waterfall" data-testid="waterfall">
      <div className="wf-toolbar">
        <span className="muted small">
          {rows.length} of {nodes.length} spans shown
        </span>
        <button className="btn btn-sm btn-ghost" onClick={() => setCollapsed(new Set())}>
          Expand all
        </button>
        <button className="btn btn-sm btn-ghost" onClick={collapseAll}>
          Collapse all
        </button>
        <Legend />
      </div>

      <div className="wf-grid wf-header">
        <div className="wf-label">Span</div>
        <div className="wf-axis">
          {ticks.map((t) => (
            <span key={t.pct} className="wf-tick" style={{ left: `${t.pct}%` }}>
              {t.label}
            </span>
          ))}
        </div>
      </div>

      <div className="wf-body" ref={listRef}>
        <div className="wf-rows">
        {/* Vertical gridlines drawn once behind all rows, aligned with the ticks. */}
        <div className="wf-grid wf-gridlines" aria-hidden="true">
          <div />
          <div className="wf-track">
            {ticks.map((t) => (
              <span key={t.pct} className="wf-gridline" style={{ left: `${t.pct}%` }} />
            ))}
          </div>
        </div>

        {rows.map((i) => {
          const n = nodes[i]
          const s = n.span
          const { left, width } = barPosition(s.start_ns, s.end_ns, win)
          const parent = hasChildren(nodes, i)
          const isCollapsed = collapsed.has(s.span_id)
          const err = isError(s)
          const tokens = spanTokens(s)
          const classes = [
            'wf-grid',
            'wf-row',
            selectedId === s.span_id && 'is-selected',
            highlighted.has(s.span_id) && 'is-highlighted',
            err && 'is-error',
          ]
            .filter(Boolean)
            .join(' ')
          // Where the duration label goes: right of the bar if there is room,
          // otherwise left of it, otherwise (a bar spanning the row) inside it.
          const end = left + width
          const labelPos = end <= 75 ? 'right' : left >= 25 ? 'left' : 'inside'
          const labelStyle =
            labelPos === 'right' ? { left: `${end}%` } : labelPos === 'left' ? { right: `${100 - left}%` } : { right: `${100 - end}%` }
          return (
            <div
              key={s.span_id}
              className={classes}
              data-span-id={s.span_id}
              data-testid="wf-row"
              onClick={() => onSelect(s.span_id)}
            >
              <div className="wf-label" style={{ paddingLeft: 8 + (n.depth - minDepth) * INDENT_PX }}>
                {parent ? (
                  <button
                    className="wf-caret"
                    aria-label={isCollapsed ? 'Expand' : 'Collapse'}
                    onClick={(e) => {
                      e.stopPropagation() // don't also select the row
                      toggle(s.span_id)
                    }}
                  >
                    {isCollapsed ? '▸' : '▾'}
                  </button>
                ) : (
                  <span className="wf-caret-spacer" />
                )}
                <span className={`kind-dot kind-${s.kind}`} title={s.kind} />
                <span className="wf-name">{spanTitle(s)}</span>
                {spanDetail(s) && <span className="wf-detail mono">{spanDetail(s)}</span>}
                {n.orphan && (
                  <span className="badge badge-amber badge-xs" title="This span's parent has not been received (yet)">
                    parent not received
                  </span>
                )}
                {flagged.has(s.span_id) && <span className="wf-flag" title="Referenced by a finding">⚑</span>}
              </div>
              <div className="wf-track">
                <span
                  className={`wf-bar kind-bg-${s.kind} ${err ? 'wf-bar-error' : ''}`}
                  style={{ left: `${left}%`, width: `${width}%` }}
                />
                <span className={`wf-dur wf-dur-${labelPos}`} style={labelStyle}>
                  {formatDuration(s.end_ns - s.start_ns)}
                  {tokens > 0 && ` · ${formatCompact(tokens)} tok`}
                  {s.cost_usd ? ` · ${formatCost(s.cost_usd)}` : ''}
                </span>
              </div>
            </div>
          )
        })}
        </div>
      </div>
    </div>
  )
}

function Legend() {
  return (
    <div className="legend">
      {['agent', 'llm', 'tool', 'step', 'event'].map((k) => (
        <span key={k} className="legend-item">
          <span className={`kind-dot kind-${k}`} />
          {k}
        </span>
      ))}
      <span className="legend-item">
        <span className="kind-dot kind-error" />
        error
      </span>
    </div>
  )
}
