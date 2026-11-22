// Pure layout math for the waterfall. No React here, so it is easy to test.
import type { SpanNode } from '../api'
import { formatDuration } from '../format'

/** The time window the waterfall draws: [startNs, endNs]. */
export interface TimeWindow {
  startNs: number
  endNs: number
}

/**
 * The trace window is the union of the summary and every span: a live trace
 * may have spans newer than the summary we fetched, and we never want a bar
 * to fall off the right edge.
 */
export function traceWindow(nodes: SpanNode[], summaryStart: number, summaryEnd: number): TimeWindow {
  let startNs = summaryStart || Infinity
  let endNs = summaryEnd || -Infinity
  for (const n of nodes) {
    startNs = Math.min(startNs, n.span.start_ns)
    endNs = Math.max(endNs, n.span.end_ns, n.span.start_ns)
  }
  if (!Number.isFinite(startNs)) startNs = 0
  if (!Number.isFinite(endNs) || endNs < startNs) endNs = startNs
  return { startNs, endNs }
}

/** Smallest bar we draw, in % of the timeline, so instant events stay visible. */
export const MIN_BAR_PCT = 0.4

/**
 * Where a span's bar goes, as percentages of the timeline width.
 *
 *   left  = (span.start - trace.start) / traceDuration
 *   width = (span.end   - span.start)  / traceDuration
 *
 * Both are clamped to [0, 100] and the width gets a floor (MIN_BAR_PCT) so a
 * 0 ms event is still a visible tick. If the floor would push the bar past the
 * right edge, the bar is shifted left instead of overflowing.
 */
export function barPosition(startNs: number, endNs: number, win: TimeWindow): { left: number; width: number } {
  const total = win.endNs - win.startNs
  if (total <= 0) return { left: 0, width: 100 } // a single instant: fill the row
  const clamp = (v: number) => Math.max(0, Math.min(100, v))
  let left = clamp(((startNs - win.startNs) / total) * 100)
  let width = clamp(((Math.max(endNs, startNs) - startNs) / total) * 100)
  if (width < MIN_BAR_PCT) width = MIN_BAR_PCT
  if (left + width > 100) left = Math.max(0, 100 - width)
  return { left, width }
}

/** Does row i have children? In depth-first order, children come right after. */
export function hasChildren(nodes: SpanNode[], i: number): boolean {
  return i + 1 < nodes.length && nodes[i + 1].depth > nodes[i].depth
}

/**
 * Hides the descendants of collapsed rows. Because the list is depth-first,
 * a row's subtree is exactly the run of following rows with a greater depth,
 * so one linear pass is enough: when we meet a collapsed row at depth d, we
 * skip everything after it until depth drops back to <= d.
 * Returns the original indexes of the rows to show.
 */
export function visibleRows(nodes: SpanNode[], collapsed: ReadonlySet<string>): number[] {
  const out: number[] = []
  let skipDeeperThan = Infinity
  nodes.forEach((n, i) => {
    if (n.depth > skipDeeperThan) return // inside a collapsed subtree
    skipDeeperThan = Infinity
    out.push(i)
    if (collapsed.has(n.span.span_id) && hasChildren(nodes, i)) skipDeeperThan = n.depth
  })
  return out
}

/** Number of spans under row i (its whole subtree, excluding itself). */
export function subtreeSize(nodes: SpanNode[], i: number): number {
  let j = i + 1
  while (j < nodes.length && nodes[j].depth > nodes[i].depth) j++
  return j - i - 1
}

export interface Tick {
  pct: number
  label: string
}

/**
 * "Nice" axis ticks: a step of 1, 2 or 5 × 10^k so labels read 0, 20 s, 40 s…
 * rather than 0, 13.7 s, 27.4 s. Aims for about `target` ticks.
 */
export function timeTicks(durationNs: number, target = 6): Tick[] {
  if (durationNs <= 0) return [{ pct: 0, label: '0' }]
  const rough = durationNs / target
  const pow = Math.pow(10, Math.floor(Math.log10(rough)))
  const step = [1, 2, 5, 10].map((m) => m * pow).find((s) => s >= rough) ?? 10 * pow
  const ticks: Tick[] = []
  for (let t = 0; t <= durationNs + 1e-6; t += step) {
    // Ticks are round numbers, so drop a trailing ".0" ("20.0 s" → "20 s").
    const label = t === 0 ? '0' : formatDuration(t).replace(/\.0+ /, ' ')
    ticks.push({ pct: (t / durationNs) * 100, label })
  }
  return ticks
}
