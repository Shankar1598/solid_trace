package cpuprofile

import (
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

func TestProfilesStartHalfAMinuteAndHalfAnHourPastTheHour(t *testing.T) {
	at := func(hour, minute, second int) time.Time {
		return time.Date(2026, 10, 21, hour, minute, second, 0, time.UTC)
	}
	for _, c := range []struct{ now, want time.Time }{
		{at(17, 0, 0), at(17, 0, 30)},
		{at(17, 0, 30), at(17, 30, 30)},
		{at(17, 12, 0), at(17, 30, 30)},
		{at(17, 30, 30), at(18, 0, 30)},
		{at(23, 59, 59), at(0, 0, 30).AddDate(0, 0, 1)},
	} {
		if got := next(c.now); !got.Equal(c.want) {
			t.Errorf("After %v, expected the next profile at %v, got %v", c.now, c.want, got)
		}
	}
}

// newTestProfiler is a Profiler of short profiles that keeps two, in a
// directory of its own.
func newTestProfiler(t *testing.T) *Profiler {
	t.Helper()
	logger.Init()
	p, err := New(filepath.Join(t.TempDir(), "profiles"), clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	p.length, p.keep = 20*time.Millisecond, 2
	return p
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestEachProfileIsWrittenAndOnlyTheNewestAreKept(t *testing.T) {
	p := newTestProfiler(t)
	if err := os.WriteFile(filepath.Join(p.dir, "notes.txt"), []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 21, 17, 0, 30, 0, time.UTC)
	for i := range 3 {
		if err := p.profile(start.Add(time.Duration(i) * 30 * time.Minute)); err != nil {
			t.Fatalf("profile %d: %v", i, err)
		}
	}
	want := []string{"cpu-20261021T173030Z.pprof", "cpu-20261021T180030Z.pprof", "notes.txt"}
	if got := files(t, p.dir); !slices.Equal(got, want) {
		t.Fatalf("Expected %v, got %v", want, got)
	}
	info, err := os.Stat(filepath.Join(p.dir, want[1]))
	if err != nil || info.Size() == 0 {
		t.Errorf("Expected a profile with bytes in it, got %v (%v)", info, err)
	}
}

func TestNoProfileIsWrittenWhileAnotherRuns(t *testing.T) {
	p := newTestProfiler(t)
	if err := pprof.StartCPUProfile(io.Discard); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	if err := p.profile(time.Date(2026, 10, 21, 17, 0, 30, 0, time.UTC)); err == nil {
		t.Error("Expected a profile to fail while another runs")
	}
	if got := files(t, p.dir); len(got) != 0 {
		t.Errorf("Expected no file left, got %v", got)
	}
}

func TestStopEndsAProfileInProgress(t *testing.T) {
	p := newTestProfiler(t)
	p.length = time.Hour
	done := make(chan error)
	go func() { done <- p.profile(time.Date(2026, 10, 21, 17, 0, 30, 0, time.UTC)) }()
	time.Sleep(50 * time.Millisecond)
	close(p.stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Expected Stop to end the profile")
	}
	if got := files(t, p.dir); len(got) != 1 {
		t.Errorf("Expected the ended profile written, got %v", got)
	}
}
