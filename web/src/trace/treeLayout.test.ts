import type { Agent } from '../api'
import { DEFAULT_LAYOUT, edgePath, layoutTree } from './treeLayout'

function agent(key: string, parent_key?: string): Agent {
  return {
    key,
    name: key,
    parent_key,
    span_id: key,
    start_ns: 0,
    end_ns: 0,
    llm_calls: 0,
    tool_calls: 0,
    errors: 0,
    input_tokens: 0,
    output_tokens: 0,
    cache_tokens: 0,
    cost_usd: 0,
  }
}

const { nodeWidth, nodeHeight, gapX, gapY, padding } = DEFAULT_LAYOUT
const col = nodeWidth + gapX
const row = nodeHeight + gapY

describe('layoutTree', () => {
  it('stacks a chain vertically (claude-code → code-reviewer → Explore)', () => {
    const l = layoutTree([agent('main'), agent('rev1', 'main'), agent('exp2', 'rev1')])
    const xs = l.nodes.map((n) => n.x)
    expect(new Set(xs).size).toBe(1) // all in one column
    expect(l.nodes.map((n) => n.y)).toEqual([padding, padding + row, padding + 2 * row])
    expect(l.edges).toHaveLength(2)
    expect(l.width).toBe(padding * 2 + nodeWidth)
  })

  it('gives leaves their own columns and centres parents over children', () => {
    //        root
    //      /   |   \
    //     a    b    c
    //    / \
    //  a1   a2
    const l = layoutTree([agent('root'), agent('a', 'root'), agent('a1', 'a'), agent('a2', 'a'), agent('b', 'root'), agent('c', 'root')])
    const x = (k: string) => l.nodes.find((n) => n.agent.key === k)!.x
    const x0 = padding + nodeWidth / 2
    expect(x('a1')).toBe(x0)
    expect(x('a2')).toBe(x0 + col)
    expect(x('a')).toBe(x0 + col / 2) // centred over a1, a2
    expect(x('b')).toBe(x0 + 2 * col)
    expect(x('c')).toBe(x0 + 3 * col)
    expect(x('root')).toBe((x('a') + x('c')) / 2) // centred over first and last child
    // no two nodes on the same level overlap
    for (const a of l.nodes)
      for (const b of l.nodes)
        if (a !== b && a.depth === b.depth) expect(Math.abs(a.x - b.x)).toBeGreaterThanOrEqual(nodeWidth / 2)
    expect(l.height).toBe(padding * 2 + 3 * row - gapY)
  })

  it('lays out agents with unknown parents as extra roots', () => {
    const l = layoutTree([agent('main'), agent('orphan', 'missing')])
    expect(l.nodes.every((n) => n.depth === 0)).toBe(true)
    expect(l.edges).toHaveLength(0)
  })

  it('survives a parent cycle', () => {
    const l = layoutTree([agent('r'), agent('a', 'b'), agent('b', 'a')])
    expect(l.nodes.length).toBeGreaterThanOrEqual(1)
  })

  it('draws edges from parent bottom to child top', () => {
    const l = layoutTree([agent('main'), agent('sub', 'main')])
    const d = edgePath(l.edges[0], nodeHeight)
    expect(d.startsWith(`M ${l.nodes[0].x} ${padding + nodeHeight}`)).toBe(true)
    expect(d.endsWith(`${l.nodes[1].x} ${padding + row}`)).toBe(true)
  })
})
