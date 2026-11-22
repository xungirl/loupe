// Package live fans newly committed spans out to Server-Sent-Events clients.
//
// The writer goroutine calls Publish after every commit. Publish must never
// block the writer, so each subscriber gets a small buffered channel and if a
// slow browser lets it fill up, we drop events for that subscriber (and count
// them) instead of slowing down ingestion for everyone.
package live

import (
	"sync"
	"sync/atomic"

	"github.com/xungirl/loupe/server/internal/model"
)

// Subscriber receives spans matching its filter.
type Subscriber struct {
	C       chan model.Span
	traceID string // "" = all traces
	dropped atomic.Int64
}

// Dropped is how many spans this subscriber missed because it was too slow.
func (s *Subscriber) Dropped() int64 { return s.dropped.Load() }

// Broker tracks subscribers.
type Broker struct {
	mu      sync.RWMutex
	subs    map[*Subscriber]struct{}
	bufSize int

	Published atomic.Int64
	Dropped   atomic.Int64
}

// NewBroker creates a broker whose subscribers buffer up to bufSize spans.
func NewBroker(bufSize int) *Broker {
	if bufSize <= 0 {
		bufSize = 1024
	}
	return &Broker{subs: map[*Subscriber]struct{}{}, bufSize: bufSize}
}

// Subscribe registers a subscriber; traceID "" means every trace.
func (b *Broker) Subscribe(traceID string) *Subscriber {
	s := &Subscriber{C: make(chan model.Span, b.bufSize), traceID: traceID}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Unsubscribe removes s and closes its channel.
func (b *Broker) Unsubscribe(s *Subscriber) {
	b.mu.Lock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.C)
	}
	b.mu.Unlock()
}

// Count returns the number of connected subscribers.
func (b *Broker) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Publish delivers spans without ever blocking.
func (b *Broker) Publish(spans []model.Span) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		for i := range spans {
			if s.traceID != "" && s.traceID != spans[i].TraceID {
				continue
			}
			select {
			case s.C <- spans[i]:
				b.Published.Add(1)
			default:
				s.dropped.Add(1)
				b.Dropped.Add(1)
			}
		}
	}
}
