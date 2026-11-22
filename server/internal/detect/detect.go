// Package detect finds failure patterns in agent traces.
//
// Every detector looks at one agent at a time (a subagent's loop is its own
// problem, not its parent's), works on the agent's tool calls in time order,
// and returns findings that point at the exact spans involved, so the UI can
// highlight them in the waterfall.
//
// Thresholds live in Config. They were tuned only on the *train* split of the
// synthetic corpus (see internal/corpus and `loupe eval-detectors`).
package detect

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/xungirl/loupe/server/internal/assemble"
	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

// Detector names (also the corpus labels).
const (
	Loop        = "loop"
	Rework      = "rework"
	ErrorStreak = "error_streak"
	Stuck       = "stuck"
	Budget      = "budget_burn"
)

// All lists every detector in display order.
var All = []string{Loop, Rework, ErrorStreak, Stuck, Budget}

// Config holds every threshold.
type Config struct {
	// Loop: at least LoopMinRepeats near-identical calls (same tool, input
	// similarity >= LoopSimilarity) that also returned near-identical output,
	// all inside a window of LoopWindow consecutive tool calls.
	LoopMinRepeats int
	LoopWindow     int
	LoopSimilarity float64

	// Rework: the same file edited at least ReworkMinEdits times, or at least
	// ReworkMinCycles edit → failing-test → edit-again cycles on one file.
	ReworkMinEdits  int
	ReworkMinCycles int

	// ErrorStreak: at least StreakMin consecutive failed tool calls.
	StreakMin int

	// Stuck: at least StuckMinCalls consecutive tool calls that only read or
	// search (no write, no test run), i.e. activity without progress.
	StuckMinCalls int

	// Budget: total tokens above BudgetTokens, or the average input size of
	// the last 3 LLM calls at least BudgetGrowth x the first 3 (context
	// blow-up), given at least BudgetMinCalls calls.
	BudgetTokens   int64
	BudgetGrowth   float64
	BudgetMinCalls int
}

// DefaultConfig holds the tuned thresholds.
func DefaultConfig() Config {
	return Config{
		LoopMinRepeats: 3, LoopWindow: 8, LoopSimilarity: 0.8,
		ReworkMinEdits: 5, ReworkMinCycles: 3,
		StreakMin:      3,
		StuckMinCalls:  13,
		BudgetTokens:   1_500_000, BudgetGrowth: 5, BudgetMinCalls: 6,
	}
}

// call is a tool span with the derived fields detectors need.
type call struct {
	span   *model.Span
	tokens []string // normalised input tokens
	outTok []string // normalised output tokens
	edit   bool
	test   bool
	read   bool
}

// Run executes every detector on one trace.
func Run(spans []model.Span, cfg Config) []store.Finding {
	tr := assemble.Build(spans)
	byAgent := map[string][]*call{}
	var agentOrder []string
	var llm []*model.Span
	for _, n := range tr.Flat {
		sp := &n.Span
		switch sp.Kind {
		case model.KindTool:
			if _, ok := byAgent[n.AgentKey]; !ok {
				agentOrder = append(agentOrder, n.AgentKey)
			}
			byAgent[n.AgentKey] = append(byAgent[n.AgentKey], newCall(sp))
		case model.KindLLM:
			llm = append(llm, sp)
		}
	}
	var out []store.Finding
	for _, agent := range agentOrder {
		calls := byAgent[agent]
		sort.SliceStable(calls, func(i, j int) bool { return calls[i].span.StartNS < calls[j].span.StartNS })
		for _, f := range [][]store.Finding{
			detectLoop(calls, cfg), detectRework(calls, cfg), detectErrorStreak(calls, cfg), detectStuck(calls, cfg),
		} {
			for i := range f {
				f[i].AgentID = agent
			}
			out = append(out, f...)
		}
	}
	sort.SliceStable(llm, func(i, j int) bool { return llm[i].StartNS < llm[j].StartNS })
	out = append(out, detectBudget(llm, cfg)...)
	return out
}

// --- classification helpers ---------------------------------------------

var editTools = map[string]bool{"edit": true, "multiedit": true, "write": true, "write_file": true,
	"str_replace": true, "apply_patch": true, "notebookedit": true, "create_file": true}

var readTools = map[string]bool{"read": true, "read_file": true, "grep": true, "glob": true, "ls": true,
	"list_files": true, "search": true, "find": true, "cat": true, "webfetch": true, "websearch": true, "view": true}

