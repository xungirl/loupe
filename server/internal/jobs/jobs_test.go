package jobs

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
	"github.com/xungirl/loupe/server/internal/testutil"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRetentionLoop(t *testing.T) {
	st := testutil.OpenStore(t)
	old := testutil.Span("old", "r", "", model.KindAgent, 0, 1) // year 2026-01-01: older than 1h
	fresh := model.Span{TraceID: "fresh", SpanID: "r", Kind: model.KindAgent, StartNS: time.Now().UnixNano(), EndNS: time.Now().UnixNano()}
	if _, err := st.InsertSpans(context.Background(), []model.Span{old, fresh}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	passes := make(chan RetentionResult, 10)
	go RunRetention(ctx, st, time.Hour, 10*time.Millisecond, quiet, func(r RetentionResult) { passes <- r })
	first := <-passes
	cancel()
	if first.Err != nil || first.Traces != 1 {
		t.Fatalf("first pass %+v", first)
	}
	if _, err := st.GetTrace(context.Background(), "fresh"); err != nil {
		t.Fatal("fresh trace removed")
	}
	// keep <= 0 disables retention and returns immediately.
	RunRetention(context.Background(), st, 0, time.Millisecond, quiet, nil)
}

func TestDetectionWaitsForSettle(t *testing.T) {
	st := testutil.OpenStore(t)
	if _, err := st.InsertSpans(context.Background(), []model.Span{testutil.Span("t", "r", "", model.KindAgent, 0, 1)}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	detect := func(ctx context.Context, tr store.TraceSummary) error {
		calls.Add(1)
		return st.ReplaceFindings(ctx, tr.TraceID, tr.LastIngestNS, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunDetection(ctx, st, 150*time.Millisecond, 10*time.Millisecond, detect, quiet)
	time.Sleep(60 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("detector ran before the trace settled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("detector ran %d times, want exactly 1", n)
	}
}
