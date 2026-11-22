// Package jobs holds Loupe's background loops: retention and detection.
// Each loop runs until its context is cancelled, so shutdown is just cancel().
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/xungirl/loupe/server/internal/store"
)

// RetentionResult is reported after each pass (for logs, metrics and tests).
type RetentionResult struct {
	Traces, Spans int64
	Err           error
}

// RunRetention deletes traces older than keep every interval. keep <= 0
// disables retention entirely (keep data forever).
func RunRetention(ctx context.Context, st *store.Store, keep, every time.Duration, log *slog.Logger, onPass func(RetentionResult)) {
	if keep <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cutoff := time.Now().Add(-keep).UnixNano()
		tr, sp, err := st.DeleteTracesBefore(ctx, cutoff, 500)
		if err != nil && ctx.Err() == nil {
			log.Error("retention pass failed", "err", err)
		} else if tr > 0 {
			log.Info("retention removed old traces", "traces", tr, "spans", sp)
		}
		if onPass != nil {
			onPass(RetentionResult{Traces: tr, Spans: sp, Err: err})
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// DetectFunc runs detectors for one trace and stores the findings.
type DetectFunc func(ctx context.Context, t store.TraceSummary) error

// RunDetection re-runs detectors on traces that changed and then stayed quiet
// for settle (so a live run isn't re-analysed on every batch).
func RunDetection(ctx context.Context, st *store.Store, settle, every time.Duration, detect DetectFunc, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for round := 0; round < 50; round++ { // bounded so failing traces can't spin
			pending, err := st.PendingDetection(ctx, time.Now().Add(-settle).UnixNano(), 100)
			if err != nil {
				if ctx.Err() == nil {
					log.Error("pending detection query failed", "err", err)
				}
				break
			}
			for _, tr := range pending {
				if err := detect(ctx, tr); err != nil && ctx.Err() == nil {
					log.Error("detection failed", "trace", tr.TraceID, "err", err)
				}
			}
			if len(pending) < 100 || ctx.Err() != nil {
				break
			}
		}
	}
}
