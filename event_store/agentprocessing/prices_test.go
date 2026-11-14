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
