import { useState } from 'react'

export interface Bar {
  label: string // x-axis label
  value: number
  title?: string // tooltip text
}

interface Props {
  bars: Bar[]
  height?: number
  format: (v: number) => string
  ariaLabel: string
}

/**
 * Vertical bar chart in plain SVG. The SVG uses a fixed coordinate system
 * (viewBox) and scales to the container width, so we can do the layout math
 * in simple numbers:
 *   barWidth = plotWidth / bars.length (minus a gap)
 *   barHeight = value / niceMax * plotHeight
 */
export default function BarChart({ bars, height = 220, format, ariaLabel }: Props) {
  const [hover, setHover] = useState<number | null>(null)
  const W = 720
  const H = height
  const pad = { top: 16, right: 12, bottom: 28, left: 56 }
  const plotW = W - pad.left - pad.right
  const plotH = H - pad.top - pad.bottom

  const max = niceMax(Math.max(0, ...bars.map((b) => b.value)))
  const slot = plotW / Math.max(1, bars.length)
  const barW = Math.max(2, Math.min(48, slot * 0.7))
  const gridValues = [0, 0.25, 0.5, 0.75, 1].map((f) => f * max)
  // Show at most ~10 x labels so they never overlap.
  const labelEvery = Math.ceil(bars.length / 10)

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="bar-chart" role="img" aria-label={ariaLabel}>
      {gridValues.map((v) => {
        const y = pad.top + plotH - (v / max) * plotH
        return (
          <g key={v}>
            <line x1={pad.left} x2={W - pad.right} y1={y} y2={y} className="chart-grid" />
            <text x={pad.left - 8} y={y + 4} textAnchor="end" className="chart-axis">
              {format(v)}
            </text>
          </g>
        )
      })}
      {bars.map((b, i) => {
        const h = max > 0 ? (b.value / max) * plotH : 0
        const x = pad.left + i * slot + (slot - barW) / 2
        const y = pad.top + plotH - h
        return (
          <g key={b.label} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}>
            {/* a full-height invisible hit area makes small bars easy to hover */}
            <rect x={pad.left + i * slot} y={pad.top} width={slot} height={plotH} fill="transparent" />
            <rect x={x} y={y} width={barW} height={Math.max(h, b.value > 0 ? 1 : 0)} rx={3} className={`chart-bar ${hover === i ? 'is-hover' : ''}`} data-testid="chart-bar">
              <title>{b.title ?? `${b.label}: ${format(b.value)}`}</title>
            </rect>
            {i % labelEvery === 0 && (
              <text x={x + barW / 2} y={H - 8} textAnchor="middle" className="chart-axis">
                {b.label}
              </text>
            )}
            {hover === i && (
              <text x={x + barW / 2} y={y - 5} textAnchor="middle" className="chart-value">
                {format(b.value)}
              </text>
            )}
          </g>
        )
      })}
      <line x1={pad.left} x2={W - pad.right} y1={pad.top + plotH} y2={pad.top + plotH} className="chart-baseline" />
    </svg>
  )
}

/** Rounds the axis maximum up to 1, 2, 2.5 or 5 × 10^k so gridlines get round labels. */
export function niceMax(v: number): number {
  if (v <= 0) return 1
  const pow = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 2.5, 5, 10]) if (m * pow >= v) return m * pow
  return 10 * pow
}
