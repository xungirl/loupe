package claudeimport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xungirl/loupe/server/internal/model"
)

// Stats reports what the importer saw. Printing it after every import makes
// format drift visible instead of silently losing data.
type Stats struct {
	Files     int            `json:"files"`
	Bytes     int64          `json:"bytes"`
	Sessions  int            `json:"sessions"`
	Lines     int            `json:"lines"`
	Entries   int            `json:"entries"`   // user/assistant entries used
	Malformed int            `json:"malformed"` // lines that are not valid JSON
	Truncated int            `json:"truncated"` // invalid final line without newline (file still being written / cut off)
	BadTime   int            `json:"bad_timestamp"`
	Skipped   map[string]int `json:"skipped_types"` // entry types we don't model, by type

	LLMCalls       int `json:"llm_calls"`
	MergedLines    int `json:"merged_message_lines"` // assistant lines merged by message.id
	ToolUses       int `json:"tool_uses"`
	ToolErrors     int `json:"tool_errors"`
	MissingResults int `json:"missing_tool_results"` // tool_use with no tool_result
	OrphanResults  int `json:"orphan_tool_results"`  // tool_result with no tool_use
	Prompts        int `json:"user_prompts"`

	Subagents      int `json:"subagents"`
	LinkedByID     int `json:"subagents_linked_by_agent_id"`
	LinkedByPrompt int `json:"subagents_linked_by_prompt"`
	LinkedByTime   int `json:"subagents_linked_by_time"`
	Unlinked       int `json:"subagents_unlinked"`

	Spans        int `json:"spans"`
	InvalidSpans int `json:"invalid_spans"`
}

func newStats() *Stats { return &Stats{Skipped: map[string]int{}} }

// SessionFiles are the files that make up one session.
type SessionFiles struct {
	SessionID string
	Project   string
	Main      string   // may be empty if only subagent files exist
	Subagents []string // newer layout: <session>/subagents/agent-*.jsonl
}

// Discover finds sessions under root (e.g. ~/.claude/projects). root may also
// be a single .jsonl file.
func Discover(root string) ([]SessionFiles, error) {
	byKey := map[string]*SessionFiles{}
	var order []string
	add := func(project, session string) *SessionFiles {
		k := project + "/" + session
		sf, ok := byKey[k]
		if !ok {
			sf = &SessionFiles{SessionID: session, Project: project}
			byKey[k] = sf
			order = append(order, k)
		}
		return sf
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		dir := filepath.Dir(path)
		if filepath.Base(dir) == "subagents" {
			sessionDir := filepath.Dir(dir)
			sf := add(filepath.Base(filepath.Dir(sessionDir)), filepath.Base(sessionDir))
			sf.Subagents = append(sf.Subagents, path)
			return nil
		}
		sf := add(filepath.Base(dir), strings.TrimSuffix(filepath.Base(path), ".jsonl"))
		sf.Main = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(order)
	out := make([]SessionFiles, 0, len(order))
	for _, k := range order {
		sort.Strings(byKey[k].Subagents)
		out = append(out, *byKey[k])
	}
	return out, nil
}

// readEntries parses one JSONL file. defaultAgent is used for subagent files
// whose lines lack an agentId.
func readEntries(path, defaultAgent string, st *Stats) ([]*entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st.Files++
	// bufio.Reader instead of Scanner: transcript lines can be megabytes long.
	rd := bufio.NewReaderSize(f, 1<<20)
	var out []*entry
	for lineNo := 1; ; lineNo++ {
		line, err := rd.ReadBytes('\n')
		if len(line) == 0 && errors.Is(err, io.EOF) {
			break
		}
		st.Bytes += int64(len(line))
		hasNewline := bytes.HasSuffix(line, []byte("\n"))
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			st.Lines++
			var e entry
			if jerr := json.Unmarshal(line, &e); jerr != nil {
				if !hasNewline {
					st.Truncated++
				} else {
					st.Malformed++
				}
			} else if e.Type != "user" && e.Type != "assistant" {
				t := e.Type
				if t == "" {
					t = "(none)"
				}
				st.Skipped[t]++
			} else if ts, ok := parseTime(e.Timestamp); !ok {
				st.BadTime++
			} else {
				e.ts, e.line = ts, lineNo
				if e.AgentID == "" && defaultAgent != "" {
					e.AgentID = defaultAgent
				}
				out = append(out, &e)
				st.Entries++
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return out, err
		}
	}
	return out, nil
}

// Sink receives spans in chunks.
type Sink func([]model.Span) error

// Import reads every session under root and sends its spans to sink.
// pricer may be nil; otherwise it fills costs (see cost.Table.Apply).
func Import(root string, sink Sink, pricer func([]model.Span) int) (*Stats, error) {
	sessions, err := Discover(root)
	if err != nil {
		return nil, err
	}
	st := newStats()
	for _, sf := range sessions {
		spans, err := ImportSession(sf, st)
		if err != nil {
			return st, fmt.Errorf("session %s: %w", sf.SessionID, err)
		}
		if pricer != nil {
			pricer(spans)
		}
		for i := 0; i < len(spans); i += 5000 {
			end := min(i+5000, len(spans))
			if err := sink(spans[i:end]); err != nil {
				return st, err
			}
		}
	}
	return st, nil
}

// ImportSession converts one session's files into normalised spans.
func ImportSession(sf SessionFiles, st *Stats) ([]model.Span, error) {
	var all []*entry
	if sf.Main != "" {
		es, err := readEntries(sf.Main, "", st)
		if err != nil {
			return nil, err
		}
		all = append(all, es...)
	}
	for i, p := range sf.Subagents {
		def := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(p), ".jsonl"), "agent-")
		es, err := readEntries(p, def, st)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			e.IsSidechain = true
			e.line += (i + 1) * 1_000_000_000 // keep lines from different files distinct
		}
		all = append(all, es...)
	}
	if len(all) == 0 {
		return nil, nil
	}
	st.Sessions++
	b := newBuilder(sf, st)
	return b.build(all), nil
}

// timeNS converts a time to Unix nanoseconds.
func timeNS(t time.Time) int64 { return t.UnixNano() }
