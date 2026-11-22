package claudeimport

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xungirl/loupe/server/internal/assemble"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/testutil"
)

const fixtures = "testdata/projects"

func importAll(t *testing.T) (map[string][]model.Span, *Stats) {
	t.Helper()
	byTrace := map[string][]model.Span{}
	st, err := Import(fixtures, func(ss []model.Span) error {
		for _, s := range ss {
			byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return byTrace, st
}

func find(spans []model.Span, id string) *model.Span {
	for i := range spans {
		if spans[i].SpanID == id {
			return &spans[i]
		}
	}
	return nil
}

func TestParseStats(t *testing.T) {
	_, st := importAll(t)
	checks := map[string][2]int{
		"sessions":       {st.Sessions, 4},
		"files":          {st.Files, 6},
		"malformed":      {st.Malformed, 1},
		"truncated":      {st.Truncated, 1},
		"bad timestamp":  {st.BadTime, 1},
		"merged lines":   {st.MergedLines, 1},
		"missing result": {st.MissingResults, 1},
		"orphan result":  {st.OrphanResults, 1},
		"subagents":      {st.Subagents, 3},
		"linked by id":   {st.LinkedByID, 1},
		"by prompt":      {st.LinkedByPrompt, 1},
		"by time":        {st.LinkedByTime, 1},
		"unlinked":       {st.Unlinked, 0},
		"invalid spans":  {st.InvalidSpans, 0},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %d, want %d", name, c[0], c[1])
		}
	}
	if st.Skipped["summary"] != 1 || st.Skipped["file-history-snapshot"] != 1 || st.Skipped["system"] != 1 {
		t.Errorf("skipped types %v", st.Skipped)
	}
}

func TestBasicSession(t *testing.T) {
	traces, _ := importAll(t)
	spans := traces["cc-sess-basic"]
	if len(spans) != 9 {
		t.Fatalf("want 9 spans, got %d", len(spans))
	}
	root := find(spans, "root")
	if root.Name != "Fix the failing test in calc.py" || root.Source != "claude-code" {
		t.Fatalf("root %+v", root)
	}
	// Split assistant lines with the same message.id become ONE llm span whose
	// usage is the final line's (output 40, not 5+40).
	llm := find(spans, "llm:msg_1")
	if llm == nil || llm.OutputTokens != 40 || llm.InputTokens != 1200 || llm.CacheWriteTokens != 3000 {
		t.Fatalf("merged llm span %+v", llm)
	}
	if llm.DurationNS() != 4e9 { // from the user prompt (10:00:00) to the last line (10:00:04)
		t.Fatalf("llm duration %d", llm.DurationNS())
	}
	bash := find(spans, "tool:toolu_2")
	if bash.Status != model.StatusError || bash.Target != "pytest -q" || bash.ErrorMsg != "1 failed, 3 passed" || bash.DurationNS() != 3e9 {
		t.Fatalf("bash span %+v", bash)
	}
	edit := find(spans, "tool:toolu_3")
	if edit.Target != "/Users/dev/old/calc.py" || edit.ParentSpanID != "root" {
		t.Fatalf("edit span %+v", edit)
	}
	if find(spans, "user:u9") != nil {
		t.Fatal("isMeta messages are not user prompts")
	}
}

func TestInlineSidechainLinkedByPrompt(t *testing.T) {
	traces, _ := importAll(t)
	spans := traces["cc-sess-inline-sidechain"]
	sub := find(spans, "agent:side-s1")
	if sub == nil || sub.ParentSpanID != "tool:toolu_task" || sub.AgentName != "Explore" {
		t.Fatalf("subagent span %+v", sub)
	}
	grep := find(spans, "tool:toolu_grep")
	if grep.ParentSpanID != "agent:side-s1" || grep.AgentName != "Explore" {
		t.Fatalf("subagent tool not attributed: %+v", grep)
	}
}

func TestNestedSubagentsBecomeAgentTree(t *testing.T) {
	traces, _ := importAll(t)
	tr := assemble.Build(traces["cc-sess-new"])
	if tr.Orphans != 0 {
		t.Fatalf("orphans: %d", tr.Orphans)
	}
	parents := map[string]string{}
	for _, a := range tr.Agents {
		parents[a.Key] = a.ParentKey
	}
	if parents["rev1"] != "main" || parents["exp2"] != "rev1" {
		t.Fatalf("agent tree wrong: %v", parents)
	}
}

func TestBrokenSessionIsTolerated(t *testing.T) {
	traces, _ := importAll(t)
	spans := traces["cc-sess-broken"]
	never := find(spans, "tool:toolu_never")
	if never == nil || never.Attributes != `{"missing_result":true}` {
		t.Fatalf("missing-result tool span %+v", never)
	}
	apiErr := find(spans, "llm:m_b5")
	if apiErr == nil || apiErr.Status != model.StatusError || apiErr.Model != "" {
		t.Fatalf("api error span %+v", apiErr)
	}
	if find(spans, "user:b1") == nil {
		t.Fatal("prompt with an image block should still be a prompt")
	}
}

func TestReimportIsIdempotent(t *testing.T) {
	st := testutil.OpenStore(t)
	ctx := context.Background()
	sink := func(ss []model.Span) error { _, err := st.InsertSpans(ctx, ss); return err }
	if _, err := Import(fixtures, sink, nil); err != nil {
		t.Fatal(err)
	}
	_, n1, _ := st.Counts(ctx)
	if _, err := Import(fixtures, sink, nil); err != nil {
		t.Fatal(err)
	}
	_, n2, _ := st.Counts(ctx)
	if n1 == 0 || n1 != n2 {
		t.Fatalf("re-import changed span count %d → %d", n1, n2)
	}
}

func TestDiscoverSingleFileAndMissingDir(t *testing.T) {
	ss, err := Discover(filepath.Join(fixtures, "-Users-dev-old", "sess-basic.jsonl"))
	if err != nil || len(ss) != 1 || ss[0].SessionID != "sess-basic" {
		t.Fatalf("%v %v", ss, err)
	}
	if _, err := Discover("does/not/exist"); err == nil {
		t.Fatal("missing dir should error")
	}
}
