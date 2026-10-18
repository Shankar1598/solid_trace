package handler

import "testing"

// A SQL run stops by sqlTimeoutSeconds whatever it asks for, so a slow query
// can't hold DuckDB's threads from ingest for long.
func TestASQLRunStopsByItsCapWhateverItAsksFor(t *testing.T) {
	short, long, wrong, tooLong := 0.25, 60.0, -1.0, float64(maxTimeoutSeconds+1)
	cases := []struct {
		asked *float64
		want  float64
	}{
		{nil, sqlTimeoutSeconds},
		{&short, 0.25},
		{&long, sqlTimeoutSeconds},
		{&wrong, -1},
		{&tooLong, maxTimeoutSeconds + 1},
	}
	for _, c := range cases {
		got := sqlTimeout(c.asked)
		if got == nil || *got != c.want {
			t.Errorf("Asked for %v, expected %v, got %v", c.asked, c.want, got)
		}
	}
	if sqlTimeoutSeconds != 10 {
		t.Errorf("Expected SQL runs capped at 10 s, got %v", sqlTimeoutSeconds)
	}
}
