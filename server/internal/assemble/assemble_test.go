package assemble

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/testutil"
)

// multiAgentTrace: main agent → Task tool → subagent → nested Task → sub-subagent.
func multiAgentTrace() []model.Span {
	S := testutil.Span
	main := S("t", "main", "", model.KindAgent, 0, 1000)
	main.AgentID, main.AgentName = "main", "main"
	task := S("t", "task1", "main", model.KindTool, 100, 800)
	task.ToolName = "Task"
	sub := S("t", "sub", "task1", model.KindAgent, 110, 790)
	sub.AgentID, sub.AgentName = "agent-a", "general-purpose"
	subLLM := S("t", "sub-llm", "sub", model.KindLLM, 120, 200)
	subLLM.InputTokens, subLLM.CostUSD = 500, 0.01
	nestedTask := S("t", "task2", "sub", model.KindTool, 300, 600)
	nestedTask.ToolName = "Task"
	subsub := S("t", "subsub", "task2", model.KindAgent, 310, 590)
	subsub.AgentID, subsub.AgentName = "agent-b", "explorer"
	ssTool := S("t", "ss-tool", "subsub", model.KindTool, 320, 330)
	ssTool.Status = model.StatusError
	mainLLM := S("t", "main-llm", "main", model.KindLLM, 10, 90)
	mainLLM.InputTokens, mainLLM.CostUSD = 1000, 0.02
	return []model.Span{main, task, sub, subLLM, nestedTask, subsub, ssTool, mainLLM}
}

func signature(tr *Trace) string {
	var b strings.Builder
	for _, n := range tr.Flat {
		fmt.Fprintf(&b, "%s@%d[%s]%v;", n.Span.SpanID, n.Depth, n.AgentKey, n.Orphan)
	}
	return b.String()
}

func TestOrderIndependent(t *testing.T) {
	spans := multiAgentTrace()
	want := signature(Build(spans))
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		shuffled := append([]model.Span(nil), spans...)
		r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if got := signature(Build(shuffled)); got != want {
			t.Fatalf("shuffle %d changed the tree:\n got %s\nwant %s", i, got, want)
		}
	}
	if !strings.HasPrefix(want, "main@0[main]false;main-llm@1[main]") {
		t.Fatalf("unexpected order %s", want)
	}
}

func TestAgentTree(t *testing.T) {
	tr := Build(multiAgentTrace())
	if len(tr.Agents) != 3 {
		t.Fatalf("want 3 agents, got %d", len(tr.Agents))
	}
	byKey := map[string]*Agent{}
	for _, a := range tr.Agents {
		byKey[a.Key] = a
	}
	if byKey["agent-a"].ParentKey != "main" || byKey["agent-b"].ParentKey != "agent-a" || byKey["main"].ParentKey != "" {
		t.Fatalf("wrong parents: a→%s b→%s", byKey["agent-a"].ParentKey, byKey["agent-b"].ParentKey)
	}
	if byKey["agent-b"].Errors != 1 || byKey["agent-b"].Name != "explorer" {
		t.Fatalf("agent-b aggregates %+v", byKey["agent-b"])
	}
	// The Task tool span belongs to the agent that called it, not the child.
	if byKey["main"].ToolCalls != 1 || byKey["agent-a"].ToolCalls != 1 || byKey["agent-a"].InputTokens != 500 {
		t.Fatalf("attribution wrong: main=%+v a=%+v", byKey["main"], byKey["agent-a"])
	}
	if tr.MaxDep != 5 {
		t.Fatalf("max depth %d", tr.MaxDep)
	}
}

func TestLateParentBecomesOrphanThenAttaches(t *testing.T) {
	spans := multiAgentTrace()
	var withoutSub []model.Span
	for _, s := range spans {
		if s.SpanID != "sub" {
			withoutSub = append(withoutSub, s)
		}
	}
	early := Build(withoutSub)
	if early.Orphans != 2 { // sub-llm and task2 lost their parent
		t.Fatalf("want 2 orphans before the parent arrives, got %d", early.Orphans)
	}
	if len(early.Flat) != len(withoutSub) {
		t.Fatal("orphaned spans must not be dropped")
	}
	late := Build(spans)
	if late.Orphans != 0 {
		t.Fatalf("after the late parent arrives there should be no orphans, got %d", late.Orphans)
	}
}

func TestCycleTerminates(t *testing.T) {
	a := testutil.Span("t", "a", "b", model.KindStep, 0, 1)
	b := testutil.Span("t", "b", "a", model.KindStep, 1, 2)
	c := testutil.Span("t", "c", "a", model.KindStep, 2, 3)
	tr := Build([]model.Span{a, b, c})
	if len(tr.Flat) != 3 {
		t.Fatalf("every span must appear exactly once, got %d", len(tr.Flat))
	}
	if tr.Orphans != 1 || tr.Flat[0].Span.SpanID != "a" {
		t.Fatalf("cycle should be broken at the earliest span: %s", signature(tr))
	}
}

func TestEmpty(t *testing.T) {
	tr := Build(nil)
	if len(tr.Flat) != 0 || len(tr.Agents) != 0 {
		t.Fatal("empty input")
	}
}

func BenchmarkBuild10k(b *testing.B) {
	spans := make([]model.Span, 0, 10_000)
	spans = append(spans, testutil.Span("t", "root", "", model.KindAgent, 0, 1))
	for i := 1; i < 10_000; i++ {
		parent := fmt.Sprint(i / 4)
		if i < 4 {
			parent = "root"
		}
		spans = append(spans, testutil.Span("t", fmt.Sprint(i), parent, model.KindTool, int64(i), int64(i)+1))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(spans)
	}
}
