package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/pkg/logger"
)

// volumesWindow is what Volumes cover: the last 24 hours, up to the end of
// the minute now is in.
const volumesWindow = 24 * time.Hour

type (
	volumesRequest struct {
		ProjectIDs []uint32 `json:"project_ids"`
	}
	volumesAnswer struct {
		Projects []projectVolume `json:"projects"`
	}
	projectVolume struct {
		ProjectID    uint32  `json:"project_id"`
		LogsPerHour  float64 `json:"logs_per_hour"`
		BytesPerHour float64 `json:"bytes_per_hour"`
		Files        int64   `json:"files"`
	}
)

// POST /volumes
//
// Answers each Project of project_ids, in that order, with its volume over
// the last 24 hours, for an agent to size a Query's range before running
// it:
//
//   - logs_per_hour, its Logs an hour, by their own time, from the counts by
//     minute;
//   - bytes_per_hour, their bytes as Source counts measure them, each Log's
//     message and Attributes' JSON: the Logs times the bytes a Log kept over
//     the UTC days the 24 hours touch;
//   - files, its Sources, a Service and file, that kept Logs on those days.
//
// It reads only the counts, never logs. A Project with no Logs answers
// zeros. A request with no Projects answers 400 with {"error"}.
func (h *OverviewHandler) Volumes(c *fiber.Ctx) error {
	var req volumesRequest
	if err := c.BodyParser(&req); err != nil || len(req.ProjectIDs) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with project_ids, at least one"})
	}
	to := h.clock.Now().UTC().Truncate(time.Minute).Add(time.Minute)
	from := to.Add(-volumesWindow)

	ctx, cancel := context.WithTimeout(c.UserContext(), overviewTimeout)
	defer cancel()
	logs, err := h.logs.CountedLogs(ctx, req.ProjectIDs, from, to)
	if err != nil {
		return h.volumesFailed(c, req, err)
	}
	// The last day is the last minute's, so a window ending at midnight
	// doesn't take in the next day.
	sources, err := h.logs.SourceCounts(ctx, req.ProjectIDs, from, to.Add(-time.Minute))
	if err != nil {
		return h.volumesFailed(c, req, err)
	}

	type kept struct{ logs, bytes, files int64 }
	days := map[uint32]kept{}
	for _, source := range sources {
		if source.Logs == 0 {
			continue
		}
		k := days[source.ProjectID]
		k.logs += source.Logs
		k.bytes += source.Bytes
		k.files++
		days[source.ProjectID] = k
	}
	hours := volumesWindow.Hours()
	answer := volumesAnswer{Projects: make([]projectVolume, len(req.ProjectIDs))}
	for i, projectID := range req.ProjectIDs {
		n, k := logs[projectID], days[projectID]
		v := projectVolume{ProjectID: projectID, LogsPerHour: float64(n) / hours, Files: k.files}
		if k.logs > 0 {
			v.BytesPerHour = float64(n) * float64(k.bytes) / float64(k.logs) / hours
		}
		answer.Projects[i] = v
	}
	return c.JSON(answer)
}

func (h *OverviewHandler) volumesFailed(c *fiber.Ctx, req volumesRequest, err error) error {
	logger.L.Error("Answering Volumes failed", "projects", len(req.ProjectIDs), "error", err)
	return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
}
