package logtransform

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/solidtrace/event_store/storage"
	"github.com/solidtrace/event_store/vrl"
)

// Stats are the running counts of a Project's current Log transform, since
// Log processing first ran it.
type Stats struct {
	// VersionID is the current version's id, or nil when the Project has no
	// Log transform.
	VersionID *int64 `json:"version_id"`
	// Processed counts the Logs the version ran on; of those, Dropped the
	// ones it dropped, and Failed the ones it failed on, or every one when
	// it doesn't compile.
	Processed int64 `json:"processed"`
	Dropped   int64 `json:"dropped"`
	Failed    int64 `json:"failed"`
	// AverageMicros is the average time a Log took, in microseconds: the
	// program and normalising what it returned.
	AverageMicros float64 `json:"average_micros"`
	// CompileError says why the version doesn't compile, if it doesn't.
	CompileError string `json:"compile_error,omitempty"`
}

// Stats returns the running counts of a Project's current Log transform.
// They are zero until Log processing runs the version, and start again with
// each new version.
func (t *Transforms) Stats(projectID uint32) (Stats, error) {
	version, err := t.console.LogTransform(projectID)
	if err != nil {
		return Stats{}, fmt.Errorf("read log transform: %w", err)
	}
	if empty(version.Program) {
		return Stats{}, nil
	}
	stats := Stats{VersionID: &version.ID}

	t.mu.Lock()
	c := t.projects[projectID]
	t.mu.Unlock()
	if c == nil || c.versionID != version.ID {
		// Log processing hasn't run this version yet; the compile error is
		// all there is to say.
		program, diagnostics := vrl.Compile(version.Program)
		if program == nil {
			stats.CompileError = compileError(diagnostics)
		} else {
			program.Free()
		}
		return stats, nil
	}

	stats.CompileError = c.compileError
	stats.Processed = c.counts.processed.Load()
	stats.Dropped = c.counts.dropped.Load()
	stats.Failed = c.counts.failed.Load()
	if stats.Processed > 0 {
		micros := float64(c.counts.took.Load()) / float64(time.Microsecond) / float64(stats.Processed)
		stats.AverageMicros = math.Round(micros*10) / 10
	}
	return stats, nil
}

// counts are a version's running counts. Log processing adds to them while
// Stats reads them.
type counts struct {
	processed, dropped, failed atomic.Int64
	took                       atomic.Int64 // nanoseconds
}

// tally is what one pass of Log processing adds to the running counts.
type tally struct {
	processed, dropped, failed int64
	took                       time.Duration
}

func (t *tally) add(o outcome) {
	t.processed++
	switch {
	case o.dropped:
		t.dropped++
	case o.err != "":
		t.failed++
	}
}

func (c *counts) add(t tally) {
	c.processed.Add(t.processed)
	c.dropped.Add(t.dropped)
	c.failed.Add(t.failed)
	c.took.Add(int64(t.took))
}

// logCount counts the Logs of batches.
func logCount(batches []storage.LogBatch) int {
	n := 0
	for _, batch := range batches {
		n += len(batch.Logs)
	}
	return n
}
