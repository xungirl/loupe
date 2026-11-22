package store

import (
	"context"
	"strings"
)

// Stats is the payload of the Insights page.
type Stats struct {
	Totals struct {
		Traces       int64   `json:"traces"`
		Spans        int64   `json:"spans"`
		Errors       int64   `json:"errors"`
		LLMCalls     int64   `json:"llm_calls"`
		ToolCalls    int64   `json:"tool_calls"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		CacheTokens  int64   `json:"cache_tokens"`
		CostUSD      float64 `json:"cost_usd"`
		Findings     int64   `json:"findings"`
	} `json:"totals"`
	ByTool     []ToolStat     `json:"by_tool"`
	ByModel    []ModelStat    `json:"by_model"`
	ByAgent    []AgentStat    `json:"by_agent"`
	ByDay      []DayStat      `json:"by_day"`
	ByDetector []DetectorStat `json:"by_detector"`
	Hotspots   []Hotspot      `json:"hotspots"`
}

// ToolStat aggregates one tool across traces.
type ToolStat struct {
	Tool      string  `json:"tool"`
	Calls     int64   `json:"calls"`
	Errors    int64   `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	AvgMS     float64 `json:"avg_ms"`
}

// ModelStat aggregates one model.
type ModelStat struct {
	Model        string  `json:"model"`
	Calls        int64   `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Unpriced     bool    `json:"unpriced"`
}

// AgentStat aggregates by agent name (e.g. "planner", "general-purpose").
type AgentStat struct {
	Agent     string  `json:"agent"`
	Runs      int64   `json:"runs"`
	LLMCalls  int64   `json:"llm_calls"`
	ToolCalls int64   `json:"tool_calls"`
	Errors    int64   `json:"errors"`
	Tokens    int64   `json:"tokens"`
	CostUSD   float64 `json:"cost_usd"`
}

// DayStat is one calendar day (UTC).
type DayStat struct {
	Day     string  `json:"day"`
	Traces  int64   `json:"traces"`
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

// DetectorStat counts findings per detector.
type DetectorStat struct {
	Detector string `json:"detector"`
	Findings int64  `json:"findings"`
	Traces   int64  `json:"traces"`
}

// Hotspot is a file (or command) edited/run unusually often — rework candidates.
type Hotspot struct {
	Target string `json:"target"`
	Tool   string `json:"tool"`
	Calls  int64  `json:"calls"`
	Traces int64  `json:"traces"`
	Errors int64  `json:"errors"`
}

// StatsFilter scopes Stats to a time window and/or source.
type StatsFilter struct {
	SinceNS int64
	Source  string
}

// Stats computes all aggregates. These are full scans over the matching spans;
// BENCHMARK.md reports how long that takes at ~1M spans.
func (s *Store) Stats(ctx context.Context, f StatsFilter) (*Stats, error) {
	var tw []string // conditions on traces t
	var args []any
	if f.SinceNS > 0 {
		tw = append(tw, "t.start_ns >= ?")
		args = append(args, f.SinceNS)
	}
	if f.Source != "" {
		tw = append(tw, "t.source = ?")
		args = append(args, f.Source)
	}
	traceWhere := ""
	spanJoin := " FROM spans s"
	if len(tw) > 0 {
		traceWhere = " WHERE " + strings.Join(tw, " AND ")
		spanJoin = " FROM spans s JOIN traces t ON t.trace_id = s.trace_id" + traceWhere
	}
	and := func(cond string) string {
		if len(tw) > 0 {
			return " AND " + cond
		}
		return " WHERE " + cond
	}

	st := &Stats{}
	T := &st.Totals
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(span_count),0), COALESCE(SUM(error_count),0),
		COALESCE(SUM(llm_calls),0), COALESCE(SUM(tool_calls),0), COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens + cache_write_tokens),0),
		COALESCE(SUM(cost_usd),0), COALESCE(SUM(finding_count),0) FROM traces t`+traceWhere, args...).
		Scan(&T.Traces, &T.Spans, &T.Errors, &T.LLMCalls, &T.ToolCalls, &T.InputTokens, &T.OutputTokens,
			&T.CacheTokens, &T.CostUSD, &T.Findings); err != nil {
		return nil, err
	}

	rows, err := s.r.QueryContext(ctx, `SELECT s.tool_name, COUNT(*), SUM(s.status = 'error'),
		AVG(s.end_ns - s.start_ns) / 1e6`+spanJoin+and("s.kind = 'tool' AND s.tool_name != ''")+
		` GROUP BY s.tool_name ORDER BY COUNT(*) DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t ToolStat
		if err := rows.Scan(&t.Tool, &t.Calls, &t.Errors, &t.AvgMS); err != nil {
			rows.Close()
			return nil, err
		}
		if t.Calls > 0 {
			t.ErrorRate = float64(t.Errors) / float64(t.Calls)
		}
		st.ByTool = append(st.ByTool, t)
	}
	rows.Close()

	rows, err = s.r.QueryContext(ctx, `SELECT s.model, COUNT(*), SUM(s.input_tokens), SUM(s.output_tokens),
		SUM(s.cache_read_tokens + s.cache_write_tokens), SUM(s.cost_usd)`+spanJoin+and("s.kind = 'llm'")+
		` GROUP BY s.model ORDER BY SUM(s.cost_usd) DESC, COUNT(*) DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m ModelStat
		if err := rows.Scan(&m.Model, &m.Calls, &m.InputTokens, &m.OutputTokens, &m.CacheTokens, &m.CostUSD); err != nil {
			rows.Close()
			return nil, err
		}
		m.Unpriced = m.CostUSD == 0 && (m.InputTokens+m.OutputTokens) > 0 && !strings.HasPrefix(m.Model, "fake-")
		st.ByModel = append(st.ByModel, m)
	}
	rows.Close()

	rows, err = s.r.QueryContext(ctx, `SELECT s.agent_name, SUM(s.kind = 'agent'), SUM(s.kind = 'llm'),
		SUM(s.kind = 'tool'), SUM(s.status = 'error'),
		SUM(s.input_tokens + s.output_tokens + s.cache_read_tokens + s.cache_write_tokens), SUM(s.cost_usd)`+
		spanJoin+and("s.agent_name != ''")+` GROUP BY s.agent_name ORDER BY 7 DESC, 3 DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a AgentStat
		if err := rows.Scan(&a.Agent, &a.Runs, &a.LLMCalls, &a.ToolCalls, &a.Errors, &a.Tokens, &a.CostUSD); err != nil {
			rows.Close()
			return nil, err
		}
		st.ByAgent = append(st.ByAgent, a)
	}
	rows.Close()

	rows, err = s.r.QueryContext(ctx, `SELECT date(t.start_ns / 1000000000, 'unixepoch') AS d, COUNT(*),
		SUM(t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens), SUM(t.cost_usd)
		FROM traces t`+traceWhere+` GROUP BY d ORDER BY d DESC LIMIT 60`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DayStat
		if err := rows.Scan(&d.Day, &d.Traces, &d.Tokens, &d.CostUSD); err != nil {
			rows.Close()
			return nil, err
		}
		st.ByDay = append(st.ByDay, d)
	}
	rows.Close()

	fjoin := " FROM findings f"
	if len(tw) > 0 {
		fjoin = " FROM findings f JOIN traces t ON t.trace_id = f.trace_id" + traceWhere
	}
	rows, err = s.r.QueryContext(ctx, `SELECT f.detector, COUNT(*), COUNT(DISTINCT f.trace_id)`+fjoin+
		` GROUP BY f.detector ORDER BY 2 DESC`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DetectorStat
		if err := rows.Scan(&d.Detector, &d.Findings, &d.Traces); err != nil {
			rows.Close()
			return nil, err
		}
		st.ByDetector = append(st.ByDetector, d)
	}
	rows.Close()

	// Rework hotspots: targets of edit-like tools, ranked by call count.
	rows, err = s.r.QueryContext(ctx, `SELECT s.target, s.tool_name, COUNT(*), COUNT(DISTINCT s.trace_id),
		SUM(s.status = 'error')`+spanJoin+and(`s.kind = 'tool' AND s.target != '' AND
		lower(s.tool_name) IN ('edit','multiedit','write','write_file','str_replace','apply_patch','notebookedit')`)+
		` GROUP BY s.target, s.tool_name HAVING COUNT(*) >= 3 ORDER BY 3 DESC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h Hotspot
		if err := rows.Scan(&h.Target, &h.Tool, &h.Calls, &h.Traces, &h.Errors); err != nil {
			rows.Close()
			return nil, err
		}
		st.Hotspots = append(st.Hotspots, h)
	}
	rows.Close()
	return st, nil
}
