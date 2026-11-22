package detect

import (
	"fmt"
	"testing"
	"time"

	"github.com/xungirl/loupe/server/internal/corpus"
	"github.com/xungirl/loupe/server/internal/model"
)

// tb builds a single-agent trace one tool call at a time.
type tb struct {
	spans []model.Span
	t     int64
}

func newTB() *tb {
	return &tb{spans: []model.Span{{TraceID: "t", SpanID: "root", Kind: model.KindAgent, AgentID: "main", StartNS: 1, EndNS: 1}}, t: 1}
}

func (b *tb) tool(name, input, output string, isErr bool) *tb {
	b.t += 1e9
	sp := model.Span{TraceID: "t", SpanID: fmt.Sprintf("s%d", len(b.spans)), ParentSpanID: "root", Kind: model.KindTool,
		ToolName: name, ToolInput: input, ToolOutput: output, Target: model.ExtractTarget(input), StartNS: b.t, EndNS: b.t + 1}
	if isErr {
		sp.Status = model.StatusError
	}
	b.spans = append(b.spans, sp)
	return b
}

func (b *tb) llm(in int64) *tb {
	b.t += 1e9
	b.spans = append(b.spans, model.Span{TraceID: "t", SpanID: fmt.Sprintf("s%d", len(b.spans)), ParentSpanID: "root",
		Kind: model.KindLLM, InputTokens: in, StartNS: b.t, EndNS: b.t + 1})
	return b
}

func count(spans []model.Span, detector string) int {
	n := 0
	for _, f := range Run(spans, DefaultConfig()) {
		if f.Detector == detector {
			n++
		}
	}
	return n
}

func TestLoop(t *testing.T) {
	b := newTB()
	for i := 0; i < 3; i++ { // digits differ, everything else the same
		b.tool("Read", fmt.Sprintf(`{"file_path":"a.py","offset":%d}`, i*10), "def f(): pass", false)
	}
	if count(b.spans, Loop) != 1 {
		t.Fatal("3 near-identical reads with identical output should be a loop")
	}
	// Same calls but the output changes each time: the agent is learning something.
	b = newTB()
	for i := 0; i < 4; i++ {
		b.tool("Bash", `{"command":"pytest -q"}`, []string{"FAILED test_a", "FAILED test_b test_c", "ok all good", "syntax error line"}[i], false)
	}
	if count(b.spans, Loop) != 0 {
		t.Fatal("different outputs → not a loop")
	}
	// Repeats separated by edits are rework, not a loop.
	b = newTB()
	for i := 0; i < 4; i++ {
		b.tool("Bash", `{"command":"pytest -q"}`, "1 failed", true)
		b.tool("Edit", fmt.Sprintf(`{"file_path":"a.py","new_string":"v%d"}`, i), "ok", false)
	}
	if count(b.spans, Loop) != 0 {
		t.Fatal("edits between repeats → not a loop")
	}
	// Two repeats is below the threshold.
	b = newTB().tool("Grep", `{"pattern":"TODO"}`, "none", false).tool("Grep", `{"pattern":"TODO"}`, "none", false)
	if count(b.spans, Loop) != 0 {
		t.Fatal("2 repeats should not fire")
	}
}

func TestRework(t *testing.T) {
	b := newTB()
	for i := 0; i < 4; i++ { // 4 edits but 3 edit→fail→edit cycles
		b.tool("Edit", fmt.Sprintf(`{"file_path":"calc.py","new_string":"v%d"}`, i), "ok", false)
		b.tool("Bash", `{"command":"pytest -q"}`, "1 failed", i < 3)
	}
	if count(b.spans, Rework) != 1 {
		t.Fatal("3 fix cycles on one file should be rework")
	}
	b = newTB()
	for i := 0; i < 4; i++ { // 4 edits to different files, no failures
		b.tool("Write", fmt.Sprintf(`{"file_path":"f%d.py"}`, i), "ok", false)
	}
	if count(b.spans, Rework) != 0 {
		t.Fatal("editing different files is not rework")
	}
}

