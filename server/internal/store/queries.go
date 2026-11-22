package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
)

// TraceSummary is one row of the traces table.
type TraceSummary struct {
	TraceID          string  `json:"trace_id"`
	Name             string  `json:"name"`
	Source           string  `json:"source"`
	StartNS          int64   `json:"start_ns"`
	EndNS            int64   `json:"end_ns"`
	SpanCount        int64   `json:"span_count"`
	ErrorCount       int64   `json:"error_count"`
	LLMCalls         int64   `json:"llm_calls"`
	ToolCalls        int64   `json:"tool_calls"`
	AgentCount       int64   `json:"agent_count"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LastIngestNS     int64   `json:"last_ingest_ns"`
	DetectedNS       int64   `json:"detected_ns"`
	FindingCount     int64   `json:"finding_count"`
}

const traceColumns = `trace_id, name, source, start_ns, end_ns, span_count, error_count, llm_calls,
	tool_calls, agent_count, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
	cost_usd, last_ingest_ns, detected_ns, finding_count`

func scanTrace(row interface{ Scan(...any) error }, t *TraceSummary) error {
	return row.Scan(&t.TraceID, &t.Name, &t.Source, &t.StartNS, &t.EndNS, &t.SpanCount, &t.ErrorCount,
		&t.LLMCalls, &t.ToolCalls, &t.AgentCount, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens,
		&t.CacheWriteTokens, &t.CostUSD, &t.LastIngestNS, &t.DetectedNS, &t.FindingCount)
}

// GetTrace returns the summary row for one trace.
func (s *Store) GetTrace(ctx context.Context, traceID string) (TraceSummary, error) {
	var t TraceSummary
	err := scanTrace(s.r.QueryRowContext(ctx, `SELECT `+traceColumns+` FROM traces WHERE trace_id = ?`, traceID), &t)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// TraceFilter selects traces for the sessions list. Zero values mean "any".
type TraceFilter struct {
	Source     string
	Query      string // substring of trace name or id
	HasError   bool
	HasFinding bool
	Detector   string // only traces with a finding from this detector
	Tool       string // only traces that called this tool
	Agent      string // only traces containing this agent name
	MinCostUSD float64
	SinceNS    int64
	Sort       string // "start" (default), "cost", "tokens", "errors", "spans"
	Limit      int
	Offset     int
}

// ListTraces returns a page of trace summaries plus the total match count.
func (s *Store) ListTraces(ctx context.Context, f TraceFilter) ([]TraceSummary, int, error) {
	var where []string
	var args []any
	if f.Source != "" {
		where = append(where, "source = ?")
		args = append(args, f.Source)
	}
	if f.Query != "" {
		where = append(where, "(name LIKE ? OR trace_id LIKE ?)")
		q := "%" + f.Query + "%"
		args = append(args, q, q)
	}
	if f.HasError {
		where = append(where, "error_count > 0")
	}
	if f.HasFinding {
		where = append(where, "finding_count > 0")
	}
	if f.Detector != "" {
		where = append(where, "trace_id IN (SELECT trace_id FROM findings WHERE detector = ?)")
		args = append(args, f.Detector)
	}
	if f.Tool != "" {
		// Uses idx_spans_tool, then the traces primary key.
		where = append(where, "trace_id IN (SELECT trace_id FROM spans WHERE tool_name = ?)")
		args = append(args, f.Tool)
	}
	if f.Agent != "" {
		where = append(where, "trace_id IN (SELECT trace_id FROM spans WHERE kind = 'agent' AND agent_name = ?)")
		args = append(args, f.Agent)
	}
	if f.MinCostUSD > 0 {
		where = append(where, "cost_usd >= ?")
		args = append(args, f.MinCostUSD)
	}
	if f.SinceNS > 0 {
		where = append(where, "start_ns >= ?")
		args = append(args, f.SinceNS)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	order := map[string]string{
		"cost":   "cost_usd DESC",
		"tokens": "(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens) DESC",
		"errors": "error_count DESC",
		"spans":  "span_count DESC",
	}[f.Sort]
	if order == "" {
		order = "start_ns DESC"
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}

	var total int
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM traces`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT `+traceColumns+` FROM traces`+clause+
		` ORDER BY `+order+`, trace_id LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []TraceSummary{}
	for rows.Next() {
		var t TraceSummary
		if err := scanTrace(rows, &t); err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// SpanFilter selects individual spans across traces.
type SpanFilter struct {
	TraceID   string
	Tool      string
	Kind      string
	AgentName string
	ErrorOnly bool
	Limit     int
}

// SearchSpans finds spans across all traces (e.g. "every failed Bash call").
func (s *Store) SearchSpans(ctx context.Context, f SpanFilter) ([]model.Span, error) {
	var where []string
	var args []any
	add := func(cond string, v any) { where = append(where, cond); args = append(args, v) }
	if f.TraceID != "" {
		add("trace_id = ?", f.TraceID)
	}
	if f.Tool != "" {
		add("tool_name = ?", f.Tool)
	}
	if f.Kind != "" {
		add("kind = ?", f.Kind)
	}
	if f.AgentName != "" {
		add("agent_name = ?", f.AgentName)
	}
	if f.ErrorOnly {
		where = append(where, "status = 'error'")
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	q := `SELECT ` + spanColumns + ` FROM spans`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY start_ns DESC LIMIT ?"
	rows, err := s.r.QueryContext(ctx, q, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Span{}
	for rows.Next() {
		var sp model.Span
		if err := scanSpan(rows, &sp); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// Finding is one detector result.
type Finding struct {
	ID        int64    `json:"id"`
	TraceID   string   `json:"trace_id"`
	Detector  string   `json:"detector"`
	Severity  string   `json:"severity"`
	AgentID   string   `json:"agent_id"`
	Summary   string   `json:"summary"`
	SpanIDs   []string `json:"span_ids"`
	Score     float64  `json:"score"`
	CreatedNS int64    `json:"created_ns"`
}

// ReplaceFindings atomically swaps a trace's findings and marks it detected.
// detectedNS should be the trace's last_ingest_ns observed before detection:
// if new spans arrived meanwhile, last_ingest_ns > detected_ns and the trace
// is picked up again on the next pass.
func (s *Store) ReplaceFindings(ctx context.Context, traceID string, detectedNS int64, fs []Finding) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE trace_id = ?`, traceID); err != nil {
		return err
	}
	now := time.Now().UnixNano()
	for _, f := range fs {
		ids, _ := json.Marshal(f.SpanIDs)
		if _, err := tx.ExecContext(ctx, `INSERT INTO findings (trace_id, detector, severity, agent_id, summary, span_ids, score, created_ns)
			VALUES (?,?,?,?,?,?,?,?)`, traceID, f.Detector, f.Severity, f.AgentID, f.Summary, string(ids), f.Score, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE traces SET detected_ns = ?, finding_count = ? WHERE trace_id = ?`,
		detectedNS, len(fs), traceID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListFindings returns findings, optionally for one trace or one detector.
func (s *Store) ListFindings(ctx context.Context, traceID, detector string, limit int) ([]Finding, error) {
	var where []string
	var args []any
	if traceID != "" {
		where = append(where, "trace_id = ?")
		args = append(args, traceID)
	}
	if detector != "" {
		where = append(where, "detector = ?")
		args = append(args, detector)
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT id, trace_id, detector, severity, agent_id, summary, span_ids, score, created_ns FROM findings`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT ?"
	rows, err := s.r.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		var f Finding
		var ids string
		if err := rows.Scan(&f.ID, &f.TraceID, &f.Detector, &f.Severity, &f.AgentID, &f.Summary, &ids, &f.Score, &f.CreatedNS); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ids), &f.SpanIDs)
		out = append(out, f)
	}
	return out, rows.Err()
}

// PendingDetection lists traces that changed since detectors last ran and have
// been quiet for a while (last ingest before settledBeforeNS). Waiting for the
// trace to settle avoids re-running detectors on every batch of a live run.
func (s *Store) PendingDetection(ctx context.Context, settledBeforeNS int64, limit int) ([]TraceSummary, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+traceColumns+` FROM traces
		WHERE last_ingest_ns > detected_ns AND last_ingest_ns < ? ORDER BY last_ingest_ns LIMIT ?`, settledBeforeNS, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TraceSummary
	for rows.Next() {
		var t TraceSummary
		if err := scanTrace(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTracesBefore removes traces whose newest span ended before cutoffNS,
// in chunks so the single writer connection is never held for long.
// It returns how many traces and spans were removed.
func (s *Store) DeleteTracesBefore(ctx context.Context, cutoffNS int64, chunk int) (traces, spans int64, err error) {
	if chunk <= 0 {
		chunk = 500
	}
	for {
		tx, err := s.w.BeginTx(ctx, nil)
		if err != nil {
			return traces, spans, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT trace_id FROM traces WHERE end_ns < ? LIMIT ?`, cutoffNS, chunk)
		if err != nil {
			tx.Rollback()
			return traces, spans, err
		}
		var ids []any
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				tx.Rollback()
				return traces, spans, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) == 0 {
			tx.Rollback()
			return traces, spans, nil
		}
		in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
		res, err := tx.ExecContext(ctx, `DELETE FROM spans WHERE trace_id IN `+in, ids...)
		if err != nil {
			tx.Rollback()
			return traces, spans, err
		}
		n, _ := res.RowsAffected()
		spans += n
		for _, q := range []string{`DELETE FROM findings WHERE trace_id IN `, `DELETE FROM traces WHERE trace_id IN `} {
			if _, err := tx.ExecContext(ctx, q+in, ids...); err != nil {
				tx.Rollback()
				return traces, spans, err
			}
		}
		if err := tx.Commit(); err != nil {
			return traces, spans, err
		}
		traces += int64(len(ids))
	}
}

// Counts returns total rows, used by /metrics and tests.
func (s *Store) Counts(ctx context.Context) (traces, spans int64, err error) {
	if err = s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM traces`).Scan(&traces); err != nil {
		return
	}
	err = s.r.QueryRowContext(ctx, `SELECT COALESCE(SUM(span_count),0) FROM traces`).Scan(&spans)
	return
}
