import type { ReactNode } from 'react'
import type { Finding, SpanNode } from '../api'
import { formatAbsolute, formatCost, formatDuration, formatNumber } from '../format'
import { DetectorBadge, KindBadge } from '../components/ui'
import { isError, spanTitle } from './spanLabel'

/** If `text` is JSON, re-indent it; otherwise return it unchanged. */
export function prettyJson(text: string | undefined): { text: string; isJson: boolean } {
  if (!text) return { text: '', isJson: false }
  const t = text.trim()
  if (!(t.startsWith('{') || t.startsWith('['))) return { text, isJson: false }
  try {
    return { text: JSON.stringify(JSON.parse(t), null, 2), isJson: true }
  } catch {
    return { text, isJson: false }
  }
}

interface Props {
  node: SpanNode
  findings: Finding[] // findings that reference this span
  onClose: () => void
}

/** Side panel listing every field of the selected span. */
export default function SpanPanel({ node, findings, onClose }: Props) {
  const s = node.span
  const rows: [string, ReactNode][] = [
    ['Span ID', <code>{s.span_id}</code>],
    ['Parent', s.parent_span_id ? <code>{s.parent_span_id}</code> : <span className="muted">root</span>],
    ['Agent', s.agent_name ? `${s.agent_name}${s.agent_id ? ` (${s.agent_id})` : ''}` : node.agent_key],
    ['Start', formatAbsolute(s.start_ns)],
    ['Duration', formatDuration(s.end_ns - s.start_ns)],
    ['Status', isError(s) ? <span className="text-red">error</span> : 'ok'],
  ]
  if (s.source) rows.push(['Source', s.source])
  if (s.model) rows.push(['Model', <code>{s.model}</code>])
  if (s.tool_name) rows.push(['Tool', <code>{s.tool_name}</code>])
  if (s.target) rows.push(['Target', <code>{s.target}</code>])
  if (s.input_tokens !== undefined) rows.push(['Input tokens', formatNumber(s.input_tokens)])
  if (s.output_tokens !== undefined) rows.push(['Output tokens', formatNumber(s.output_tokens)])
  if (s.cache_read_tokens) rows.push(['Cache read', formatNumber(s.cache_read_tokens)])
  if (s.cache_write_tokens) rows.push(['Cache write', formatNumber(s.cache_write_tokens)])
  if (s.cost_usd !== undefined) rows.push(['Cost', formatCost(s.cost_usd)])

  return (
    <aside className="span-panel" data-testid="span-panel">
      <div className="span-panel-head">
        <div>
          <KindBadge kind={s.kind} />
          <h3>{spanTitle(s)}</h3>
        </div>
        <button className="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close panel">
          ✕
        </button>
      </div>

      {node.orphan && (
        <div className="notice notice-amber">Parent span not received yet — shown at the top level until it arrives.</div>
      )}
      {s.error_msg && (
        <div className="notice notice-red">
          <strong>Error:</strong> {s.error_msg}
        </div>
      )}
      {findings.length > 0 && (
        <div className="panel-section">
          <div className="panel-label">Flagged by</div>
          {findings.map((f) => (
            <div key={f.id} className="panel-finding">
              <DetectorBadge detector={f.detector} /> {f.summary}
            </div>
          ))}
        </div>
      )}

      <dl className="kv">
        {rows.map(([k, v]) => (
          <div key={k} className="kv-row">
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>

      <Block label="Tool input" text={s.tool_input} />
      <Block label="Tool output" text={s.tool_output} />
      <Block label="Attributes" text={s.attributes} />
    </aside>
  )
}

function Block({ label, text }: { label: string; text?: string }) {
  if (!text) return null
  const p = prettyJson(text)
  return (
    <div className="panel-section">
      <div className="panel-label">
        {label} {p.isJson && <span className="muted small">JSON</span>}
      </div>
      <pre className="code-block">{p.text}</pre>
    </div>
  )
}
