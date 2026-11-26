package handler

import (
	"time"

	"github.com/solidtrace/event_store/storage"
)

// The reasons why a Project's Logs are not complete.
const (
	// IncompleteIngestGap: ingest was down before this start, and senders
	// may still send the Logs that they kept (package ingestgap).
	IncompleteIngestGap = "ingest_gap"
	// IncompleteLogProcessingBehind: the Project's oldest waiting Log batch
	// has waited longer than storage.LogProcessingLag.
	IncompleteLogProcessingBehind = "log_processing_behind"
)

// incompleteLogs says why a Project's Logs are not complete. BehindSeconds
// is for IncompleteLogProcessingBehind, and Until for IncompleteIngestGap.
type incompleteLogs struct {
	Reason        string     `json:"reason"`
	BehindSeconds int64      `json:"behind_seconds,omitempty"`
	Until         *time.Time `json:"until,omitempty"`
}

// holdIfIncomplete says why a run that asked for hold_if_incomplete (hold)
// is held, or nil when it runs: first the ingest gap of this start, then the
// age of the Project's oldest Log batch that Log processing has not stored.
// A held run reads nothing and answers {"incomplete": {"reason",
// "behind_seconds" | "until"}, "elapsed_ms"}. Thus a held Monitor puts no
// load on ClickHouse while Log processing catches up.
func (h *LogsHandler) holdIfIncomplete(projectID uint32, hold bool) (*incompleteLogs, error) {
	if !hold {
		return nil, nil
	}
	now := h.clock.Now()
	if h.IngestGap.Holds(now) {
		until := h.IngestGap.Until.UTC()
		return &incompleteLogs{Reason: IncompleteIngestGap, Until: &until}, nil
	}
	oldest, waiting, err := h.waiting.OldestWaitingLogBatch(projectID)
	if err != nil || !waiting {
		return nil, err
	}
	if behind := now.Sub(storage.LogBatchWrittenAt(oldest)); behind > storage.LogProcessingLag {
		return &incompleteLogs{Reason: IncompleteLogProcessingBehind, BehindSeconds: int64(behind.Seconds())}, nil
	}
	return nil, nil
}
