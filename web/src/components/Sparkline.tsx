interface Props {
  values: number[] // oldest first
  slots: number // how many values fit across the width (e.g. 60 seconds)
  width?: number
  height?: number
}

/**
 * A tiny SVG line chart. The newest value is always at the right edge and
 * older values scroll left, so the chart "fills in" from the right as data
 * arrives (slot i of `slots` maps to x = i / (slots - 1) * width).
 */
export default function Sparkline({ values, slots, width = 240, height = 56 }: Props) {
  const top = 14 // room for the "max" label
  const max = Math.max(1, ...values)
  const offset = slots - values.length // empty slots on the left
  // SVG y grows downward, so a bigger value means a smaller y.
  const pts = values.map((v, i) => [((offset + i) / (slots - 1)) * width, height - 1 - (v / max) * (height - top - 1)] as const)
  const line = pts.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const area = pts.length > 1 ? `${line} L${width},${height} L${pts[0][0].toFixed(1)},${height} Z` : ''
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="sparkline" role="img" aria-label="spans per second">
      <line x1={0} x2={width} y1={height - 0.5} y2={height - 0.5} className="spark-base" />
      {area && <path d={area} className="spark-area" />}
      {pts.length > 1 && <path d={line} className="spark-line" />}
      <text x={width} y={10} textAnchor="end" className="spark-max">
        peak {max}/s
      </text>
    </svg>
  )
}
