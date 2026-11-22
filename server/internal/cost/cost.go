// Package cost turns token counts into money using a configurable price table.
//
// Prices are per million tokens. Models are matched by the longest prefix, so
// "claude-sonnet-4-6" also prices dated ids like "claude-sonnet-4-6-20260101".
// Models missing from the table are left at cost 0 and counted as "unpriced"
// rather than guessed.
package cost

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/xungirl/loupe/server/internal/model"
)

// Price is one row of the table, in the row's currency per 1M tokens.
type Price struct {
	Prefix     string  `json:"prefix"`
	Currency   string  `json:"currency"` // "USD" or "CNY"
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// Table is the full configuration.
type Table struct {
	// USDPerCNY converts CNY-priced models into USD so all costs share one unit.
	// It is a user setting, not a measured value.
	USDPerCNY float64 `json:"usd_per_cny"`
	Source    string  `json:"source"`
	Prices    []Price `json:"prices"`
}

// Load reads a JSON price table.
func Load(path string) (*Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	t.sortPrefixes()
	return &t, nil
}

// Default is the built-in table (also shipped as config/prices.json).
// Claude rows come from Anthropic's published first-party prices; cache rows
// use the published multipliers (write = 1.25 x input, read = 0.1 x input)
// unless a model lists its own cache-read price. Qwen rows use the DashScope
// prices given for this project. Verify before trusting dollar figures.
func Default() *Table {
	usd := func(prefix string, in, out, cacheRead float64) Price {
		if cacheRead == 0 {
			cacheRead = round6(in * 0.1)
		}
		return Price{Prefix: prefix, Currency: "USD", Input: in, Output: out, CacheRead: cacheRead, CacheWrite: round6(in * 1.25)}
	}
	t := &Table{
		USDPerCNY: 0.14,
		Source: "Claude: Anthropic first-party list prices per 1M tokens (as cached 2026-11-03), cache write = 1.25x input, " +
			"cache read = 0.1x input unless listed; Qwen: DashScope prices given for this project; usd_per_cny is a user setting; " +
			"sim-* is a made-up price for synthetic demo traces",
		Prices: []Price{
			usd("claude-opus-5-5", 4, 20, 0.20),
			usd("claude-opus-5", 5, 25, 0),
			usd("claude-opus-4-8", 5, 25, 0),
			usd("claude-opus-4-7", 5, 25, 0),
			usd("claude-opus-4-6", 5, 25, 0),
			usd("claude-sonnet-5-5", 2, 10, 0.10),
			usd("claude-sonnet-5", 2, 10, 0),
			usd("claude-sonnet-4-6", 3, 15, 0),
			usd("claude-haiku-5-5", 0.10, 0.50, 0),
			usd("claude-haiku-4-5", 1, 5, 0),
			{Prefix: "qwen3.8-flash", Currency: "CNY", Input: 0.8, Output: 2.7},
			{Prefix: "fake-", Currency: "USD"}, // the deterministic fake provider is free
			// Synthetic corpus/demo traces only: a made-up price so the demo's cost views are not all zero.
			{Prefix: "sim-", Currency: "USD", Input: 1, Output: 5, CacheRead: 0.1, CacheWrite: 1.25},
		},
	}
	t.sortPrefixes()
	return t
}

func round6(x float64) float64 { return math.Round(x*1e6) / 1e6 }

func (t *Table) sortPrefixes() {
	// Longest prefix first so "claude-opus-5-5" wins over "claude-opus-5".
	sort.SliceStable(t.Prices, func(i, j int) bool { return len(t.Prices[i].Prefix) > len(t.Prices[j].Prefix) })
}

// Lookup finds the price row for a model.
func (t *Table) Lookup(modelName string) (Price, bool) {
	m := strings.ToLower(modelName)
	for _, p := range t.Prices {
		if strings.HasPrefix(m, strings.ToLower(p.Prefix)) {
			return p, true
		}
	}
	return Price{}, false
}

// CostUSD prices a token usage. ok=false means the model is unknown.
func (t *Table) CostUSD(modelName string, in, out, cacheRead, cacheWrite int64) (float64, bool) {
	p, ok := t.Lookup(modelName)
	if !ok {
		return 0, false
	}
	c := (float64(in)*p.Input + float64(out)*p.Output + float64(cacheRead)*p.CacheRead + float64(cacheWrite)*p.CacheWrite) / 1e6
	if p.Currency == "CNY" {
		c *= t.USDPerCNY
	}
	return c, true
}

// Apply fills CostUSD on LLM spans that have tokens but no client-supplied
// cost. It returns how many spans had tokens for a model not in the table.
func (t *Table) Apply(spans []model.Span) (unpriced int) {
	for i := range spans {
		sp := &spans[i]
		if sp.CostUSD > 0 || sp.TotalTokens() == 0 {
			continue
		}
		c, ok := t.CostUSD(sp.Model, sp.InputTokens, sp.OutputTokens, sp.CacheReadTokens, sp.CacheWriteTokens)
		if !ok {
			unpriced++
			continue
		}
		sp.CostUSD = c
	}
	return unpriced
}
