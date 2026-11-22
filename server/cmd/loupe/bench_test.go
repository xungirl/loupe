package main

import (
	"path/filepath"
	"testing"

	"github.com/xungirl/loupe/server/internal/claudeimport"
	"github.com/xungirl/loupe/server/internal/model"
)

func TestPct(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if pct(xs, 50) != 5 || pct(xs, 99) != 9 || pct(xs, 100) != 10 || pct(nil, 50) != 0 {
		t.Fatalf("pct wrong: %v %v %v", pct(xs, 50), pct(xs, 99), pct(xs, 100))
	}
}

// The synthetic benchmark transcripts must be fully understood by the importer,
// otherwise the importer benchmark would measure a different code path.
func TestGeneratedTranscriptsImportCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tr")
	if err := runGenTranscripts([]string{"-dir", dir, "-sessions", "12", "-turns", "30"}); err != nil {
		t.Fatal(err)
	}
	n := 0
	st, err := claudeimport.Import(dir, func(s []model.Span) error { n += len(s); return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions != 12 || st.Malformed+st.Truncated+st.BadTime != 0 || st.MissingResults != 0 || st.OrphanResults != 0 {
		t.Fatalf("unexpected parse stats %+v", st)
	}
	if st.Subagents == 0 || st.Unlinked != 0 || st.LinkedByID != st.Subagents {
		t.Fatalf("subagents not linked by id: %+v", st)
	}
	if n != st.Spans || st.MergedLines == 0 {
		t.Fatalf("spans %d stats %+v", n, st)
	}
}
