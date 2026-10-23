package handler

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/pkg/logger"
)

const (
	// defaultFieldAttributes is how many Attributes a Project's Fields list
	// when the request gives no limit, and maxFieldAttributes the most.
	defaultFieldAttributes = 100
	maxFieldAttributes     = 500
)

// GET /api/:project_id/logs/fields?from&to&limit
//
// Answers a Project's Fields over [from, to) (RFC 3339), so that a client
// can learn what a Query can read: {"built_in": [{"name", "type"}],
// "attributes": [{"name", "type", "logs"}], "truncated"}.
//
//   - built_in are the built-in fields every Log has: time, a datetime, and
//     the strings level, message, service, environment, trace_id and span_id.
//   - attributes are the top Attributes of the UTC days the range touches,
//     with how many Logs carried each, most first; type is "string",
//     "number" or "bool", and a name whose values have two types is listed
//     once for each. They come from Log processing's popularity counts
//     (storage.ClickHouseLogs.AttributeCounts), so they leave out JSON Attributes.
//   - truncated is true when there are more Attributes than limit (default
//     100, at most 500).
//
// A range or limit that isn't one answers 400 with {"error"}.
func (h *LogsHandler) Fields(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	from, fromErr := time.Parse(time.RFC3339Nano, c.Query("from"))
	to, toErr := time.Parse(time.RFC3339Nano, c.Query("to"))
	if fromErr != nil || toErr != nil {
		return c.Status(400).JSON(rangeProblem(time.Time{}, time.Time{}))
	}
	if problem := rangeProblem(from, to); problem != nil {
		return c.Status(400).JSON(problem)
	}
	limit := defaultFieldAttributes
	if text := c.Query("limit"); text != "" {
		if limit, err = strconv.Atoi(text); err != nil || limit < 1 {
			return c.Status(400).JSON(fiber.Map{"error": "limit must be a positive whole number"})
		}
		limit = min(limit, maxFieldAttributes)
	}

	attributes, truncated, err := h.clickHouse.AttributeCounts(c.UserContext(), projectID, from, to, limit)
	if err != nil {
		logger.L.Error("Counting a Project's Attributes failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	return c.JSON(fiber.Map{
		"built_in":   logquery.BuiltInFields(),
		"attributes": attributes,
		"truncated":  truncated,
	})
}
