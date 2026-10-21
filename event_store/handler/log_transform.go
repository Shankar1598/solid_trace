package handler

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/pkg/logger"
)

// LogTransformHandler serves what Console needs of each Project's Log
// transform on the query app: checking a program, previewing a draft, and
// reading the current version's running counts.
type LogTransformHandler struct {
	transforms *logtransform.Transforms
}

// NewLogTransformHandler serves the Log transforms that transforms runs in
// Log processing.
func NewLogTransformHandler(transforms *logtransform.Transforms) *LogTransformHandler {
	return &LogTransformHandler{transforms: transforms}
}

// POST /api/:project_id/logs/transform/check
//
// Body {"program"}. Answers {"errors": […], "warnings": […]}, each item
// {"message", "code", "start", "end", "text"}, with start and end byte
// offsets in the program. A program over 64 KB is an error.
func (h *LogTransformHandler) Check(c *fiber.Ctx) error {
	if _, err := parseProjectID(c); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req struct {
		Program string `json:"program"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with program"})
	}
	return c.JSON(logtransform.CheckProgram(req.Program))
}

// POST /api/:project_id/logs/transform/preview
//
// Body {"program", "logs"?}. Runs program as Log processing would once it
// is saved, on logs, up to 200 JSON objects normalised first as Log ingest
// normalises them, or on the Project's recent arrivals, newest first, when
// logs is absent or empty. Nothing is stored. Answers {"results": […],
// "kept", "dropped", "failed"}, each result {"before", "after"},
// {"before", "dropped": true} or {"before", "error"}, with each Log as the
// query API shows one; or the check's {"errors", "warnings"} when program
// doesn't compile.
func (h *LogTransformHandler) Preview(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	var req struct {
		Program string            `json:"program"`
		Logs    []json.RawMessage `json:"logs"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "body must be JSON with program, and logs as a list"})
	}
	preview, check, err := h.transforms.Preview(projectID, req.Program, req.Logs)
	var given *logtransform.GivenLogsError
	switch {
	case errors.As(err, &given):
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	case err != nil:
		logger.L.Error("Log transform preview failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	case len(check.Errors) > 0:
		return c.JSON(check)
	}
	return c.JSON(preview)
}
