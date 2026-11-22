// Package model defines Loupe's core data type: the Span.
//
// Every source (Claude Code transcripts, the Python SDK, OpenTelemetry GenAI
// spans) is normalised into this one shape before it touches storage. Keeping
// a single flat struct makes ingestion, storage and detectors independent of
// where the data came from.
package model

import (
	"errors"
	"fmt"
	"strings"
)

// Span kinds. A trace is a tree of spans; an "agent" span marks the start of a
// (sub)agent and everything under it belongs to that agent until another agent
// span appears.
const (
	KindAgent = "agent" // a whole agent run (root, or a subagent spawned by a tool)
	KindLLM   = "llm"   // one model call
	KindTool  = "tool"  // one tool call (read file, bash, run tests, ...)
	KindStep  = "step"  // a logical phase (plan, restate, review, ...)
	KindEvent = "event" // an instant (user prompt, hook event, ...)
)

// Status values.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Limits applied during validation so one bad client can't blow up storage.
const (
	MaxIDLen       = 128
	MaxNameLen     = 256
	MaxPayloadLen  = 4096 // tool input / output are truncated to this many bytes
	MaxAttrsLen    = 8192
	maxFutureSkewS = 24 * 3600
)

// Span is one unit of work inside a trace. Times are Unix nanoseconds.
type Span struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`

	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Source string `json:"source,omitempty"` // e.g. "claude-code", "sdk", "otlp"

	// Agent identity. Spans inherit the agent of their nearest agent-span ancestor
	// at assembly time if these are empty.
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`

	StartNS int64 `json:"start_ns"`
	EndNS   int64 `json:"end_ns"`

	Status   string `json:"status,omitempty"`
	ErrorMsg string `json:"error_msg,omitempty"`

	// LLM fields.
	Model            string  `json:"model,omitempty"`
	InputTokens      int64   `json:"input_tokens,omitempty"`
	OutputTokens     int64   `json:"output_tokens,omitempty"`
	CacheReadTokens  int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64   `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"` // filled by the price table if 0

	// Tool fields. Target is the "thing acted on" (a file path or a command),
	// extracted so detectors can group by it without parsing JSON.
	ToolName   string `json:"tool_name,omitempty"`
	Target     string `json:"target,omitempty"`
	ToolInput  string `json:"tool_input,omitempty"`
	ToolOutput string `json:"tool_output,omitempty"`

	// Free-form extra attributes as a JSON object string.
	Attributes string `json:"attributes,omitempty"`
}

// DurationNS is the span length; negative/unknown end times count as zero.
func (s *Span) DurationNS() int64 {
	if s.EndNS < s.StartNS {
		return 0
	}
	return s.EndNS - s.StartNS
}

// TotalTokens counts every token the call was billed for.
func (s *Span) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheWriteTokens
}

// IsError reports whether the span failed.
func (s *Span) IsError() bool { return s.Status == StatusError }

var validKinds = map[string]bool{KindAgent: true, KindLLM: true, KindTool: true, KindStep: true, KindEvent: true}

// ErrInvalid wraps all validation failures so callers can map them to HTTP 400.
var ErrInvalid = errors.New("invalid span")

// Normalize validates a span in place and fills defaults. It truncates long
// payloads instead of rejecting them (lossy but keeps the trace usable) and
// rejects only spans that cannot be stored meaningfully.
func (s *Span) Normalize(nowNS int64) error {
	if s.TraceID == "" || s.SpanID == "" {
		return fmt.Errorf("%w: trace_id and span_id are required", ErrInvalid)
	}
	if len(s.TraceID) > MaxIDLen || len(s.SpanID) > MaxIDLen || len(s.ParentSpanID) > MaxIDLen {
		return fmt.Errorf("%w: id longer than %d bytes", ErrInvalid, MaxIDLen)
	}
	if s.ParentSpanID == s.SpanID {
		return fmt.Errorf("%w: span %q is its own parent", ErrInvalid, s.SpanID)
	}
	if s.Kind == "" {
		s.Kind = KindStep
	}
	s.Kind = strings.ToLower(s.Kind)
	if !validKinds[s.Kind] {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalid, s.Kind)
	}
	if s.StartNS <= 0 {
		return fmt.Errorf("%w: start_ns must be positive", ErrInvalid)
	}
	if s.StartNS > nowNS+maxFutureSkewS*1e9 {
		return fmt.Errorf("%w: start_ns is more than a day in the future", ErrInvalid)
	}
	if s.EndNS != 0 && s.EndNS < s.StartNS {
		return fmt.Errorf("%w: end_ns before start_ns", ErrInvalid)
	}
	if s.EndNS == 0 {
		s.EndNS = s.StartNS // still-open or instant span
	}
	if s.InputTokens < 0 || s.OutputTokens < 0 || s.CacheReadTokens < 0 || s.CacheWriteTokens < 0 || s.CostUSD < 0 {
		return fmt.Errorf("%w: negative token count or cost", ErrInvalid)
	}
	switch s.Status {
	case "", "unset", StatusOK:
		s.Status = StatusOK
	case StatusError:
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, s.Status)
	}
	if s.Name == "" {
		s.Name = s.Kind
		if s.ToolName != "" {
			s.Name = s.ToolName
		}
	}
	s.Name = truncate(s.Name, MaxNameLen)
	s.AgentName = truncate(s.AgentName, MaxNameLen)
	s.Target = truncate(s.Target, MaxNameLen*2)
	s.ToolInput = truncate(s.ToolInput, MaxPayloadLen)
	s.ToolOutput = truncate(s.ToolOutput, MaxPayloadLen)
	s.ErrorMsg = truncate(s.ErrorMsg, MaxPayloadLen)
	if len(s.Attributes) > MaxAttrsLen {
		s.Attributes = "" // can't safely truncate JSON; drop it
	}
	return nil
}

// truncate cuts s to at most n bytes without splitting a UTF-8 character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
