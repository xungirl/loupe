package live

import (
	"testing"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
)

func TestSlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	b := NewBroker(2)
	slow := b.Subscribe("")      // never reads
	filtered := b.Subscribe("t2") // only trace t2
	spans := []model.Span{{TraceID: "t1", SpanID: "a"}, {TraceID: "t1", SpanID: "b"}, {TraceID: "t1", SpanID: "c"}, {TraceID: "t2", SpanID: "d"}}
	done := make(chan struct{})
	go func() { b.Publish(spans); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
	if slow.Dropped() != 2 || len(slow.C) != 2 {
		t.Fatalf("slow: dropped=%d buffered=%d", slow.Dropped(), len(slow.C))
	}
	if got := <-filtered.C; got.SpanID != "d" || len(filtered.C) != 0 {
		t.Fatalf("filter broken: %+v", got)
	}
	b.Unsubscribe(slow)
	b.Unsubscribe(slow) // idempotent
	if b.Count() != 1 {
		t.Fatalf("count %d", b.Count())
	}
	b.Publish(spans) // must not panic on the closed channel
}
