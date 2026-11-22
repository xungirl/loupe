package store_test

import (
	"context"
	"testing"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
	"github.com/xungirl/loupe/server/internal/testutil"
)

var ctx = context.Background()

func llm(trace, id, parent string, start, end, in, out int64, cost float64) model.Span {
	s := testutil.Span(trace, id, parent, model.KindLLM, start, end)
	s.InputTokens, s.OutputTokens, s.CostUSD = in, out, cost
	return s
}

func TestInsertIsIdempotent(t *testing.T) {
	st := testutil.OpenStore(t)
	batch := []model.Span{
		testutil.Span("t1", "root", "", model.KindAgent, 0, 100),
		llm("t1", "a", "root", 10, 20, 100, 10, 0.5),
		llm("t1", "a", "root", 10, 20, 100, 10, 0.5), // duplicate inside the same batch
	}
	ins, err := st.InsertSpans(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 2 {
		t.Fatalf("inserted %d, want 2", len(ins))
	}
	// A client retry of the whole batch must not change anything.
	ins, err = st.InsertSpans(ctx, batch)
	if err != nil || len(ins) != 0 {
		t.Fatalf("retry inserted %d (err %v), want 0", len(ins), err)
	}
	sum, err := st.GetTrace(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if sum.SpanCount != 2 || sum.InputTokens != 100 || sum.CostUSD != 0.5 || sum.LLMCalls != 1 || sum.AgentCount != 1 {
		t.Fatalf("summary double-counted: %+v", sum)
	}
}

func TestSummaryToleratesOutOfOrderBatches(t *testing.T) {
	st := testutil.OpenStore(t)
	// The child (later start) arrives first, the root arrives in a later batch.
	if _, err := st.InsertSpans(ctx, []model.Span{llm("t", "c", "root", 50, 80, 1, 1, 0)}); err != nil {
		t.Fatal(err)
	}
	root := testutil.Span("t", "root", "", model.KindAgent, 0, 100)
	root.Name = "session"
	errSpan := testutil.Span("t", "e", "root", model.KindTool, 90, 95)
	errSpan.Status = model.StatusError
	if _, err := st.InsertSpans(ctx, []model.Span{root, errSpan}); err != nil {
		t.Fatal(err)
	}
	sum, _ := st.GetTrace(ctx, "t")
	if sum.StartNS != root.StartNS || sum.EndNS != root.EndNS {
		t.Fatalf("start/end not min/max: %+v", sum)
	}
	if sum.Name != "session" || sum.ErrorCount != 1 || sum.SpanCount != 3 {
		t.Fatalf("bad summary %+v", sum)
	}
	spans, _ := st.TraceSpans(ctx, "t")
	if len(spans) != 3 || spans[0].SpanID != "root" {
		t.Fatalf("spans not ordered by start: %+v", spans)
	}
}

func TestListTracesFilters(t *testing.T) {
	st := testutil.OpenStore(t)
	for i, id := range []string{"a", "b", "c"} {
		root := testutil.Span(id, "r", "", model.KindAgent, int64(i)*1000, int64(i)*1000+10)
		root.Source = "sdk"
		spans := []model.Span{root}
		if id == "b" {
			tool := testutil.Span(id, "x", "r", model.KindTool, int64(i)*1000+1, int64(i)*1000+2)
			tool.ToolName, tool.Status = "Bash", model.StatusError
			spans = append(spans, tool, llm(id, "l", "r", int64(i)*1000, int64(i)*1000+1, 10, 10, 2))
		}
		if _, err := st.InsertSpans(ctx, spans); err != nil {
			t.Fatal(err)
		}
	}
	check := func(f store.TraceFilter, want ...string) {
		t.Helper()
		got, total, err := st.ListTraces(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if total != len(want) || len(got) != len(want) {
			t.Fatalf("filter %+v: got %d/%d traces, want %v", f, len(got), total, want)
		}
		for i := range want {
			if got[i].TraceID != want[i] {
				t.Fatalf("filter %+v: order %v, want %v", f, got, want)
			}
		}
	}
	check(store.TraceFilter{}, "c", "b", "a") // newest first
	check(store.TraceFilter{HasError: true}, "b")
	check(store.TraceFilter{Tool: "Bash"}, "b")
	check(store.TraceFilter{MinCostUSD: 1}, "b")
	check(store.TraceFilter{Sort: "cost"}, "b", "a", "c") // ties broken by trace_id
	if got, total, _ := st.ListTraces(ctx, store.TraceFilter{Limit: 1, Offset: 1}); total != 3 || len(got) != 1 || got[0].TraceID != "b" {
		t.Fatalf("pagination: %v total %d", got, total)
	}
	check(store.TraceFilter{Source: "nope"})
}

func TestRetentionDeletesOldTracesOnly(t *testing.T) {
	st := testutil.OpenStore(t)
	old := testutil.Span("old", "r", "", model.KindAgent, 0, 10)
	fresh := testutil.Span("new", "r", "", model.KindAgent, 100_000, 100_010)
	if _, err := st.InsertSpans(ctx, []model.Span{old, fresh}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceFindings(ctx, "old", 1, []store.Finding{{Detector: "loop", Severity: "warn", Summary: "x"}}); err != nil {
		t.Fatal(err)
	}
	tr, sp, err := st.DeleteTracesBefore(ctx, testutil.Base+50_000*1e6, 1)
	if err != nil || tr != 1 || sp != 1 {
		t.Fatalf("deleted %d traces %d spans err %v", tr, sp, err)
	}
	if _, err := st.GetTrace(ctx, "old"); err != store.ErrNotFound {
		t.Fatal("old trace still present")
	}
	if fs, _ := st.ListFindings(ctx, "old", "", 10); len(fs) != 0 {
		t.Fatal("findings of deleted trace remain")
	}
	if _, err := st.GetTrace(ctx, "new"); err != nil {
		t.Fatal("fresh trace deleted")
	}
}

func TestPendingDetectionAndReplaceFindings(t *testing.T) {
	st := testutil.OpenStore(t)
	if _, err := st.InsertSpans(ctx, []model.Span{testutil.Span("t", "r", "", model.KindAgent, 0, 1)}); err != nil {
		t.Fatal(err)
	}
	far := int64(1) << 62
	p, _ := st.PendingDetection(ctx, far, 10)
	if len(p) != 1 {
		t.Fatalf("want 1 pending, got %d", len(p))
	}
	fs := []store.Finding{{Detector: "loop", Severity: "warn", Summary: "s", SpanIDs: []string{"r"}}}
	if err := st.ReplaceFindings(ctx, "t", p[0].LastIngestNS, fs); err != nil {
		t.Fatal(err)
	}
	if p, _ = st.PendingDetection(ctx, far, 10); len(p) != 0 {
		t.Fatal("trace still pending after detection")
	}
	got, _ := st.ListFindings(ctx, "t", "", 10)
	if len(got) != 1 || got[0].SpanIDs[0] != "r" {
		t.Fatalf("findings round-trip failed: %+v", got)
	}
	// A late span makes the trace pending again.
	if _, err := st.InsertSpans(ctx, []model.Span{testutil.Span("t", "late", "r", model.KindTool, 2, 3)}); err != nil {
		t.Fatal(err)
	}
	if p, _ = st.PendingDetection(ctx, far, 10); len(p) != 1 {
		t.Fatal("late span did not re-queue detection")
	}
}

func TestStats(t *testing.T) {
	st := testutil.OpenStore(t)
	root := testutil.Span("t", "r", "", model.KindAgent, 0, 100)
	root.AgentName = "main"
	var spans []model.Span
	spans = append(spans, root)
	for i, status := range []string{"ok", "error", "ok", "ok"} {
		e := testutil.Span("t", string(rune('a'+i)), "r", model.KindTool, int64(i), int64(i)+1)
		e.ToolName, e.Target, e.Status, e.AgentName = "Edit", "/x.py", status, "main"
		spans = append(spans, e)
	}
	l := llm("t", "l", "r", 0, 1, 1000, 100, 0.25)
	l.Model, l.AgentName = "claude-sonnet-4-6", "main"
	spans = append(spans, l)
	if _, err := st.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}
	s, err := st.Stats(ctx, store.StatsFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals.Traces != 1 || s.Totals.Spans != 6 || s.Totals.CostUSD != 0.25 {
		t.Fatalf("totals %+v", s.Totals)
	}
	if len(s.ByTool) != 1 || s.ByTool[0].Calls != 4 || s.ByTool[0].ErrorRate != 0.25 {
		t.Fatalf("by_tool %+v", s.ByTool)
	}
	if len(s.Hotspots) != 1 || s.Hotspots[0].Target != "/x.py" || s.Hotspots[0].Calls != 4 {
		t.Fatalf("hotspots %+v", s.Hotspots)
	}
	if len(s.ByAgent) != 1 || s.ByAgent[0].Runs != 1 || s.ByAgent[0].ToolCalls != 4 {
		t.Fatalf("by_agent %+v", s.ByAgent)
	}
	// Scoped stats with a filter exercise the JOIN path.
	s, err = st.Stats(ctx, store.StatsFilter{SinceNS: 1, Source: ""})
	if err != nil || s.Totals.Traces != 1 || len(s.ByModel) != 1 {
		t.Fatalf("filtered stats %+v err %v", s, err)
	}
}
