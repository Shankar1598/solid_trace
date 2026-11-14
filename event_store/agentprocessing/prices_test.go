package agentprocessing

import (
	"math"
	"testing"
)

// One request of each model family at the list prices of the claude-api
// skill (shared/models.md, 2026-11-03): 1000 input, 100 output and 100,000
// cache-read tokens.
func TestTheEstimatedCostOfEachModelFamily(t *testing.T) {
	request := tokens{input: 1000, output: 100, cacheRead: 100_000}
	cases := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{"claude-fable-5-2", 8, 40, 0.20},
		{"claude-mythos-5-2", 8, 40, 0.20},
		{"claude-fable-5-1", 10, 50, 0.25},
		{"claude-mythos-5-1", 10, 50, 0.25},
		{"claude-fable-5", 10, 50, 1},
		{"claude-mythos-5", 10, 50, 1},
		{"claude-opus-5-5", 4, 20, 0.20},
		{"claude-opus-5", 5, 25, 0.50},
		{"claude-sonnet-5-5", 2, 10, 0.10},
		{"claude-haiku-4-5", 1, 5, 0.10},
	}
	for _, c := range cases {
		want := (1000*c.input + 100*c.output + 100_000*c.cacheRead) / 1e6
		got, ok := estimatedCost(c.model, request)
		if !ok || math.Abs(got-want) > 1e-12 {
			t.Errorf("%s: expected %v USD, got %v (%v)", c.model, want, got, ok)
		}
	}
}

// Claude Haiku 5.5 has the long price for a prompt of more than 100,000
// tokens. The prompt is the input, cache-read and cache-write tokens.
func TestTheLongPriceStartsAbove100000PromptTokens(t *testing.T) {
	cases := []struct {
		prompt                          tokens
		input, output, cacheRead, write float64
	}{
		// Exactly 100,000: the standard price.
		{tokens{input: 40_000, output: 100, cacheRead: 50_000, cacheWrite5m: 10_000}, 0.10, 0.50, 0.01, 0.10 * 1.25},
		// One more token of each kind gives the long price.
		{tokens{input: 40_001, output: 100, cacheRead: 50_000, cacheWrite5m: 10_000}, 0.50, 2.50, 0.05, 0.50 * 1.25},
		{tokens{input: 40_000, output: 100, cacheRead: 50_001, cacheWrite5m: 10_000}, 0.50, 2.50, 0.05, 0.50 * 1.25},
		{tokens{input: 40_000, output: 100, cacheRead: 50_000, cacheWrite5m: 10_001}, 0.50, 2.50, 0.05, 0.50 * 1.25},
		{tokens{input: 40_000, output: 100, cacheRead: 50_000, cacheWrite5m: 9_000, cacheWrite1h: 1_001}, 0.50, 2.50, 0.05, 0.50 * 1.25},
	}
	for i, c := range cases {
		p := c.prompt
		want := (float64(p.input)*c.input + float64(p.output)*c.output + float64(p.cacheRead)*c.cacheRead +
			float64(p.cacheWrite5m)*c.write + float64(p.cacheWrite1h)*c.input*2) / 1e6
		got, ok := estimatedCost("claude-haiku-5-5", p)
		if !ok || math.Abs(got-want) > 1e-12 {
			t.Errorf("Case %d: expected %v USD, got %v (%v)", i, want, got, ok)
		}
	}
}