func TestErrorStreak(t *testing.T) {
	b := newTB().tool("Bash", `{"command":"a"}`, "x", true).tool("Bash", `{"command":"b"}`, "x", true).
		tool("Read", `{"file_path":"c"}`, "x", false).tool("Bash", `{"command":"d"}`, "x", true)
	if count(b.spans, ErrorStreak) != 0 {
		t.Fatal("2 errors + success + 1 error is not a streak of 3")
	}
	b.tool("Bash", `{"command":"e"}`, "x", true).tool("Bash", `{"command":"f"}`, "x", true)
	if count(b.spans, ErrorStreak) != 1 {
		t.Fatal("3 trailing errors should fire")
	}
}

func TestStuck(t *testing.T) {
	b := newTB()
	for i := 0; i < 13; i++ {
		b.tool("Read", fmt.Sprintf(`{"file_path":"src/m%d.py"}`, i), fmt.Sprintf("content %c", 'a'+i), false)
	}
	if count(b.spans, Stuck) != 1 {
		t.Fatal("13 reads without edits should be stuck")
	}
	b = newTB()
	for i := 0; i < 13; i++ {
		if i == 6 {
			b.tool("Bash", `{"command":"go test ./..."}`, "ok", false) // a test run is progress
		}
		b.tool("Grep", fmt.Sprintf(`{"pattern":"p%c"}`, 'a'+i), fmt.Sprintf("hit %c", 'a'+i), false)
	}
	if count(b.spans, Stuck) != 0 {
		t.Fatal("a test run in the middle breaks the stretch")
	}
}

func TestBudget(t *testing.T) {
	b := newTB()
	for _, in := range []int64{10_000, 11_000, 12_000, 30_000, 60_000, 80_000} {
		b.llm(in)
	}
	if count(b.spans, Budget) != 1 { // (30k+60k+80k)/3 ÷ 11k ≈ 5.2x
		t.Fatal("5x context growth should fire")
	}
	b = newTB().llm(2_000_000)
	if count(b.spans, Budget) != 1 {
		t.Fatal("over the absolute budget should fire")
	}
	b = newTB()
	for _, in := range []int64{10_000, 11_000, 12_000, 14_000, 15_000, 16_000} {
		b.llm(in)
	}
	if count(b.spans, Budget) != 0 {
		t.Fatal("normal growth should not fire")
	}
}

func TestFindingsAreAttributedPerAgent(t *testing.T) {
	// The same 3 repeats split across two agents is not a loop in either.
	spans := []model.Span{
		{TraceID: "t", SpanID: "root", Kind: model.KindAgent, AgentID: "main", StartNS: 1, EndNS: 1},
		{TraceID: "t", SpanID: "sub", ParentSpanID: "root", Kind: model.KindAgent, AgentID: "child", StartNS: 1, EndNS: 1},
	}
	for i := 0; i < 3; i++ {
		parent := "root"
		if i == 1 {
			parent = "sub"
		}
		spans = append(spans, model.Span{TraceID: "t", SpanID: fmt.Sprint(i), ParentSpanID: parent, Kind: model.KindTool,
			ToolName: "Read", ToolInput: `{"file_path":"a"}`, ToolOutput: "same", StartNS: int64(10 + i), EndNS: int64(11 + i)})
	}
	if count(spans, Loop) != 0 {
		t.Fatal("repeats in different agents must not be merged")
	}
}

// TestCorpusSanity guards the generator: deterministic, both splits, all labels.
func TestCorpusSanity(t *testing.T) {
	a := corpus.Generate(60, 7, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(), 0)
	b := corpus.Generate(60, 7, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(), 0)
	if len(a) != 60 || a[17].Variant != b[17].Variant || len(a[17].Spans) != len(b[17].Spans) {
		t.Fatal("generator not deterministic")
	}
	splits := map[string]int{}
	for _, tr := range a {
		splits[tr.Split]++
		for i := range tr.Spans {
			sp := tr.Spans[i]
			if err := sp.Normalize(sp.StartNS); err != nil {
				t.Fatalf("%s/%s invalid: %v", tr.TraceID, sp.SpanID, err)
			}
		}
	}
	if splits["train"] == 0 || splits["test"] == 0 {
		t.Fatalf("splits %v", splits)
	}
	rep := Evaluate(a, "all", DefaultConfig())
	if rep.Traces != 60 || len(rep.Scores) != len(All) {
		t.Fatalf("report %+v", rep)
	}
}
