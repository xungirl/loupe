// Package corpus generates a labelled, synthetic corpus of agent traces for
// measuring the detectors (fault injection).
//
// Each trace simulates a coding agent: explore the repo, edit files, run tests,
// fix failures. Clean traces include realistic noise (a failed read, a
// duplicated read, one or two fix cycles, natural context growth), and some
// are deliberately placed just under detector thresholds ("hard negatives").
// Fault traces inject one or two of: loop, rework, error_streak, stuck,
// budget_burn, with intensities that sometimes fall *below* what the detector
// can see ("hard positives", e.g. rework spread across two files).
//
// Honest caveat: the same author wrote the generator and the detectors, so
// scores here measure "does the detector catch the patterns as I modelled
// them", not real-world accuracy. The held-out split only guards against
// tuning thresholds to specific traces.
package corpus

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"

	"github.com/xungirl/loupe/server/internal/model"
)

// Fault labels (same strings as the detector names).
const (
	Loop        = "loop"
	Rework      = "rework"
	ErrorStreak = "error_streak"
	Stuck       = "stuck"
	Budget      = "budget_burn"
)

// Faults lists every label.
var Faults = []string{Loop, Rework, ErrorStreak, Stuck, Budget}

// Trace is one labelled example.
type Trace struct {
	TraceID string       `json:"trace_id"`
	Labels  []string     `json:"labels"`  // empty = clean
	Variant string       `json:"variant"` // how it was generated, for error analysis
	Split   string       `json:"split"`   // "train" or "test"
	Spans   []model.Span `json:"spans"`
}

// HasLabel reports whether the trace carries a fault label.
func (t *Trace) HasLabel(l string) bool {
	for _, x := range t.Labels {
		if x == l {
			return true
		}
	}
	return false
}

// SplitOf assigns train/test by hashing the trace id (stable, ~50/50).
func SplitOf(traceID string) string {
	h := fnv.New32a()
	h.Write([]byte(traceID))
	if h.Sum32()%2 == 0 {
		return "train"
	}
	return "test"
}

// Generate produces n traces deterministically from seed, with timestamps
// starting at baseNS and spread over spreadNS.
func Generate(n int, seed, baseNS, spreadNS int64) []Trace {
	r := rand.New(rand.NewSource(seed))
	out := make([]Trace, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("corpus-%04d", i)
		start := baseNS
		if spreadNS > 0 {
			start += r.Int63n(spreadNS)
		}
		t := generateOne(rand.New(rand.NewSource(r.Int63())), id, start)
		t.Split = SplitOf(id)
		out = append(out, t)
	}
	return out
}

// --- simulation -----------------------------------------------------------

type sim struct {
	r      *rand.Rand
	trace  string
	now    int64
	seq    int
	spans  []model.Span
	cc     bool // Claude-Code style tool names vs mini-agent style
	ctx    float64
	growth func() float64 // context added per step

	agentID, agentName, agentSpan string
}

func (s *sim) id() string { s.seq++; return fmt.Sprintf("s%03d", s.seq) }

func (s *sim) advance(minMS, maxMS int) int64 {
	d := int64(minMS+s.r.Intn(maxMS-minMS+1)) * 1e6
	s.now += d
	return d
}

func (s *sim) llm() {
	start := s.now
	s.advance(700, 6000)
	out := int64(40 + s.r.Intn(400))
	in := int64(s.ctx)
	cached := in * int64(50+s.r.Intn(40)) / 100 // prompt caching: most of the context is a cache read
	s.spans = append(s.spans, model.Span{TraceID: s.trace, SpanID: s.id(), ParentSpanID: s.agentSpan, Kind: model.KindLLM,
		Name: "llm", Model: "fake-sim-1", AgentID: s.agentID, AgentName: s.agentName, StartNS: start, EndNS: s.now,
		InputTokens: in - cached, CacheReadTokens: cached, OutputTokens: out})
	s.ctx += float64(out) + s.growth()
}

