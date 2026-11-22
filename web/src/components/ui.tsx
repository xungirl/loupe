// Small presentational building blocks shared by the pages.
import type { ReactNode } from 'react'
import type { DetectorName, Severity, SpanKind } from '../api'
import { formatAbsolute, formatRelative } from '../format'

export function Badge({ children, tone = 'neutral', title }: { children: ReactNode; tone?: string; title?: string }) {
  return (
    <span className={`badge badge-${tone}`} title={title}>
      {children}
    </span>
  )
}

const SOURCE_TONES: Record<string, string> = {
  'claude-code': 'orange',
  corpus: 'slate',
  'demo-live': 'green',
  sdk: 'blue',
  otlp: 'violet',
}
export function SourceBadge({ source }: { source: string }) {
  return <Badge tone={SOURCE_TONES[source] ?? 'neutral'}>{source || 'unknown'}</Badge>
}

export const DETECTORS: DetectorName[] = ['loop', 'rework', 'error_streak', 'stuck', 'budget_burn']
export const DETECTOR_INFO: Record<string, { label: string; help: string }> = {
  loop: { label: 'loop', help: 'Near-identical tool calls returning the same result' },
  rework: { label: 'rework', help: 'The same file edited again and again with failing tests in between' },
  error_streak: { label: 'error streak', help: 'Several tool calls failed in a row' },
  stuck: { label: 'stuck', help: 'Many reads/searches with no edit or test run' },
  budget_burn: { label: 'budget burn', help: 'Token use above the configured budget' },
}
export function DetectorBadge({ detector, count }: { detector: string; count?: number }) {
  const info = DETECTOR_INFO[detector]
  return (
    <span className={`badge det det-${detector}`} title={info?.help}>
      {info?.label ?? detector}
      {count !== undefined && count > 1 && <span className="badge-count">×{count}</span>}
    </span>
  )
}

export function SeverityBadge({ severity }: { severity: Severity | string }) {
  return <Badge tone={severity === 'critical' ? 'red' : 'amber'}>{severity}</Badge>
}

export function KindBadge({ kind }: { kind: SpanKind | string }) {
  return <span className={`kind-badge kind-${kind}`}>{kind}</span>
}

/** Relative time ("5m ago") with the exact timestamp in a tooltip. */
export function TimeAgo({ ns }: { ns: number }) {
  if (!ns) return <span className="muted">–</span>
  return <span title={formatAbsolute(ns)}>{formatRelative(ns)}</span>
}

export function StatCard({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: string }) {
  return (
    <div className={`stat-card ${tone ? `stat-${tone}` : ''}`}>
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {sub && <div className="stat-sub">{sub}</div>}
    </div>
  )
}

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="loading" role="status">
      <span className="spinner" /> {label}
    </div>
  )
}

export function ErrorBox({ error }: { error: Error }) {
  return (
    <div className="error-box" role="alert">
      <strong>Could not load data.</strong> {error.message}
      <div className="muted small">Is the Loupe server running? (make build &amp;&amp; bin/loupe serve …)</div>
    </div>
  )
}

export function EmptyState({ title, body, children }: { title: string; body?: ReactNode; children?: ReactNode }) {
  return (
    <div className="empty-state">
      <div className="empty-title">{title}</div>
      {body && <div className="empty-body">{body}</div>}
      {children}
    </div>
  )
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {subtitle && <p className="page-subtitle">{subtitle}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  )
}

/** A thin horizontal bar used inside table cells (error rate, cost share…). */
export function InlineBar({ ratio, tone = 'accent' }: { ratio: number; tone?: string }) {
  const pct = Math.max(0, Math.min(1, ratio)) * 100
  return (
    <span className="inline-bar">
      <span className={`inline-bar-fill tone-${tone}`} style={{ width: `${pct}%` }} />
    </span>
  )
}
