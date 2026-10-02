package logslayouts

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLogIsAPureFunctionOfSeedAndID(t *testing.T) {
	a, b := NewGenerator(7), NewGenerator(7)
	for _, id := range []int64{0, 1, 999, 123456} {
		if !reflect.DeepEqual(a.Log(id), b.Log(id)) {
			t.Fatalf("Log(%d) differs between two generators with the same seed", id)
		}
	}
	if reflect.DeepEqual(a.Log(5), NewGenerator(8).Log(5)) {
		t.Fatal("different seeds gave the same Log")
	}
}

func TestLogTimeFollowsTheTargetRate(t *testing.T) {
	g := NewGenerator(1)
	if got := g.Log(0).Time; !got.Equal(Base) {
		t.Fatalf("Log(0).Time = %v, want %v", got, Base)
	}
	if got := g.Log(LogsPerSecond).Time.Sub(Base).Seconds(); got != 1 {
		t.Fatalf("Log(LogsPerSecond) is %vs after Base, want 1s", got)
	}
}

func TestAttrsJSONMatchesAttrs(t *testing.T) {
	g := NewGenerator(1)
	for id := int64(0); id < 2000; id++ {
		l := g.Log(id)
		var got map[string]any
		if err := json.Unmarshal(l.AttrsJSON, &got); err != nil {
			t.Fatalf("Log(%d) AttrsJSON is not JSON: %v\n%s", id, err, l.AttrsJSON)
		}
		if !reflect.DeepEqual(got, l.AttrsMap()) {
			t.Fatalf("Log(%d) AttrsJSON %v != Attrs %v", id, got, l.AttrsMap())
		}
	}
}

func TestDistributionMatchesTheTicket(t *testing.T) {
	g := NewGenerator(1)
	const n = 100_000
	var size, traced, minMsg, maxMsg int
	minMsg = 1 << 30
	levels := map[string]int{}
	services := map[string]bool{}
	tailKeys := map[string]bool{}
	mixed := map[string]map[Kind]bool{}
	for id := int64(0); id < n; id++ {
		l := g.Log(id)
		size += l.Size
		if l.TraceID != "" {
			traced++
		}
		minMsg, maxMsg = min(minMsg, len(l.Message)), max(maxMsg, len(l.Message))
		levels[l.Level]++
		services[l.Service] = true
		for _, a := range l.Attrs {
			if isTailKey(a.Key) {
				tailKeys[a.Key] = true
			}
			if mixed[a.Key] == nil {
				mixed[a.Key] = map[Kind]bool{}
			}
			mixed[a.Key][a.Value.Kind] = true
		}
	}
	t.Logf("average Log size %d B", size/n)
	if avg := size / n; avg < 500 || avg > 700 {
		t.Errorf("average Log size %d B, want about 600", avg)
	}
	if minMsg < 50 || maxMsg > 500 {
		t.Errorf("message lengths %d..%d, want within 50..500", minMsg, maxMsg)
	}
	if pct := float64(traced) / n * 100; pct < 1.5 || pct > 2.5 {
		t.Errorf("%.2f%% of Logs carry a trace id, want about 2%%", pct)
	}
	if len(services) < 3 || len(services) > 8 {
		t.Errorf("%d services, want 3..8", len(services))
	}
	if len(levels) != 6 {
		t.Errorf("levels %v, want all six", levels)
	}
	for lvl, c := range levels {
		if lvl != "info" && c >= levels["info"] {
			t.Errorf("level %s (%d) is not rarer than info (%d)", lvl, c, levels["info"])
		}
	}
	if len(tailKeys) < 1000 {
		t.Errorf("%d distinct long-tail keys in %d Logs, want a long tail toward %d", len(tailKeys), n, TailKeys)
	}
	var mixedKeys int
	for _, kinds := range mixed {
		if kinds[KindString] && (kinds[KindInt] || kinds[KindFloat]) {
			mixedKeys++
		}
	}
	if pct := float64(mixedKeys) / float64(len(mixed)) * 100; pct < 3 || pct > 7 {
		t.Errorf("%.1f%% of keys are mixed-type, want about 5%%", pct)
	}
}

func TestQueryTargetsAreSelective(t *testing.T) {
	g := NewGenerator(1)
	const n = 200_000
	var common, rare, mixedRange int
	for id := int64(0); id < n; id++ {
		m := g.Log(id).AttrsMap()
		if m[CommonKey] == CommonValue {
			common++
		}
		if m[RareKey] == RareValue {
			rare++
		}
		if v, ok := m[MixedKey].(float64); ok && v >= MixedLo && v <= MixedHi {
			mixedRange++
		}
	}
	if pct := float64(common) / n * 100; pct < 5 || pct > 25 {
		t.Errorf("common equality matches %.2f%%, want a common filter", pct)
	}
	if pct := float64(rare) / n * 100; pct <= 0 || pct > 0.1 {
		t.Errorf("rare equality matches %.4f%%, want a rare filter", pct)
	}
	if mixedRange == 0 {
		t.Error("mixed-type range matches nothing")
	}
}
