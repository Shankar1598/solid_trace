package storage

import (
	"testing"
	"time"
)

func TestADayPassesRetentionOnTheFirstDayItIsNoLongerKept(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	passes := LogDayPassesRetention(day.Add(15*time.Hour), 7)
	if want := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC); !passes.Equal(want) {
		t.Fatalf("Expected Oct 1 to pass a 7-day Retention on %s, got %s", want, passes)
	}
	if kept := FirstLogDayKept(passes.Add(-time.Second), 7); kept.After(day) {
		t.Errorf("Expected Oct 1 still kept just before %s, but the first day kept is %s", passes, kept)
	}
	if kept := FirstLogDayKept(passes, 7); !kept.After(day) {
		t.Errorf("Expected Oct 1 deleted on %s, but the first day kept is %s", passes, kept)
	}
}
