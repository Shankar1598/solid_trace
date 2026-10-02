package handler_test

import (
	"encoding/json"
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/models"
)

// eventAt is an Event of project 1 for a fingerprint at a given time.
func eventAt(fingerprintID int64, at time.Time) *models.Event {
	event := newEvent(1)
	event.IssueFingerprintID = fingerprintID
	event.Timestamp = at
	return event
}

func countAt(t *testing.T, app *fiber.App, query url.Values) int64 {
	t.Helper()
	resp := get(t, app, "/api/1/events/count?"+query.Encode())
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	var body struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	return body.Count
}

func TestEventCountHonoursTheTimeWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ago := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }

	app := newTestApps(t,
		eventAt(10, now.Add(-3*time.Hour)),
		eventAt(10, now.Add(-2*time.Hour)),
		eventAt(10, now.Add(-1*time.Hour)),
		eventAt(20, now.Add(-1*time.Hour)),
	).query

	cases := []struct {
		name  string
		query url.Values
		want  int64
	}{
		{"lifetime count without a window", url.Values{"fingerprint_ids": {"10"}}, 3},
		{"newer_than alone", url.Values{"fingerprint_ids": {"10"}, "newer_than": {ago(90 * time.Minute)}}, 1},
		{"older_than alone", url.Values{"fingerprint_ids": {"10"}, "older_than": {ago(90 * time.Minute)}}, 2},
		{"both bounds", url.Values{"fingerprint_ids": {"10"}, "newer_than": {ago(150 * time.Minute)}, "older_than": {ago(30 * time.Minute)}}, 2},
		{"window with several fingerprints", url.Values{"fingerprint_ids": {"10,20"}, "newer_than": {ago(90 * time.Minute)}}, 2},
		{"window with another fingerprint", url.Values{"fingerprint_ids": {"20"}, "newer_than": {ago(90 * time.Minute)}}, 1},
		{"window outside a fingerprint's Events", url.Values{"fingerprint_ids": {"20"}, "older_than": {ago(90 * time.Minute)}}, 0},
		{"window across the Project", url.Values{"newer_than": {ago(90 * time.Minute)}}, 2},
		{"timestamp with an offset", url.Values{"fingerprint_ids": {"10"}, "newer_than": {now.Add(-90 * time.Minute).In(time.FixedZone("IST", 19800)).Format(time.RFC3339)}}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countAt(t, app, tc.query); got != tc.want {
				t.Errorf("expected count %d, got %d", tc.want, got)
			}
		})
	}
}

// Regression: the count ignored the window and returned the lifetime count,
// so a threshold compared against every Event the Issue ever had.
func TestWindowedEventCountIsNotTheLifetimeCount(t *testing.T) {
	now := time.Now().UTC()
	app := newTestApps(t,
		eventAt(10, now.Add(-48*time.Hour)),
		eventAt(10, now.Add(-24*time.Hour)),
		eventAt(10, now.Add(-time.Minute)),
	).query

	got := countAt(t, app, url.Values{
		"fingerprint_ids": {"10"},
		"newer_than":      {now.Add(-10 * time.Minute).Format(time.RFC3339)},
	})
	if got != 1 {
		t.Errorf("expected the 1 Event inside the window, got %d", got)
	}
}

func TestEventListingHonoursTheTimeWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	inside := eventAt(10, now.Add(-time.Hour))
	app := newTestApps(t,
		eventAt(10, now.Add(-3*time.Hour)),
		inside,
		eventAt(20, now.Add(-time.Hour)),
	).query

	query := url.Values{
		"fingerprint_ids": {"10"},
		"newer_than":      {now.Add(-2 * time.Hour).Format(time.RFC3339)},
		"older_than":      {now.Format(time.RFC3339)},
	}
	resp := get(t, app, "/api/1/events?"+query.Encode())
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var events []models.Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events) != 1 || events[0].EventUUID != inside.EventUUID {
		t.Errorf("expected only Event %s, got %+v", inside.EventUUID, events)
	}
}

func TestMalformedTimeBoundIsRejected(t *testing.T) {
	app := newTestApps(t, eventAt(10, time.Now().UTC())).query

	for _, path := range []string{"/api/1/events/count", "/api/1/events", "/api/1/events/context"} {
		for _, bound := range []string{"newer_than", "older_than"} {
			for _, value := range []string{"yesterday", "2026-10-02", "1759395600"} {
				query := url.Values{"fingerprint_ids": {"10"}, bound: {value}}
				t.Run(path+" "+bound+"="+value, func(t *testing.T) {
					resp := get(t, app, path+"?"+query.Encode())
					if resp.StatusCode != fiber.StatusBadRequest {
						t.Fatalf("expected 400, got %d", resp.StatusCode)
					}
					var body struct {
						Error string `json:"error"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error == "" {
						t.Errorf("expected a JSON error, got %+v (decode error %v)", body, err)
					}
				})
			}
		}
	}
}

func TestEventContextRejectsATimeWindow(t *testing.T) {
	now := time.Now().UTC()
	app := newTestApps(t, eventAt(10, now.Add(-time.Hour))).query

	for _, bound := range []string{"newer_than", "older_than"} {
		query := url.Values{"fingerprint_ids": {"10"}, bound: {now.Format(time.RFC3339)}}
		t.Run(bound, func(t *testing.T) {
			resp := get(t, app, "/api/1/events/context?"+query.Encode())
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("expected 400, got %d", resp.StatusCode)
			}
		})
	}

	resp := get(t, app, "/api/1/events/context?fingerprint_ids=10")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 without a time window, got %d", resp.StatusCode)
	}
}
