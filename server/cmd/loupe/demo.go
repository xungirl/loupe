package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/xungirl/loupe/server/internal/claudeimport"
	"github.com/xungirl/loupe/server/internal/corpus"
	"github.com/xungirl/loupe/server/internal/cost"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

// runDemoSeed builds a self-contained demo database:
//   - synthetic agent traces from the fault-injection corpus (labelled faults),
//   - the synthetic Claude Code transcripts used by the importer tests,
//   - any span files passed with -spans (e.g. fake-provider agent runs),
//
// then runs the detectors over everything.
func runDemoSeed(args []string) error {
	fs := flag.NewFlagSet("demo-seed", flag.ExitOnError)
	dbPath := fs.String("db", "data/demo.db", "database to (re)create")
	n := fs.Int("traces", 120, "synthetic corpus traces to include")
	claudeDir := fs.String("claude", "server/internal/claudeimport/testdata/projects", "synthetic Claude Code transcripts")
	var spanFiles multiFlag
	fs.Var(&spanFiles, "spans", "extra spans JSONL file (repeatable)")
	_ = fs.Parse(args)

	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(*dbPath + suffix)
	}
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return err
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	prices := cost.Default()

	// Spread the corpus over the last 10 days so the UI's time views look alive.
	base := time.Now().Add(-10 * 24 * time.Hour).UnixNano()
	traces := corpus.Generate(*n, 2026, base, int64(10*24*time.Hour))
	spans := 0
	for _, t := range traces {
		prices.Apply(t.Spans)
		if _, err := st.InsertSpans(ctx, t.Spans); err != nil {
			return err
		}
		spans += len(t.Spans)
	}
	fmt.Fprintf(os.Stderr, "demo: %d synthetic traces (%d spans)\n", len(traces), spans)

	if *claudeDir != "" {
		cst, err := claudeimport.Import(*claudeDir, func(ss []model.Span) error {
			_, err := st.InsertSpans(ctx, ss)
			return err
		}, prices.Apply)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "demo: %d Claude Code sessions from %s\n", cst.Sessions, *claudeDir)
	}
	for _, f := range spanFiles {
		if err := runIngestFile([]string{"-db", *dbPath, f}); err != nil {
			return err
		}
	}
	return runDetectAll([]string{"-db", *dbPath})
}

type multiFlag []string

func (m *multiFlag) String() string     { return fmt.Sprint(*m) }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// runDemoLive streams a simulated agent run every few seconds so the Live page
// has something to show in the demo. Spans are sent when they "finish",
// with simulated time compressed 8x.
func runDemoLive(ctx context.Context, submit func([]model.Span) error) {
	const speedup = 8
	for i := 0; ; i++ {
		now := time.Now().UnixNano()
		t := corpus.Generate(1, time.Now().UnixNano(), now, 0)[0]
		id := fmt.Sprintf("live-%s-%d", time.Now().Format("150405"), i)
		for j := range t.Spans {
			sp := &t.Spans[j]
			sp.TraceID, sp.Source = id, "demo-live"
			sp.StartNS = now + (sp.StartNS-now)/speedup
			sp.EndNS = now + (sp.EndNS-now)/speedup
		}
		spans := t.Spans
		sort.SliceStable(spans, func(a, b int) bool { return spans[a].EndNS < spans[b].EndNS })
		// The root span goes first so the trace has a name immediately.
		for j := range spans {
			if spans[j].SpanID == "root" {
				root := spans[j]
				_ = submit([]model.Span{root})
				break
			}
		}
		for _, sp := range spans {
			if sp.SpanID == "root" {
				continue
			}
			if wait := time.Until(time.Unix(0, sp.EndNS)); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(min(wait, 2*time.Second)):
				}
			}
			_ = submit([]model.Span{sp})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(4 * time.Second):
		}
	}
}
