package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xungirl/loupe/server/internal/corpus"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

// ---------------------------------------------------------------------------
// loadgen: ingest throughput and backpressure.

type loadResult struct {
	Label            string  `json:"label"`
	Mode             string  `json:"mode"`
	Spans            int     `json:"spans"`
	Batch            int     `json:"batch"`
	Concurrency      int     `json:"concurrency"`
	Requests         int64   `json:"requests"`
	Rejected429      int64   `json:"rejected_429"`
	Errors           int64   `json:"errors"`
	AcceptedSpans    int64   `json:"accepted_spans"`
	SendSeconds      float64 `json:"send_seconds"`      // until the last response
	CommitSeconds    float64 `json:"commit_seconds"`    // until the server wrote everything accepted
	AcceptedPerSec   float64 `json:"accepted_spans_per_sec"`
	CommittedPerSec  float64 `json:"committed_spans_per_sec"`
	WrittenOnServer  int64   `json:"written_on_server"`
	ReqP50MS         float64 `json:"request_p50_ms"`
	ReqP99MS         float64 `json:"request_p99_ms"`
	MaxQueueDepthObs float64 `json:"max_queue_depth_observed"`
}

// metricValue scrapes one un-labelled metric from /metrics.
func metricValue(base, name string) (float64, error) {
	resp, err := http.Get(base + "/metrics")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, name+" ") {
			return strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, name)), 64)
		}
	}
	return 0, fmt.Errorf("metric %s not found", name)
}

func runLoadgen(args []string) error {
	fs := flag.NewFlagSet("loadgen", flag.ExitOnError)
	base := fs.String("url", "http://127.0.0.1:7070", "server")
	mode := fs.String("mode", "batched", "batched | single (one span per request)")
	total := fs.Int("spans", 200_000, "spans to send")
	batch := fs.Int("batch", 500, "spans per request in batched mode")
	conc := fs.Int("concurrency", 8, "parallel clients")
	retry := fs.Bool("retry", true, "retry 429s (false = count them and move on, for overload tests)")
	label := fs.String("label", "", "label stored in the result")
	out := fs.String("out", "", "append the JSON result to this file")
	_ = fs.Parse(args)
	if *mode == "single" {
		*batch = 1
	}

	// Pre-encode every request body so the client's JSON cost isn't measured.
	nReq := (*total + *batch - 1) / *batch
	bodies := make([][]byte, nReq)
	now := time.Now().UnixNano()
	run := fmt.Sprintf("lg%d", now%1e9)
	for r := 0; r < nReq; r++ {
		spans := make([]model.Span, 0, *batch)
		for i := 0; i < *batch && r**batch+i < *total; i++ {
			n := r**batch + i
			sp := model.Span{TraceID: fmt.Sprintf("%s-t%d", run, n/100), SpanID: fmt.Sprintf("s%d", n),
				Kind: model.KindTool, Name: "Read", ToolName: "Read", Target: fmt.Sprintf("src/f%d.py", n%50),
				ToolInput: `{"file_path":"src/x.py"}`, ToolOutput: "def f(): return 1", StartNS: now + int64(n)*1000, EndNS: now + int64(n)*1000 + 500}
			if n%100 != 0 {
				sp.ParentSpanID = fmt.Sprintf("s%d", (n/100)*100)
			} else {
				sp.Kind, sp.ToolName, sp.Name = model.KindAgent, "", "agent"
			}
			spans = append(spans, sp)
		}
		bodies[r], _ = json.Marshal(map[string]any{"spans": spans})
	}
	written0, err := metricValue(*base, "loupe_ingest_written_spans_total")
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}

	res := loadResult{Label: *label, Mode: *mode, Spans: *total, Batch: *batch, Concurrency: *conc}
	var requests, rejected, errs, accepted atomic.Int64
	lat := make([][]float64, *conc)
	jobs := make(chan int, nReq)
	for i := range bodies {
		jobs <- i
	}
	close(jobs)
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: *conc * 2}, Timeout: 30 * time.Second}

	// Sample the server's queue depth while the load runs.
	stopSample := make(chan struct{})
	var maxDepth float64
	var sampleWG sync.WaitGroup
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopSample:
				return
			case <-t.C:
				if d, err := metricValue(*base, "loupe_ingest_queue_depth"); err == nil && d > maxDepth {
					maxDepth = d
				}
			}
		}
	}()

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < *conc; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range jobs {
				for attempt := 0; ; attempt++ {
					t0 := time.Now()
					resp, err := client.Post(*base+"/api/v1/spans", "application/json", bytes.NewReader(bodies[i]))
					requests.Add(1)
					if err != nil {
						errs.Add(1)
						break
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					lat[w] = append(lat[w], float64(time.Since(t0).Microseconds())/1000)
					if resp.StatusCode == http.StatusTooManyRequests {
						rejected.Add(1)
						if *retry && attempt < 1000 {
							time.Sleep(time.Duration(min(attempt+1, 20)) * 5 * time.Millisecond)
							continue
						}
						break
					}
					if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
						accepted.Add(int64(bytes.Count(bodies[i], []byte(`"span_id"`))))
					} else {
						errs.Add(1)
					}
					break
				}
			}
		}(w)
	}
	wg.Wait()
	res.SendSeconds = time.Since(start).Seconds()

	// Wait for the server to commit everything it accepted.
	want := written0 + float64(accepted.Load())
	deadline := time.Now().Add(2 * time.Minute)
	var written float64
	for time.Now().Before(deadline) {
		written, _ = metricValue(*base, "loupe_ingest_written_spans_total")
		if written >= want {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	res.CommitSeconds = time.Since(start).Seconds()
	close(stopSample)
	sampleWG.Wait()

	var all []float64
	for _, l := range lat {
		all = append(all, l...)
	}
	sort.Float64s(all)
	res.Requests, res.Rejected429, res.Errors, res.AcceptedSpans = requests.Load(), rejected.Load(), errs.Load(), accepted.Load()
	res.AcceptedPerSec = float64(res.AcceptedSpans) / res.SendSeconds
	res.CommittedPerSec = float64(res.AcceptedSpans) / res.CommitSeconds
	res.WrittenOnServer = int64(written - written0)
	res.ReqP50MS, res.ReqP99MS = pct(all, 50), pct(all, 99)
	res.MaxQueueDepthObs = maxDepth
	b, _ := json.Marshal(res)
	fmt.Println(string(b))
	if res.WrittenOnServer != res.AcceptedSpans {
		fmt.Fprintf(os.Stderr, "WARNING: accepted %d spans but the server wrote %d\n", res.AcceptedSpans, res.WrittenOnServer)
	}
	if *out != "" {
		return appendLine(*out, b)
	}
	return nil
}

func appendLine(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p / 100)
	return sorted[i]
}

