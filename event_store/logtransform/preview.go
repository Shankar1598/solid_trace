package logtransform

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

// MaxPreviewLogs caps the Logs a preview is given.
const MaxPreviewLogs = 200

// Preview is what a draft program does with some Logs. Nothing is stored.
type Preview struct {
	Results []Result `json:"results"`
	Kept    int      `json:"kept"`
	Dropped int      `json:"dropped"`
	Failed  int      `json:"failed"`
}

// Result is what a draft program does with one Log: Before is the Log as it
// arrived, and After the Log Log processing would store, unless the program
// dropped the Log or failed on it with Error.
type Result struct {
	Before  models.Log  `json:"before"`
	After   *models.Log `json:"after,omitempty"`
	Dropped bool        `json:"dropped,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// GivenLogsError is what is wrong with the Logs a preview was given.
type GivenLogsError struct{ reason string }

func (e *GivenLogsError) Error() string { return e.reason }

// arrival is a Log as it arrived, before any program, and when SolidTrace
// received it.
type arrival struct {
	log        models.Log
	receivedAt time.Time
}

// Preview runs program on a Project's Logs as Log processing would once it
// is saved, and stores nothing. The Logs are logs, JSON objects normalised
// first as Log ingest normalises a sender's Logs, or the Project's recent
// arrivals when there are none. When the program doesn't compile, it
// returns the check's errors instead. A GivenLogsError says what is wrong
// with logs.
func (t *Transforms) Preview(projectID uint32, program string, logs []json.RawMessage) (Preview, Check, error) {
	retentionDays, err := t.console.LogRetentionDays(projectID)
	if err != nil {
		return Preview{}, Check{}, fmt.Errorf("read retention: %w", err)
	}
	arrivals, err := t.given(logs, retentionDays)
	if err != nil {
		return Preview{}, Check{}, err
	}

	preview := Preview{Results: make([]Result, len(arrivals))}
	if strings.TrimSpace(program) == "" {
		// No program: Log processing stores each Log as it arrived.
		for i, a := range arrivals {
			preview.Results[i] = Result{Before: a.log, After: &a.log}
		}
		preview.Kept = len(arrivals)
		return preview, Check{}, nil
	}
	compiled, check := compile(program)
	if compiled == nil {
		return Preview{}, check, nil
	}
	defer compiled.Free()

	before := make([]models.Log, len(arrivals))
	for i, a := range arrivals {
		before[i] = a.log
	}
	outcomes := runProgram(compiled, before, func(i int) normalise.Receipt {
		return normalise.Receipt{At: arrivals[i].receivedAt, RetentionDays: retentionDays}
	})
	for i, o := range outcomes {
		result := Result{Before: before[i]}
		switch {
		case o.dropped:
			result.Dropped = true
			preview.Dropped++
		case o.err != "":
			result.Error = o.err
			preview.Failed++
		default:
			result.After = &o.log
			preview.Kept++
		}
		preview.Results[i] = result
	}
	return preview, check, nil
}

// given normalises the Logs a preview was given, as Log ingest normalises
// a request's Logs.
func (t *Transforms) given(logs []json.RawMessage, retentionDays int) ([]arrival, error) {
	if len(logs) > MaxPreviewLogs {
		return nil, &GivenLogsError{fmt.Sprintf("a preview takes at most %d Logs", MaxPreviewLogs)}
	}
	receipt := normalise.Receipt{At: t.clock.Now().UTC(), RetentionDays: retentionDays}
	arrivals := make([]arrival, len(logs))
	for i, raw := range logs {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return nil, &GivenLogsError{fmt.Sprintf("logs[%d] isn't a JSON object", i)}
		}
		log, outcome := normalise.JSON(object, receipt)
		if outcome == normalise.Dropped {
			return nil, &GivenLogsError{fmt.Sprintf("logs[%d] is over 1 MB, so Log ingest drops it", i)}
		}
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("generate log id: %w", err)
		}
		log.ID = id
		arrivals[i] = arrival{log: log, receivedAt: receipt.At}
	}
	return arrivals, nil
}
