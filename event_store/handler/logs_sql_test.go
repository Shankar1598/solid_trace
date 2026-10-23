package handler

import (
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// A SQL run stops by the SQL users' time limit whatever it asks for, so a
// slow query can't hold ClickHouse's threads from ingest for long.
func TestASQLRunStopsByItsCapWhateverItAsksFor(t *testing.T) {
	limit := 10 * time.Second
	short, long, wrong, tooLong := 0.25, 60.0, -1.0, float64(maxTimeoutSeconds+1)
	cases := []struct {
		asked *float64
		want  float64
	}{
		{nil, 10},
		{&short, 0.25},
		{&long, 10},
		{&wrong, -1},
		{&tooLong, maxTimeoutSeconds + 1},
	}
	for _, c := range cases {
		got := sqlTimeout(limit, c.asked)
		if got == nil || *got != c.want {
			t.Errorf("Asked for %v, expected %v, got %v", c.asked, c.want, got)
		}
	}
	if got := storage.DefaultSQLLimits().Time; got != limit {
		t.Errorf("Expected SQL runs capped at 10 s, got %v", got)
	}
}
