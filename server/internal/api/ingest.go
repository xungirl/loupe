package api

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xungirl/loupe/server/internal/ingest"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/otlp"
)

// ingestResponse mirrors OTLP's partial-success idea for both endpoints.
type ingestResponse struct {
	Accepted int      `json:"accepted"`
	Rejected int      `json:"rejected"`
	Errors   []string `json:"errors,omitempty"`
}

// readBody enforces the size limit and transparently un-gzips.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body := http.MaxBytesReader(w, r.Body, s.MaxBodySize)
	var rd io.Reader = body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return nil, fmt.Errorf("bad gzip body: %w", err)
		}
		defer gz.Close()
		rd = io.LimitReader(gz, s.MaxBodySize*4) // guard against gzip bombs
	}
	return io.ReadAll(rd)
}

// handleNativeIngest accepts {"spans":[...]} or a bare JSON array of spans.
func (s *Server) handleNativeIngest(w http.ResponseWriter, r *http.Request) {
	b, err := s.readBody(w, r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	var raw []json.RawMessage
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "[") {
		err = json.Unmarshal(b, &raw)
	} else {
		var env struct {
			Spans []json.RawMessage `json:"spans"`
		}
		err = json.Unmarshal(b, &env)
		raw = env.Spans
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON: {\"spans\": [...]} or [...]: "+err.Error())
		return
	}
	now := s.Now().UnixNano()
	res := ingestResponse{}
	spans := make([]model.Span, 0, len(raw))
	for i, m := range raw {
		var sp model.Span
		err := json.Unmarshal(m, &sp)
		if err == nil {
			err = sp.Normalize(now)
		}
		if err != nil {
			res.Rejected++
			if len(res.Errors) < 5 {
				res.Errors = append(res.Errors, fmt.Sprintf("span %d: %v", i, err))
			}
			continue
		}
		if sp.Target == "" && sp.Kind == model.KindTool {
			sp.Target = model.ExtractTarget(sp.ToolInput)
		}
		spans = append(spans, sp)
	}
	s.submit(w, r, spans, res)
}

// handleOTLPIngest accepts OTLP/HTTP JSON at the standard /v1/traces path.
func (s *Server) handleOTLPIngest(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); strings.Contains(ct, "protobuf") {
		writeError(w, http.StatusUnsupportedMediaType, "only OTLP/HTTP JSON is supported; set the exporter protocol to http/json")
		return
	}
	b, err := s.readBody(w, r)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	dec, err := otlp.Decode(b, s.Now().UnixNano())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.submit(w, r, dec.Spans, ingestResponse{Rejected: dec.Rejected, Errors: dec.Errors})
}

// submit prices the spans and hands them to the pipeline, mapping pipeline
// errors to HTTP status codes.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, spans []model.Span, res ingestResponse) {
	s.Metrics.Counter("loupe_ingest_rejected_invalid_total", "Spans rejected by validation.").Add(float64(res.Rejected))
	if len(spans) == 0 {
		code := http.StatusOK
		if res.Rejected > 0 {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, res)
		return
	}
	if n := s.Prices.Apply(spans); n > 0 {
		s.Metrics.Counter("loupe_ingest_unpriced_total", "LLM spans whose model is missing from the price table.").Add(float64(n))
	}
	wait := r.URL.Query().Get("wait") == "true"
	switch err := s.Pipeline.Submit(r.Context(), spans, wait); {
	case err == nil:
		res.Accepted = len(spans)
		code := http.StatusAccepted
		if wait {
			code = http.StatusOK // committed, not just queued
		}
		writeJSON(w, code, res)
	case errors.Is(err, ingest.ErrQueueFull):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "ingest queue full, retry later")
	case errors.Is(err, ingest.ErrClosed):
		writeError(w, http.StatusServiceUnavailable, "server shutting down")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
