package agentprocessing

import (
	"regexp"
	"strings"
)

// price is the list price of a model, in US dollars for each million
// tokens. A 5-minute cache write costs 1.25 times the input price and a
// 1-hour cache write 2 times, as the cache-state lines of Claude Code
// count them.
type price struct {
	input, output, cacheRead float64
	// longAbove is the size of a prompt (input, cache read and cache write
	// tokens) above which the request has the price long. 0: no such price.
	longAbove int64
	long      *price
	// fast is true for a model with a fast mode, at 2 times its price.
	fast bool
}

// prices is the price table of the estimated cost: Anthropic's list prices
// of the first-party API on 2026-11-03. A model that is not in it has no
// cost. The cost is an estimate: a subscription, a discount or a partner
// platform has other prices.
var prices = map[string]price{
	"claude-fable-5-2":  {input: 8, output: 40, cacheRead: 0.20, fast: true},
	"claude-mythos-5-2": {input: 8, output: 40, cacheRead: 0.20, fast: true},
	"claude-fable-5-1":  {input: 10, output: 50, cacheRead: 1},
	"claude-mythos-5-1": {input: 10, output: 50, cacheRead: 1},
	"claude-fable-5":    {input: 10, output: 50, cacheRead: 1},
	"claude-opus-5-5":   {input: 4, output: 20, cacheRead: 0.20, fast: true},
	"claude-opus-5":     {input: 5, output: 25, cacheRead: 0.50, fast: true},
	"claude-opus-4-8":   {input: 5, output: 25, cacheRead: 0.50, fast: true},
	"claude-opus-4-7":   {input: 5, output: 25, cacheRead: 0.50},
	"claude-opus-4-6":   {input: 5, output: 25, cacheRead: 0.50},
	"claude-opus-4-5":   {input: 5, output: 25, cacheRead: 0.50},
	"claude-sonnet-5-5": {input: 2, output: 10, cacheRead: 0.10},
	"claude-sonnet-5":   {input: 2, output: 10, cacheRead: 0.20},
	"claude-sonnet-4-6": {input: 3, output: 15, cacheRead: 0.30},
	"claude-sonnet-4-5": {input: 3, output: 15, cacheRead: 0.30},
	"claude-haiku-5-5": {input: 0.10, output: 0.50, cacheRead: 0.01,
		longAbove: 100_000, long: &price{input: 0.50, output: 2.50, cacheRead: 0.05}},
	"claude-haiku-4-5": {input: 1, output: 5, cacheRead: 0.10},
}

// A model id can end in a date or in a context size, as
// claude-haiku-4-5-20251001 or claude-opus-5-5[1m].
var modelSuffix = regexp.MustCompile(`(-\d{8}|\[[0-9a-z]+\])$`)

// tokens are the tokens of one request, by kind.
type tokens struct {
	input, output, cacheRead, cacheWrite5m, cacheWrite1h int64
	speed                                                string
}

// estimatedCost is the cost of a request at the list price of its model.
// ok is false for a model with no price, which has no cost, never 0.
func estimatedCost(model string, t tokens) (cost float64, ok bool) {
	model = modelSuffix.ReplaceAllString(strings.TrimSpace(model), "")
	p, ok := prices[model]
	if !ok {
		return 0, false
	}
	if p.long != nil && t.input+t.cacheRead+t.cacheWrite5m+t.cacheWrite1h > p.longAbove {
		p = *p.long
	}
	multiplier := 1.0
	switch t.speed {
	case "", "standard":
	case "fast":
		if !p.fast {
			return 0, false
		}
		multiplier = 2
	default:
		return 0, false
	}
	perToken := multiplier / 1e6
	return (float64(t.input)*p.input + float64(t.output)*p.output + float64(t.cacheRead)*p.cacheRead +
		float64(t.cacheWrite5m)*p.input*1.25 + float64(t.cacheWrite1h)*p.input*2) * perToken, true
}
