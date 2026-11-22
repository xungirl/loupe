// Top-down tidy tree layout for the agent graph. Pure functions, no React.
import type { Agent } from '../api'

export interface TreeNode {
  agent: Agent
  depth: number
  x: number // centre of the node box, in px
  y: number // top of the node box, in px
}

export interface TreeEdge {
  from: TreeNode
  to: TreeNode
}

export interface TreeLayout {
  nodes: TreeNode[]
  edges: TreeEdge[]
  width: number
  height: number
}

export interface LayoutOptions {
  nodeWidth: number
  nodeHeight: number
  gapX: number // horizontal space between neighbouring boxes
  gapY: number // vertical space between levels
  padding: number
}

export const DEFAULT_LAYOUT: LayoutOptions = { nodeWidth: 210, nodeHeight: 96, gapX: 28, gapY: 56, padding: 24 }

/**
 * Lays agents out as a top-down tree.
 *
 * The idea (a simplified Reingold–Tilford "tidy tree"):
 *  1. Walk the tree depth-first. Each LEAF takes the next free column
 *     (0, 1, 2, …), so leaves never overlap.
 *  2. Each PARENT sits centred above its children: halfway between its
 *     first and last child's columns.
 *  3. y comes from depth: one row per level.
 * Columns are then scaled to pixels. Agents whose parent is unknown become
 * extra roots, laid out side by side (a forest).
 */
export function layoutTree(agents: Agent[], opts: LayoutOptions = DEFAULT_LAYOUT): TreeLayout {
  const byKey = new Map(agents.map((a) => [a.key, a]))

  // Build children lists from parent_key (more robust than trusting
  // `children`, which may be missing), keeping the original agent order.
  const children = new Map<string, Agent[]>()
  const roots: Agent[] = []
  for (const a of agents) {
    if (a.parent_key && byKey.has(a.parent_key) && a.parent_key !== a.key) {
      const list = children.get(a.parent_key) ?? []
      list.push(a)
      children.set(a.parent_key, list)
    } else {
      roots.push(a)
    }
  }

  const column = new Map<string, number>() // agent key → column (may be fractional)
  const depthOf = new Map<string, number>()
  let nextLeafColumn = 0
  const visiting = new Set<string>() // guards against a malformed cycle

  function place(a: Agent, depth: number): number {
    visiting.add(a.key)
    depthOf.set(a.key, depth)
    const kids = (children.get(a.key) ?? []).filter((k) => !visiting.has(k.key))
    let col: number
    if (kids.length === 0) {
      col = nextLeafColumn++
    } else {
      const cols = kids.map((k) => place(k, depth + 1))
      col = (cols[0] + cols[cols.length - 1]) / 2
    }
    column.set(a.key, col)
    return col
  }
  roots.forEach((r) => place(r, 0))

  const colWidth = opts.nodeWidth + opts.gapX
  const rowHeight = opts.nodeHeight + opts.gapY
  const nodes: TreeNode[] = agents
    .filter((a) => column.has(a.key))
    .map((a) => ({
      agent: a,
      depth: depthOf.get(a.key)!,
      x: opts.padding + opts.nodeWidth / 2 + column.get(a.key)! * colWidth,
      y: opts.padding + depthOf.get(a.key)! * rowHeight,
    }))

  const nodeByKey = new Map(nodes.map((n) => [n.agent.key, n]))
  const edges: TreeEdge[] = []
  for (const n of nodes) {
    const parent = n.agent.parent_key ? nodeByKey.get(n.agent.parent_key) : undefined
    if (parent && parent !== n) edges.push({ from: parent, to: n })
  }

  const maxDepth = Math.max(0, ...nodes.map((n) => n.depth))
  return {
    nodes,
    edges,
    width: opts.padding * 2 + Math.max(1, nextLeafColumn) * colWidth - opts.gapX,
    height: opts.padding * 2 + (maxDepth + 1) * rowHeight - opts.gapY,
  }
}

/**
 * SVG path for an edge: a smooth S-curve from the bottom-centre of the
 * parent box to the top-centre of the child box.
 */
export function edgePath(e: TreeEdge, nodeHeight: number): string {
  const x1 = e.from.x
  const y1 = e.from.y + nodeHeight
  const x2 = e.to.x
  const y2 = e.to.y
  const midY = (y1 + y2) / 2
  return `M ${x1} ${y1} C ${x1} ${midY}, ${x2} ${midY}, ${x2} ${y2}`
}
