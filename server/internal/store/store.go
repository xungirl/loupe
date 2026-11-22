// Package store persists spans, trace summaries and findings in SQLite.
//
// SQLite in WAL mode allows one writer and many concurrent readers. We mirror
// that in Go with two connection pools: a single-connection write pool (all
// writes are serialised by the ingest pipeline anyway) and a multi-connection
// read pool used by the query API.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Store wraps the two SQLite pools.
type Store struct {
	w *sql.DB // write pool, exactly one connection
	r *sql.DB // read pool
}

// ErrNotFound is returned when a trace does not exist.
var ErrNotFound = errors.New("not found")

// Open opens (creating if needed) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	pragmas := "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(OFF)"
	w, err := sql.Open("sqlite", "file:"+path+pragmas)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if _, err := w.Exec(schema); err != nil {
		w.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	r, err := sql.Open("sqlite", "file:"+path+pragmas+"&_pragma=query_only(1)")
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(runtime.NumCPU())
	return &Store{w: w, r: r}, nil
}

// Close closes both pools.
func (s *Store) Close() error {
	return errors.Join(s.r.Close(), s.w.Close())
}

// traceDelta accumulates the summary changes caused by newly inserted spans.
type traceDelta struct {
	name, source                    string
	start, end                      int64
	spans, errors, llm, tools, agts int64
	in, out, cr, cw                 int64
	cost                            float64
}

func (d *traceDelta) add(sp *model.Span) {
	if d.spans == 0 || sp.StartNS < d.start {
		d.start = sp.StartNS
	}
	if sp.EndNS > d.end {
		d.end = sp.EndNS
	}
	d.spans++
	if sp.IsError() {
		d.errors++
	}
	switch sp.Kind {
	case model.KindLLM:
		d.llm++
	case model.KindTool:
		d.tools++
	case model.KindAgent:
		d.agts++
		if sp.ParentSpanID == "" && d.name == "" {
			d.name = sp.Name // the root agent names the trace
		}
	}
	if d.source == "" {
		d.source = sp.Source
	}
	d.in += sp.InputTokens
	d.out += sp.OutputTokens
	d.cr += sp.CacheReadTokens
	d.cw += sp.CacheWriteTokens
	d.cost += sp.CostUSD
}

