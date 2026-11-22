package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xungirl/loupe/server/internal/cost"
	"github.com/xungirl/loupe/server/internal/ingest"
	"github.com/xungirl/loupe/server/internal/live"
	"github.com/xungirl/loupe/server/internal/metrics"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
	"github.com/xungirl/loupe/server/internal/testutil"
)

type gatedStore struct {
	*store.Store
	gate chan struct{}
}

func (g *gatedStore) InsertSpans(ctx context.Context, s []model.Span) ([]model.Span, error) {
	<-g.gate
	return g.Store.InsertSpans(ctx, s)
}

// newTestServer wires a real store + pipeline. If gate is non-nil the writer
// blocks on it, which lets tests fill the queue deterministically.
func newTestServer(t *testing.T, queue int, gate chan struct{}) (*httptest.Server, *Server) {
	t.Helper()
	st := testutil.OpenStore(t)
	broker := live.NewBroker(64)
	var w ingest.Writer = st
	if gate != nil {
		w = &gatedStore{st, gate}
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := ingest.New(ingest.Config{QueueSpans: queue, MaxBatch: 500, FlushInterval: 2 * time.Millisecond}, w, broker.Publish, quiet)
	done := make(chan struct{})
	s := &Server{Store: st, Pipeline: p, Broker: broker, Prices: cost.Default(), Metrics: metrics.NewRegistry(),
		Log: quiet, Done: done, Now: func() time.Time { return time.Unix(0, testutil.Base).Add(time.Hour) }}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		close(done)
		ts.Close()
		if gate != nil {
			select {
			case <-gate:
			default:
				close(gate)
			}
		}
		p.Close()
	})
	return ts, s
}

