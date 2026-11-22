// Package api is Loupe's HTTP layer: ingest endpoints, the query API, the SSE
// live tail, /metrics, and (optionally) the built web UI as static files.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xungirl/loupe/server/internal/cost"
	"github.com/xungirl/loupe/server/internal/ingest"
	"github.com/xungirl/loupe/server/internal/live"
	"github.com/xungirl/loupe/server/internal/metrics"
	"github.com/xungirl/loupe/server/internal/store"
)

// Detector runs detectors for one trace on demand (implemented in cmd/loupe to
// avoid an import cycle with the detect package).
type Detector func(r *http.Request, traceID string) ([]store.Finding, error)

// Server holds the dependencies every handler needs.
type Server struct {
	Store       *store.Store
	Pipeline    *ingest.Pipeline
	Broker      *live.Broker
	Prices      *cost.Table
	Metrics     *metrics.Registry
	Log         *slog.Logger
	Detect      Detector
	WebDir      string // built UI (web/dist); empty = API only
	ResultsDir  string // experiment results root, served read-only
	MaxBodySize int64
	Now         func() time.Time
	// Done is closed when the server starts shutting down; long-lived SSE
	// streams watch it because http.Server.Shutdown does not cancel them.
	Done <-chan struct{}
}

// Handler wires up all routes.
func (s *Server) Handler() http.Handler {
	if s.MaxBodySize == 0 {
		s.MaxBodySize = 16 << 20
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	mux := http.NewServeMux()

	// Ingest.
	mux.HandleFunc("POST /api/v1/spans", s.handleNativeIngest)
	mux.HandleFunc("POST /v1/traces", s.handleOTLPIngest) // standard OTLP/HTTP path

	// Query.
	mux.HandleFunc("GET /api/v1/traces", s.handleListTraces)
	mux.HandleFunc("GET /api/v1/traces/{id}", s.handleGetTrace)
	mux.HandleFunc("GET /api/v1/traces/{id}/findings", s.handleTraceFindings)
	mux.HandleFunc("POST /api/v1/traces/{id}/detect", s.handleDetectNow)
	mux.HandleFunc("GET /api/v1/spans", s.handleSearchSpans)
	mux.HandleFunc("GET /api/v1/findings", s.handleListFindings)
	mux.HandleFunc("GET /api/v1/stats", s.handleStats)
	mux.HandleFunc("GET /api/v1/prices", s.handlePrices)
	mux.HandleFunc("GET /api/v1/experiments", s.handleExperiments)
	mux.HandleFunc("GET /api/v1/live", s.handleLive)

	// Ops.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queue_depth": s.Pipeline.QueueDepth()})
	})
	mux.Handle("GET /metrics", s.Metrics)

	if s.WebDir != "" {
		mux.Handle("GET /", spaHandler(s.WebDir))
	}
	return s.withAccessMetrics(withCORS(mux))
}

// withAccessMetrics counts requests by route class and status.
func (s *Server) withAccessMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		route := routeClass(r.URL.Path)
		s.Metrics.Counter("loupe_http_requests_total", "HTTP requests by route class and status code.",
			"route", route, "code", http.StatusText(rec.status)).Inc()
		if route != "live" {
			s.Metrics.Histogram("loupe_http_request_seconds", "HTTP request latency by route class.", "route", route).
				Observe(time.Since(start).Seconds())
		}
	})
}

func routeClass(p string) string {
	switch {
	case p == "/api/v1/spans" || p == "/v1/traces":
		return "ingest"
	case p == "/api/v1/live":
		return "live"
	case strings.HasPrefix(p, "/api/"):
		return "query"
	case p == "/metrics" || p == "/healthz":
		return "ops"
	}
	return "static"
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush lets SSE work through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withCORS allows the Vite dev server (another port) to call the API.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Encoding")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler serves files from dir and falls back to index.html so client-side
// routes like /traces/abc work on reload.
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			if r.URL.Path != "/" {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// isNotFound maps store.ErrNotFound to 404.
func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
