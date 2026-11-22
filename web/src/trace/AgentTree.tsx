import { useMemo } from 'react'
import type { Agent } from '../api'
import { formatCompact, formatCost, formatDuration, truncate } from '../format'
import { DEFAULT_LAYOUT, edgePath, layoutTree } from './treeLayout'

interface Props {
  agents: Agent[]
  selectedKey?: string
  onSelect: (key: string) => void
}

/** SVG graph of which agent spawned which. Click a node to filter the waterfall. */
export default function AgentTree({ agents, selectedKey, onSelect }: Props) {
  const layout = useMemo(() => layoutTree(agents), [agents])
  const { nodeWidth: W, nodeHeight: H } = DEFAULT_LAYOUT

  if (agents.length === 0) return <div className="muted">No agent spans in this trace.</div>

  return (
    <div className="agent-tree" data-testid="agent-tree">
      <p className="muted small">
        {agents.length} agent{agents.length === 1 ? '' : 's'}. Click a node to show only that agent's spans in the waterfall.
      </p>
      <div className="tree-scroll">
        <svg width={layout.width} height={layout.height} role="img" aria-label="Agent tree">
          <defs>
            <linearGradient id="node-accent" x1="0" y1="0" x2="1" y2="0">
              <stop offset="0" stopColor="var(--c-agent)" />
              <stop offset="1" stopColor="var(--c-llm)" />
            </linearGradient>
          </defs>

          {layout.edges.map((e) => (
            <path key={`${e.from.agent.key}-${e.to.agent.key}`} d={edgePath(e, H)} className="tree-edge" />
          ))}

          {layout.nodes.map((n) => {
            const a = n.agent
            // (x, y) is the top-centre of the box; translate so (0,0) is its top-left.
            const tokens = a.input_tokens + a.output_tokens + a.cache_tokens
            return (
              <g
                key={a.key}
                transform={`translate(${n.x - W / 2}, ${n.y})`}
                className={`tree-node ${selectedKey === a.key ? 'is-selected' : ''} ${a.errors ? 'has-errors' : ''}`}
                onClick={() => onSelect(a.key)}
                data-testid="tree-node"
                role="button"
                tabIndex={0}
                onKeyDown={(ev) => ev.key === 'Enter' && onSelect(a.key)}
              >
                <title>{`${a.name} (${a.key})`}</title>
                <rect width={W} height={H} rx={10} className="tree-box" />
                <rect width={W} height={4} rx={2} fill="url(#node-accent)" />
                <text x={14} y={26} className="tree-title">
                  {truncate(a.name || a.key, 22)}
                </text>
                {a.errors > 0 && (
                  <g transform={`translate(${W - 14}, 21)`}>
                    <text textAnchor="end" className="tree-err">
                      {a.errors} err
                    </text>
                  </g>
                )}
                <text x={14} y={48} className="tree-meta">
                  {a.llm_calls} LLM · {a.tool_calls} tool calls
                </text>
                <text x={14} y={68} className="tree-meta">
                  {formatCompact(tokens)} tokens · {formatCost(a.cost_usd)}
                </text>
                <text x={14} y={86} className="tree-meta tree-faint">
                  {formatDuration(a.end_ns - a.start_ns)} · key {truncate(a.key, 14)}
                </text>
              </g>
            )
          })}
        </svg>
      </div>
    </div>
  )
}
