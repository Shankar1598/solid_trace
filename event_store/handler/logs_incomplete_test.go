package handler_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingestgap"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// waitingProject has a Log batch that Log processing has not stored.
// Project 8 has none.
const waitingProject = 7

// incompleteAnswer is the part of a run's answer these tests read.
type incompleteAnswer struct {
	Incomplete *struct {
		Reason        string     `json:"reason"`
		BehindSeconds *int64     `json:"behind_seconds"`
		Until         *time.Time `json:"until"`
	} `json:"incomplete"`
	Table json.RawMessage `json:"table"`
}

// newIncompleteApp is a query app at now, by its clock, whose start had gap,
// with a Log batch of waitingProject's written batchAge before now and not
// stored, and its Logs handler.
func newIncompleteApp(t *testing.T, now time.Time, batchAge time.Duration, gap ingestgap.Gap) (*fiber.App, *handler.LogsHandler) {
	t.Helper()
	logger.Init()
	pebble, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	t.Cleanup(pebble.Close)
	written := now.Add(-batchAge)
	log := models.Log{Time: written, Message: "waits for Log processing"}
	if err := pebble.WriteLogBatch(waitingProject, []models.Log{log}, written); err != nil {
		t.Fatalf("Write a Log batch: %v", err)
	}

	clickhousetest.Empty(t)
	clickHouse, err := clickhousetest.OpenClickHouse(clickhousetest.Config(), nil)
	if err != nil {
		t.Fatalf("Open ClickHouse: %v", err)
	}
	t.Cleanup(func() { clickHouse.Close() })
	clickHouseLogs := storage.NewClickHouseLogs(clickHouse, pebble)

	logs := handler.NewLogsHandler(clickHouseLogs, storage.NewClickHouseSQL(clickHouse, storage.DefaultSQLLimits()), sqlOn{}, clock.NewManual(now))
	logs.IngestGap = gap
	health := handler.NewHealthHandler(pebble, answers{})
	app := fiber.New()
	handler.RegisterQueryRoutes(app, handler.NewEventsHandler(pebble, clickHouse), logs,
		handler.NewLogTransformHandler(logtransform.New(nil, clock.System{})),
		handler.NewOverviewHandler(handler.OverviewConfig{Logs: clickHouseLogs, Clock: clock.System{}}),
		handler.NewTracesHandler(clickHouse), health, handler.NewConfigHandler(&config.Config{}, 0))
	return app, logs
}

// runOn posts a Threshold monitor's Query, or its SQL with sql set, for
// projectID, with hold_if_incomplete as hold, and expects 200.
func runOn(t *testing.T, app *fiber.App, now time.Time, projectID string, sql, hold bool) incompleteAnswer {
	t.Helper()
	status, answer := postRun(t, app, now, projectID, sql, hold)
	if status != fiber.StatusOK {
		t.Fatalf("Project %s, SQL %v: expected 200, got %d: %s", projectID, sql, status, answer)
	}
	var got incompleteAnswer
	if err := json.Unmarshal(answer, &got); err != nil {
		t.Fatalf("Decode %s: %v", answer, err)
	}
	return got
}

// postRun posts the run that runOn posts, and returns its status and answer.
func postRun(t *testing.T, app *fiber.App, now time.Time, projectID string, sql, hold bool) (int, []byte) {
	t.Helper()
	body := map[string]any{
		"from": now.Add(-10 * time.Minute).Format(time.RFC3339), "to": now.Add(-time.Minute).Format(time.RFC3339),
	}
	path := "/api/" + projectID + "/logs/query"
	if sql {
		path = "/api/" + projectID + "/logs/sql"
		body["sql"] = "SELECT count() AS n FROM logs"
	} else {
		body["query"] = "summarize count()"
	}
	if hold {
		body["hold_if_incomplete"] = true
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 10000)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, answer
}

func TestARunThatAsksIsHeldWhileItsProjectsLogProcessingIsBehind(t *testing.T) {
	now := time.Now().UTC()
	app, _ := newIncompleteApp(t, now, 2*time.Minute, ingestgap.Gap{})

	for _, sql := range []bool{false, true} {
		got := runOn(t, app, now, "7", sql, true)
		if got.Incomplete == nil || got.Table != nil {
			t.Fatalf("SQL %v: expected incomplete and no table, got %+v", sql, got)
		}
		if got.Incomplete.Reason != handler.IncompleteLogProcessingBehind || got.Incomplete.Until != nil {
			t.Fatalf("SQL %v: expected log_processing_behind with no until, got %+v", sql, *got.Incomplete)
		}
		if behind := got.Incomplete.BehindSeconds; behind == nil || *behind < 119 || *behind > 121 {
			t.Fatalf("SQL %v: expected about 120 s behind, got %v", sql, behind)
		}

		if got := runOn(t, app, now, "8", sql, true); got.Incomplete != nil || got.Table == nil {
			t.Fatalf("SQL %v: expected the caught-up Project's table, got %+v", sql, got)
		}
		if got := runOn(t, app, now, "7", sql, false); got.Incomplete != nil || got.Table == nil {
			t.Fatalf("SQL %v: expected the table without hold_if_incomplete, got %+v", sql, got)
		}
	}
}

// A batch that has waited less than Console's PROCESSING_LAG is no backlog:
// a Threshold monitor's range ends before it.
func TestABatchUnderAMinuteOldDoesNotHold(t *testing.T) {
	now := time.Now().UTC()
	app, _ := newIncompleteApp(t, now, 30*time.Second, ingestgap.Gap{})
	if got := runOn(t, app, now, "7", false, true); got.Incomplete != nil || got.Table == nil {
		t.Fatalf("Expected the table, got %+v", got)
	}
}

func TestARunThatAsksIsHeldForEveryProjectDuringTheIngestGap(t *testing.T) {
	now := time.Now().UTC()
	until := now.Add(4 * time.Minute)
	app, _ := newIncompleteApp(t, now, 2*time.Minute, ingestgap.Gap{Until: until})

	for _, project := range []string{"7", "8"} {
		got := runOn(t, app, now, project, false, true)
		if got.Incomplete == nil || got.Incomplete.Reason != handler.IncompleteIngestGap || got.Incomplete.BehindSeconds != nil {
			t.Fatalf("Project %s: expected ingest_gap, got %+v", project, got)
		}
		if got.Incomplete.Until == nil || !got.Incomplete.Until.Equal(until) {
			t.Fatalf("Project %s: expected until %v, got %v", project, until, got.Incomplete.Until)
		}
		if got := runOn(t, app, now, project, false, false); got.Incomplete != nil || got.Table == nil {
			t.Fatalf("Project %s: expected the table without hold_if_incomplete, got %+v", project, got)
		}
	}
}

// A check that fails, as on a Pebble read error, answers 500 to a run that
// asks, and a run that does not ask runs.
func TestARunThatAsksAnswers500WhenTheCheckFails(t *testing.T) {
	now := time.Now().UTC()
	app, logs := newIncompleteApp(t, now, 2*time.Minute, ingestgap.Gap{})
	handler.FailWaitingLogBatchReads(logs, errors.New("pebble: read error"))

	for _, sql := range []bool{false, true} {
		if status, answer := postRun(t, app, now, "7", sql, true); status != fiber.StatusInternalServerError {
			t.Fatalf("SQL %v: expected 500, got %d: %s", sql, status, answer)
		}
		if got := runOn(t, app, now, "7", sql, false); got.Incomplete != nil || got.Table == nil {
			t.Fatalf("SQL %v: expected the table without hold_if_incomplete, got %+v", sql, got)
		}
	}
}
