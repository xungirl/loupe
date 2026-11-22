package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/xungirl/loupe/server/internal/api"
	"github.com/xungirl/loupe/server/internal/cost"
	"github.com/xungirl/loupe/server/internal/ingest"
	"github.com/xungirl/loupe/server/internal/jobs"
	"github.com/xungirl/loupe/server/internal/live"
	"github.com/xungirl/loupe/server/internal/metrics"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7070", "listen address")
	dbPath := fs.String("db", "data/loupe.db", "SQLite database path")
	webDir := fs.String("web", "", "built web UI directory to serve (e.g. web/dist)")
	resultsDir := fs.String("results", "results/experiment", "experiment results directory")
	pricesPath := fs.String("prices", "", "price table JSON (default: built-in table)")
	cfg := ingest.DefaultConfig()
	fs.IntVar(&cfg.QueueSpans, "queue", cfg.QueueSpans, "ingest queue capacity in spans (beyond this → HTTP 429)")
	fs.IntVar(&cfg.MaxBatch, "batch", cfg.MaxBatch, "max spans per write transaction")
	fs.DurationVar(&cfg.FlushInterval, "flush", cfg.FlushInterval, "max time a span waits for its batch")
	retention := fs.Duration("retention", 30*24*time.Hour, "delete traces older than this (0 = keep forever)")
	retentionEvery := fs.Duration("retention-every", 10*time.Minute, "how often the retention job runs")
	settle := fs.Duration("settle", 3*time.Second, "run detectors once a trace has been quiet this long")
	demoLive := fs.Bool("demo-live", false, "stream a simulated agent run every few seconds (demo only)")
	_ = fs.Parse(args)

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return err
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	prices := cost.Default()
	if *pricesPath != "" {
		if prices, err = cost.Load(*pricesPath); err != nil {
			return err
		}
	}

	reg := metrics.NewRegistry()
	broker := live.NewBroker(1024)
	pipe := ingest.New(cfg, st, broker.Publish, log)
	batchHist := reg.Histogram("loupe_ingest_batch_seconds", "Time to commit one ingest batch.")
	pipe.BatchTiming = func(_ int, d time.Duration) { batchHist.Observe(d.Seconds()) }
	registerPipelineMetrics(reg, pipe, broker, st)

	det := newTraceDetector(st)
	done := make(chan struct{})
	srv := &api.Server{
		Store: st, Pipeline: pipe, Broker: broker, Prices: prices, Metrics: reg, Log: log,
		Detect: det.httpHandler, WebDir: *webDir, ResultsDir: *resultsDir, Done: done,
	}
	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Background jobs share one cancellable context and a WaitGroup so
	// shutdown can wait for them to exit.
	bgCtx, cancelBG := context.WithCancel(context.Background())
	var bg sync.WaitGroup
	bg.Add(2)
	go func() {
		defer bg.Done()
		jobs.RunRetention(bgCtx, st, *retention, *retentionEvery, log, nil)
	}()
	go func() {
		defer bg.Done()
		jobs.RunDetection(bgCtx, st, *settle, time.Second, det.run, log)
	}()
	if *demoLive {
		bg.Add(1)
		go func() {
			defer bg.Done()
			runDemoLive(bgCtx, func(spans []model.Span) error {
				prices.Apply(spans)
				return pipe.Submit(bgCtx, spans, false)
			})
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("loupe listening", "addr", "http://"+*addr, "db", *dbPath, "web", *webDir)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			cancelBG()
			pipe.Close()
			return err
		}
	case <-ctx.Done():
	}

	// Graceful shutdown, in dependency order:
	//  1. close `done` so SSE streams end (Shutdown would wait on them forever);
	//  2. stop accepting connections and let in-flight requests finish;
	//  3. flush everything still in the ingest queue to SQLite;
	//  4. stop background jobs; the deferred st.Close() runs last.
	log.Info("shutting down")
	close(done)
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Error("http shutdown", "err", err)
	}
	pipe.Close()
	cancelBG()
	bg.Wait()
	log.Info("shutdown complete", "written", pipe.Stats.Written.Load(), "dropped", pipe.Stats.Dropped.Load())
	return nil
}

func registerPipelineMetrics(reg *metrics.Registry, p *ingest.Pipeline, b *live.Broker, st *store.Store) {
	reg.GaugeFunc("loupe_ingest_queue_depth", "Spans waiting to be written.", func() float64 { return float64(p.QueueDepth()) })
	reg.GaugeFunc("loupe_ingest_queue_capacity", "Ingest queue capacity in spans.", func() float64 { return float64(p.Capacity()) })
	reg.CounterFunc("loupe_ingest_accepted_spans_total", "Spans accepted into the queue.", func() float64 { return float64(p.Stats.Accepted.Load()) })
	reg.CounterFunc("loupe_ingest_rejected_backpressure_spans_total", "Spans rejected with 429.", func() float64 { return float64(p.Stats.Rejected.Load()) })
	reg.CounterFunc("loupe_ingest_written_spans_total", "New spans committed to SQLite.", func() float64 { return float64(p.Stats.Written.Load()) })
	reg.CounterFunc("loupe_ingest_duplicate_spans_total", "Spans ignored as duplicates.", func() float64 { return float64(p.Stats.Duplicates.Load()) })
	reg.CounterFunc("loupe_ingest_dropped_spans_total", "Spans lost to storage errors.", func() float64 { return float64(p.Stats.Dropped.Load()) })
	reg.CounterFunc("loupe_ingest_batches_total", "Write transactions committed.", func() float64 { return float64(p.Stats.Batches.Load()) })
	reg.GaugeFunc("loupe_live_subscribers", "Connected live-tail clients.", func() float64 { return float64(b.Count()) })
	reg.CounterFunc("loupe_live_dropped_total", "Live events dropped for slow clients.", func() float64 { return float64(b.Dropped.Load()) })
	reg.GaugeFunc("loupe_traces", "Traces stored.", func() float64 {
		t, _, _ := st.Counts(context.Background())
		return float64(t)
	})
}
