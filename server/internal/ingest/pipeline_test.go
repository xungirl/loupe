package ingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/testutil"
)

// fakeWriter records batches and can be blocked to simulate a slow disk.
type fakeWriter struct {
	mu      sync.Mutex
	batches [][]model.Span
	seen    map[string]bool
	gate    chan struct{} // if non-nil, each write waits for a token
	fail    int           // fail this many writes first
}

func (f *fakeWriter) InsertSpans(_ context.Context, spans []model.Span) ([]model.Span, error) {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return nil, errors.New("disk on fire")
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	var out []model.Span
	for _, s := range spans {
		if !f.seen[s.SpanID] {
			f.seen[s.SpanID] = true
			out = append(out, s)
		}
	}
	f.batches = append(f.batches, spans)
	return out, nil
}

func (f *fakeWriter) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func spans(prefix string, n int) []model.Span {
	out := make([]model.Span, n)
	for i := range out {
		out[i] = testutil.Span("t", fmt.Sprintf("%s-%d", prefix, i), "", model.KindTool, 0, 1)
	}
	return out
}

func TestBackpressureRejectsWhenFull(t *testing.T) {
	w := &fakeWriter{gate: make(chan struct{})}
	p := New(Config{QueueSpans: 10, MaxBatch: 100, FlushInterval: time.Millisecond}, w, nil, nil)
	ctx := context.Background()

	if err := p.Submit(ctx, spans("a", 6), false); err != nil {
		t.Fatal(err)
	}
	if err := p.Submit(ctx, spans("b", 4), false); err != nil {
		t.Fatal(err)
	}
	// Queue holds 10/10 while the writer is blocked → next request must bounce.
	if err := p.Submit(ctx, spans("c", 1), false); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	if err := p.Submit(ctx, spans("huge", 11), false); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("oversized request: want ErrQueueFull, got %v", err)
	}
	if got := p.Stats.Rejected.Load(); got != 12 {
		t.Fatalf("rejected = %d, want 12", got)
	}
	// Unblock the writer: capacity is released and submits work again.
	close(w.gate)
	deadline := time.Now().Add(2 * time.Second)
	for p.QueueDepth() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := p.Submit(ctx, spans("d", 5), true); err != nil {
		t.Fatalf("after drain: %v", err)
	}
	p.Close()
	if w.total() != 15 {
		t.Fatalf("written %d, want 15 (no accepted span may be lost)", w.total())
	}
}

func TestBatchingCoalescesRequests(t *testing.T) {
	w := &fakeWriter{}
	p := New(Config{QueueSpans: 10_000, MaxBatch: 500, FlushInterval: 20 * time.Millisecond}, w, nil, nil)
	for i := 0; i < 100; i++ {
		if err := p.Submit(context.Background(), spans(fmt.Sprint(i), 10), false); err != nil {
			t.Fatal(err)
		}
	}
	p.Close()
	if w.total() != 1000 {
		t.Fatalf("written %d, want 1000", w.total())
	}
	// 1000 spans with MaxBatch 500 submitted in a burst should need only a few
	// transactions, not 100.
	if n := len(w.batches); n > 10 {
		t.Fatalf("%d transactions for 100 requests; batching not working", n)
	}
}

func TestConcurrentSubmitAndCloseLosesNothingAccepted(t *testing.T) {
	w := &fakeWriter{}
	var committed sync.Map
	p := New(Config{QueueSpans: 200, MaxBatch: 64, FlushInterval: time.Millisecond}, w,
		func(ss []model.Span) {
			for _, s := range ss {
				committed.Store(s.SpanID, true)
			}
		}, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := map[string]bool{}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				batch := spans(fmt.Sprintf("g%d-%d", g, i), 3)
				if err := p.Submit(context.Background(), batch, false); err == nil {
					mu.Lock()
					for _, s := range batch {
						accepted[s.SpanID] = true
					}
					mu.Unlock()
				} else if !errors.Is(err, ErrQueueFull) && !errors.Is(err, ErrClosed) {
					t.Errorf("unexpected error %v", err)
				}
			}
		}(g)
	}
	time.Sleep(5 * time.Millisecond)
	p.Close() // races with the submitters on purpose
	wg.Wait()
	if err := p.Submit(context.Background(), spans("late", 1), false); !errors.Is(err, ErrClosed) {
		t.Fatalf("submit after close: %v", err)
	}
	for id := range accepted {
		if _, ok := committed.Load(id); !ok {
			t.Fatalf("accepted span %s never committed", id)
		}
	}
}

func TestWriteFailureDropsAndReportsError(t *testing.T) {
	w := &fakeWriter{fail: 3} // all 3 attempts fail
	p := New(Config{QueueSpans: 10, MaxBatch: 10, FlushInterval: time.Millisecond}, w, nil, nil)
	err := p.Submit(context.Background(), spans("x", 2), true)
	if err == nil {
		t.Fatal("want write error surfaced to a waiting caller")
	}
	p.Close()
	if p.Stats.Dropped.Load() != 2 || p.QueueDepth() != 0 {
		t.Fatalf("dropped=%d depth=%d", p.Stats.Dropped.Load(), p.QueueDepth())
	}
}

func TestDuplicatesCounted(t *testing.T) {
	w := &fakeWriter{}
	p := New(Config{QueueSpans: 100, MaxBatch: 100, FlushInterval: time.Millisecond}, w, nil, nil)
	b := spans("dup", 5)
	_ = p.Submit(context.Background(), b, true)
	_ = p.Submit(context.Background(), b, true)
	p.Close()
	if p.Stats.Written.Load() != 5 || p.Stats.Duplicates.Load() != 5 {
		t.Fatalf("written=%d duplicates=%d", p.Stats.Written.Load(), p.Stats.Duplicates.Load())
	}
}
