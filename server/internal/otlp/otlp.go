// Package otlp decodes OTLP/HTTP JSON trace exports (ExportTraceServiceRequest)
// and maps OpenTelemetry GenAI semantic-convention attributes onto Loupe spans.
//
// Only the JSON encoding is supported (not protobuf); it is what browsers,
// curl and most SDKs' "http/json" exporters send. IDs arrive as hex strings
// and 64-bit integers arrive as JSON strings, both per the OTLP/JSON spec.
package otlp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/xungirl/loupe/server/internal/model"
)

// ExportRequest mirrors the subset of opentelemetry.proto.collector.trace.v1
// ExportTraceServiceRequest that Loupe uses. Unknown fields are ignored.
type ExportRequest struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []KeyValue `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Spans []Span `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

// Span is an OTLP span in JSON form.
type Span struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId"`
	Name              string     `json:"name"`
	StartTimeUnixNano flexInt    `json:"startTimeUnixNano"`
	EndTimeUnixNano   flexInt    `json:"endTimeUnixNano"`
	Attributes        []KeyValue `json:"attributes"`
	Status            struct {
		Code    json.RawMessage `json:"code"` // int (2) or enum string ("STATUS_CODE_ERROR")
		Message string          `json:"message"`
	} `json:"status"`
}

// KeyValue is an OTLP attribute.
type KeyValue struct {
	Key   string   `json:"key"`
	Value AnyValue `json:"value"`
}

// AnyValue is the OTLP tagged union for attribute values.
type AnyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *flexInt `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	ArrayValue  *struct {
		Values []AnyValue `json:"values"`
	} `json:"arrayValue,omitempty"`
}

// flexInt accepts 123 or "123" (OTLP/JSON encodes int64 as strings).
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// Some exporters send floats for times; accept and truncate.
		fl, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return fmt.Errorf("not an integer: %s", s)
		}
		n = int64(fl)
	}
	*f = flexInt(n)
	return nil
}

// value converts an AnyValue to a plain Go value.
func (v AnyValue) value() any {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.IntValue != nil:
		return int64(*v.IntValue)
	case v.DoubleValue != nil:
		return *v.DoubleValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.ArrayValue != nil:
		out := make([]any, 0, len(v.ArrayValue.Values))
		for _, e := range v.ArrayValue.Values {
			out = append(out, e.value())
		}
		return out
	}
	return nil
}

// Result reports how many spans were accepted and why others were rejected.
type Result struct {
	Spans    []model.Span
	Rejected int
	Errors   []string // first few validation errors, for the response body
}

// Decode parses an OTLP/JSON body and maps every span. Invalid spans are
// counted in Rejected (OTLP "partial success") rather than failing the batch.
func Decode(body []byte, nowNS int64) (Result, error) {
	var req ExportRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return Result{}, fmt.Errorf("decode OTLP JSON: %w", err)
	}
	var res Result
	for _, rs := range req.ResourceSpans {
		resAttrs := toMap(rs.Resource.Attributes)
		source, _ := resAttrs["service.name"].(string)
		if source == "" {
			source = "otlp"
		}
		for _, ss := range rs.ScopeSpans {
			for _, os := range ss.Spans {
				sp := mapSpan(os, source)
				if err := sp.Normalize(nowNS); err != nil {
					res.Rejected++
					if len(res.Errors) < 5 {
						res.Errors = append(res.Errors, err.Error())
					}
					continue
				}
				res.Spans = append(res.Spans, sp)
			}
		}
	}
	return res, nil
}

func toMap(kvs []KeyValue) map[string]any {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.value()
	}
	return m
}

// mapSpan applies the GenAI semantic conventions:
//
//	gen_ai.operation.name   chat|text_completion|generate_content → llm
//	                        execute_tool → tool, invoke_agent|create_agent → agent
//	gen_ai.request.model / gen_ai.response.model → Model
//	gen_ai.usage.input_tokens / output_tokens     → token counts
//	gen_ai.tool.name, gen_ai.tool.call.arguments  → tool fields
//	gen_ai.agent.id / gen_ai.agent.name           → agent identity
//
// Attributes Loupe does not model stay in Attributes as JSON.
func mapSpan(os Span, source string) model.Span {
	attrs := toMap(os.Attributes)
	take := func(keys ...string) any {
		for _, k := range keys {
			if v, ok := attrs[k]; ok {
				delete(attrs, k)
				return v
			}
		}
		return nil
	}
	str := func(keys ...string) string { s, _ := take(keys...).(string); return s }
	num := func(keys ...string) int64 {
		switch v := take(keys...).(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case string:
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
		return 0
	}

	sp := model.Span{
		TraceID:      strings.ToLower(os.TraceID),
		SpanID:       strings.ToLower(os.SpanID),
		ParentSpanID: strings.ToLower(os.ParentSpanID),
		Name:         os.Name,
		Source:       source,
		StartNS:      int64(os.StartTimeUnixNano),
		EndNS:        int64(os.EndTimeUnixNano),
	}
	op := str("gen_ai.operation.name")
	kind := str("loupe.kind")
	sp.Model = str("gen_ai.response.model")
	if reqModel := str("gen_ai.request.model"); sp.Model == "" {
		sp.Model = reqModel
	}
	sp.InputTokens = num("gen_ai.usage.input_tokens", "gen_ai.usage.prompt_tokens")
	sp.OutputTokens = num("gen_ai.usage.output_tokens", "gen_ai.usage.completion_tokens")
	sp.CacheReadTokens = num("gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.cache_read_input_tokens")
	sp.CacheWriteTokens = num("gen_ai.usage.cache_creation.input_tokens", "gen_ai.usage.cache_creation_input_tokens")
	sp.ToolName = str("gen_ai.tool.name")
	sp.ToolInput = str("gen_ai.tool.call.arguments", "loupe.tool.input")
	sp.ToolOutput = str("gen_ai.tool.call.result", "loupe.tool.output")
	sp.AgentID = str("gen_ai.agent.id")
	sp.AgentName = str("gen_ai.agent.name")
	sp.Target = str("loupe.target", "file.path")
	if cost, ok := take("loupe.cost_usd").(float64); ok {
		sp.CostUSD = cost
	}

	if kind == "" {
		switch op {
		case "chat", "text_completion", "generate_content", "embeddings":
			kind = model.KindLLM
		case "execute_tool":
			kind = model.KindTool
		case "invoke_agent", "create_agent":
			kind = model.KindAgent
		default:
			switch {
			case sp.ToolName != "":
				kind = model.KindTool
			case sp.Model != "":
				kind = model.KindLLM
			default:
				kind = model.KindStep
			}
		}
	}
	sp.Kind = kind
	if sp.Target == "" && sp.Kind == model.KindTool {
		sp.Target = model.ExtractTarget(sp.ToolInput)
	}

	if isErrorStatus(os.Status.Code) {
		sp.Status = model.StatusError
		sp.ErrorMsg = os.Status.Message
		if et := str("error.type"); sp.ErrorMsg == "" {
			sp.ErrorMsg = et
		}
	}
	if len(attrs) > 0 {
		if b, err := json.Marshal(attrs); err == nil {
			sp.Attributes = string(b)
		}
	}
	return sp
}

func isErrorStatus(code json.RawMessage) bool {
	s := strings.Trim(string(code), `" `)
	return s == "2" || s == "STATUS_CODE_ERROR"
}
