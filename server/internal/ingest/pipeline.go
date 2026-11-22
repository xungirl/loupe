// Package ingest turns HTTP requests into batched SQLite writes.
//
//	handler ──Submit──▶ [bounded queue] ──▶ writer goroutine ──▶ store.InsertSpans
//	           │ full?                         (batches up to MaxBatch spans
//	           ▼                                or FlushInterval, whichever first)
//	        ErrQueueFull → HTTP 429
//
// The queue is bounded in *spans*, not requests, so memory use is predictable
// no matter how clients batch. When it is full we reject immediately instead
// of blocking the handler: the client gets a 429 + Retry-After and can back
// off, and the server never builds an unbounded backlog (backpressure).
package ingest

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
)

// ErrQueueFull means the request was rejected for backpressure.
var ErrQueueFull = errors.New("ingest queue full")

// ErrClosed means the pipeline is shutting down.
var ErrClosed = errors.New("ingest pipeline closed")

// Writer is what the pipeline needs from storage.
type Writer interface {
	InsertSpans(ctx context.Context, spans []model.Span) ([]model.Span, error)
}

// Config tunes the pipeline.
type Config struct {
	QueueSpans    int           // max spans waiting to be written
	MaxBatch      int           // max spans per transaction
	FlushInterval time.Duration // max time a span waits for a batch to fill
}

// DefaultConfig is used by `loupe serve` unless flags override it.
func DefaultConfig() Config {
	return Config{QueueSpans: 50_000, MaxBatch: 2_000, FlushInterval: 50 * time.Millisecond}
}

// job is one accepted request. done is non-nil when the caller wants to wait
// for the commit (used by `?wait=true` and by tests).
type job struct {
	spans []model.Span
	done  chan error
}

// Stats are exported to /metrics.
type Stats struct {
	Accepted   atomic.Int64 // spans accepted into the queue
	Rejected   atomic.Int64 // spans rejected with ErrQueueFull
	Written    atomic.Int64 // new spans committed
	Duplicates atomic.Int64 // spans ignored because the ID already existed
	Dropped    atomic.Int64 // spans lost to storage errors
	Batches    atomic.Int64 // transactions committed
}

// Pipeline is the bounded queue plus its single writer goroutine.
type Pipeline struct {
	cfg      Config
	w        Writer
	onCommit func([]model.Span) // called with newly inserted spans (live tail, detectors)
	log      *slog.Logger

	queue  chan *job
	queued atomic.Int64 // spans currently in the queue (reserved capacity)

	mu     sync.RWMutex // guards closed + sends on queue vs close(queue)
	closed bool
	wg     sync.WaitGroup

	Stats       Stats
	BatchTiming func(spans int, d time.Duration) // optional histogram hook
}

// New starts the writer goroutine.
func New(cfg Config, w Writer, onCommit func([]model.Span), log *slog.Logger) *Pipeline {
	if cfg.QueueSpans <= 0 || cfg.MaxBatch <= 0 || cfg.FlushInterval <= 0 {
		cfg = DefaultConfig()
	}
	if log == nil {
		log = slog.Default()
	}
	p := &Pipeline{
		cfg:      cfg,
		w:        w,
		onCommit: onCommit,
		log:      log,
		// One slot per span is the worst case (every job holds one span), so a
		// send after a successful reservation can never block.
		queue: make(chan *job, cfg.QueueSpans),
	}
	p.wg.Add(1)
	go p.run()
	return p
}

// QueueDepth is the number of spans waiting to be written.
func (p *Pipeline) QueueDepth() int64 { return p.queued.Load() }

// Capacity is the configured queue size in spans.
func (p *Pipeline) Capacity() int { return p.cfg.QueueSpans }

// Submit enqueues spans. If wait is true it blocks until they are committed
// (or ctx ends). Spans must already be normalised.
func (p *Pipeline) Submit(ctx context.Context, spans []model.Span, wait bool) error {
	n := int64(len(spans))
	if n == 0 {
		return nil
	}
	if n > int64(p.cfg.QueueSpans) {
		p.Stats.Rejected.Add(n)
		return ErrQueueFull // a single request larger than the whole queue can never fit
	}
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return ErrClosed
	}
	// Reserve capacity with a CAS loop so concurrent handlers never overshoot.
	for {
		cur := p.queued.Load()
		if cur+n > int64(p.cfg.QueueSpans) {
			p.mu.RUnlock()
			p.Stats.Rejected.Add(n)
			return ErrQueueFull
		}
		if p.queued.CompareAndSwap(cur, cur+n) {
			break
		}
	}
	j := &job{spans: spans}
	if wait {
		j.done = make(chan error, 1)
	}
	p.queue <- j // never blocks: capacity was reserved above
	p.mu.RUnlock()
	p.Stats.Accepted.Add(n)

	if !wait {
		return nil
	}
	select {
	case err := <-j.done:
		return err
	case <-ctx.Done():
		return ctx.Err() // spans are still queued and will be written
	}
}

// Close stops accepting new spans, flushes everything queued and waits for the
// writer to finish. Safe to call more than once.
func (p *Pipeline) Close() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()
	p.wg.Wait()
}

// run is the writer loop: gather jobs into a batch, write, repeat.
func (p *Pipeline) run() {
	defer p.wg.Done()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	var batch []*job
	size := 0

	flush := func() {
		if len(batch) > 0 {
			p.write(batch, size)
			batch, size = nil, 0
		}
	}
	for {
		if len(batch) == 0 {
			// Idle: block until the first job of the next batch arrives.
			j, ok := <-p.queue
			if !ok {
				return
			}
			batch, size = append(batch, j), len(j.spans)
			timer.Reset(p.cfg.FlushInterval)
			continue
		}
		if size >= p.cfg.MaxBatch {
			timer.Stop()
			flush()
			continue
		}
		select {
		case j, ok := <-p.queue:
			if !ok {
				timer.Stop()
				flush()
				return
			}
			batch, size = append(batch, j), size+len(j.spans)
		case <-timer.C:
			flush()
		}
	}
}

// write commits one batch with a small retry for transient SQLite errors.
func (p *Pipeline) write(batch []*job, size int) {
	all := make([]model.Span, 0, size)
	for _, j := range batch {
		all = append(all, j.spans...)
	}
	start := time.Now()
	var inserted []model.Span
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		inserted, err = p.w.InsertSpans(context.Background(), all)
		if err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	p.queued.Add(-int64(size)) // release capacity whether or not the write worked
	if err != nil {
		p.Stats.Dropped.Add(int64(size))
		p.log.Error("batch write failed; spans dropped", "spans", size, "err", err)
	} else {
		p.Stats.Batches.Add(1)
		p.Stats.Written.Add(int64(len(inserted)))
		p.Stats.Duplicates.Add(int64(size - len(inserted)))
		if p.BatchTiming != nil {
			p.BatchTiming(size, time.Since(start))
		}
	}
	for _, j := range batch {
		if j.done != nil {
			j.done <- err
		}
	}
	if err == nil && len(inserted) > 0 && p.onCommit != nil {
		p.onCommit(inserted)
	}
}
