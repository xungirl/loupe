// Package metrics is a tiny Prometheus text-format registry.
//
// It deliberately avoids the official client library: Loupe needs counters,
// callback gauges and one histogram type, which fit in ~150 lines that are easy
// to read and explain. Output follows the Prometheus exposition format v0.0.4.
package metrics

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Registry holds all metric families.
type Registry struct {
	mu       sync.Mutex
	families map[string]*family
}

type family struct {
	name, help, typ string
	series          map[string]any // label string → *Counter | *Histogram | func() float64
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{families: map[string]*family{}} }

func (r *Registry) get(name, help, typ string, labels []string, create func() any) any {
	key := labelString(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.families[name]
	if !ok {
		f = &family{name: name, help: help, typ: typ, series: map[string]any{}}
		r.families[name] = f
	}
	s, ok := f.series[key]
	if !ok {
		s = create()
		f.series[key] = s
	}
	return s
}

// labelString renders k1,v1,k2,v2 as {k1="v1",k2="v2"}.
func labelString(kv []string) string {
	if len(kv) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%q", kv[i], kv[i+1])
	}
	b.WriteByte('}')
	return b.String()
}

// Counter is a monotonically increasing value.
type Counter struct{ bits atomic.Uint64 }

// Add increases the counter by v (v must be >= 0).
func (c *Counter) Add(v float64) {
	for {
		old := c.bits.Load()
		if c.bits.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+v)) {
			return
		}
	}
}

// Inc adds one.
func (c *Counter) Inc() { c.Add(1) }

// Value returns the current value.
func (c *Counter) Value() float64 { return math.Float64frombits(c.bits.Load()) }

// Counter returns (creating if needed) the counter for name+labels.
func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	return r.get(name, help, "counter", labels, func() any { return &Counter{} }).(*Counter)
}

// GaugeFunc registers a gauge whose value is read at scrape time.
func (r *Registry) GaugeFunc(name, help string, fn func() float64, labels ...string) {
	r.get(name, help, "gauge", labels, func() any { return fn })
}

// CounterFunc registers a counter whose value is read at scrape time (for
// counters that already live elsewhere as atomics).
func (r *Registry) CounterFunc(name, help string, fn func() float64, labels ...string) {
	r.get(name, help, "counter", labels, func() any { return fn })
}

// Histogram counts observations into cumulative buckets.
type Histogram struct {
	mu      sync.Mutex
	bounds  []float64
	counts  []uint64 // per bucket (non-cumulative), last is +Inf
	sum     float64
	samples uint64
}

// DefaultBuckets suits request latencies in seconds (0.5ms .. 10s).
var DefaultBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Histogram returns (creating if needed) the histogram for name+labels.
func (r *Registry) Histogram(name, help string, labels ...string) *Histogram {
	return r.get(name, help, "histogram", labels, func() any {
		return &Histogram{bounds: DefaultBuckets, counts: make([]uint64, len(DefaultBuckets)+1)}
	}).(*Histogram)
}

// Observe records one value.
func (h *Histogram) Observe(v float64) {
	i := sort.SearchFloat64s(h.bounds, v) // first bound >= v
	h.mu.Lock()
	h.counts[i]++
	h.sum += v
	h.samples++
	h.mu.Unlock()
}

// ServeHTTP writes every family in text exposition format.
func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	r.mu.Lock()
	names := make([]string, 0, len(r.families))
	for n := range r.families {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		f := r.families[n]
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.typ)
		keys := make([]string, 0, len(f.series))
		for k := range f.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch s := f.series[k].(type) {
			case *Counter:
				fmt.Fprintf(&b, "%s%s %g\n", f.name, k, s.Value())
			case func() float64:
				fmt.Fprintf(&b, "%s%s %g\n", f.name, k, s())
			case *Histogram:
				writeHistogram(&b, f.name, k, s)
			}
		}
	}
	r.mu.Unlock()
	_, _ = w.Write([]byte(b.String()))
}

func writeHistogram(b *strings.Builder, name, labels string, h *Histogram) {
	h.mu.Lock()
	defer h.mu.Unlock()
	withLE := func(le string) string {
		if labels == "" {
			return `{le="` + le + `"}`
		}
		return labels[:len(labels)-1] + `,le="` + le + `"}`
	}
	var cum uint64
	for i, bound := range h.bounds {
		cum += h.counts[i]
		fmt.Fprintf(b, "%s_bucket%s %d\n", name, withLE(fmt.Sprintf("%g", bound)), cum)
	}
	cum += h.counts[len(h.bounds)]
	fmt.Fprintf(b, "%s_bucket%s %d\n", name, withLE("+Inf"), cum)
	fmt.Fprintf(b, "%s_sum%s %g\n%s_count%s %d\n", name, labels, h.sum, name, labels, h.samples)
}
