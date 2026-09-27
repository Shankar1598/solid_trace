package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/pkg/logger"
)

// Sentry SDKs retry no status, so these codes only decide what the SDK logs
// and whether it backs off. A 429 would pause the SDK for 60s.
func TestRenderIngestErrorStatus(t *testing.T) {
	logger.Init()

	tests := []struct {
		err  error
		want int
	}{
		{ingest.ErrInvalidEnvelope, fiber.StatusBadRequest},
		{ingest.ErrInvalidItemHeader, fiber.StatusBadRequest},
		{ingest.ErrInvalidEventJSON, fiber.StatusBadRequest},
		{ingest.ErrOverloaded, fiber.StatusServiceUnavailable},
		{fmt.Errorf("create issue: %w", fmt.Errorf("disk I/O error")), fiber.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			app := fiber.New()
			app.Post("/", func(c *fiber.Ctx) error {
				return (&IngestHandler{}).renderIngestError(c, tt.err)
			})

			req, _ := http.NewRequest(http.MethodPost, "/", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.want {
				t.Errorf("expected %d, got %d", tt.want, resp.StatusCode)
			}
		})
	}
}
