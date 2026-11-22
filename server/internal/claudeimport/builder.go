package claudeimport

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
)

const mainAgent = "main"

// group is the entries of one agent (the main session or one subagent).
type group struct {
	key     string
	entries []*entry
	start   time.Time
	end     time.Time

	parentTool string // tool_use id of the Task call that spawned it ("" = root)
	agentType  string // subagent_type from that Task call
	desc       string
}

// toolUse is a tool_use block plus its matched result.
type toolUse struct {
	id, name  string
	input     json.RawMessage
	start     time.Time
	group     string
	result    *toolResult
	task      taskInput // only for Task/Agent calls
	linked    bool      // a subagent has been attached to this Task call
	resultAID string    // agentId reported in the Task result, if any
}

type toolResult struct {
	ts      time.Time
	text    string
	isError bool
	extra   json.RawMessage // toolUseResult
}

type builder struct {
	sf      SessionFiles
	st      *Stats
	traceID string
	groups  map[string]*group
	order   []*group
	tools   map[string]*toolUse
	toolSeq []*toolUse
}

func newBuilder(sf SessionFiles, st *Stats) *builder {
	return &builder{sf: sf, st: st, traceID: "cc-" + sf.SessionID,
		groups: map[string]*group{}, tools: map[string]*toolUse{}}
}

func agentSpanID(key string) string {
	if key == mainAgent {
		return "root"
	}
	return "agent:" + key
}

// groupEntries assigns every entry to an agent.
//   - non-sidechain entries belong to the main agent;
//   - sidechain entries with an agentId are grouped by it;
//   - older inline sidechains without agentId are grouped by following
//     parentUuid links back to the first entry of the chain.
func (b *builder) groupEntries(all []*entry) {
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].ts.Equal(all[j].ts) {
			return all[i].ts.Before(all[j].ts)
		}
		return all[i].line < all[j].line
	})
	chainOf := map[string]string{} // uuid → group key, for inline sidechains
	for _, e := range all {
		key := mainAgent
		switch {
		case !e.IsSidechain:
		case e.AgentID != "":
			key = e.AgentID
		default:
			if k, ok := chainOf[e.parent()]; ok {
				key = k
			} else {
				key = "side-" + shortID(e.UUID)
			}
		}
		if e.UUID != "" {
			chainOf[e.UUID] = key
		}
		g, ok := b.groups[key]
		if !ok {
			g = &group{key: key, start: e.ts}
			b.groups[key] = g
			b.order = append(b.order, g)
		}
		g.entries = append(g.entries, e)
		if e.ts.After(g.end) {
			g.end = e.ts
		}
	}
}

