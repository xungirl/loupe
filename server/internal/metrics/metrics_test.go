package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExposition(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("reqs_total", "Requests.", "route", "ingest")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Inc() }()
	}
	wg.Wait()
	r.GaugeFunc("depth", "Queue depth.", func() float64 { return 7 })
	h := r.Histogram("lat_seconds", "Latency.", "route", "q")
	h.Observe(0.0007)
	h.Observe(0.3)
	h.Observe(99)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, nil)
	out := rec.Body.String()
	for _, want := range []string{
		"# TYPE reqs_total counter",
		`reqs_total{route="ingest"} 100`,
		"depth 7",
		"# TYPE lat_seconds histogram",
		`lat_seconds_bucket{route="q",le="0.001"} 1`,
		`lat_seconds_bucket{route="q",le="0.5"} 2`,
		`lat_seconds_bucket{route="q",le="+Inf"} 3`,
		`lat_seconds_count{route="q"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if r.Counter("reqs_total", "Requests.", "route", "ingest") != c {
		t.Fatal("same name+labels must return the same counter")
	}
}
