package handler

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/pkg/logger"
)

type DailyPass interface {
	RunDailyPass() (logprocessing.RetentionPass, error)
}

// RegisterRetentionRoute mounts POST /retention on the loopback query
// listener: it runs the daily pass of Retention and Evidence Retention now,
// for an operator or a test that cannot wait for the next UTC day.
//
// It answers {"days_ms", "evidence_ms", "evidence_files_deleted",
// "objects_with_no_row_deleted"}, with 500 and an error as well when a part
// failed.
func RegisterRetentionRoute(app *fiber.App, cleanup DailyPass) {
	app.Post("/retention", func(c *fiber.Ctx) error {
		pass, err := cleanup.RunDailyPass()
		answer := fiber.Map{
			"days_ms":                     milliseconds(pass.DaysTook),
			"evidence_ms":                 milliseconds(pass.EvidenceTook),
			"evidence_files_deleted":      pass.Deleted,
			"objects_with_no_row_deleted": pass.Unreferenced,
		}
		if err != nil {
			logger.L.Error("The daily pass of Retention failed", "error", err)
			answer["error"] = err.Error()
			return c.Status(fiber.StatusInternalServerError).JSON(answer)
		}
		return c.JSON(answer)
	})
}

func milliseconds(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