func shortID(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// collectTools finds every tool_use and pairs it with its tool_result.
func (b *builder) collectTools() {
	for _, g := range b.order {
		for _, e := range g.entries {
			if e.Type != "assistant" || e.Message == nil {
				continue
			}
			for _, bl := range e.Message.blocks() {
				if bl.Type != "tool_use" || bl.ID == "" {
					continue
				}
				if _, dup := b.tools[bl.ID]; dup {
					continue // same block repeated on a split assistant line
				}
				tu := &toolUse{id: bl.ID, name: bl.Name, input: bl.Input, start: e.ts, group: g.key}
				if isAgentTool(bl.Name) {
					_ = json.Unmarshal(bl.Input, &tu.task)
				}
				b.tools[bl.ID] = tu
				b.toolSeq = append(b.toolSeq, tu)
			}
		}
	}
	for _, g := range b.order {
		for _, e := range g.entries {
			if e.Type != "user" || e.Message == nil {
				continue
			}
			for _, bl := range e.Message.blocks() {
				if bl.Type != "tool_result" {
					continue
				}
				tu, ok := b.tools[bl.ToolUseID]
				if !ok {
					b.st.OrphanResults++
					continue
				}
				if tu.result != nil {
					continue
				}
				tu.result = &toolResult{ts: e.ts, text: resultText(bl.Content), isError: bl.IsError, extra: e.ToolUseResult}
				if isAgentTool(tu.name) {
					tu.resultAID = agentIDFromResult(e.ToolUseResult)
				}
			}
		}
	}
}

// agentIDFromResult looks for an agentId in a Task tool's toolUseResult.
func agentIDFromResult(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if s, ok := m["agentId"].(string); ok {
		return s
	}
	return ""
}

// firstPrompt is the text of a group's first user message.
func firstPrompt(g *group) string {
	for _, e := range g.entries {
		if e.Type == "user" && e.Message != nil {
			for _, bl := range e.Message.blocks() {
				if bl.Type == "text" && strings.TrimSpace(bl.Text) != "" {
					return strings.TrimSpace(bl.Text)
				}
			}
		}
	}
	return ""
}

// linkSubagents attaches each subagent to the Task call that spawned it, using
// the strongest evidence available: agentId in the Task result, then an exact
// prompt match, then time containment. Unmatched subagents hang off the root.
func (b *builder) linkSubagents() {
	var tasks []*toolUse
	for _, tu := range b.toolSeq {
		if isAgentTool(tu.name) {
			tasks = append(tasks, tu)
		}
	}
	link := func(g *group, tu *toolUse) {
		tu.linked = true
		g.parentTool, g.agentType, g.desc = tu.id, tu.task.SubagentType, tu.task.Description
	}
	var subs []*group
	for _, g := range b.order {
		if g.key != mainAgent {
			subs = append(subs, g)
			b.st.Subagents++
		}
	}
	// Pass 1: agentId.
	for _, g := range subs {
		for _, tu := range tasks {
			if !tu.linked && tu.resultAID != "" && (tu.resultAID == g.key || "agent-"+tu.resultAID == g.key) {
				link(g, tu)
				b.st.LinkedByID++
				break
			}
		}
	}
	// Pass 2: prompt text.
	for _, g := range subs {
		if g.parentTool != "" {
			continue
		}
		p := firstPrompt(g)
		for _, tu := range tasks {
			if !tu.linked && tu.group != g.key && p != "" && strings.TrimSpace(tu.task.Prompt) == p {
				link(g, tu)
				b.st.LinkedByPrompt++
				break
			}
		}
	}
	// Pass 3: the latest-starting unlinked Task whose window contains the subagent's start.
	for _, g := range subs {
		if g.parentTool != "" {
			continue
		}
		var best *toolUse
		for _, tu := range tasks {
			if tu.linked || tu.group == g.key || tu.start.After(g.start) {
				continue
			}
			if tu.result != nil && tu.result.ts.Before(g.start) {
				continue
			}
			if best == nil || tu.start.After(best.start) {
				best = tu
			}
		}
		if best != nil {
			link(g, best)
			b.st.LinkedByTime++
		} else {
			b.st.Unlinked++
		}
	}
}

// build runs the whole conversion.
func (b *builder) build(all []*entry) []model.Span {
	b.groupEntries(all)
	b.collectTools()
	b.linkSubagents()

	var spans []model.Span
	emit := func(sp model.Span) {
		sp.TraceID = b.traceID
		sp.Source = "claude-code"
		if err := sp.Normalize(time.Now().UnixNano()); err != nil {
			b.st.InvalidSpans++
			return
		}
		spans = append(spans, sp)
		b.st.Spans++
	}

	mainG := b.groups[mainAgent]
	sessStart, sessEnd := b.order[0].start, b.order[0].end
	for _, g := range b.order {
		if g.start.Before(sessStart) {
			sessStart = g.start
		}
		if g.end.After(sessEnd) {
			sessEnd = g.end
		}
	}
	// Root span. Named after the first prompt so the sessions list is readable.
	name := b.sf.SessionID
	var meta map[string]string
	if mainG != nil {
		if p := firstPrompt(mainG); p != "" {
			name = oneLine(p, 80)
		}
		e := mainG.entries[0]
		meta = map[string]string{"cwd": e.Cwd, "version": e.Version, "git_branch": e.GitBranch}
	}
	if meta == nil {
		meta = map[string]string{}
	}
	meta["project"] = b.sf.Project
	meta["session_id"] = b.sf.SessionID
	attrs, _ := json.Marshal(meta)
	emit(model.Span{SpanID: "root", Kind: model.KindAgent, Name: name, AgentID: mainAgent, AgentName: "claude-code",
		StartNS: timeNS(sessStart), EndNS: timeNS(sessEnd), Attributes: string(attrs)})

	for _, g := range b.order {
		agentName := "claude-code"
		if g.key != mainAgent {
			agentName = g.agentType
			if agentName == "" {
				agentName = "subagent"
			}
			parent := "root"
			if g.parentTool != "" {
				parent = "tool:" + g.parentTool
			}
			label := g.desc
			if label == "" {
				label = oneLine(firstPrompt(g), 60)
			}
			emit(model.Span{SpanID: agentSpanID(g.key), ParentSpanID: parent, Kind: model.KindAgent,
				Name: "subagent: " + label, AgentID: g.key, AgentName: agentName,
				StartNS: timeNS(g.start), EndNS: timeNS(g.end)})
		}
		b.emitGroup(g, agentName, emit)
	}
	for _, tu := range b.toolSeq {
		b.emitTool(tu, emit)
	}
	return spans
}

// emitGroup emits the LLM calls and user prompts of one agent.
func (b *builder) emitGroup(g *group, agentName string, emit func(model.Span)) {
	parent := agentSpanID(g.key)
	type llmCall struct {
		id        string
		model     string
		usage     *usage
		start     time.Time
		end       time.Time
		apiError  bool
		firstLine int
	}
	calls := map[string]*llmCall{}
	var seq []*llmCall
	var prev *entry
	for _, e := range g.entries {
		switch {
		case e.Type == "assistant" && e.Message != nil:
			id := e.Message.ID
			if id == "" {
				id = e.UUID
			}
			c, ok := calls[id]
			if !ok {
				start := e.ts
				if prev != nil {
					start = prev.ts // the call ran between the previous entry and this one
				}
				c = &llmCall{id: id, start: start, end: e.ts, firstLine: e.line}
				calls[id] = c
				seq = append(seq, c)
			} else {
				b.st.MergedLines++
				if e.ts.After(c.end) {
					c.end = e.ts
				}
			}
			if e.Message.Model != "" && e.Message.Model != "<synthetic>" {
				c.model = e.Message.Model
			}
			// Split lines of one message repeat usage; the last one is final.
			if e.Message.Usage != nil {
				c.usage = e.Message.Usage
			}
			c.apiError = c.apiError || e.IsAPIError
		case e.Type == "user" && e.Message != nil && !e.IsMeta:
			if text := promptText(e.Message.blocks()); text != "" {
				b.st.Prompts++
				attrs, _ := json.Marshal(map[string]string{"text": truncateStr(text, 2000)})
				emit(model.Span{SpanID: "user:" + e.UUID, ParentSpanID: parent, Kind: model.KindEvent,
					Name: "user: " + oneLine(text, 60), AgentID: g.key, AgentName: agentName,
					StartNS: timeNS(e.ts), EndNS: timeNS(e.ts), Attributes: string(attrs)})
			}
		}
		prev = e
	}
	for _, c := range seq {
		b.st.LLMCalls++
		sp := model.Span{SpanID: "llm:" + c.id, ParentSpanID: parent, Kind: model.KindLLM,
			Name: "llm " + c.model, Model: c.model, AgentID: g.key, AgentName: agentName,
			StartNS: timeNS(c.start), EndNS: timeNS(c.end)}
		if sp.Model == "" {
			sp.Name = "llm"
		}
		if c.usage != nil {
			sp.InputTokens = c.usage.InputTokens
			sp.OutputTokens = c.usage.OutputTokens
			sp.CacheReadTokens = c.usage.CacheReadInputTokens
			sp.CacheWriteTokens = c.usage.CacheCreationInputTokens
		}
		if c.apiError {
			sp.Status, sp.ErrorMsg = model.StatusError, "API error"
		}
		emit(sp)
	}
}

func (b *builder) emitTool(tu *toolUse, emit func(model.Span)) {
	b.st.ToolUses++
	g := b.groups[tu.group]
	agentName := "claude-code"
	if g.key != mainAgent {
		agentName = g.agentType
		if agentName == "" {
			agentName = "subagent"
		}
	}
	input := string(tu.input)
	sp := model.Span{SpanID: "tool:" + tu.id, ParentSpanID: agentSpanID(tu.group), Kind: model.KindTool,
		Name: tu.name, ToolName: tu.name, ToolInput: input, Target: model.ExtractTarget(input),
		AgentID: g.key, AgentName: agentName, StartNS: timeNS(tu.start), EndNS: timeNS(tu.start)}
	if tu.result == nil {
		b.st.MissingResults++
		sp.Attributes = `{"missing_result":true}`
	} else {
		if tu.result.ts.After(tu.start) {
			sp.EndNS = timeNS(tu.result.ts)
		}
		sp.ToolOutput = tu.result.text
		if tu.result.isError {
			b.st.ToolErrors++
			sp.Status = model.StatusError
			sp.ErrorMsg = oneLine(tu.result.text, 300)
		}
	}
	emit(sp)
}

// promptText returns the user-typed text of a message, or "" if it is only
// tool results (those are part of the agent loop, not prompts).
func promptText(bs []block) string {
	var parts []string
	for _, bl := range bs {
		switch bl.Type {
		case "tool_result":
			return ""
		case "text":
			if t := strings.TrimSpace(bl.Text); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func oneLine(s string, n int) string {
	return truncateStr(strings.Join(strings.Fields(s), " "), n)
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
