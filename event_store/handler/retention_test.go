package handler_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// fakeDailyPass answers each daily pass with pass and err.
type fakeDailyPass struct {
	pass logprocessing.RetentionPass
	err  error
}

func (f fakeDailyPass) RunDailyPass() (logprocessing.RetentionPass, error) { return f.pass, f.err }

func postRetention(t *testing.T, cleanup fakeDailyPass) (int, map[string]any) {
	t.Helper()
	logger.Init()
	app := fiber.New()
	handler.RegisterRetentionRoute(app, cleanup)
	resp, err := app.Test(httptest.NewRequest("POST", "/retention", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func TestPostRetentionAnswersWhatTheDailyPassDidAndHowLongEachPartTook(t *testing.T) {
	status, body := postRetention(t, fakeDailyPass{pass: logprocessing.RetentionPass{
		DaysTook: 1500 * time.Microsecond, EvidenceTook: 25 * time.Millisecond,
		EvidenceSweep: storage.EvidenceSweep{Deleted: 3, Unreferenced: 1},
	}})
	want := map[string]any{"days_ms": 1.5, "evidence_ms": 25.0, "evidence_files_deleted": 3.0, "objects_with_no_row_deleted": 1.0}
	if status != fiber.StatusOK || len(body) != len(want) {
		t.Fatalf("Expected 200 with %v, got %d with %v", want, status, body)
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("Expected %s %v, got %v", key, value, body[key])
		}
	}
}

func TestPostRetentionAnswers500WithTheErrorWhenAPartFailed(t *testing.T) {
	status, body := postRetention(t, fakeDailyPass{
		pass: logprocessing.RetentionPass{EvidenceSweep: storage.EvidenceSweep{Deleted: 2}},
		err:  errors.New("read Evidence Retention: database is locked"),
	})
	if status != fiber.StatusInternalServerError || body["error"] != "read Evidence Retention: database is locked" || body["evidence_files_deleted"] != 2.0 {
		t.Errorf("Expected 500 with the error and the counts, got %d with %v", status, body)
	}
}