// ---------------------------------------------------------------------------
// bench-query: build a large database and measure query/assembly latency.

func runBenchQuery(args []string) error {
	fs := flag.NewFlagSet("bench-query", flag.ExitOnError)
	dbPath := fs.String("db", "data/bench-1m.db", "database (created with -generate)")
	generate := fs.Int("generate", 0, "first fill the DB with this many spans (synthetic corpus traces)")
	base := fs.String("url", "", "server serving -db; if empty only -generate runs")
	n := fs.Int("n", 300, "requests per endpoint")
	label := fs.String("label", "", "label")
	out := fs.String("out", "", "append JSON result here")
	_ = fs.Parse(args)

	if *generate > 0 {
		if err := generateBenchDB(*dbPath, *generate); err != nil {
			return err
		}
	}
	if *base == "" {
		return nil
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	traces, total, err := st.ListTraces(context.Background(), store.TraceFilter{Limit: 500, Sort: "spans"})
	if err != nil {
		return err
	}
	var ids []string
	r := rand.New(rand.NewSource(1))
	all, _, _ := st.ListTraces(context.Background(), store.TraceFilter{Limit: 500, Offset: r.Intn(max(1, total-500))})
	for _, t := range all {
		ids = append(ids, t.TraceID)
	}
	biggest := traces[0]
	_, spanCount, _ := st.Counts(context.Background())
	st.Close()

	endpoints := []struct {
		name string
		path func(i int) string
	}{
		{"get_trace_random (assembly)", func(i int) string { return "/api/v1/traces/" + ids[i%len(ids)] }},
		{fmt.Sprintf("get_trace_largest (%d spans)", biggest.SpanCount), func(int) string { return "/api/v1/traces/" + biggest.TraceID }},
		{"list_traces_page", func(i int) string { return fmt.Sprintf("/api/v1/traces?limit=50&offset=%d", (i*50)%max(1, total-50)) }},
		{"list_traces_has_error_sort_cost", func(int) string { return "/api/v1/traces?has_error=true&sort=cost&limit=50" }},
		{"list_traces_by_tool", func(int) string { return "/api/v1/traces?tool=Bash&limit=50" }},
		{"search_failed_bash_spans", func(int) string { return "/api/v1/spans?tool=Bash&error=true&limit=100" }},
		{"findings_list", func(int) string { return "/api/v1/findings?limit=200" }},
		{"stats_full_aggregation", func(int) string { return "/api/v1/stats" }},
	}
	type epResult struct {
		Endpoint string  `json:"endpoint"`
		N        int     `json:"n"`
		P50MS    float64 `json:"p50_ms"`
		P95MS    float64 `json:"p95_ms"`
		P99MS    float64 `json:"p99_ms"`
		MaxMS    float64 `json:"max_ms"`
		Bytes    int     `json:"response_bytes_last"`
	}
	result := struct {
		Label    string     `json:"label"`
		Spans    int64      `json:"spans"`
		Traces   int        `json:"traces"`
		Results  []epResult `json:"results"`
		Measured string     `json:"measured"`
	}{Label: *label, Spans: spanCount, Traces: total, Measured: "sequential requests (concurrency 1) over HTTP on localhost, after 20 warm-up requests per endpoint"}

	client := &http.Client{Timeout: 60 * time.Second}
	for _, ep := range endpoints {
		reqs := *n
		if strings.HasPrefix(ep.name, "stats") || strings.HasPrefix(ep.name, "get_trace_largest") || strings.Contains(ep.name, "by_tool") || strings.Contains(ep.name, "search_failed") {
			reqs = max(20, *n/10) // the heavy ones
		}
		var lat []float64
		size := 0
		for i := -20; i < reqs; i++ {
			t0 := time.Now()
			resp, err := client.Get(*base + ep.path(max(i, 0)))
			if err != nil {
				return err
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("%s: HTTP %d %s", ep.name, resp.StatusCode, b)
			}
			if i >= 0 {
				lat = append(lat, float64(time.Since(t0).Microseconds())/1000)
				size = len(b)
			}
		}
		sort.Float64s(lat)
		e := epResult{Endpoint: ep.name, N: len(lat), P50MS: pct(lat, 50), P95MS: pct(lat, 95), P99MS: pct(lat, 99), MaxMS: lat[len(lat)-1], Bytes: size}
		result.Results = append(result.Results, e)
		fmt.Fprintf(os.Stderr, "%-38s n=%-4d p50=%8.2fms p95=%8.2fms p99=%8.2fms max=%8.2fms\n", e.Endpoint, e.N, e.P50MS, e.P95MS, e.P99MS, e.MaxMS)
	}
	b, _ := json.Marshal(result)
	fmt.Println(string(b))
	if *out != "" {
		return appendLine(*out, b)
	}
	return nil
}

// generateBenchDB fills a database with corpus traces until it holds ~target spans.
func generateBenchDB(path string, target int) error {
	for _, s := range []string{"", "-wal", "-shm"} {
		os.Remove(path + s)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()
	start := time.Now()
	base := time.Now().Add(-30 * 24 * time.Hour).UnixNano()
	written, seed := 0, int64(1)
	var batch []model.Span
	for written < target {
		ts := corpus.Generate(200, seed, base, int64(30*24*time.Hour))
		for i := range ts {
			for j := range ts[i].Spans {
				ts[i].Spans[j].TraceID = fmt.Sprintf("b%d-%s", seed, ts[i].TraceID)
			}
			batch = append(batch, ts[i].Spans...)
			written += len(ts[i].Spans)
		}
		seed++
		if _, err := st.InsertSpans(context.Background(), batch); err != nil {
			return err
		}
		batch = batch[:0]
	}
	// One very large trace (10k spans) to measure worst-case assembly.
	big := make([]model.Span, 0, 10_000)
	now := time.Now().UnixNano()
	big = append(big, model.Span{TraceID: "big-trace", SpanID: "root", Kind: model.KindAgent, Name: "huge session", StartNS: now, EndNS: now + 1e12})
	for i := 1; i < 10_000; i++ {
		big = append(big, model.Span{TraceID: "big-trace", SpanID: fmt.Sprintf("s%d", i), ParentSpanID: "root", Kind: model.KindTool,
			ToolName: "Bash", Name: "Bash", StartNS: now + int64(i)*1e8, EndNS: now + int64(i)*1e8 + 5e7})
	}
	if _, err := st.InsertSpans(context.Background(), big); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "generated %d spans in %s (%.0f spans/s bulk insert, single process, no HTTP)\n",
		written+len(big), time.Since(start).Round(time.Millisecond), float64(written+len(big))/time.Since(start).Seconds())
	return nil
}

var _ = errors.New
