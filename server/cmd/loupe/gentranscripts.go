package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// runGenTranscripts writes a synthetic Claude Code projects directory with the
// same shapes as the importer fixtures (split assistant lines, tool_use /
// tool_result pairs, errors, subagent files with agentId, unknown entry types).
// It exists to benchmark the importer without touching real transcripts.
func runGenTranscripts(args []string) error {
	fs := flag.NewFlagSet("gen-transcripts", flag.ExitOnError)
	dir := fs.String("dir", "data/bench-transcripts", "output directory (recreated)")
	sessions := fs.Int("sessions", 400, "number of sessions")
	turns := fs.Int("turns", 120, "tool-using turns per session (subagents get a third)")
	seed := fs.Int64("seed", 7, "random seed")
	_ = fs.Parse(args)
	os.RemoveAll(*dir)
	r := rand.New(rand.NewSource(*seed))
	var bytes int64
	lines := 0
	for s := 0; s < *sessions; s++ {
		project := filepath.Join(*dir, fmt.Sprintf("-Users-dev-proj%d", s%12))
		if err := os.MkdirAll(project, 0o755); err != nil {
			return err
		}
		sid := fmt.Sprintf("sess-%05d", s)
		t := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(s) * 37 * time.Minute)
		g := &tgen{r: r, sid: sid, t: t}
		g.user("", "Please fix the failing tests and refactor the parser module", false)
		nSub := 0
		if r.Intn(3) == 0 {
			nSub = 1 + r.Intn(2)
		}
		subAt := map[int]int{}
		for k := 0; k < nSub; k++ {
			subAt[r.Intn(*turns)] = k
		}
		for i := 0; i < *turns; i++ {
			if k, ok := subAt[i]; ok {
				agentID := fmt.Sprintf("a%d%d", s, k)
				g.taskCall(agentID)
				sub := &tgen{r: r, sid: sid, t: g.t, agent: agentID}
				sub.user("", "Investigate the parser module and report problems", true)
				for j := 0; j < *turns/3; j++ {
					sub.toolTurn()
				}
				sub.assistantText("Found 3 problems in parser.py")
				subDir := filepath.Join(project, sid, "subagents")
				os.MkdirAll(subDir, 0o755)
				n, err := sub.write(filepath.Join(subDir, "agent-"+agentID+".jsonl"))
				if err != nil {
					return err
				}
				bytes += n
				lines += len(sub.lines)
				g.t = sub.t
				g.taskResult(agentID)
				continue
			}
			g.toolTurn()
		}
		g.assistantText("All tests pass now.")
		n, err := g.write(filepath.Join(project, sid+".jsonl"))
		if err != nil {
			return err
		}
		bytes += n
		lines += len(g.lines)
	}
	fmt.Fprintf(os.Stderr, "wrote %d sessions, %d lines, %.1f MB to %s\n", *sessions, lines, float64(bytes)/1e6, *dir)
	return nil
}

type tgen struct {
	r     *rand.Rand
	sid   string
	agent string
	t     time.Time
	n     int
	prev  string
	lines [][]byte
	pend  string // pending Task tool_use id
}

func (g *tgen) emit(e map[string]any) {
	g.n++
	id := fmt.Sprintf("%s-%s-%d", g.sid, g.agent, g.n)
	e["uuid"], e["sessionId"], e["timestamp"], e["cwd"], e["version"] = id, g.sid, g.t.Format(time.RFC3339Nano), "/Users/dev/proj", "2.1.0"
	if g.prev != "" {
		e["parentUuid"] = g.prev
	} else {
		e["parentUuid"] = nil
	}
	if g.agent != "" {
		e["isSidechain"], e["agentId"] = true, g.agent
	} else {
		e["isSidechain"] = false
	}
	g.prev = id
	b, _ := json.Marshal(e)
	g.lines = append(g.lines, b)
}

func (g *tgen) tick(minMS, maxMS int) {
	g.t = g.t.Add(time.Duration(minMS+g.r.Intn(maxMS-minMS+1)) * time.Millisecond)
}