func post(t *testing.T, url, body string, hdr ...string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func get(t *testing.T, url string, out any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func spanJSON(trace, id, parent, kind string, startMS int64, extra string) string {
	return fmt.Sprintf(`{"trace_id":%q,"span_id":%q,"parent_span_id":%q,"kind":%q,"start_ns":%d,"end_ns":%d%s}`,
		trace, id, parent, kind, testutil.Base+startMS*1e6, testutil.Base+(startMS+5)*1e6, extra)
}

func TestIngestThenQueryTrace(t *testing.T) {
	ts, _ := newTestServer(t, 1000, nil)
	// Child arrives before parent, across two requests.
	child := spanJSON("tr", "c", "root", "llm", 10, `,"model":"claude-sonnet-4-6","input_tokens":1000000,"output_tokens":0`)
	resp, body := post(t, ts.URL+"/api/v1/spans?wait=true", `{"spans":[`+child+`]}`)
	if resp.StatusCode != 200 || body["accepted"].(float64) != 1 {
		t.Fatalf("ingest: %d %v", resp.StatusCode, body)
	}
	root := spanJSON("tr", "root", "", "agent", 0, `,"name":"my session","source":"sdk"`)
	resp, _ = post(t, ts.URL+"/api/v1/spans?wait=true", `[`+root+`]`) // bare array form
	if resp.StatusCode != 200 {
		t.Fatalf("ingest root: %d", resp.StatusCode)
	}

	var tr struct {
		Summary store.TraceSummary `json:"summary"`
		Spans   []struct {
			Span  model.Span `json:"span"`
			Depth int        `json:"depth"`
		} `json:"spans"`
		Orphans int `json:"orphans"`
	}
	if code := get(t, ts.URL+"/api/v1/traces/tr", &tr); code != 200 {
		t.Fatalf("get trace: %d", code)
	}
	if len(tr.Spans) != 2 || tr.Spans[0].Span.SpanID != "root" || tr.Spans[1].Depth != 1 || tr.Orphans != 0 {
		t.Fatalf("assembly wrong: %+v", tr)
	}
	// The price table filled cost: 1M input tokens of sonnet-4-6 = $3.
	if tr.Summary.CostUSD != 3 || tr.Summary.Name != "my session" {
		t.Fatalf("summary %+v", tr.Summary)
	}
	if code := get(t, ts.URL+"/api/v1/traces/nope", nil); code != 404 {
		t.Fatalf("missing trace: %d", code)
	}
	var list struct {
		Total int `json:"total"`
	}
	get(t, ts.URL+"/api/v1/traces?source=sdk", &list)
	if list.Total != 1 {
		t.Fatalf("list total %d", list.Total)
	}
}

func TestMalformedInput(t *testing.T) {
	ts, _ := newTestServer(t, 1000, nil)
	cases := []struct{ body, why string }{
		{`{"spans": [`, "truncated JSON"},
		{`"hello"`, "wrong top-level type"},
		{`{"spans":[{"trace_id":"t"}]}`, "missing span_id"},
		{`{"spans":[` + spanJSON("t", "s", "", "wizard", 0, "") + `]}`, "unknown kind"},
	}
	for _, c := range cases {
		resp, _ := post(t, ts.URL+"/api/v1/spans", c.body)
		if resp.StatusCode != 400 {
			t.Errorf("%s: got %d, want 400", c.why, resp.StatusCode)
		}
	}
	// Partial success: one good span, one bad → 202 with rejected=1.
	resp, body := post(t, ts.URL+"/api/v1/spans", `{"spans":[`+spanJSON("t", "ok", "", "tool", 0, "")+`,{"span_id":"x"}]}`)
	if resp.StatusCode != 202 || body["accepted"].(float64) != 1 || body["rejected"].(float64) != 1 {
		t.Fatalf("partial: %d %v", resp.StatusCode, body)
	}
	resp, _ = post(t, ts.URL+"/v1/traces", `{}`, "Content-Type", "application/x-protobuf")
	if resp.StatusCode != 415 {
		t.Fatalf("protobuf: %d", resp.StatusCode)
	}
}

func TestBodyLimitAndGzip(t *testing.T) {
	ts, s := newTestServer(t, 1000, nil)
	s.MaxBodySize = 1024
	big := `{"spans":[` + spanJSON("t", "s", "", "tool", 0, `,"tool_input":"`+strings.Repeat("x", 2000)+`"`) + `]}`
	if resp, _ := post(t, ts.URL+"/api/v1/spans", big); resp.StatusCode != 413 {
		t.Fatalf("oversized body: %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte(`{"spans":[` + spanJSON("gz", "s", "", "tool", 0, "") + `]}`))
	gz.Close()
	resp, body := post(t, ts.URL+"/api/v1/spans?wait=true", buf.String(), "Content-Encoding", "gzip")
	if resp.StatusCode != 200 || body["accepted"].(float64) != 1 {
		t.Fatalf("gzip: %d %v", resp.StatusCode, body)
	}
}

func TestBackpressure429(t *testing.T) {
	gate := make(chan struct{})
	ts, s := newTestServer(t, 3, gate)
	three := `{"spans":[` + spanJSON("t", "a", "", "tool", 0, "") + "," + spanJSON("t", "b", "", "tool", 0, "") + "," + spanJSON("t", "c", "", "tool", 0, "") + `]}`
	if resp, _ := post(t, ts.URL+"/api/v1/spans", three); resp.StatusCode != 202 {
		t.Fatalf("first: %d", resp.StatusCode)
	}
	resp, _ := post(t, ts.URL+"/api/v1/spans", `{"spans":[`+spanJSON("t", "d", "", "tool", 0, "")+`]}`)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("want 429 with Retry-After, got %d", resp.StatusCode)
	}
	close(gate)
	deadline := time.Now().Add(2 * time.Second)
	for s.Pipeline.QueueDepth() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if resp, _ := post(t, ts.URL+"/api/v1/spans?wait=true", `{"spans":[`+spanJSON("t", "d", "", "tool", 0, "")+`]}`); resp.StatusCode != 200 {
		t.Fatalf("after drain: %d", resp.StatusCode)
	}
	// /metrics reports the rejection.
	mresp, _ := http.Get(ts.URL + "/metrics")
	mb, _ := io.ReadAll(mresp.Body)
	mresp.Body.Close()
	if !strings.Contains(string(mb), `loupe_http_requests_total{route="ingest",code="Too Many Requests"} 1`) {
		t.Fatalf("metrics missing 429 counter:\n%s", mb)
	}
}

func TestOTLPEndpoint(t *testing.T) {
	ts, _ := newTestServer(t, 1000, nil)
	body := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"ab","spanId":"cd","name":"chat",
	  "startTimeUnixNano":"1767225600000000000","endTimeUnixNano":"1767225601000000000",
	  "attributes":[{"key":"gen_ai.operation.name","value":{"stringValue":"chat"}},
	                {"key":"gen_ai.request.model","value":{"stringValue":"qwen3.8-flash"}},
	                {"key":"gen_ai.usage.input_tokens","value":{"intValue":"1000000"}}]}]}]}]}`
	resp, _ := post(t, ts.URL+"/v1/traces?wait=true", body)
	if resp.StatusCode != 200 {
		t.Fatalf("otlp ingest %d", resp.StatusCode)
	}
	var tr struct {
		Summary store.TraceSummary `json:"summary"`
	}
	get(t, ts.URL+"/api/v1/traces/ab", &tr)
	// ¥0.8 per 1M input tokens × default 0.14 USD/CNY.
	if tr.Summary.LLMCalls != 1 || tr.Summary.CostUSD < 0.111 || tr.Summary.CostUSD > 0.113 {
		t.Fatalf("otlp summary %+v", tr.Summary)
	}
}

func TestLiveTailSSE(t *testing.T) {
	ts, _ := newTestServer(t, 1000, nil)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/live?trace_id=live1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %s", ct)
	}
	rd := bufio.NewReader(resp.Body)
	line, _ := rd.ReadString('\n')
	if !strings.HasPrefix(line, "event: hello") {
		t.Fatalf("first event %q", line)
	}
	rd.ReadString('\n') // hello's data line
	rd.ReadString('\n') // blank separator
	// One span for another trace (filtered out) and one for ours.
	post(t, ts.URL+"/api/v1/spans?wait=true", `{"spans":[`+spanJSON("other", "x", "", "tool", 0, "")+`,`+spanJSON("live1", "y", "", "tool", 0, "")+`]}`)
	got := make(chan string, 1)
	go func() {
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(l, "data: {") {
				got <- l
				return
			}
		}
	}()
	select {
	case l := <-got:
		if !strings.Contains(l, `"span_id":"y"`) {
			t.Fatalf("wrong span streamed: %s", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no span received over SSE")
	}
}

func TestStatsAndFindingsEndpoints(t *testing.T) {
	ts, s := newTestServer(t, 1000, nil)
	post(t, ts.URL+"/api/v1/spans?wait=true", `{"spans":[`+spanJSON("t", "r", "", "agent", 0, "")+`,`+
		spanJSON("t", "x", "r", "tool", 1, `,"tool_name":"Bash","status":"error"`)+`]}`)
	if err := s.Store.ReplaceFindings(context.Background(), "t", 1, []store.Finding{{Detector: "error_streak", Severity: "warn", Summary: "x"}}); err != nil {
		t.Fatal(err)
	}
	var st store.Stats
	if code := get(t, ts.URL+"/api/v1/stats?since=48h", &st); code != 200 || st.Totals.Errors != 1 || len(st.ByTool) != 1 {
		t.Fatalf("stats %d %+v", code, st)
	}
	var fs struct{ Findings []store.Finding }
	get(t, ts.URL+"/api/v1/findings?detector=error_streak", &fs)
	if len(fs.Findings) != 1 {
		t.Fatalf("findings %+v", fs)
	}
	var sp struct{ Spans []model.Span }
	get(t, ts.URL+"/api/v1/spans?tool=Bash&error=true", &sp)
	if len(sp.Spans) != 1 {
		t.Fatalf("span search %+v", sp)
	}
	var list struct{ Total int }
	get(t, ts.URL+"/api/v1/traces?detector=error_streak", &list)
	if list.Total != 1 {
		t.Fatalf("detector filter total %d", list.Total)
	}
}
