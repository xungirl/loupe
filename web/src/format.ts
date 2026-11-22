// Number and time formatting helpers, shared by every page.
// The API speaks Unix nanoseconds; JavaScript Dates speak milliseconds.

const NS_PER_MS = 1e6
const NS_PER_S = 1e9

export function nsToMs(ns: number): number {
  return ns / NS_PER_MS
}

/** Unix ns → JS Date. */
export function nsToDate(ns: number): Date {
  return new Date(ns / NS_PER_MS)
}

/**
 * A duration in ns, at a precision that fits its size:
 * 420 µs · 12 ms · 3.4 s · 2m 05s · 1h 03m
 */
export function formatDuration(ns: number): string {
  if (!Number.isFinite(ns) || ns <= 0) return '0 ms'
  if (ns < NS_PER_MS) return `${Math.round(ns / 1e3)} µs`
  const ms = ns / NS_PER_MS
  if (ms < 1000) return `${ms < 10 ? ms.toFixed(1) : Math.round(ms)} ms`
  const s = ns / NS_PER_S
  if (s < 60) return `${s < 10 ? s.toFixed(2) : s.toFixed(1)} s`
  const totalS = Math.round(s)
  if (totalS < 3600) return `${Math.floor(totalS / 60)}m ${String(totalS % 60).padStart(2, '0')}s`
  const totalM = Math.round(totalS / 60)
  return `${Math.floor(totalM / 60)}h ${String(totalM % 60).padStart(2, '0')}m`
}

/** Compact counts for tokens etc.: 950 · 1.2k · 34k · 1.2M · 3.4B */
export function formatCompact(n: number | undefined | null): string {
  if (n === undefined || n === null || !Number.isFinite(n)) return '–'
  const abs = Math.abs(n)
  const units: [number, string][] = [
    [1e9, 'B'],
    [1e6, 'M'],
    [1e3, 'k'],
  ]
  for (const [size, suffix] of units) {
    if (abs >= size) {
      const v = n / size
      // One decimal below 10 (1.2M), none above (34M), and drop a trailing ".0".
      const txt = Math.abs(v) < 10 ? v.toFixed(1).replace(/\.0$/, '') : Math.round(v).toString()
      return txt + suffix
    }
  }
  return Math.round(n).toString()
}

/** Thousands separators: 12345 → "12,345". */
export function formatNumber(n: number | undefined | null): string {
  if (n === undefined || n === null || !Number.isFinite(n)) return '–'
  return Math.round(n).toLocaleString('en-US')
}

/**
 * USD with precision that matches the magnitude:
 * $193.03 · $1.24 · $0.037 · $0.0042 · $0.00
 */
export function formatCost(usd: number | undefined | null): string {
  if (usd === undefined || usd === null || !Number.isFinite(usd)) return '–'
  if (usd === 0) return '$0.00'
  const abs = Math.abs(usd)
  if (abs >= 1) return `$${usd.toFixed(2)}`
  if (abs >= 0.01) return `$${usd.toFixed(3)}`
  // Tiny amounts: two significant digits so they don't all read "$0.00".
  return `$${usd.toPrecision(2)}`
}

/** Chinese yuan, used by experiment reports (DashScope bills in CNY). */
export function formatCNY(cny: number | undefined | null, digits = 2): string {
  if (cny === undefined || cny === null || !Number.isFinite(cny)) return '–'
  return `¥${cny.toFixed(digits)}`
}

/** 0.4835 → "48.4%" */
export function formatPercent(ratio: number | undefined | null, digits = 1): string {
  if (ratio === undefined || ratio === null || !Number.isFinite(ratio)) return '–'
  return `${(ratio * 100).toFixed(digits)}%`
}

/** "just now" · "42s ago" · "5m ago" · "3h ago" · "2d ago" · then a date. */
export function formatRelative(ns: number, nowMs: number = Date.now()): string {
  const diffS = Math.round((nowMs - ns / NS_PER_MS) / 1000)
  if (diffS < 0) return 'in the future'
  if (diffS < 5) return 'just now'
  if (diffS < 60) return `${diffS}s ago`
  if (diffS < 3600) return `${Math.floor(diffS / 60)}m ago`
  if (diffS < 86400) return `${Math.floor(diffS / 3600)}h ago`
  if (diffS < 30 * 86400) return `${Math.floor(diffS / 86400)}d ago`
  return nsToDate(ns).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

/** Full local timestamp for tooltips: "Nov 22, 2026, 02:25:59". */
export function formatAbsolute(ns: number): string {
  return nsToDate(ns).toLocaleString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

/** Clock time with milliseconds, for the live tail: "02:25:59.123". */
export function formatClock(ns: number): string {
  const d = nsToDate(ns)
  const pad = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

/** Shortens long ids in the middle: "corpus-0001-abcdef…" */
export function truncate(s: string, max = 48): string {
  return s.length <= max ? s : s.slice(0, max - 1) + '…'
}
