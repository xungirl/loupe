import type { Finding } from '../api'
import { DetectorBadge, SeverityBadge, TimeAgo } from '../components/ui'

interface Props {
  findings: Finding[]
  activeId?: number
  onPick: (f: Finding) => void
}

/** The findings of one trace. Clicking one highlights its spans in the waterfall. */
export default function FindingsList({ findings, activeId, onPick }: Props) {
  if (findings.length === 0) {
    return <div className="muted findings-empty">No findings — the detectors found nothing suspicious in this trace.</div>
  }
  return (
    <ul className="finding-list" data-testid="trace-findings">
      {findings.map((f) => (
        <li key={f.id}>
          <button className={`finding-item ${activeId === f.id ? 'is-active' : ''}`} onClick={() => onPick(f)}>
            <div className="finding-top">
              <SeverityBadge severity={f.severity} />
              <DetectorBadge detector={f.detector} />
              <span className="muted small">
                agent {f.agent_id || '–'} · {f.span_ids.length} spans · score {f.score}
              </span>
              <span className="muted small finding-time">
                <TimeAgo ns={f.created_ns} />
              </span>
            </div>
            <div className="finding-summary">{f.summary}</div>
          </button>
        </li>
      ))}
    </ul>
  )
}