const insertSpanSQL = `INSERT OR IGNORE INTO spans (
	trace_id, span_id, parent_span_id, kind, name, source, agent_id, agent_name,
	start_ns, end_ns, status, error_msg, model, input_tokens, output_tokens,
	cache_read_tokens, cache_write_tokens, cost_usd, tool_name, target,
	tool_input, tool_output, attributes, ingested_ns)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

const upsertTraceSQL = `INSERT INTO traces (
	trace_id, name, source, start_ns, end_ns, span_count, error_count, llm_calls,
	tool_calls, agent_count, input_tokens, output_tokens, cache_read_tokens,
	cache_write_tokens, cost_usd, last_ingest_ns)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(trace_id) DO UPDATE SET
	name               = CASE WHEN traces.name = '' THEN excluded.name ELSE traces.name END,
	source             = CASE WHEN traces.source = '' THEN excluded.source ELSE traces.source END,
	start_ns           = MIN(traces.start_ns, excluded.start_ns),
	end_ns             = MAX(traces.end_ns, excluded.end_ns),
	span_count         = traces.span_count + excluded.span_count,
	error_count        = traces.error_count + excluded.error_count,
	llm_calls          = traces.llm_calls + excluded.llm_calls,
	tool_calls         = traces.tool_calls + excluded.tool_calls,
	agent_count        = traces.agent_count + excluded.agent_count,
	input_tokens       = traces.input_tokens + excluded.input_tokens,
	output_tokens      = traces.output_tokens + excluded.output_tokens,
	cache_read_tokens  = traces.cache_read_tokens + excluded.cache_read_tokens,
	cache_write_tokens = traces.cache_write_tokens + excluded.cache_write_tokens,
	cost_usd           = traces.cost_usd + excluded.cost_usd,
	last_ingest_ns     = excluded.last_ingest_ns`

// InsertSpans writes a batch in one transaction and returns only the spans that
// were actually new. Duplicates (same trace_id+span_id) are silently ignored,
// which makes client retries safe. Trace summaries are updated from the new
// spans only, so a retried batch never double-counts tokens or cost.
func (s *Store) InsertSpans(ctx context.Context, spans []model.Span) ([]model.Span, error) {
	if len(spans) == 0 {
		return nil, nil
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // no-op after Commit

	ins, err := tx.PrepareContext(ctx, insertSpanSQL)
	if err != nil {
		return nil, err
	}
	defer ins.Close()

	now := time.Now().UnixNano()
	deltas := map[string]*traceDelta{}
	order := []string{} // deterministic upsert order
	inserted := make([]model.Span, 0, len(spans))
	for i := range spans {
		sp := &spans[i]
		res, err := ins.ExecContext(ctx, sp.TraceID, sp.SpanID, sp.ParentSpanID, sp.Kind, sp.Name,
			sp.Source, sp.AgentID, sp.AgentName, sp.StartNS, sp.EndNS, sp.Status, sp.ErrorMsg,
			sp.Model, sp.InputTokens, sp.OutputTokens, sp.CacheReadTokens, sp.CacheWriteTokens,
			sp.CostUSD, sp.ToolName, sp.Target, sp.ToolInput, sp.ToolOutput, sp.Attributes, now)
		if err != nil {
			return nil, fmt.Errorf("insert span %s/%s: %w", sp.TraceID, sp.SpanID, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // duplicate
		}
		d, ok := deltas[sp.TraceID]
		if !ok {
			d = &traceDelta{}
			deltas[sp.TraceID] = d
			order = append(order, sp.TraceID)
		}
		d.add(sp)
		inserted = append(inserted, *sp)
	}

	if len(order) > 0 {
		up, err := tx.PrepareContext(ctx, upsertTraceSQL)
		if err != nil {
			return nil, err
		}
		defer up.Close()
		for _, id := range order {
			d := deltas[id]
			if _, err := up.ExecContext(ctx, id, d.name, d.source, d.start, d.end, d.spans, d.errors,
				d.llm, d.tools, d.agts, d.in, d.out, d.cr, d.cw, d.cost, now); err != nil {
				return nil, fmt.Errorf("upsert trace %s: %w", id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return inserted, nil
}

const spanColumns = `trace_id, span_id, parent_span_id, kind, name, source, agent_id, agent_name,
	start_ns, end_ns, status, error_msg, model, input_tokens, output_tokens,
	cache_read_tokens, cache_write_tokens, cost_usd, tool_name, target,
	tool_input, tool_output, attributes`

func scanSpan(rows interface{ Scan(...any) error }, sp *model.Span) error {
	return rows.Scan(&sp.TraceID, &sp.SpanID, &sp.ParentSpanID, &sp.Kind, &sp.Name, &sp.Source,
		&sp.AgentID, &sp.AgentName, &sp.StartNS, &sp.EndNS, &sp.Status, &sp.ErrorMsg, &sp.Model,
		&sp.InputTokens, &sp.OutputTokens, &sp.CacheReadTokens, &sp.CacheWriteTokens, &sp.CostUSD,
		&sp.ToolName, &sp.Target, &sp.ToolInput, &sp.ToolOutput, &sp.Attributes)
}

// TraceSpans returns every span of a trace ordered by start time.
func (s *Store) TraceSpans(ctx context.Context, traceID string) ([]model.Span, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+spanColumns+` FROM spans WHERE trace_id = ? ORDER BY start_ns, span_id`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Span
	for rows.Next() {
		var sp model.Span
		if err := scanSpan(rows, &sp); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// DB exposes the read pool for packages that run ad-hoc aggregate queries.
func (s *Store) DB() *sql.DB { return s.r }
