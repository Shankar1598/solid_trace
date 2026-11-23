package handler_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

// fakeAgentSessionReader records the filter of each read of the list,
// and has no Agent sessions.
type fakeAgentSessionReader struct {
	filters []storage.AgentSessionFilter
}

func (f *fakeAgentSessionReader) AgentSessionList(_ context.Context, _ uint32, filter storage.AgentSessionFilter) ([]storage.AgentSessionTotals, error) {
	f.filters = append(f.filters, filter)
	return nil, nil
}

func (f *fakeAgentSessionReader) AgentSessionRepositories(context.Context, uint32, time.Time) ([]string, error) {
	return nil, nil
}

func (f *fakeAgentSessionReader) AgentSessionFactoryProjects(context.Context, uint32, time.Time) ([]string, error) {
	return nil, nil
}

func (f *fakeAgentSessionReader) AgentSessionTotals(context.Context, uint32, ...uuid.UUID) ([]storage.AgentSessionTotals, error) {
	return nil, nil
}

func (f *fakeAgentSessionReader) AgentSessionTimeline(context.Context, uint32, storage.AgentSessionTotals, storage.AgentTimelinePage) ([]storage.AgentTimelineStep, error) {
	return nil, nil
}

func (f *fakeAgentSessionReader) AgentSubagentCalls(context.Context, uint32, storage.AgentSessionTotals) (map[string]string, error) {
	return nil, nil
}

func (f *fakeAgentSessionReader) RunEvents(context.Context, uint32, string) ([]models.Log, error) {
	return nil, nil
}

func (f *fakeAgentSessionReader) ReadAgentRawLine(context.Context, uint32, uuid.UUID, string, uint32, uint64) (storage.AgentRawLine, bool, error) {
	return storage.AgentRawLine{}, false, nil
}

// The sort and the direction of the Agent sessions list: each value goes
// to the read as its order, and any other value gets 400 with no read.
func TestTheAgentSessionsListTakesASortAndADirection(t *testing.T) {
	const timeRange = `"from":"2026-10-01T00:00:00Z","to":"2026-10-04T00:00:00Z"`
	cases := []struct {
		name, body string
		status     int
		errorText  string
		order      storage.AgentSessionOrder
	}{
		{"no sort", `{` + timeRange + `}`, 200, "", storage.AgentSessionOrder{}},
		{"the last activity", `{` + timeRange + `,"sort":"last"}`, 200, "", storage.AgentSessionOrder{Sort: storage.AgentSortLast}},
		{"the start, ascending", `{` + timeRange + `,"sort":"start","direction":"asc"}`, 200, "",
			storage.AgentSessionOrder{Sort: storage.AgentSortStart, Ascending: true}},
		{"the tokens, descending", `{` + timeRange + `,"sort":"tokens","direction":"desc"}`, 200, "",
			storage.AgentSessionOrder{Sort: storage.AgentSortTokens}},
		{"the cost", `{` + timeRange + `,"sort":"cost"}`, 200, "", storage.AgentSessionOrder{Sort: storage.AgentSortCost}},
		{"the default sort, ascending", `{` + timeRange + `,"direction":"asc"}`, 200, "", storage.AgentSessionOrder{Ascending: true}},
		{"a sort that is not one", `{` + timeRange + `,"sort":"title"}`, 400, "sort must be last, start, tokens or cost", storage.AgentSessionOrder{}},
		{"a sort in capitals", `{` + timeRange + `,"sort":"Tokens"}`, 400, "sort must be last, start, tokens or cost", storage.AgentSessionOrder{}},
		{"a direction that is not one", `{` + timeRange + `,"direction":"up"}`, 400, "direction must be desc or asc", storage.AgentSessionOrder{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reader := &fakeAgentSessionReader{}
			app := fiber.New()
			handler.RegisterAgentQueryRoutes(app, handler.NewAgentSessionsHandler(reader))
			req := httptest.NewRequest("POST", "/api/7/agent-sessions", strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != c.status || !strings.Contains(string(body), c.errorText) {
				t.Fatalf("Expected %d with %q, got %d: %s", c.status, c.errorText, resp.StatusCode, body)
			}
			if c.status != 200 {
				if len(reader.filters) != 0 {
					t.Fatalf("Expected no read for a bad request, got %d", len(reader.filters))
				}
				return
			}
			if len(reader.filters) != 1 || reader.filters[0].Order != c.order {
				t.Fatalf("Expected one read with the order %+v, got %+v", c.order, reader.filters)
			}
		})
	}
}
