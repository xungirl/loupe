import type { Span } from '../api'

/** The main text for a span row: what happened, in a few words. */
export function spanTitle(s: Span): string {
  if (s.kind === 'tool') return s.tool_name || s.name
  if (s.kind === 'llm') return s.model ? `${s.name === 'llm' ? 'LLM' : s.name} · ${s.model}` : s.name
  if (s.kind === 'agent') return s.agent_name ? `${s.agent_name}: ${s.name}` : s.name
  return s.name
}

/** Secondary text: the tool's target (file, command…) when there is one. */
export function spanDetail(s: Span): string {
  return s.target ?? ''
}

export function isError(s: Span): boolean {
  return s.status === 'error'
}

export function spanTokens(s: Span): number {
  return (s.input_tokens ?? 0) + (s.output_tokens ?? 0) + (s.cache_read_tokens ?? 0) + (s.cache_write_tokens ?? 0)
}
