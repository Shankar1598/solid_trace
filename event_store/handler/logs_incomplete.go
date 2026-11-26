package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// logProcessingLag is how long a Project's oldest waiting Log batch may wait
// before its Logs are not complete. It is the same 60 s as Console's
// LogMonitor::PROCESSING_LAG: a Threshold monitor's range ends that long
// before its run, so a shorter backlog misses no Log of the range.
const logProcessingLag = time.Minute

// incompleteLogs says why a Project's Logs are not complete. BehindSeconds
// is for log_processing_behind, and Until for ingest_gap.
type incompleteLogs struct {
	Reason        string     `json:"reason"`
	BehindSeconds int64      `json:"behind_seconds,omitempty"`
	Until         *time.Time `json:"until,omitempty"`
}

// incomplete says why a Project's Logs are not complete now, or nil when they
// are: first the ingest gap of this start, then the age of the Project's
// oldest Log batch that Log processing has not stored.
func (h *LogsHandler) incomplete(projectID uint32) (*incompleteLogs, error) {
	now := h.clock.Now()
	if h.IngestGap.Holds(now) {
		until := h.IngestGap.Until.UTC()
		return &incompleteLogs{Reason: "ingest_gap", Until: &until}, nil
	}
	oldest, err := h.clickHouse.OldestWaitingLogBatches([]uint32{projectID})
	if err != nil || len(oldest) == 0 {
		return nil, err
	}
	if behind := now.Sub(storage.LogBatchWrittenAt(oldest[0].BatchID)); behind > logProcessingLag {
		return &incompleteLogs{Reason: "log_processing_behind", BehindSeconds: int64(behind.Seconds())}, nil
	}
	return nil, nil
}

// holdIfIncomplete answers a run that asked for hold_if_incomplete, when the
// Project's Logs are not complete: {"incomplete": {"reason",
// "behind_seconds" | "until"}, "elapsed_ms"}, and the run reads nothing.
// Thus a held Monitor puts no load on ClickHouse while Log processing
// catches up.
func (h *LogsHandler) holdIfIncomplete(c *fiber.Ctx, run *run, projectID uint32, hold bool) (answered bool, err error) {
	if !hold {
		return false, nil
	}
	incomplete, err := h.incomplete(projectID)
	if err != nil {
		logger.L.Error("Checking whether a Project's Logs are complete failed", "project_id", projectID, "error", err)
		return true, c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	if incomplete == nil {
		return false, nil
	}
	return true, run.answer(c, fiber.Map{"incomplete": incomplete})
}
