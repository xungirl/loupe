package model

import (
	"encoding/json"
	"strings"
)

// targetKeys are the tool-input fields that name "the thing being acted on",
// in priority order. They cover Claude Code's built-in tools (Read/Edit/Write
// use file_path, Bash uses command, Grep/Glob use pattern) and the mini agent.
var targetKeys = []string{"file_path", "path", "notebook_path", "filename", "command", "cmd", "url", "pattern", "query"}

// ExtractTarget pulls a short target string out of a JSON tool input.
// It returns "" if the input is not a JSON object or has none of the keys.
func ExtractTarget(toolInput string) string {
	toolInput = strings.TrimSpace(toolInput)
	if !strings.HasPrefix(toolInput, "{") {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(toolInput), &m); err != nil {
		return ""
	}
	for _, k := range targetKeys {
		if v, ok := m[k].(string); ok && v != "" {
			v = strings.Join(strings.Fields(v), " ") // collapse whitespace/newlines
			return truncate(v, 200)
		}
	}
	return ""
}
