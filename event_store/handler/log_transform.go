package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logtransform"
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
