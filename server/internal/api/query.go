package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/xungirl/loupe/server/internal/assemble"
	"github.com/xungirl/loupe/server/internal/store"
)

func qInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

func qFloat(r *http.Request, key string) float64 {
	v, _ := strconv.ParseFloat(r.URL.Query().Get(key), 64)
	return v
}

func qBool(r *http.Request, key string) bool {
	v, _ := strconv.ParseBool(r.URL.Query().Get(key))
	return v
}

// qSince parses ?since=24h (a Go duration back from now) into Unix ns.
func (s *Server) qSince(r *http.Request) int64 {
	d, err := time.ParseDuration(r.URL.Query().Get("since"))
	if err != nil || d <= 0 {
		return 0
	}
	return s.Now().Add(-d).UnixNano()
}

// GET /api/v1/traces?q=&source=&has_error=&has_finding=&detector=&tool=&agent=&min_cost=&since=&sort=&limit=&offset=
func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TraceFilter{
		Source:     q.Get("source"),
		Query:      q.Get("q"),
		HasError:   qBool(r, "has_error"),
		HasFinding: qBool(r, "has_finding"),
		Detector:   q.Get("detector"),
		Tool:       q.Get("tool"),
		Agent:      q.Get("agent"),
		MinCostUSD: qFloat(r, "min_cost"),
		SinceNS:    s.qSince(r),
		Sort:       q.Get("sort"),
		Limit:      qInt(r, "limit", 50),
		Offset:     qInt(r, "offset", 0),
	}
	traces, total, err := s.Store.ListTraces(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": traces, "total": total, "limit": f.Limit, "offset": f.Offset})
}

// GET /api/v1/traces/{id} → summary + assembled span tree + agent tree + findings.
func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sum, err := s.Store.GetTrace(r.Context(), id)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "trace not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	spans, err := s.Store.TraceSpans(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	start := time.Now()
	tree := assemble.Build(spans)
	s.Metrics.Histogram("loupe_assembly_seconds", "Time to assemble a trace tree in memory.").Observe(time.Since(start).Seconds())

	findings, err := s.Store.ListFindings(r.Context(), id, "", 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The flat depth-first list is what the waterfall renders; Roots would
	// duplicate every span in nested form, so we omit it from the payload.
	type flatNode struct {
		*assemble.Node
		Children []*assemble.Node `json:"children,omitempty"`
	}
	flat := make([]flatNode, len(tree.Flat))
	for i, n := range tree.Flat {
		flat[i] = flatNode{Node: n}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary":   sum,
		"spans":     flat,
		"agents":    tree.Agents,
		"orphans":   tree.Orphans,
		"max_depth": tree.MaxDep,
		"findings":  findings,
	})
}

// GET /api/v1/traces/{id}/findings
func (s *Server) handleTraceFindings(w http.ResponseWriter, r *http.Request) {
	fs, err := s.Store.ListFindings(r.Context(), r.PathValue("id"), "", 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": fs})
}

// POST /api/v1/traces/{id}/detect runs detectors now instead of waiting for
// the trace to settle.
func (s *Server) handleDetectNow(w http.ResponseWriter, r *http.Request) {
	if s.Detect == nil {
		writeError(w, http.StatusNotImplemented, "detectors not configured")
		return
	}
	fs, err := s.Detect(r, r.PathValue("id"))
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "trace not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": fs})
}

// GET /api/v1/spans?trace_id=&tool=&kind=&agent=&error=true&limit=
func (s *Server) handleSearchSpans(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	spans, err := s.Store.SearchSpans(r.Context(), store.SpanFilter{
		TraceID: q.Get("trace_id"), Tool: q.Get("tool"), Kind: q.Get("kind"),
		AgentName: q.Get("agent"), ErrorOnly: qBool(r, "error"), Limit: qInt(r, "limit", 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
}

// GET /api/v1/findings?detector=&limit=
func (s *Server) handleListFindings(w http.ResponseWriter, r *http.Request) {
	fs, err := s.Store.ListFindings(r.Context(), "", r.URL.Query().Get("detector"), qInt(r, "limit", 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": fs})
}

// GET /api/v1/stats?since=&source=
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Stats(r.Context(), store.StatsFilter{SinceNS: s.qSince(r), Source: r.URL.Query().Get("source")})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// GET /api/v1/prices — the active price table, so the UI can show its source.
func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Prices)
}

// GET /api/v1/experiments — every results/experiment/*/report.json, newest first.
func (s *Server) handleExperiments(w http.ResponseWriter, r *http.Request) {
	out := []json.RawMessage{}
	if s.ResultsDir != "" {
		paths, _ := filepath.Glob(filepath.Join(s.ResultsDir, "*", "report.json"))
		sort.Sort(sort.Reverse(sort.StringSlice(paths)))
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil || !json.Valid(b) {
				continue
			}
			out = append(out, b)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out, "dir": s.ResultsDir})
}

// GET /api/v1/live?trace_id= — Server-Sent Events stream of new spans.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := s.Broker.Subscribe(r.URL.Query().Get("trace_id"))
	defer s.Broker.Unsubscribe(sub)
	fmt.Fprintf(w, "event: hello\ndata: {\"subscribers\":%d}\n\n", s.Broker.Count())
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	enc := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.Done:
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping dropped=%d\n\n", sub.Dropped())
			flusher.Flush()
		case sp, ok := <-sub.C:
			if !ok {
				return
			}
			writeSpan := func(sp any) {
				fmt.Fprint(w, "event: span\ndata: ")
				_ = enc.Encode(sp) // Encode adds the trailing newline
				fmt.Fprint(w, "\n")
			}
			writeSpan(sp)
			// Drain anything else already buffered, then flush once.
		drain:
			for {
				select {
				case more, ok := <-sub.C:
					if !ok {
						flusher.Flush()
						return
					}
					writeSpan(more)
				default:
					break drain
				}
			}
			flusher.Flush()
		}
	}
}
