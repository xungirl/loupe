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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xungirl/loupe/server/internal/claudeimport"
	"github.com/xungirl/loupe/server/internal/cost"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

// openSink returns a function that writes spans either straight into a SQLite
// database (offline, fastest) or to a running server over HTTP.
func openSink(dbPath, url string) (claudeimport.Sink, func(), error) {
	if url != "" {
		return func(spans []model.Span) error { return postSpans(url, spans) }, func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	sink := func(spans []model.Span) error {
		_, err := st.InsertSpans(context.Background(), spans)
		return err
	}
	return sink, func() { st.Close() }, nil
}

// postSpans sends one batch, retrying on 429 as the server asks.
func postSpans(url string, spans []model.Span) error {
	body, err := json.Marshal(map[string]any{"spans": spans})
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 20; attempt++ {
		resp, err := http.Post(strings.TrimRight(url, "/")+"/api/v1/spans", "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		msg, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
			continue
		case resp.StatusCode >= 300:
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, msg)
		}
		return nil
	}
	return errors.New("server kept returning 429")
}

func runImportClaude(args []string) error {
	fs := flag.NewFlagSet("import-claude", flag.ExitOnError)
	home, _ := os.UserHomeDir()
	dir := fs.String("dir", filepath.Join(home, ".claude", "projects"), "Claude Code projects directory (or one .jsonl file)")
	dbPath := fs.String("db", "data/local.db", "SQLite database to write (ignored with -url)")
	url := fs.String("url", "", "send to a running Loupe server instead, e.g. http://127.0.0.1:7070")
	pricesPath := fs.String("prices", "", "price table JSON (default: built-in)")
	_ = fs.Parse(args)

	prices := cost.Default()
	if *pricesPath != "" {
		var err error
		if prices, err = cost.Load(*pricesPath); err != nil {
			return err
		}
	}
	sink, closeFn, err := openSink(*dbPath, *url)
	if err != nil {
		return err
	}
	defer closeFn()

	start := time.Now()
	unpriced := 0
	st, err := claudeimport.Import(*dir, sink, func(ss []model.Span) int { n := prices.Apply(ss); unpriced += n; return n })
	if st != nil {
		elapsed := time.Since(start)
		out, _ := json.MarshalIndent(st, "", "  ")
		fmt.Fprintf(os.Stderr, "parse stats:\n%s\n", out)
		fmt.Fprintf(os.Stderr, "imported %d spans from %d sessions (%.1f MB) in %s; %d LLM spans had a model missing from the price table\n",
			st.Spans, st.Sessions, float64(st.Bytes)/1e6, elapsed.Round(time.Millisecond), unpriced)
		if *url == "" {
			fmt.Fprintf(os.Stderr, "database: %s  →  view it with: bin/loupe serve -db %s -web web/dist\n", *dbPath, *dbPath)
		}
	}
	return err
}

// runIngestFile loads a JSONL file of spans (one model.Span per line), e.g.
// what the Python SDK's file exporter writes.
func runIngestFile(args []string) error {
	fs := flag.NewFlagSet("ingest-file", flag.ExitOnError)
	dbPath := fs.String("db", "data/loupe.db", "SQLite database to write")
	url := fs.String("url", "", "send to a running server instead")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		return errors.New("usage: loupe ingest-file [-db path | -url http://...] spans.jsonl...")
	}
	sink, closeFn, err := openSink(*dbPath, *url)
	if err != nil {
		return err
	}
	defer closeFn()
	prices := cost.Default()
	total, bad := 0, 0
	for _, path := range fs.Args() {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		rd := bufio.NewReaderSize(f, 1<<20)
		var batch []model.Span
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			prices.Apply(batch)
			err := sink(batch)
			total += len(batch)
			batch = batch[:0]
			return err
		}
		now := time.Now().UnixNano()
		for {
			line, rerr := rd.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				var sp model.Span
				if json.Unmarshal(line, &sp) != nil || sp.Normalize(now) != nil {
					bad++
				} else {
					if sp.Target == "" && sp.Kind == model.KindTool {
						sp.Target = model.ExtractTarget(sp.ToolInput)
					}
					batch = append(batch, sp)
					if len(batch) >= 5000 {
						if err := flush(); err != nil {
							f.Close()
							return err
						}
					}
				}
			}
			if rerr != nil {
				break
			}
		}
		f.Close()
		if err := flush(); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "ingested %d spans (%d invalid lines skipped)\n", total, bad)
	return nil
}
