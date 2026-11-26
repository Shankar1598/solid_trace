package handler_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/handler"
)

// fails is a store whose ping returns err.
type fails struct{ err error }

func (f fails) Ping() error { return f.err }

// /health on each port says which part is up. The ingest port answers 200
// while ingest is up, so a load balancer keeps ingest in service while
// ClickHouse starts or is down. The query port answers 200 only when both
// are up.
func TestHealthSaysWhichPartIsUp(t *testing.T) {
	lastTry := errors.New("dial tcp 127.0.0.1:4200: connection refused")
	for _, tc := range []struct {
		name                  string
		pebble, clickHouse    handler.Pinger
		ingestCode, queryCode int
		ingest, clickHouseIs  string
		clickHouseCheckHas    string
	}{
		{"both up", answers{}, answers{}, 200, 200, "up", "ready", "ok"},
		{"ClickHouse starts", answers{}, fails{handler.ErrClickHouseStarting}, 200, 503, "up", "starting", "ClickHouse is starting"},
		{"a step of the start fails", answers{}, fails{fmt.Errorf("%w. The last try failed: %w", handler.ErrClickHouseStarting, lastTry)},
			200, 503, "up", "starting", lastTry.Error()},
		{"ClickHouse fails after the start passed", answers{}, fails{lastTry}, 200, 503, "up", "down", lastTry.Error()},
		{"Pebble fails", fails{errors.New("pebble: disk full")}, answers{}, 503, 503, "down", "ready", "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			health := handler.NewHealthHandler(tc.pebble, tc.clickHouse)
			ingestApp := fiber.New()
			handler.RegisterIngestRoutes(ingestApp, nil, health)
			queryApp := fiber.New()
			queryApp.Get("/health", health.Health)

			for _, port := range []struct {
				name string
				app  *fiber.App
				code int
			}{{"ingest", ingestApp, tc.ingestCode}, {"query", queryApp, tc.queryCode}} {
				req, _ := http.NewRequest("GET", "/health", nil)
				resp, err := port.app.Test(req)
				if err != nil {
					t.Fatalf("%s /health: %v", port.name, err)
				}
				body, _ := io.ReadAll(resp.Body)
				var got struct {
					Status, Ingest, ClickHouse string
					Checks                     map[string]string
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("%s /health: %v in %s", port.name, err, body)
				}
				wantStatus := "unhealthy"
				if tc.ingest == "up" && tc.clickHouseIs == "ready" {
					wantStatus = "healthy"
				}
				if resp.StatusCode != port.code || got.Status != wantStatus || got.Ingest != tc.ingest ||
					got.ClickHouse != tc.clickHouseIs || !strings.Contains(got.Checks["clickhouse"], tc.clickHouseCheckHas) {
					t.Errorf("%s /health = %d %s, want %d with status %q, ingest %q, ClickHouse %q and a ClickHouse check that has %q",
						port.name, resp.StatusCode, body, port.code, wantStatus, tc.ingest, tc.clickHouseIs, tc.clickHouseCheckHas)
				}
			}
		})
	}
}