func newCall(sp *model.Span) *call {
	name := strings.ToLower(sp.ToolName)
	c := &call{span: sp, tokens: tokens(sp.ToolName + " " + sp.Target + " " + sp.ToolInput), outTok: tokens(sp.ToolOutput)}
	c.edit = editTools[name]
	c.test = isTestCall(name, sp.Target+" "+sp.ToolInput)
	c.read = readTools[name] || (name == "bash" && isReadOnlyCommand(sp.Target))
	return c
}

func isTestCall(tool, input string) bool {
	if tool == "run_tests" || tool == "test" {
		return true
	}
	if tool != "bash" {
		return false
	}
	in := strings.ToLower(input)
	for _, k := range []string{"pytest", "go test", "npm test", "npm run test", "jest", "vitest", "cargo test", "unittest", "make test", "mvn test", "gradle test"} {
		if strings.Contains(in, k) {
			return true
		}
	}
	return false
}

func isReadOnlyCommand(cmd string) bool {
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return false
	}
	switch f[0] {
	case "ls", "cat", "head", "tail", "grep", "rg", "find", "wc", "tree", "pwd", "which", "less":
		return true
	case "git":
		return len(f) > 1 && (f[1] == "status" || f[1] == "log" || f[1] == "diff" || f[1] == "show")
	}
	return false
}

// tokens normalises text into a bag of tokens: lower-case, digits collapsed
// to '#', split on anything that isn't a letter, digit or '#'. Collapsing
// digits makes "read lines 10-20" and "read lines 20-30" look identical,
// which is exactly the kind of near-duplicate a looping agent produces.
func tokens(s string) []string {
	if len(s) > 2000 {
		s = s[:2000]
	}
	var b strings.Builder
	prevDigit := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsDigit(r):
			if !prevDigit {
				b.WriteByte('#')
			}
			prevDigit = true
			continue
		case unicode.IsLetter(r) || r == '_' || r == '.' || r == '/':
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
		prevDigit = false
	}
	return strings.Fields(b.String())
}

// jaccard is |A∩B| / |A∪B| over token sets; two empty sets are identical.
func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	set := make(map[string]uint8, len(a)+len(b))
	for _, t := range a {
		set[t] |= 1
	}
	for _, t := range b {
		set[t] |= 2
	}
	inter := 0
	for _, v := range set {
		if v == 3 {
			inter++
		}
	}
	return float64(inter) / float64(len(set))
}

func ids(cs []*call) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.span.SpanID
	}
	return out
}

func severity(n, warnAt int) string {
	if n >= 2*warnAt {
		return "critical"
	}
	return "warn"
}

// --- detectors --------------------------------------------------------------

// detectLoop finds clusters of near-identical calls with near-identical
// results and no edit in between: the agent repeats an action, changes
// nothing, and learns nothing new. The output and no-edit conditions are what
// separate a loop from legitimate repetition such as re-running tests after
// each fix (that pattern is rework, handled below).
func detectLoop(calls []*call, cfg Config) []store.Finding {
	var out []store.Finding
	used := make([]bool, len(calls))
	for i := range calls {
		if used[i] {
			continue
		}
		cluster := []*call{calls[i]}
		idx := []int{i}
		for j := i + 1; j < len(calls) && j < i+cfg.LoopWindow*2; j++ {
			// The window slides with the cluster: each repeat must be within
			// LoopWindow calls of the previous one.
			if j-idx[len(idx)-1] > cfg.LoopWindow {
				break
			}
			if calls[j].edit {
				break // something changed: later repeats are not "the same situation"
			}
			if used[j] || calls[j].span.ToolName != calls[i].span.ToolName {
				continue
			}
			if jaccard(calls[i].tokens, calls[j].tokens) >= cfg.LoopSimilarity &&
				jaccard(calls[i].outTok, calls[j].outTok) >= cfg.LoopSimilarity {
				cluster = append(cluster, calls[j])
				idx = append(idx, j)
			}
		}
		if len(cluster) >= cfg.LoopMinRepeats {
			for _, k := range idx {
				used[k] = true
			}
			out = append(out, store.Finding{
				Detector: Loop, Severity: severity(len(cluster), cfg.LoopMinRepeats), Score: float64(len(cluster)),
				Summary: fmt.Sprintf("%d near-identical %s calls with the same result (%s)", len(cluster),
					calls[i].span.ToolName, clip(calls[i].span.Target, 60)),
				SpanIDs: ids(cluster),
			})
		}
	}
	return out
}

