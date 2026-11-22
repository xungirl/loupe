package otlp

import (
	"strings"
	"testing"

	"github.com/xungirl/loupe/server/internal/model"
)

const now = int64(1767225600_000000000)

// A trimmed OTLP/JSON export with GenAI semantic-convention attributes, in the
// shape the OpenTelemetry SDKs' http/json exporters produce.
const sample = `{
 "resourceSpans": [{
  "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "my-agent"}}]},
  "scopeSpans": [{
   "scope": {"name": "x"},
   "spans": [
    {"traceId": "5B8EFFF798038103D269B633813FC60C", "spanId": "EEE19B7EC3C1B174", "name": "invoke_agent coder",
     "startTimeUnixNano": "1767225600000000000", "endTimeUnixNano": "1767225605000000000",
     "attributes": [
       {"key": "gen_ai.operation.name", "value": {"stringValue": "invoke_agent"}},
       {"key": "gen_ai.agent.name", "value": {"stringValue": "coder"}},
       {"key": "gen_ai.agent.id", "value": {"stringValue": "agent-1"}}]},
    {"traceId": "5B8EFFF798038103D269B633813FC60C", "spanId": "AAA19B7EC3C1B174", "parentSpanId": "EEE19B7EC3C1B174",
     "name": "chat qwen", "startTimeUnixNano": 1767225601000000000, "endTimeUnixNano": "1767225602000000000",
     "attributes": [
       {"key": "gen_ai.operation.name", "value": {"stringValue": "chat"}},
       {"key": "gen_ai.request.model", "value": {"stringValue": "qwen3.8-flash"}},
       {"key": "gen_ai.usage.input_tokens", "value": {"intValue": "1200"}},
       {"key": "gen_ai.usage.output_tokens", "value": {"intValue": 80}},
       {"key": "custom.flag", "value": {"boolValue": true}}]},
    {"traceId": "5B8EFFF798038103D269B633813FC60C", "spanId": "BBB19B7EC3C1B174", "parentSpanId": "EEE19B7EC3C1B174",
     "name": "execute_tool run_tests", "startTimeUnixNano": "1767225602000000000", "endTimeUnixNano": "1767225603000000000",
     "status": {"code": 2, "message": "2 tests failed"},
     "attributes": [
       {"key": "gen_ai.operation.name", "value": {"stringValue": "execute_tool"}},
       {"key": "gen_ai.tool.name", "value": {"stringValue": "run_tests"}},
       {"key": "gen_ai.tool.call.arguments", "value": {"stringValue": "{\"path\": \"solution.py\"}"}}]},
    {"traceId": "", "spanId": "CCC", "name": "broken", "startTimeUnixNano": "1"}
   ]}]}]}`

func TestDecodeGenAI(t *testing.T) {
	res, err := Decode([]byte(sample), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Spans) != 3 || res.Rejected != 1 || len(res.Errors) != 1 {
		t.Fatalf("spans=%d rejected=%d errors=%v", len(res.Spans), res.Rejected, res.Errors)
	}
	agent, chat, tool := res.Spans[0], res.Spans[1], res.Spans[2]
	if agent.Kind != model.KindAgent || agent.AgentName != "coder" || agent.AgentID != "agent-1" || agent.Source != "my-agent" {
		t.Fatalf("agent span %+v", agent)
	}
	if agent.TraceID != "5b8efff798038103d269b633813fc60c" {
		t.Fatalf("ids should be lower-cased hex, got %s", agent.TraceID)
	}
	if chat.Kind != model.KindLLM || chat.Model != "qwen3.8-flash" || chat.InputTokens != 1200 || chat.OutputTokens != 80 {
		t.Fatalf("chat span %+v", chat)
	}
	if !strings.Contains(chat.Attributes, `"custom.flag":true`) {
		t.Fatalf("unknown attributes should be kept, got %q", chat.Attributes)
	}
	if tool.Kind != model.KindTool || tool.Status != model.StatusError || tool.ErrorMsg != "2 tests failed" || tool.Target != "solution.py" {
		t.Fatalf("tool span %+v", tool)
	}
}

func TestDecodeMalformed(t *testing.T) {
	for _, body := range []string{`{`, `{"resourceSpans": 5}`, `{"resourceSpans":[{"scopeSpans":[{"spans":[{"startTimeUnixNano":"abc"}]}]}]}`} {
		if _, err := Decode([]byte(body), now); err == nil {
			t.Errorf("Decode(%q) should fail", body)
		}
	}
	res, err := Decode([]byte(`{}`), now)
	if err != nil || len(res.Spans) != 0 {
		t.Fatalf("empty export should decode to nothing: %v %v", res, err)
	}
}

func TestStatusCodeEnumString(t *testing.T) {
	body := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"a","spanId":"b","name":"x",
	  "startTimeUnixNano":"1767225600000000000","status":{"code":"STATUS_CODE_ERROR"}}]}]}]}`
	res, err := Decode([]byte(body), now)
	if err != nil || len(res.Spans) != 1 || !res.Spans[0].IsError() || res.Spans[0].Source != "otlp" {
		t.Fatalf("%+v %v", res, err)
	}
}
