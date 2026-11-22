package model

import (
	"errors"
	"strings"
	"testing"
)

const now = int64(1767225600_000000000)

func valid() Span {
	return Span{TraceID: "t", SpanID: "s", Kind: KindTool, StartNS: now - 1e9, EndNS: now}
}

func TestNormalizeRejects(t *testing.T) {
	cases := map[string]func(*Span){
		"missing trace id": func(s *Span) { s.TraceID = "" },
		"missing span id":  func(s *Span) { s.SpanID = "" },
		"own parent":       func(s *Span) { s.ParentSpanID = s.SpanID },
		"unknown kind":     func(s *Span) { s.Kind = "banana" },
		"zero start":       func(s *Span) { s.StartNS = 0 },
		"end before start": func(s *Span) { s.EndNS = s.StartNS - 1 },
		"negative tokens":  func(s *Span) { s.InputTokens = -1 },
		"negative cost":    func(s *Span) { s.CostUSD = -0.1 },
		"bad status":       func(s *Span) { s.Status = "meh" },
		"far future":       func(s *Span) { s.StartNS = now + 48*3600*1e9; s.EndNS = s.StartNS },
		"id too long":      func(s *Span) { s.SpanID = strings.Repeat("x", MaxIDLen+1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := valid()
			mutate(&s)
			if err := s.Normalize(now); !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestNormalizeDefaultsAndTruncation(t *testing.T) {
	s := valid()
	s.Kind = "TOOL"
	s.EndNS = 0
	s.ToolName = "Bash"
	s.ToolInput = strings.Repeat("é", MaxPayloadLen) // 2 bytes each → must cut on a rune boundary
	s.Attributes = strings.Repeat("a", MaxAttrsLen+1)
	if err := s.Normalize(now); err != nil {
		t.Fatal(err)
	}
	if s.Kind != KindTool || s.Status != StatusOK || s.Name != "Bash" || s.EndNS != s.StartNS {
		t.Fatalf("defaults not applied: %+v", s)
	}
	if !strings.HasSuffix(s.ToolInput, "…") || len(s.ToolInput) > MaxPayloadLen+len("…") {
		t.Fatalf("payload not truncated: len=%d", len(s.ToolInput))
	}
	if !strings.HasPrefix(s.ToolInput, "é") || strings.ContainsRune(s.ToolInput, '�') {
		t.Fatal("truncation split a UTF-8 rune")
	}
	if s.Attributes != "" {
		t.Fatal("oversized attributes should be dropped")
	}
}

func TestExtractTarget(t *testing.T) {
	cases := map[string]string{
		`{"file_path":"/a/b.py","old_string":"x"}`: "/a/b.py",
		`{"command":"go   test\n ./..."}`:          "go test ./...",
		`{"pattern":"TODO"}`:                       "TODO",
		`not json`:                                 "",
		`{"other":1}`:                              "",
	}
	for in, want := range cases {
		if got := ExtractTarget(in); got != want {
			t.Errorf("ExtractTarget(%q) = %q, want %q", in, got, want)
		}
	}
}
