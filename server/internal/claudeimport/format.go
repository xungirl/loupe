// Package claudeimport converts Claude Code session transcripts (JSONL) into
// Loupe spans.
//
// Format (publicly documented / observable; we only rely on these fields):
//
//	~/.claude/projects/<project-slug>/<sessionId>.jsonl                  main session
//	~/.claude/projects/<project-slug>/<sessionId>/subagents/agent-*.jsonl subagents (newer versions)
//
// Each line is one JSON entry:
//
//	type          "user" | "assistant" | others (summary, system, ...) we skip
//	uuid          entry id; parentUuid links to the previous entry
//	sessionId     session the entry belongs to
//	timestamp     RFC 3339
//	isSidechain   true for subagent entries (older versions keep them inline)
//	agentId       subagent id (newer versions)
//	message       {role, model, id, content, usage}
//	  content     string, or blocks: text | thinking | tool_use{id,name,input}
//	              | tool_result{tool_use_id, content, is_error}
//	  usage       {input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens}
//	toolUseResult extra result data; for the Task/Agent tool it may carry agentId
//
// Anything else is ignored, so new Claude Code versions that add fields keep
// working. Lines that fail to parse are counted, not fatal.
package claudeimport

import (
	"encoding/json"
	"strings"
	"time"
)

// entry is one transcript line.
type entry struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	ParentUUID    *string         `json:"parentUuid"`
	SessionID     string          `json:"sessionId"`
	Timestamp     string          `json:"timestamp"`
	IsSidechain   bool            `json:"isSidechain"`
	IsMeta        bool            `json:"isMeta"`
	IsAPIError    bool            `json:"isApiErrorMessage"`
	AgentID       string          `json:"agentId"`
	Cwd           string          `json:"cwd"`
	Version       string          `json:"version"`
	GitBranch     string          `json:"gitBranch"`
	Message       *message        `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`

	ts   time.Time // parsed Timestamp
	line int       // 1-based line number, for stable ordering
}

type message struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// block is one content block. Fields are a union over block types.
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (e *entry) parent() string {
	if e.ParentUUID == nil {
		return ""
	}
	return *e.ParentUUID
}

// blocks returns the content as blocks; a plain string becomes one text block.
func (m *message) blocks() []block {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	if json.Unmarshal(m.Content, &bs) == nil {
		return bs
	}
	return nil // unknown content shape: ignore rather than fail
}

// resultText flattens a tool_result's content (string or [{type:text,text}]).
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []block
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

// isAgentTool reports whether a tool spawns a subagent. Claude Code has called
// it "Task" and, in newer versions, "Agent".
func isAgentTool(name string) bool { return name == "Task" || name == "Agent" }

// taskInput is the input of a Task/Agent tool call.
type taskInput struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
}

func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
