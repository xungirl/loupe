import { formatCompact, formatCost, formatDuration, formatPercent, formatRelative, formatCNY } from './format'

describe('formatDuration (input in ns)', () => {
  it.each([
    [0, '0 ms'],
    [420_000, '420 µs'],
    [3_400_000, '3.4 ms'],
    [123_000_000, '123 ms'],
    [3_456_000_000, '3.46 s'],
    [42_100_000_000, '42.1 s'],
    [125_000_000_000, '2m 05s'],
    [3_780_000_000_000, '1h 03m'],
  ])('%d ns → %s', (ns, want) => {
    expect(formatDuration(ns)).toBe(want)
  })
})

describe('formatCompact', () => {
  it.each([
    [950, '950'],
    [1234, '1.2k'],
    [1000, '1k'],
    [34_500, '35k'],
    [1_200_000, '1.2M'],
    [150_069_744, '150M'],
    [3_400_000_000, '3.4B'],
  ])('%d → %s', (n, want) => {
    expect(formatCompact(n)).toBe(want)
  })
  it('handles missing values', () => {
    expect(formatCompact(undefined)).toBe('–')
  })
})

describe('formatCost', () => {
  it.each([
    [0, '$0.00'],
    [193.0281545, '$193.03'],
    [1.2436, '$1.24'],
    [0.0370751, '$0.037'],
    [0.0041888, '$0.0042'],
  ])('%f → %s', (usd, want) => {
    expect(formatCost(usd)).toBe(want)
  })
})

describe('formatRelative', () => {
  const now = Date.UTC(2026, 10, 22, 12, 0, 0)
  const ago = (s: number) => (now - s * 1000) * 1e6 // ms → ns
  it.each([
    [2, 'just now'],
    [42, '42s ago'],
    [5 * 60, '5m ago'],
    [3 * 3600, '3h ago'],
    [2 * 86400, '2d ago'],
  ])('%d s ago → %s', (s, want) => {
    expect(formatRelative(ago(s), now)).toBe(want)
  })
  it('falls back to a date after a month', () => {
    expect(formatRelative(ago(90 * 86400), now)).toMatch(/2026/)
  })
})

describe('misc', () => {
  it('formats percents and yuan', () => {
    expect(formatPercent(0.4835)).toBe('48.4%')
    expect(formatCNY(4.5)).toBe('¥4.50')
  })
})
