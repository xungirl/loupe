package cost

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xungirl/loupe/server/internal/model"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestLongestPrefixWins(t *testing.T) {
	tb := Default()
	p, ok := tb.Lookup("claude-opus-5-5")
	if !ok || p.Input != 4 {
		t.Fatalf("opus 5.5 should not match the shorter claude-opus-5 row: %+v", p)
	}
	p, ok = tb.Lookup("Claude-Sonnet-4-6-20260101") // dated ids, any case
	if !ok || p.Input != 3 || p.Output != 15 {
		t.Fatalf("dated id lookup: %+v %v", p, ok)
	}
	if _, ok := tb.Lookup("gpt-unknown"); ok {
		t.Fatal("unknown model must not be priced")
	}
}

func TestCostMath(t *testing.T) {
	tb := Default()
	// sonnet-4-6: $3 in, $15 out, cache read 0.1x, cache write 1.25x.
	c, ok := tb.CostUSD("claude-sonnet-4-6", 1_000_000, 1_000_000, 1_000_000, 1_000_000)
	if !ok || !near(c, 3+15+0.3+3.75) {
		t.Fatalf("sonnet cost %v", c)
	}
	// qwen is priced in CNY and converted with the configured rate.
	c, _ = tb.CostUSD("qwen3.8-flash", 1_000_000, 1_000_000, 0, 0)
	if !near(c, (0.8+2.7)*tb.USDPerCNY) {
		t.Fatalf("qwen cost %v", c)
	}
}

func TestApply(t *testing.T) {
	tb := Default()
	spans := []model.Span{
		{Model: "claude-haiku-4-5", InputTokens: 1_000_000},
		{Model: "claude-haiku-4-5", InputTokens: 1_000_000, CostUSD: 9}, // client-supplied cost wins
		{Model: "mystery-model", OutputTokens: 10},
		{Model: "mystery-model"}, // no tokens → not counted
	}
	if n := tb.Apply(spans); n != 1 {
		t.Fatalf("unpriced = %d", n)
	}
	if spans[0].CostUSD != 1 || spans[1].CostUSD != 9 || spans[2].CostUSD != 0 {
		t.Fatalf("costs %v %v %v", spans[0].CostUSD, spans[1].CostUSD, spans[2].CostUSD)
	}
}

// config/prices.json is the user-editable copy of Default(); keep them equal.
func TestShippedConfigMatchesDefault(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "prices.json")
	if os.Getenv("LOUPE_WRITE_PRICES") == "1" {
		b, _ := json.MarshalIndent(Default(), "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Default()
	if !reflect.DeepEqual(got, want) {
		t.Fatal("config/prices.json differs from cost.Default(); regenerate with LOUPE_WRITE_PRICES=1 go test ./internal/cost")
	}
}
