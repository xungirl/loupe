package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/xungirl/loupe/server/internal/corpus"
	"github.com/xungirl/loupe/server/internal/detect"
	"github.com/xungirl/loupe/server/internal/store"
)

// traceDetector runs detectors for a stored trace and saves the findings.
type traceDetector struct {
	st  *store.Store
	cfg detect.Config
}

func newTraceDetector(st *store.Store) *traceDetector {
	return &traceDetector{st: st, cfg: detect.DefaultConfig()}
}

func (d *traceDetector) run(ctx context.Context, t store.TraceSummary) error {
	_, err := d.detect(ctx, t)
	return err
}

func (d *traceDetector) detect(ctx context.Context, t store.TraceSummary) ([]store.Finding, error) {
	spans, err := d.st.TraceSpans(ctx, t.TraceID)
	if err != nil {
		return nil, err
	}
	fs := detect.Run(spans, d.cfg)
	if err := d.st.ReplaceFindings(ctx, t.TraceID, t.LastIngestNS, fs); err != nil {
		return nil, err
	}
	return fs, nil
}

func (d *traceDetector) httpHandler(r *http.Request, id string) ([]store.Finding, error) {
	t, err := d.st.GetTrace(r.Context(), id)
	if err != nil {
		return nil, err
	}
	return d.detect(r.Context(), t)
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readCorpus(path string) ([]corpus.Trace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []corpus.Trace
	rd := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 1 {
			var t corpus.Trace
			if jerr := json.Unmarshal(line, &t); jerr != nil {
				return nil, jerr
			}
			out = append(out, t)
		}
		if err != nil {
			break
		}
	}
	return out, nil
}

func runCorpus(args []string) error {
	fs := flag.NewFlagSet("corpus", flag.ExitOnError)
	n := fs.Int("n", 300, "number of traces")
	seed := fs.Int64("seed", 42, "random seed")
	out := fs.String("out", "results/corpus/corpus.jsonl", "output JSONL")
	_ = fs.Parse(args)
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixNano()
	traces := corpus.Generate(*n, *seed, base, int64(14*24*time.Hour))
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	counts := map[string]int{}
	splits := map[string]int{}
	spans := 0
	for _, t := range traces {
		b, _ := json.Marshal(t)
		w.Write(append(b, '\n'))
		splits[t.Split]++
		spans += len(t.Spans)
		if len(t.Labels) == 0 {
			counts["clean"]++
		}
		for _, l := range t.Labels {
			counts[l]++
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %d traces (%d spans) to %s\nlabels: %v\nsplits: %v\n", len(traces), spans, *out, counts, splits)
	return nil
}

func runEvalDetectors(args []string) error {
	fs := flag.NewFlagSet("eval-detectors", flag.ExitOnError)
	in := fs.String("corpus", "results/corpus/corpus.jsonl", "corpus JSONL")
	split := fs.String("split", "train", "train | test | all  (thresholds are tuned on train only)")
	out := fs.String("out", "", "write the JSON report here")
	_ = fs.Parse(args)
	if *split != "train" && *split != "test" && *split != "all" {
		return errors.New("split must be train, test or all")
	}
	traces, err := readCorpus(*in)
	if err != nil {
		return err
	}
	rep := detect.Evaluate(traces, *split, detect.DefaultConfig())
	fmt.Printf("split=%s traces=%d clean=%d\n%-13s %4s %4s %4s %4s  %9s %6s %6s\n", rep.Split, rep.Traces, rep.Clean,
		"detector", "TP", "FP", "FN", "TN", "precision", "recall", "F1")
	for _, s := range rep.Scores {
		fmt.Printf("%-13s %4d %4d %4d %4d  %9.3f %6.3f %6.3f\n", s.Detector, s.TP, s.FP, s.FN, s.TN, s.Precision, s.Recall, s.F1)
	}
	fmt.Printf("macro-F1 %.3f\n", rep.MacroF1)
	for _, s := range rep.Scores {
		if len(s.MissedVariants) > 0 || len(s.FPFromLabels) > 0 {
			fmt.Printf("  %s: missed=%v fp_from=%v\n", s.Detector, s.MissedVariants, s.FPFromLabels)
		}
	}
	if *out != "" {
		return writeJSONFile(*out, rep)
	}
	return nil
}

// runDetectAll recomputes findings for every trace in a database.
func runDetectAll(args []string) error {
	fs := flag.NewFlagSet("detect", flag.ExitOnError)
	dbPath := fs.String("db", "data/loupe.db", "database")
	_ = fs.Parse(args)
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	d := newTraceDetector(st)
	ctx := context.Background()
	total, findings := 0, 0
	for offset := 0; ; offset += 500 {
		ts, _, err := st.ListTraces(ctx, store.TraceFilter{Limit: 500, Offset: offset})
		if err != nil {
			return err
		}
		for _, t := range ts {
			fs, err := d.detect(ctx, t)
			if err != nil {
				return err
			}
			total++
			findings += len(fs)
		}
		if len(ts) < 500 {
			break
		}
	}
	fmt.Fprintf(os.Stderr, "ran detectors on %d traces: %d findings\n", total, findings)
	return nil
}
