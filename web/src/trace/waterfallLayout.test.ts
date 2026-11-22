import type { SpanNode } from '../api'
import { MIN_BAR_PCT, barPosition, hasChildren, subtreeSize, timeTicks, traceWindow, visibleRows } from './waterfallLayout'

// Builds a SpanNode with just the fields layout code uses.
function node(id: string, depth: number, start = 0, end = 0): SpanNode {
  return {
    depth,
    agent_key: 'main',
    span: { trace_id: 't', span_id: id, kind: 'tool', name: id, start_ns: start, end_ns: end },
  }
}

describe('barPosition', () => {
  const win = { startNs: 1_000, endNs: 11_000 } // a 10 µs trace

  it('places a bar by start/end relative to the trace start', () => {
    expect(barPosition(1_000, 11_000, win)).toEqual({ left: 0, width: 100 })
    expect(barPosition(3_500, 6_000, win)).toEqual({ left: 25, width: 25 })
  })

  it('gives zero-length spans a minimum visible width', () => {
    const p = barPosition(6_000, 6_000, win)
    expect(p.left).toBe(50)
    expect(p.width).toBe(MIN_BAR_PCT)
  })

  it('shifts a minimum-width bar left instead of overflowing the right edge', () => {
    const p = barPosition(11_000, 11_000, win)
    expect(p.left + p.width).toBeCloseTo(100)
  })

  it('clamps spans that fall outside the window', () => {
    expect(barPosition(0, 20_000, win)).toEqual({ left: 0, width: 100 })
  })

  it('fills the row when the trace has zero duration', () => {
    expect(barPosition(5, 5, { startNs: 5, endNs: 5 })).toEqual({ left: 0, width: 100 })
  })
})

describe('traceWindow', () => {
  it('extends the summary window to cover late spans', () => {
    const w = traceWindow([node('a', 0, 100, 200), node('b', 1, 150, 900)], 100, 500)
    expect(w).toEqual({ startNs: 100, endNs: 900 })
  })
})

describe('tree helpers on depth-first rows', () => {
  //  root
  //  ├─ a
  //  │  └─ a1
  //  └─ b
  const rows = [node('root', 0), node('a', 1), node('a1', 2), node('b', 1)]

  it('knows which rows have children', () => {
    expect(rows.map((_, i) => hasChildren(rows, i))).toEqual([true, true, false, false])
  })

  it('counts subtree sizes', () => {
    expect(subtreeSize(rows, 0)).toBe(3)
    expect(subtreeSize(rows, 1)).toBe(1)
    expect(subtreeSize(rows, 3)).toBe(0)
  })

  it('hides the descendants of collapsed rows', () => {
    expect(visibleRows(rows, new Set())).toEqual([0, 1, 2, 3])
    expect(visibleRows(rows, new Set(['a']))).toEqual([0, 1, 3])
    expect(visibleRows(rows, new Set(['root']))).toEqual([0])
    // collapsing a leaf does nothing
    expect(visibleRows(rows, new Set(['b']))).toEqual([0, 1, 2, 3])
  })
})

describe('timeTicks', () => {
  it('uses round 1/2/5 steps', () => {
    const ticks = timeTicks(65e9) // 65 s
    // 65 s / 6 ≈ 10.8 s per tick → rounded up to a 20 s step
    expect(ticks.map((t) => t.label)).toEqual(['0', '20 s', '40 s', '1m 00s'])
    expect(ticks[1].pct).toBeCloseTo((20 / 65) * 100)
    expect(timeTicks(1e9).map((t) => t.label)).toEqual(['0', '200 ms', '400 ms', '600 ms', '800 ms', '1 s'])
  })
  it('never returns an empty axis', () => {
    expect(timeTicks(0)).toHaveLength(1)
  })
})