// tool appends an LLM call (the model deciding) followed by the tool call.
func (s *sim) tool(kind, target, output string, isErr bool) {
	s.llm()
	name, input := s.toolShape(kind, target)
	start := s.now
	switch kind {
	case "test":
		s.advance(800, 7000)
	default:
		s.advance(20, 1500)
	}
	sp := model.Span{TraceID: s.trace, SpanID: s.id(), ParentSpanID: s.agentSpan, Kind: model.KindTool, Name: name,
		ToolName: name, ToolInput: input, ToolOutput: output, Target: model.ExtractTarget(input),
		AgentID: s.agentID, AgentName: s.agentName, StartNS: start, EndNS: s.now, Status: model.StatusOK}
	if isErr {
		sp.Status = model.StatusError
		sp.ErrorMsg = firstLine(output)
	}
	s.spans = append(s.spans, sp)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func js(m map[string]any) string { b, _ := json.Marshal(m); return string(b) }

// toolShape maps an abstract action to a concrete tool name + JSON input.
func (s *sim) toolShape(kind, target string) (string, string) {
	if s.cc {
		switch kind {
		case "read":
			return "Read", js(map[string]any{"file_path": target})
		case "grep":
			return "Grep", js(map[string]any{"pattern": target})
		case "ls":
			return "Glob", js(map[string]any{"pattern": target})
		case "edit":
			return "Edit", js(map[string]any{"file_path": target, "old_string": s.snippet(), "new_string": s.snippet()})
		case "test":
			return "Bash", js(map[string]any{"command": "pytest -q " + target})
		case "bash":
			return "Bash", js(map[string]any{"command": target})
		}
	}
	switch kind {
	case "read", "grep":
		return "read_file", js(map[string]any{"path": target})
	case "ls":
		return "list_files", js(map[string]any{"path": target})
	case "edit":
		return "write_file", js(map[string]any{"path": target, "content": s.snippet() + "\n" + s.snippet()})
	case "test":
		return "run_tests", js(map[string]any{"path": target})
	}
	return "bash", js(map[string]any{"command": target})
}

var words = []string{"value", "result", "items", "count", "user", "token", "cache", "retry", "limit", "offset",
	"payload", "config", "session", "handler", "request", "response", "error", "index", "buffer", "timeout"}

// snippet is a random line of code, so successive edits have different content.
func (s *sim) snippet() string {
	w := func() string { return words[s.r.Intn(len(words))] }
	return fmt.Sprintf("%s = %s_%s(%s, %s)", w(), w(), w(), w(), w())
}

var modules = []string{"auth", "billing", "cache", "config", "db", "events", "export", "parser", "queue", "router",
	"search", "session", "storage", "users", "utils", "views", "worker", "metrics", "notify", "schema"}

func (s *sim) file() string { return "src/" + modules[s.r.Intn(len(modules))] + ".py" }

func (s *sim) testOutput(fail bool, file string) string {
	n := 3 + s.r.Intn(12)
	if fail {
		return fmt.Sprintf("FAILED tests/test_%s::test_case_%d - AssertionError\n1 failed, %d passed in 0.%02ds",
			baseName(file), s.r.Intn(9), n, s.r.Intn(99))
	}
	return fmt.Sprintf("%d passed in 0.%02ds", n+1, s.r.Intn(99))
}

func baseName(f string) string {
	f = f[strings.LastIndexByte(f, '/')+1:]
	return strings.TrimSuffix(f, ".py")
}

// readOutput is file content that is distinct per file (and per version).
func (s *sim) readOutput(file string, version int) string {
	return fmt.Sprintf("def %s_main(v%d):\n    return handle_%s(v%d)\n", baseName(file), version, baseName(file), version)
}

// explore performs k read-only calls over different files.
func (s *sim) explore(k int) {
	for i := 0; i < k; i++ {
		switch s.r.Intn(5) {
		case 0:
			s.tool("grep", modules[s.r.Intn(len(modules))]+"_handler", fmt.Sprintf("%s:%d: match", s.file(), 10+s.r.Intn(300)), false)
		case 1:
			s.tool("ls", "src/"+modules[s.r.Intn(len(modules))], strings.Join([]string{s.file(), s.file()}, "\n"), false)
		default:
			f := s.file()
			s.tool("read", f, s.readOutput(f, s.r.Intn(3)), false)
		}
	}
}

// fixCycle: edit a file, run tests (maybe failing), repeat up to maxFails.
func (s *sim) fixCycle(file string, failures int) {
	for i := 0; i <= failures; i++ {
		s.tool("edit", file, "ok", false)
		s.tool("test", "tests/test_"+baseName(file)+".py", s.testOutput(i < failures, file), i < failures)
	}
}

// --- trace generation -----------------------------------------------------

func generateOne(r *rand.Rand, id string, startNS int64) Trace {
	s := &sim{r: r, trace: id, now: startNS, cc: r.Intn(2) == 0}
	s.ctx = float64(8000 + r.Intn(12000)) // system prompt + tool definitions
	s.growth = func() float64 { return float64(200 + s.r.Intn(1300)) }

	// Choose labels: 40% clean, 50% one fault, 10% two faults.
	var labels []string
	switch p := r.Float64(); {
	case p < 0.40:
	case p < 0.90:
		labels = []string{Faults[r.Intn(len(Faults))]}
	default:
		a := r.Intn(len(Faults))
		b := (a + 1 + r.Intn(len(Faults)-1)) % len(Faults)
		labels = []string{Faults[a], Faults[b]}
	}
	has := func(l string) bool {
		for _, x := range labels {
			if x == l {
				return true
			}
		}
		return false
	}
	var variants []string

	// Root agent span (end time filled in at the end).
	root := model.Span{TraceID: id, SpanID: "root", Kind: model.KindAgent, Name: "fix: " + modules[r.Intn(len(modules))] + " bug",
		AgentID: "main", AgentName: "coder", StartNS: s.now, Source: "corpus"}
	s.spans = append(s.spans, root)
	s.agentID, s.agentName, s.agentSpan = "main", "coder", "root"

	multi := r.Float64() < 0.25
	if multi {
		root.AgentName = "orchestrator"
		s.spans[0].AgentName = "orchestrator"
		s.spans[0].Name = "multi-agent: " + strings.TrimPrefix(root.Name, "fix: ")
		// Planner subagent: thinks, maybe reads a file or two.
		s.subagent("planner", func() {
			s.llm()
			if s.r.Intn(2) == 0 {
				s.explore(1 + s.r.Intn(2))
			}
			s.llm()
		})
	}

	work := func() {
		if has(Budget) {
			if s.r.Intn(3) == 0 {
				variants = append(variants, "budget:subtle")
				s.growth = func() float64 { return float64(4000 + s.r.Intn(6000)) }
			} else {
				variants = append(variants, "budget:blowup")
				s.growth = func() float64 { return s.ctx * (0.15 + 0.25*s.r.Float64()) }
			}
		}
		// Exploration.
		k := 2 + s.r.Intn(8)
		if !has(Stuck) && s.r.Float64() < 0.12 {
			k = 10 + s.r.Intn(3) // hard negative: long but normal exploration
			variants = append(variants, "clean:long-explore")
		}
		if has(Stuck) {
			k = 13 + s.r.Intn(13)
			if s.r.Float64() < 0.25 {
				// Hard positive: scratch-note writes break the read streak.
				variants = append(variants, "stuck:with-notes")
				for i := 0; i < k; i++ {
					if i > 0 && i%7 == 0 {
						s.tool("edit", "NOTES.md", "ok", false)
					}
					s.explore(1)
				}
			} else {
				variants = append(variants, "stuck:read-only")
				s.explore(k)
			}
			if s.r.Intn(2) == 0 {
				s.llm() // gives up without changing anything
				return
			}
		} else {
			s.explore(k)
		}

		// Noise: a duplicated read (2 identical calls, below the loop threshold).
		if s.r.Float64() < 0.25 {
			f := s.file()
			out := s.readOutput(f, 1)
			s.tool("read", f, out, false)
			s.tool("read", f, out, false)
		}
		// Noise: a failed read or two consecutive failures (below the streak threshold).
		if !has(ErrorStreak) {
			switch p := s.r.Float64(); {
			case p < 0.20:
				s.tool("read", s.file(), "Error: file not found", true)
			case p < 0.30:
				variants = append(variants, "clean:two-errors")
				s.tool("bash", "python -m app", "ModuleNotFoundError: No module named 'app'", true)
				s.tool("read", "app/__main__.py", "Error: file not found", true)
			}
		}

		if has(Loop) {
			s.injectLoop(&variants)
		}
		if has(ErrorStreak) {
			n := 3 + s.r.Intn(4)
			variants = append(variants, fmt.Sprintf("error_streak:%d", n))
			cmds := []string{"pip install -e .", "python -m app", "make build", "npm run build", "cat .env.example", "docker compose up"}
			for i := 0; i < n; i++ {
				s.tool("bash", cmds[(i+s.r.Intn(2))%len(cmds)], fmt.Sprintf("Error: exit status %d", 1+s.r.Intn(127)), true)
			}
		}

		// Editing and fixing.
		files := 1 + s.r.Intn(3)
		target := s.file()
		for f := 0; f < files; f++ {
			file := s.file()
			if f == 0 {
				file = target
			}
			fails := 0
			if s.r.Float64() < 0.35 {
				fails = 1
				if s.r.Float64() < 0.3 {
					fails = 2 // hard negative: two fix cycles on one file
					variants = append(variants, "clean:two-fix-cycles")
				}
			}
			s.fixCycle(file, fails)
		}
		if has(Rework) {
			s.injectRework(&variants)
		}
	}

	if multi {
		s.subagent("coder", work)
		s.subagent("reviewer", func() {
			s.explore(1 + s.r.Intn(3))
			s.tool("test", "tests", s.testOutput(false, "all"), false)
			s.llm()
		})
	} else {
		work()
	}
	s.llm() // final answer
	s.spans[0].EndNS = s.now
	if len(variants) == 0 {
		variants = append(variants, "plain")
	}
	if labels == nil {
		labels = []string{}
	}
	return Trace{TraceID: id, Labels: labels, Variant: strings.Join(variants, ","), Spans: s.spans}
}

// subagent runs fn as a child agent spawned by a Task tool call.
func (s *sim) subagent(name string, fn func()) {
	s.llm()
	taskStart := s.now
	taskID := s.id()
	parentAgentSpan, parentID, parentName := s.agentSpan, s.agentID, s.agentName
	s.spans = append(s.spans, model.Span{TraceID: s.trace, SpanID: taskID, ParentSpanID: parentAgentSpan, Kind: model.KindTool,
		Name: "Task", ToolName: "Task", ToolInput: js(map[string]any{"subagent_type": name}), AgentID: parentID,
		AgentName: parentName, StartNS: taskStart, Status: model.StatusOK})
	taskIdx := len(s.spans) - 1
	agentSpan := s.id()
	s.spans = append(s.spans, model.Span{TraceID: s.trace, SpanID: agentSpan, ParentSpanID: taskID, Kind: model.KindAgent,
		Name: "subagent: " + name, AgentID: name, AgentName: name, StartNS: s.now})
	agentIdx := len(s.spans) - 1
	s.agentSpan, s.agentID, s.agentName = agentSpan, name, name
	fn()
	s.advance(10, 100)
	s.spans[agentIdx].EndNS = s.now
	s.spans[taskIdx].EndNS = s.now
	s.agentSpan, s.agentID, s.agentName = parentAgentSpan, parentID, parentName
}

func (s *sim) injectLoop(variants *[]string) {
	n := 3 + s.r.Intn(5)
	switch s.r.Intn(4) {
	case 0: // the same read with shifting line numbers
		*variants = append(*variants, fmt.Sprintf("loop:paged-read:%d", n))
		f := s.file()
		out := s.readOutput(f, 1)
		for i := 0; i < n; i++ {
			s.tool("read", fmt.Sprintf("%s lines %d-%d", f, 1+i*2, 51+i*2), out, false)
		}
	case 1: // alternating pair A, B, A, B, ...
		*variants = append(*variants, fmt.Sprintf("loop:alternating:%d", n))
		a, b := s.file(), s.file()
		for i := 0; i < n; i++ {
			s.tool("read", a, s.readOutput(a, 1), false)
			s.tool("grep", "TODO", fmt.Sprintf("%s:%d: TODO", b, 42), false)
		}
	case 2: // polling a status command that never changes
		*variants = append(*variants, fmt.Sprintf("loop:poll:%d", n))
		for i := 0; i < n; i++ {
			s.tool("bash", "curl -s localhost:8000/health", fmt.Sprintf("503 Service Unavailable (attempt %d)", i+1), false)
		}
	default: // the same search
		*variants = append(*variants, fmt.Sprintf("loop:search:%d", n))
		q := modules[s.r.Intn(len(modules))] + "_config"
		for i := 0; i < n; i++ {
			s.tool("grep", q, "no matches", false)
		}
	}
}

func (s *sim) injectRework(variants *[]string) {
	if s.r.Float64() < 0.25 {
		// Hard positive: churn spread across two files, each below thresholds.
		*variants = append(*variants, "rework:two-files")
		a, b := s.file(), s.file()
		for i := 0; i < 3; i++ {
			s.tool("edit", a, "ok", false)
			s.tool("test", "tests", s.testOutput(true, a), true)
			s.tool("edit", b, "ok", false)
			s.tool("test", "tests", s.testOutput(i < 2, b), i < 2)
		}
		return
	}
	n := 4 + s.r.Intn(6)
	*variants = append(*variants, fmt.Sprintf("rework:single-file:%d", n))
	f := s.file()
	for i := 0; i < n; i++ {
		s.tool("edit", f, "ok", false)
		if s.r.Float64() < 0.8 || i == n-1 {
			s.tool("test", "tests/test_"+baseName(f)+".py", s.testOutput(i < n-1, f), i < n-1)
		}
	}
}
