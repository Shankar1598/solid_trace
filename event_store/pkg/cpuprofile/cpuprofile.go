// Package cpuprofile writes CPU profiles of EventStore to a directory twice
// an hour, so that production's CPU can be read without opening a port:
// fetch a file over SSH and read it with go tool pprof
// (.scratch/eventstore-performance/issues/04-cpu-profiles-on-production.md).
//
// A profile samples only Go's threads: the Log transform's time in VRL shows
// as runtime.cgocall under the Go function that ran it, and a query's work
// is ClickHouse's, in its own process, so it isn't here at all. Each
// profile's log line also gives the process's CPU time over the same window,
// every thread's.
package cpuprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

const (
	// length is how long each profile runs.
	length = 30 * time.Second
	// keep is how many profiles stay in the directory, a day's worth.
	keep = 48
	// prefix and suffix make a profile's file name around its UTC start.
	prefix, suffix = "cpu-", ".pprof"
)

// offsets are when each hour's profiles start: just after the hour starts,
// and half an hour later.
var offsets = []time.Duration{30 * time.Second, 30*time.Minute + 30*time.Second}

// Profiler writes a profile of length at each offset past the hour, by its
// clock, to its directory, and keeps the newest keep.
type Profiler struct {
	dir    string
	clock  clock.Clock
	length time.Duration
	keep   int

	stop chan struct{}
	done chan struct{}
}

// New returns a Profiler that writes to dir, which it makes if need be.
func New(dir string, clock clock.Clock) (*Profiler, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("make the CPU profile directory: %w", err)
	}
	return &Profiler{dir: dir, clock: clock, length: length, keep: keep, stop: make(chan struct{}), done: make(chan struct{})}, nil
}

// Start runs the profiles in their own goroutine.
func (p *Profiler) Start() {
	go func() {
		defer close(p.done)
		p.run()
	}()
}

// Stop stops the profiles, ending one in progress early.
func (p *Profiler) Stop() {
	close(p.stop)
	<-p.done
}

func (p *Profiler) run() {
	for {
		timer := time.NewTimer(next(p.clock.Now()).Sub(p.clock.Now()))
		select {
		case <-p.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := p.profile(p.clock.Now()); err != nil {
			logger.L.Warn("CPU profile failed", "error", err)
		}
	}
}

// next is the start of the next profile after now.
func next(now time.Time) time.Time {
	hour := now.Truncate(time.Hour)
	for _, at := range offsets {
		if start := hour.Add(at); start.After(now) {
			return start
		}
	}
	return hour.Add(time.Hour + offsets[0])
}

// profile writes a profile of p.length starting now, then removes all but the
// newest p.keep. Stop ends it early. It fails if another CPU profile runs.
func (p *Profiler) profile(now time.Time) error {
	name := filepath.Join(p.dir, prefix+now.UTC().Format("20060102T150405Z")+suffix)
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		os.Remove(name)
		return err
	}
	began, cpuBefore := time.Now(), processCPU()
	timer := time.NewTimer(p.length)
	select {
	case <-p.stop:
	case <-timer.C:
	}
	timer.Stop()
	pprof.StopCPUProfile()
	took, cpu := time.Since(began), processCPU()-cpuBefore
	if err := file.Close(); err != nil {
		return err
	}
	logger.L.Info("Wrote a CPU profile", "file", name,
		"took", took.Round(time.Millisecond).String(), "process_cpu", cpu.Round(time.Millisecond).String())
	return p.prune()
}

// processCPU is the CPU time the process has used so far, user and system,
// on every thread.
func processCPU() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}

// prune removes all but the newest p.keep profiles, which sort by name.
func (p *Profiler) prune() error {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return err
	}
	var profiles []string
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			profiles = append(profiles, name)
		}
	}
	slices.Sort(profiles)
	for _, name := range profiles[:max(0, len(profiles)-p.keep)] {
		if err := os.Remove(filepath.Join(p.dir, name)); err != nil {
			return err
		}
	}
	return nil
}
