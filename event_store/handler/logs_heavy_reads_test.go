package handler_test

import (
	"testing"
	"time"

	"github.com/solidtrace/event_store/handler"
)

func TestHeavyReadsOfOneQueryAndRangeShareASlotAndOthersWaitForNone(t *testing.T) {
	from := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	reads := handler.NewHeavyReads(1)

	run, ok := reads.Take(1, `search "timeout"`, from, to)
	if !ok {
		t.Fatal("Expected the first heavy read to take the free slot")
	}
	histogram, ok := reads.Take(1, `search "timeout"`, from, to)
	if !ok {
		t.Error("Expected a read of the same Query and range to share its slot")
	}
	for _, other := range []struct {
		what    string
		project uint32
		query   string
		from    time.Time
	}{
		{"another Query", 1, `search "deadlock"`, from},
		{"another range", 1, `search "timeout"`, from.Add(-6 * time.Hour)},
		{"another Project", 2, `search "timeout"`, from},
	} {
		if _, ok := reads.Take(other.project, other.query, other.from, to); ok {
			t.Errorf("Expected a read of %s refused while the slot is held", other.what)
		}
	}

	run()
	run()
	if _, ok := reads.Take(1, `search "deadlock"`, from, to); ok {
		t.Error("Expected the slot held while the histogram still reads, however often the run gave it back")
	}
	histogram()
	if reads.Held() != 0 {
		t.Errorf("Expected no slot held once both reads ended, got %d", reads.Held())
	}
	if _, ok := reads.Take(1, `search "deadlock"`, from, to); !ok {
		t.Error("Expected another Query to take the slot once it was free")
	}
}

func TestNoSlotsCapNoHeavyRead(t *testing.T) {
	from := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	reads := handler.NewHeavyReads(0)
	for i := range 5 {
		if _, ok := reads.Take(1, `search "timeout"`, from.Add(time.Duration(i)*time.Hour), from.Add(24*time.Hour)); !ok {
			t.Fatalf("Expected heavy read %d to run with no cap", i+1)
		}
	}
}
