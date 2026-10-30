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

// The Logs explorer's user edits a large search: the read they abandoned
// stops, and the new one takes its slot.
func TestAUsersNewerHeavyReadReplacesTheirOlderOne(t *testing.T) {
	from := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	reads := handler.NewHeavyReads(1)
	var runCancelled, histogramCancelled bool
	_, ok := reads.Replace("user 7", func() { runCancelled = true }, 1, `search "timeout"`, from, to)
	if !ok {
		t.Fatal("Expected the first heavy read to take the free slot")
	}
	_, ok = reads.Replace("user 7", func() { histogramCancelled = true }, 1, `search "timeout"`, from, to)
	if !ok || runCancelled {
		t.Fatal("Expected the histogram to share its run's slot, without stopping the run")
	}

	if _, ok := reads.Replace("user 7", func() {}, 1, `search "timeout" and "db"`, from, to); !ok {
		t.Error("Expected the user's newer read to take the slot of their older one")
	}
	if !runCancelled || !histogramCancelled {
		t.Errorf("Expected the older read's run and histogram stopped, got run %v, histogram %v", runCancelled, histogramCancelled)
	}
	if reads.Held() != 1 {
		t.Errorf("Expected one slot held, by the newer read, got %d", reads.Held())
	}
}

func TestAHeavyReadNeverReplacesAnotherUsersRead(t *testing.T) {
	from := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	reads := handler.NewHeavyReads(1)
	theirsCancelled := false
	release, _ := reads.Replace("user 8", func() { theirsCancelled = true }, 1, `search "timeout"`, from, to)

	if _, ok := reads.Replace("user 7", func() {}, 1, `search "deadlock"`, from, to); ok {
		t.Error("Expected another user's read refused while theirs holds the slot")
	}
	if theirsCancelled {
		t.Error("Expected another user's read left running")
	}
	release()
	if _, ok := reads.Replace("user 7", func() {}, 1, `search "deadlock"`, from, to); !ok {
		t.Error("Expected the read to take the slot once theirs ended")
	}
}

// Users 7 and 8 run the same search, sharing its slot, and user 7 edits
// theirs: stopping their read would free no slot.
func TestAReadThatCantTakeASlotStopsNothing(t *testing.T) {
	from := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	reads := handler.NewHeavyReads(1)
	mineCancelled := false
	reads.Replace("user 7", func() { mineCancelled = true }, 1, `search "timeout"`, from, to)
	reads.Replace("user 8", func() {}, 1, `search "timeout"`, from, to)

	if _, ok := reads.Replace("user 7", func() {}, 1, `search "deadlock"`, from, to); ok {
		t.Error("Expected the read refused while another user's read of the older search holds the slot")
	}
	if mineCancelled {
		t.Error("Expected the older read left running when the newer one is refused")
	}
}

func TestAReplacedReadGivingBackItsSlotLeavesTheNewerOnesSlotHeld(t *testing.T) {
	from := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	reads := handler.NewHeavyReads(1)
	older, _ := reads.Replace("user 7", func() {}, 1, `search "timeout"`, from, to)
	newer, _ := reads.Replace("user 7", func() {}, 1, `search "timeout" and "db"`, from, to)

	// The older run, stopped, ends.
	older()
	if reads.Held() != 1 {
		t.Errorf("Expected the newer read's slot still held, got %d held", reads.Held())
	}
	if _, ok := reads.Take(1, `search "deadlock"`, from, to); ok {
		t.Error("Expected another read refused while the newer one holds the slot")
	}
	newer()
	if reads.Held() != 0 {
		t.Errorf("Expected no slot held once the newer read ended, got %d", reads.Held())
	}
}
