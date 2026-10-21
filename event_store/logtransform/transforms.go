// Package logtransform runs each Project's Log transform, the VRL program an
// admin saves in Console, on the Project's Logs in Log processing: after
// their Log batches are read from Pebble and before they are stored. See
// docs/adr/0012-log-transform-runs-vrl-through-cgo.md.
package logtransform

import (
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
	"github.com/solidtrace/event_store/vrl"
)

// Console is where a Project's current Log transform and Retention are read.
// *storage.ConsoleSettings is the production adapter.
type Console interface {
	LogTransform(projectID uint32) (storage.LogTransformVersion, error)
	LogRetentionDays(projectID uint32) (int, error)
}

// errorAttribute holds why the program failed on a Log, or why it doesn't
// compile.
const errorAttribute = "solidtrace.transform_error"

// maxErrorBytes caps errorAttribute's value.
const maxErrorBytes = 1 << 10

// logsPerRun is how many Logs go to the program in one call at most.
const logsPerRun = 500

// Transforms runs each Project's current Log transform, compiling each
// version once. It is safe for concurrent use.
type Transforms struct {
	console Console

	mu       sync.Mutex
	projects map[uint32]*current // by Project, only those with a program
}

// current is a Project's current Log transform, compiled.
type current struct {
	versionID int64
	program   *vrl.Program // nil when it doesn't compile
	// compileError says why it doesn't compile, as errorAttribute holds it.
	compileError string
}

// New returns Transforms that read each Project's Log transform from console.
func New(console Console) *Transforms {
	return &Transforms{console: console, projects: make(map[uint32]*current)}
}

// Apply runs a Project's current Log transform on the Logs of its Log batches,
// and returns the batches as they are to be stored. It reads the current
// version on every call, so a save applies from the next call on.
//
// A Log the program drops is left out of its batch, which may end up empty.
// What the program returns is normalised as Log ingest normalises a sender's
// Log, and keeps the Log's id. A Log the program fails on, or every Log when
// the version doesn't compile, is kept as it arrived, with the error in
// solidtrace.transform_error. Only reading Console's database fails Apply.
func (t *Transforms) Apply(projectID uint32, batches []storage.LogBatch) ([]storage.LogBatch, error) {
	version, err := t.console.LogTransform(projectID)
	if err != nil {
		return nil, fmt.Errorf("read log transform: %w", err)
	}
	c := t.current(projectID, version)
	if c == nil {
		return batches, nil
	}
	if c.program == nil {
		return failAll(batches, c.compileError), nil
	}
	retentionDays, err := t.console.LogRetentionDays(projectID)
	if err != nil {
		return nil, fmt.Errorf("read retention: %w", err)
	}
	return run(c.program, batches, retentionDays), nil
}

// current returns a Project's compiled Log transform for version, compiling
// it if it is new and freeing the one it replaces, or nil when the Project
// has none.
func (t *Transforms) current(projectID uint32, version storage.LogTransformVersion) *current {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.projects[projectID]
	if c != nil && c.versionID == version.ID {
		return c
	}
	if c != nil {
		c.free()
		delete(t.projects, projectID)
	}
	if strings.TrimSpace(version.Program) == "" {
		return nil
	}
	c = &current{versionID: version.ID}
	program, diagnostics := vrl.Compile(version.Program)
	if program == nil {
		c.compileError = compileError(diagnostics)
	}
	c.program = program
	t.projects[projectID] = c
	return c
}

// Close frees every compiled program.
func (t *Transforms) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for projectID, c := range t.projects {
		c.free()
		delete(t.projects, projectID)
	}
}

func (c *current) free() {
	if c.program != nil {
		c.program.Free()
	}
}

// compileError names why a program doesn't compile, in VRL's own words.
func compileError(diagnostics []vrl.Diagnostic) string {
	var errors []string
	for _, d := range diagnostics {
		if d.Severity != "error" && d.Severity != "bug" {
			continue
		}
		text := d.Formatted
		if text == "" {
			text = d.Message
		}
		errors = append(errors, strings.TrimSpace(text))
	}
	return "the Log transform doesn't compile: " + strings.Join(errors, "\n")
}

// failAll keeps every Log as it arrived, with why.
func failAll(batches []storage.LogBatch, why string) []storage.LogBatch {
	out := make([]storage.LogBatch, len(batches))
	for i, batch := range batches {
		out[i] = storage.LogBatch{ID: batch.ID, Logs: make([]models.Log, len(batch.Logs))}
		for j, log := range batch.Logs {
			out[i].Logs[j] = failed(log, why)
		}
	}
	return out
}

// failed is a Log as it arrived, with why the program failed on it.
func failed(log models.Log, why string) models.Log {
	log.Attributes.String = maps.Clone(log.Attributes.String)
	if log.Attributes.String == nil {
		log.Attributes.String = make(map[string]string, 1)
	}
	log.Attributes.String[errorAttribute] = cut(why, maxErrorBytes)
	return log
}

// cut returns s cut to max bytes, without splitting a character.
func cut(s string, max int) string {
	if len(s) <= max {
		return s
	}
	end := max
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// batchedLog is a Log and the index of its batch.
type batchedLog struct {
	batch int
	log   models.Log
}

// run runs program on the batches' Logs, a few hundred to a call, and
// returns the batches as they are to be stored.
func run(program *vrl.Program, batches []storage.LogBatch, retentionDays int) []storage.LogBatch {
	var logs []batchedLog
	out := make([]storage.LogBatch, len(batches))
	receipts := make([]normalise.Receipt, len(batches))
	for i, batch := range batches {
		out[i].ID = batch.ID
		receipts[i] = normalise.Receipt{At: receivedAt(batch.ID), RetentionDays: retentionDays}
		for _, log := range batch.Logs {
			logs = append(logs, batchedLog{i, log})
		}
	}

	for start := 0; start < len(logs); start += logsPerRun {
		chunk := logs[start:min(start+logsPerRun, len(logs))]
		views := make([]map[string]any, len(chunk))
		for i, l := range chunk {
			views[i] = view(l.log)
		}
		outcomes, err := program.Run(views)
		if err != nil {
			// Unreachable unless the bridge itself breaks. Nothing is lost.
			outcomes = make([]vrl.Outcome, len(chunk))
			for i := range outcomes {
				outcomes[i].Error = err.Error()
			}
		}
		for i, l := range chunk {
			if log, ok := stored(l.log, outcomes[i], receipts[l.batch]); ok {
				out[l.batch].Logs = append(out[l.batch].Logs, log)
			}
		}
	}
	return out
}

// receivedAt is when a Log batch was received: the time of its UUIDv7 id.
func receivedAt(batchID uuid.UUID) time.Time {
	sec, nsec := batchID.Time().UnixTime()
	return time.Unix(sec, nsec).UTC()
}

// stored is the Log to store for a Log that arrived as arrived, after the
// program's outcome, or false when the program dropped it.
func stored(arrived models.Log, outcome vrl.Outcome, r normalise.Receipt) (models.Log, bool) {
	switch {
	case outcome.Dropped:
		return models.Log{}, false
	case outcome.Error != "":
		return failed(arrived, outcome.Error), true
	}
	log, normalised, err := normalise.Transformed(outcome.Log, r)
	switch {
	case err != nil:
		return failed(arrived, err.Error()), true
	case normalised == normalise.Dropped:
		return failed(arrived, "the Log the program returned is over 1 MB"), true
	}
	log.ID = arrived.ID
	return log, true
}
