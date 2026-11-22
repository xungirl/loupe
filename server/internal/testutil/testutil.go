// Package testutil has small helpers shared by tests.
package testutil

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/xungirl/loupe/server/internal/model"
	"github.com/xungirl/loupe/server/internal/store"
)

// OpenStore opens a fresh SQLite database in a temp dir, closed at test end.
func OpenStore(t testing.TB) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// Base is an arbitrary fixed timestamp (2026-01-01T00:00:00Z) for tests.
const Base int64 = 1767225600_000000000

// Span builds a valid span; offsets are in milliseconds from Base.
func Span(trace, id, parent, kind string, startMS, endMS int64) model.Span {
	return model.Span{
		TraceID: trace, SpanID: id, ParentSpanID: parent, Kind: kind,
		Name: fmt.Sprintf("%s-%s", kind, id), Status: model.StatusOK,
		StartNS: Base + startMS*1e6, EndNS: Base + endMS*1e6,
	}
}
