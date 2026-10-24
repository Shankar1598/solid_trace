package handler

import (
	"context"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// The Overview's Health: how long Log processing keeps each Project's Log
// batches waiting, how fast its Log reads ran over the last day, whatever
// the range (query_log keeps only a few days), and the Events of the range.
// Monitors and Issues are Console's to add.

// slowestReads is how many of the slowest reads Health names.
const slowestReads = 10

// overviewHealthData is what Health's parts read.
type overviewHealthData struct {
	oldest []storage.OldestLogBatch
	reads  storage.LogReadTimes
	events []storage.ProjectEvents
}

// healthParts are Health's reads, each filling its own field of d.
func (h *OverviewHandler) healthParts(q overviewQuery, d *overviewHealthData) []func(context.Context) error {
	return []func(context.Context) error{
		func(context.Context) (err error) {
			d.oldest, err = h.logs.OldestWaitingLogBatches(q.scope.ProjectIDs)
			return err
		},
		func(ctx context.Context) (err error) {
			d.reads, err = h.logs.LogReadTimes(ctx, q.scope.ProjectIDs, slowestReads)
			return err
		},
		func(ctx context.Context) (err error) {
			d.events, err = h.logs.CountEventsBetween(ctx, q.scope.ProjectIDs, q.scope.From, q.scope.To)
			return err
		},
	}
}

// Health's answer.
type (
	healthAnswer struct {
		Processing []overviewWaiting `json:"processing"`
		Reads      overviewReads     `json:"reads"`
		Events     []overviewEvents  `json:"events"`
	}
	// overviewWaiting is a Project's oldest Log batch waiting for Log
	// processing: when it arrived and how long it has waited by now.
	overviewWaiting struct {
		ProjectID     uint32    `json:"project_id"`
		OldestBatchAt time.Time `json:"oldest_batch_at"`
		WaitedSeconds float64   `json:"waited_seconds"`
	}
	overviewReads struct {
		Projects []overviewProjectReads `json:"projects"`
		Slowest  []overviewRead         `json:"slowest"`
	}
	overviewProjectReads struct {
		ProjectID uint32  `json:"project_id"`
		Reads     int64   `json:"reads"`
		MedianMs  float64 `json:"median_ms"`
		P95Ms     float64 `json:"p95_ms"`
	}
	overviewRead struct {
		ProjectID  uint32    `json:"project_id"`
		Kind       string    `json:"kind"`
		At         time.Time `json:"at"`
		DurationMs float64   `json:"duration_ms"`
	}
	overviewEvents struct {
		ProjectID uint32 `json:"project_id"`
		Events    int64  `json:"events"`
	}
)

// assembleHealth makes Health's answer: only the Projects with a batch
// waiting, with reads, or with Events appear in each list.
func assembleHealth(q overviewQuery, d overviewHealthData) healthAnswer {
	health := healthAnswer{
		Processing: []overviewWaiting{},
		Reads:      overviewReads{Projects: []overviewProjectReads{}, Slowest: []overviewRead{}},
		Events:     []overviewEvents{},
	}
	for _, batch := range d.oldest {
		at := storage.LogBatchWrittenAt(batch.BatchID).UTC()
		health.Processing = append(health.Processing, overviewWaiting{
			ProjectID: batch.ProjectID, OldestBatchAt: at, WaitedSeconds: max(q.now.Sub(at), 0).Seconds(),
		})
	}
	for _, p := range d.reads.Projects {
		health.Reads.Projects = append(health.Reads.Projects, overviewProjectReads(p))
	}
	for _, r := range d.reads.Slowest {
		health.Reads.Slowest = append(health.Reads.Slowest, overviewRead(r))
	}
	for _, e := range d.events {
		health.Events = append(health.Events, overviewEvents(e))
	}
	return health
}