func (g *tgen) user(_, text string, _ bool) {
	g.emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
}

func (g *tgen) usage() map[string]any {
	return map[string]any{"input_tokens": 5 + g.r.Intn(50), "output_tokens": 20 + g.r.Intn(400),
		"cache_creation_input_tokens": g.r.Intn(3000), "cache_read_input_tokens": 10000 + g.r.Intn(60000)}
}

func (g *tgen) toolTurn() {
	tools := []struct{ name, key, val string }{{"Read", "file_path", "/Users/dev/proj/src/parser.py"},
		{"Edit", "file_path", "/Users/dev/proj/src/parser.py"}, {"Bash", "command", "pytest -q tests/"},
		{"Grep", "pattern", "def parse"}, {"Glob", "pattern", "src/**/*.py"}}
	tl := tools[g.r.Intn(len(tools))]
	g.tick(800, 9000)
	msgID := fmt.Sprintf("msg_%s_%d", g.sid, g.n)
	toolID := fmt.Sprintf("toolu_%s_%s_%d", g.sid, g.agent, g.n)
	// Assistant message split over two lines (text, then tool_use), as Claude Code writes it.
	g.emit(map[string]any{"type": "assistant", "requestId": "req", "message": map[string]any{"id": msgID, "role": "assistant",
		"model": "claude-sonnet-4-6", "content": []any{map[string]any{"type": "text", "text": "Let me check that."}}, "usage": g.usage()}})
	g.emit(map[string]any{"type": "assistant", "requestId": "req", "message": map[string]any{"id": msgID, "role": "assistant",
		"model": "claude-sonnet-4-6", "content": []any{map[string]any{"type": "tool_use", "id": toolID, "name": tl.name,
			"input": map[string]any{tl.key: tl.val}}}, "usage": g.usage()}})
	g.tick(50, 4000)
	isErr := g.r.Intn(12) == 0
	out := "def parse(src):\n    tokens = lex(src)\n    return build(tokens)\n"
	if isErr {
		out = "Error: command failed with exit code 1"
	}
	g.emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{
		"type": "tool_result", "tool_use_id": toolID, "content": out, "is_error": isErr}}}, "toolUseResult": map[string]any{"stdout": out}})
	if g.r.Intn(40) == 0 {
		g.emit(map[string]any{"type": "system", "content": "hook ran", "level": "info"})
	}
}

func (g *tgen) taskCall(agentID string) {
	g.tick(500, 3000)
	g.pend = "toolu_task_" + agentID
	g.emit(map[string]any{"type": "assistant", "message": map[string]any{"id": "msg_task_" + agentID, "role": "assistant",
		"model": "claude-opus-4-6", "content": []any{map[string]any{"type": "tool_use", "id": g.pend, "name": "Task",
			"input": map[string]any{"description": "Investigate parser", "prompt": "Investigate the parser module and report problems", "subagent_type": "general-purpose"}}},
		"usage": g.usage()}})
}

func (g *tgen) taskResult(agentID string) {
	g.tick(10, 100)
	g.emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{
		"type": "tool_result", "tool_use_id": g.pend, "content": []any{map[string]any{"type": "text", "text": "Found 3 problems"}}}}},
		"toolUseResult": map[string]any{"agentId": agentID, "status": "completed"}})
}

func (g *tgen) assistantText(text string) {
	g.tick(500, 4000)
	g.emit(map[string]any{"type": "assistant", "message": map[string]any{"id": fmt.Sprintf("msg_end_%s_%s", g.sid, g.agent), "role": "assistant",
		"model": "claude-sonnet-4-6", "content": []any{map[string]any{"type": "text", "text": text}}, "usage": g.usage()}})
}

func (g *tgen) write(path string) (int64, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	var n int64
	for _, l := range g.lines {
		w.Write(l)
		w.WriteByte('\n')
		n += int64(len(l) + 1)
	}
	return n, w.Flush()
}
