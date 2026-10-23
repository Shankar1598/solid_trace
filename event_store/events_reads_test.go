package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
)

// getQuery sends a GET to the query app and returns the status and the raw
// answer.
func getQuery(t *testing.T, env *TestEnv, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	resp, err := env.QueryApp.Test(req, 10000)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// getEventsJSON GETs one of the Event reads, expecting 200, and decodes its
// answer into v.
func getEventsJSON(t *testing.T, env *TestEnv, path string, v any) {
	t.Helper()
	status, raw := getQuery(t, env, path)
	if status != 200 {
		t.Fatalf("GET %s expected 200, got %d: %s", path, status, raw)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
}

// listedUUIDs is the UUIDs of the Events a list read answers, in its order.
func listedUUIDs(t *testing.T, env *TestEnv, path string) []string {
	t.Helper()
	var events []struct {
		UUID string `json:"uuid"`
	}
	getEventsJSON(t, env, path, &events)
	uuids := make([]string, len(events))
	for i, event := range events {
		uuids[i] = event.UUID
	}
	return uuids
}

// countOf is what the count read answers.
func countOf(t *testing.T, env *TestEnv, path string) int64 {
	t.Helper()
	var body struct {
		Count int64 `json:"count"`
	}
	getEventsJSON(t, env, path, &body)
	return body.Count
}

// eventContext is what the context read answers: the Event and its
// neighbours over its Issue's Events.
type eventContext struct {
	UUID, Prev, Next string
}

func contextOf(t *testing.T, env *TestEnv, path string) eventContext {
	t.Helper()
	var body struct {
		Event struct {
			UUID string `json:"uuid"`
		} `json:"event"`
		Prev string `json:"prev_uuid"`
		Next string `json:"next_uuid"`
	}
	getEventsJSON(t, env, path, &body)
	return eventContext{body.Event.UUID, body.Prev, body.Next}
}

// fingerprintOf is the id of the fingerprint of the Issue titled title.
func fingerprintOf(t *testing.T, env *TestEnv, title string) int64 {
	t.Helper()
	var id int64
	sqliteQuery(t, env, &id, "SELECT f.id FROM issue_fingerprints f JOIN issues i ON i.id = f.issue_id WHERE i.project_id = 123 AND i.title = ?", title)
	return id
}

func TestEventReadsListCountAndPageThroughAnIssuesEvents(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(hour int) string {
		return time.Date(2026, 9, 30, hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	// Posted out of time order, and across two months.
	second := postEvent(t, env, eventAt("a", at(12)))
	first := postEvent(t, env, eventAt("a", "2026-08-31T23:00:00Z"))
	other := postEvent(t, env, eventAt("b", at(11)))
	third := postEvent(t, env, eventAt("a", at(13)))
	env.Process(t)

	issue := fingerprintOf(t, env, "a")
	of := func(path string, query url.Values) string {
		query.Set("fingerprint_ids", fmt.Sprint(issue))
		return path + "?" + query.Encode()
	}

	lists := []struct {
		name  string
		query url.Values
		want  []string
	}{
		{"oldest first", url.Values{}, []string{first, second, third}},
		{"newest first", url.Values{"sort": {"desc"}}, []string{third, second, first}},
		{"newest first, a page", url.Values{"sort": {"desc"}, "limit": {"1"}, "offset": {"1"}}, []string{second}},
		{"oldest first, past the first", url.Values{"offset": {"1"}}, []string{second, third}},
		{"in a window", url.Values{"newer_than": {at(0)}, "older_than": {at(13)}}, []string{second}},
	}
	for _, list := range lists {
		if got := listedUUIDs(t, env, of("/api/123/events", list.query)); !slices.Equal(got, list.want) {
			t.Errorf("%s: expected %v, got %v", list.name, list.want, got)
		}
	}
	if got := listedUUIDs(t, env, "/api/123/events?sort=desc"); !slices.Equal(got, []string{third, second, other, first}) {
		t.Errorf("Expected the Project's Events newest first, got %v", got)
	}

	counts := []struct {
		name  string
		query url.Values
		want  int64
	}{
		{"the Issue's lifetime", url.Values{}, 3},
		{"newer than", url.Values{"newer_than": {at(12)}}, 1},
		{"older than", url.Values{"older_than": {at(12)}}, 1},
		{"both", url.Values{"newer_than": {at(0)}, "older_than": {at(14)}}, 2},
	}
	for _, count := range counts {
		if got := countOf(t, env, of("/api/123/events/count", count.query)); got != count.want {
			t.Errorf("Count over %s: expected %d, got %d", count.name, count.want, got)
		}
	}
	if got := countOf(t, env, "/api/123/events/count"); got != 4 {
		t.Errorf("Expected the Project's 4 Events counted, got %d", got)
	}

	contexts := []struct {
		name  string
		query url.Values
		want  eventContext
	}{
		{"the latest", url.Values{}, eventContext{third, second, ""}},
		{"the middle", url.Values{"uuid": {second}}, eventContext{second, first, third}},
		{"the first", url.Values{"uuid": {first}}, eventContext{first, "", second}},
		{"the last", url.Values{"uuid": {third}}, eventContext{third, second, ""}},
	}
	for _, c := range contexts {
		if got := contextOf(t, env, of("/api/123/events/context", c.query)); got != c.want {
			t.Errorf("Context of %s: expected %+v, got %+v", c.name, c.want, got)
		}
	}
	for _, uuid := range []string{other, "not-a-uuid"} {
		if status, raw := getQuery(t, env, of("/api/123/events/context", url.Values{"uuid": {uuid}})); status != 404 {
			t.Errorf("Expected 404 for %s outside the Issue, got %d: %s", uuid, status, raw)
		}
	}
}

func TestEventsAtTheSameTimeArePagedEachOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	var posted []string
	for range 4 {
		posted = append(posted, postEvent(t, env, divideByZero))
	}
	env.Process(t)

	var paged []string
	for offset := range 4 {
		paged = append(paged, listedUUIDs(t, env, fmt.Sprintf("/api/123/events?sort=desc&limit=1&offset=%d", offset))...)
	}
	slices.Sort(paged)
	if !slices.Equal(paged, posted) {
		t.Errorf("Expected each Event on one page, got %v of %v", paged, posted)
	}

	// From the latest, prev reaches every Event once, and next leads back.
	at := contextOf(t, env, "/api/123/events/context")
	visited := []string{at.UUID}
	for at.Prev != "" {
		back := contextOf(t, env, "/api/123/events/context?uuid="+at.Prev)
		if back.Next != at.UUID {
			t.Fatalf("Expected next of %s to lead back to %s, got %+v", back.UUID, at.UUID, back)
		}
		at = back
		visited = append(visited, at.UUID)
	}
	slices.Sort(visited)
	if !slices.Equal(visited, posted) {
		t.Errorf("Expected prev to visit each Event once, got %v of %v", visited, posted)
	}
}

func TestEventReadsAnswer503WhileClickHouseIsUnreachable(t *testing.T) {
	link := newCuttableLink(t, clickhousetest.Config().Address)
	env := newTestEnv(t, func(env *TestEnv) { env.ClickHouseConfig.Address = link.Addr() })
	defer env.Cleanup()

	event := postEvent(t, env, divideByZero)
	env.Process(t)
	paths := []string{
		"/api/123/events?sort=desc",
		"/api/123/events/count",
		"/api/123/events/context?uuid=" + event,
	}

	link.Cut()
	for _, path := range paths {
		if status, raw := getQuery(t, env, path); status != 503 || !strings.Contains(string(raw), "Events can't be read now") {
			t.Errorf("Expected GET %s to answer 503 while ClickHouse is unreachable, got %d: %s", path, status, raw)
		}
	}
	// The raw Event is in Pebble, so it is still read.
	if status, raw := getQuery(t, env, "/api/123/events/"+event); status != 200 {
		t.Errorf("Expected the Event by UUID while ClickHouse is unreachable, got %d: %s", status, raw)
	}

	link.Restore()
	for _, path := range paths {
		if status, raw := getQuery(t, env, path); status != 200 || !strings.Contains(string(raw), event) && !strings.Contains(string(raw), `"count":1`) {
			t.Errorf("Expected GET %s to answer once ClickHouse is back, got %d: %s", path, status, raw)
		}
	}
}
