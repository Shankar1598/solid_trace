package handler

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/redaction"
)

// RegisterRedactionRoutes mounts the check of the extra Redaction patterns
// on the loopback query listener, as RegisterQueryRoutes' routes are.
func RegisterRedactionRoutes(app *fiber.App) {
	app.Post("/redaction-patterns/check", CheckRedactionPatterns)
}

// CheckRedactionPatterns checks extra Redaction patterns before Console
// saves them on Settings → Instance. EventStore applies them, so it checks
// them with its own regular expression syntax (redaction.Check).
//
// POST /redaction-patterns/check {"patterns": [...]} answers {"errors":
// [{"index", "pattern", "error"}]}, with index from 0, and no error when
// each pattern can be saved.
func CheckRedactionPatterns(c *fiber.Ctx) error {
	var body struct {
		Patterns []string `json:"patterns"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": `Send {"patterns": [...]}, a list of strings.`})
	}
	errs := []fiber.Map{}
	if len(body.Patterns) > redaction.MaxPatterns {
		errs = append(errs, fiber.Map{"index": redaction.MaxPatterns, "pattern": body.Patterns[redaction.MaxPatterns],
			"error": fmt.Sprintf("Add at most %d extra patterns.", redaction.MaxPatterns)})
		return c.JSON(fiber.Map{"errors": errs})
	}
	for i, pattern := range body.Patterns {
		if err := redaction.Check(pattern); err != nil {
			errs = append(errs, fiber.Map{"index": i, "pattern": pattern, "error": err.Error()})
		}
	}
	return c.JSON(fiber.Map{"errors": errs})
}
