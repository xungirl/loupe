// Package assemble turns a flat list of spans into the two views the UI shows:
// a span tree (for the waterfall) and an agent tree (who spawned whom).
//
// Assembly happens at query time from whatever spans have arrived, so it must
// cope with:
//   - out-of-order arrival: children stored before their parents. We build
//     the tree only after all spans are loaded, so order doesn't matter, and
//     we sort siblings by (start time, span id) so the output is deterministic.
//   - late / missing parents: a span whose parent hasn't arrived (yet) becomes
//     a root flagged Orphan=true instead of being dropped. When the parent
//     arrives later, the next query attaches it normally.
//   - cycles (corrupt data: A→B→A): nodes unreachable from any root are
//     promoted to orphan roots, so the walk always terminates.
package assemble

import (
	"sort"

	"github.com/xungirl/loupe/server/internal/model"
)

// Node is one span in the tree.
type Node struct {
	Span     model.Span `json:"span"`
	Depth    int        `json:"depth"`
	Orphan   bool       `json:"orphan,omitempty"` // parent_span_id set but parent not present
	AgentKey string     `json:"agent_key"`        // effective agent (see agentKey)
	Children []*Node    `json:"children,omitempty"`
}

// Agent is one node of the agent tree with its aggregates.
type Agent struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	ParentKey    string   `json:"parent_key,omitempty"`
	SpanID       string   `json:"span_id"` // the agent span (empty for implicit root)
	StartNS      int64    `json:"start_ns"`
	EndNS        int64    `json:"end_ns"`
	LLMCalls     int      `json:"llm_calls"`
	ToolCalls    int      `json:"tool_calls"`
	Errors       int      `json:"errors"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	CacheTokens  int64    `json:"cache_tokens"`
	CostUSD      float64  `json:"cost_usd"`
	Children     []string `json:"children,omitempty"`
}

// Trace is the assembled result.
type Trace struct {
	Roots   []*Node  `json:"roots"`
	Flat    []*Node  `json:"flat"` // depth-first order, what the waterfall renders
	Agents  []*Agent `json:"agents"`
	Orphans int      `json:"orphans"`
	MaxDep  int      `json:"max_depth"`
}

// Build assembles spans (any order, any completeness) into a Trace.
func Build(spans []model.Span) *Trace {
	nodes := make(map[string]*Node, len(spans))
	for i := range spans {
		// Duplicate IDs can't reach here via the store, but be defensive: last wins.
		nodes[spans[i].SpanID] = &Node{Span: spans[i]}
	}

	var roots []*Node
	for _, n := range nodes {
		p := n.Span.ParentSpanID
		switch parent, ok := nodes[p]; {
		case p == "":
			roots = append(roots, n)
		case !ok:
			n.Orphan = true
			roots = append(roots, n)
		default:
			parent.Children = append(parent.Children, n)
		}
	}

	t := &Trace{}
	visited := make(map[string]bool, len(nodes))
	sortNodes(roots)
	for _, r := range roots {
		t.walk(r, 0, "", visited)
	}
	// Anything not visited sits on a cycle. Break it by promoting the earliest
	// unvisited node to an orphan root, then walk again until all are placed.
	for len(visited) < len(nodes) {
		var rest []*Node
		for _, n := range nodes {
			if !visited[n.Span.SpanID] {
				rest = append(rest, n)
			}
		}
		sortNodes(rest)
		r := rest[0]
		r.Orphan = true
		// Detach r from its (cyclic) parent so the walk can't come back to it.
		if parent, ok := nodes[r.Span.ParentSpanID]; ok {
			parent.Children = removeChild(parent.Children, r)
		}
		roots = append(roots, r)
		t.walk(r, 0, "", visited)
	}
	t.Roots = roots
	for _, n := range t.Flat {
		if n.Orphan {
			t.Orphans++
		}
	}
	t.Agents = buildAgents(t.Flat)
	return t
}

// walk visits n depth-first, assigning depth and effective agent.
func (t *Trace) walk(n *Node, depth int, parentAgent string, visited map[string]bool) {
	if visited[n.Span.SpanID] {
		return
	}
	visited[n.Span.SpanID] = true
	n.Depth = depth
	if depth > t.MaxDep {
		t.MaxDep = depth
	}
	n.AgentKey = agentKey(n, parentAgent)
	t.Flat = append(t.Flat, n)
	sortNodes(n.Children)
	for _, c := range n.Children {
		t.walk(c, depth+1, n.AgentKey, visited)
	}
}

// agentKey decides which agent a span belongs to:
//  1. an agent span starts a new agent, keyed by its agent_id (or span id);
//  2. otherwise an explicit agent_id on the span wins;
//  3. otherwise it inherits its parent's agent; roots fall back to "root".
func agentKey(n *Node, parentAgent string) string {
	if n.Span.Kind == model.KindAgent {
		if n.Span.AgentID != "" {
			return n.Span.AgentID
		}
		return "span:" + n.Span.SpanID
	}
	if n.Span.AgentID != "" {
		return n.Span.AgentID
	}
	if parentAgent != "" {
		return parentAgent
	}
	return "root"
}

func buildAgents(flat []*Node) []*Agent {
	byKey := map[string]*Agent{}
	var order []*Agent
	get := func(key string) *Agent {
		a, ok := byKey[key]
		if !ok {
			a = &Agent{Key: key, Name: key}
			byKey[key] = a
			order = append(order, a)
		}
		return a
	}
	// parentAgentOf maps span id → agent key, to find the spawning agent.
	agentOfSpan := make(map[string]string, len(flat))
	for _, n := range flat {
		agentOfSpan[n.Span.SpanID] = n.AgentKey
	}
	for _, n := range flat {
		a := get(n.AgentKey)
		sp := &n.Span
		if sp.Kind == model.KindAgent && a.SpanID == "" {
			a.SpanID = sp.SpanID
			if sp.AgentName != "" {
				a.Name = sp.AgentName
			} else {
				a.Name = sp.Name
			}
			if pk, ok := agentOfSpan[sp.ParentSpanID]; ok && pk != a.Key {
				a.ParentKey = pk
			}
		} else if a.Name == a.Key && sp.AgentName != "" {
			a.Name = sp.AgentName
		}
		if a.StartNS == 0 || sp.StartNS < a.StartNS {
			a.StartNS = sp.StartNS
		}
		if sp.EndNS > a.EndNS {
			a.EndNS = sp.EndNS
		}
		switch sp.Kind {
		case model.KindLLM:
			a.LLMCalls++
		case model.KindTool:
			a.ToolCalls++
		}
		if sp.IsError() {
			a.Errors++
		}
		a.InputTokens += sp.InputTokens
		a.OutputTokens += sp.OutputTokens
		a.CacheTokens += sp.CacheReadTokens + sp.CacheWriteTokens
		a.CostUSD += sp.CostUSD
	}
	for _, a := range order {
		if a.ParentKey != "" {
			if p, ok := byKey[a.ParentKey]; ok {
				p.Children = append(p.Children, a.Key)
			}
		}
	}
	return order
}

func sortNodes(ns []*Node) {
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].Span.StartNS != ns[j].Span.StartNS {
			return ns[i].Span.StartNS < ns[j].Span.StartNS
		}
		return ns[i].Span.SpanID < ns[j].Span.SpanID
	})
}

func removeChild(cs []*Node, x *Node) []*Node {
	out := cs[:0]
	for _, c := range cs {
		if c != x {
			out = append(out, c)
		}
	}
	return out
}