// detectRework flags files the agent keeps rewriting, especially around
// failing tests (edit → test fails → edit the same file again).
func detectRework(calls []*call, cfg Config) []store.Finding {
	type fileState struct {
		edits      []*call
		cycles     int
		failedTest bool // a failing test ran since the last edit of this file
	}
	files := map[string]*fileState{}
	var order []string
	for _, c := range calls {
		switch {
		case c.edit && c.span.Target != "":
			fs, ok := files[c.span.Target]
			if !ok {
				fs = &fileState{}
				files[c.span.Target] = fs
				order = append(order, c.span.Target)
			}
			if fs.failedTest {
				fs.cycles++
				fs.failedTest = false
			}
			fs.edits = append(fs.edits, c)
		case c.test && c.span.IsError():
			for _, fs := range files {
				if len(fs.edits) > 0 {
					fs.failedTest = true
				}
			}
		}
	}
	var out []store.Finding
	for _, path := range order {
		fs := files[path]
		if len(fs.edits) >= cfg.ReworkMinEdits || fs.cycles >= cfg.ReworkMinCycles {
			out = append(out, store.Finding{
				Detector: Rework, Severity: severity(len(fs.edits), cfg.ReworkMinEdits), Score: float64(len(fs.edits)),
				Summary: fmt.Sprintf("%s edited %d times, %d edit→failing-test→edit cycles", clip(path, 60), len(fs.edits), fs.cycles),
				SpanIDs: ids(fs.edits),
			})
		}
	}
	return out
}

// detectErrorStreak flags runs of consecutive failed tool calls.
func detectErrorStreak(calls []*call, cfg Config) []store.Finding {
	var out []store.Finding
	var run []*call
	flush := func() {
		if len(run) >= cfg.StreakMin {
			out = append(out, store.Finding{
				Detector: ErrorStreak, Severity: severity(len(run), cfg.StreakMin), Score: float64(len(run)),
				Summary: fmt.Sprintf("%d tool calls failed in a row (first: %s: %s)", len(run), run[0].span.ToolName, clip(run[0].span.ErrorMsg, 60)),
				SpanIDs: ids(run),
			})
		}
		run = nil
	}
	for _, c := range calls {
		if c.span.IsError() {
			run = append(run, c)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// detectStuck flags long stretches of read/search-only activity: the agent is
// busy but not changing anything or checking anything.
func detectStuck(calls []*call, cfg Config) []store.Finding {
	var out []store.Finding
	var run []*call
	flush := func() {
		if len(run) >= cfg.StuckMinCalls {
			distinct := map[string]bool{}
			for _, c := range run {
				distinct[c.span.Target] = true
			}
			out = append(out, store.Finding{
				Detector: Stuck, Severity: severity(len(run), cfg.StuckMinCalls), Score: float64(len(run)),
				Summary: fmt.Sprintf("%d read/search calls in a row over %d targets with no edit or test run", len(run), len(distinct)),
				SpanIDs: ids(run),
			})
		}
		run = nil
	}
	for _, c := range calls {
		if c.read && !c.edit && !c.test {
			run = append(run, c)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// detectBudget flags runaway token use for the whole trace.
func detectBudget(llm []*model.Span, cfg Config) []store.Finding {
	var total int64
	for _, s := range llm {
		total += s.TotalTokens()
	}
	var out []store.Finding
	allIDs := func() []string {
		r := make([]string, len(llm))
		for i, s := range llm {
			r[i] = s.SpanID
		}
		return r
	}
	if total > cfg.BudgetTokens {
		sev := "warn"
		if total > 2*cfg.BudgetTokens {
			sev = "critical"
		}
		out = append(out, store.Finding{Detector: Budget, Severity: sev, Score: float64(total),
			Summary: fmt.Sprintf("%d tokens used, above the %d-token budget", total, cfg.BudgetTokens), SpanIDs: allIDs()})
		return out
	}
	if len(llm) >= cfg.BudgetMinCalls {
		ctx := func(s *model.Span) float64 { return float64(s.InputTokens + s.CacheReadTokens + s.CacheWriteTokens) }
		first := (ctx(llm[0]) + ctx(llm[1]) + ctx(llm[2])) / 3
		n := len(llm)
		last := (ctx(llm[n-1]) + ctx(llm[n-2]) + ctx(llm[n-3])) / 3
		if first > 0 && last/first >= cfg.BudgetGrowth {
			out = append(out, store.Finding{Detector: Budget, Severity: "warn", Score: last / first,
				Summary: fmt.Sprintf("context grew %.1fx over %d LLM calls (%.0f → %.0f input tokens per call)", last/first, n, first, last),
				SpanIDs: allIDs()})
		}
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
