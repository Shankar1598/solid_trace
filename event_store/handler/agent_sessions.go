package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// AgentSessionReader reads the Agent sessions of an Agent Project.
// *storage.ClickHouseLogs is the production adapter.
type AgentSessionReader interface {
	AgentSessionList(ctx context.Context, projectID uint32, filter storage.AgentSessionFilter) ([]storage.AgentSessionTotals, error)
	AgentSessionRepositories(ctx context.Context, projectID uint32, from time.Time) ([]string, error)
	AgentSessionTotals(ctx context.Context, projectID uint32, sessionIDs ...uuid.UUID) ([]storage.AgentSessionTotals, error)
	AgentSessionTimeline(ctx context.Context, projectID uint32, session storage.AgentSessionTotals, page storage.AgentTimelinePage) ([]storage.AgentTimelineStep, error)
	AgentSubagentCalls(ctx context.Context, projectID uint32, session storage.AgentSessionTotals) (map[string]string, error)
}

// AgentSessionsHandler answers Console's read of the Agent sessions list.
// Console sends the Organization's Agent Project, and the person of the
// filter as the user id that EventStore stores.
type AgentSessionsHandler struct {
	sessions AgentSessionReader
}

func NewAgentSessionsHandler(sessions AgentSessionReader) *AgentSessionsHandler {
	return &AgentSessionsHandler{sessions: sessions}
}

// RegisterAgentQueryRoutes mounts the Query API's routes for the Agent
// sessions pages, on the loopback query listener as RegisterQueryRoutes'
// are.
func RegisterAgentQueryRoutes(app *fiber.App, h *AgentSessionsHandler) {
	app.Post("/api/:project_id/agent-sessions", h.List)
	app.Post("/api/:project_id/agent-sessions/:session_id", h.Show)
}

const (
	// agentSessionsTimeout bounds a read of the list.
	agentSessionsTimeout = 30 * time.Second
	// agentSessionsLimit is the most rows that the list gives, and the
	// rows that it gives when the request sets no limit.
	agentSessionsLimit = 500
)

type agentSessionsRequest struct {
	From       *time.Time `json:"from"`
	To         *time.Time `json:"to"`
	UserID     int64      `json:"user_id"`
	Repository string     `json:"repository"`
	Project    string     `json:"project"`
	Run        string     `json:"run"`
	Limit      int        `json:"limit"`
}

type (
	agentSessionsAnswer struct {
		AgentSessions []agentSessionRow `json:"agent_sessions"`
		// Totals are of all the Agent sessions that the filters select,
		// also when the limit gives fewer rows.
		Totals agentSessionsTotals `json:"totals"`
		// More is true when the filters select more Agent sessions than
		// the rows.
		More bool `json:"more"`
		// Repositories are the repositories of the Agent sessions stored
		// in the range, for the repository filter.
		Repositories []string `json:"repositories"`
	}
	agentSessionRow struct {
		ID              string    `json:"id"`
		First           time.Time `json:"first"`
		Last            time.Time `json:"last"`
		UserID          int64     `json:"user_id"`
		Harness         string    `json:"harness"`
		Repository      string    `json:"repository"`
		Folder          string    `json:"folder"`
		RunID           string    `json:"run_id"`
		Project         string    `json:"project"`
		ParentSessionID string    `json:"parent_session_id"`
		Model           string    `json:"model"`
		Input           uint64    `json:"input_tokens"`
		Output          uint64    `json:"output_tokens"`
		CacheRead       uint64    `json:"cache_read_tokens"`
		CacheWrite      uint64    `json:"cache_write_tokens"`
		// CostUSD is the estimated cost at list prices; null is "no cost".
		CostUSD        *float64 `json:"cost_usd"`
		PersonMessages uint64   `json:"person_messages"`
		Turns          uint64   `json:"turns"`
		ToolCalls      uint64   `json:"tool_calls"`
		Subagents      int      `json:"subagents"`
		Flags          []string `json:"flags"`
	}
	agentSessionsTotals struct {
		AgentSessions int    `json:"agent_sessions"`
		Input         uint64 `json:"input_tokens"`
		Output        uint64 `json:"output_tokens"`
		CacheRead     uint64 `json:"cache_read_tokens"`
		CacheWrite    uint64 `json:"cache_write_tokens"`
		// CostUSD is the sum of the costs; null when no Agent session has
		// one. NoCost counts the Agent sessions with no cost.
		CostUSD *float64 `json:"cost_usd"`
		NoCost  int      `json:"no_cost"`
	}
)

// List answers POST /api/:project_id/agent-sessions: the Agent sessions
// of the Agent Project that the filters select, latest activity first,
// at most limit rows, and the totals of all that the filters select.
func (h *AgentSessionsHandler) List(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	var req agentSessionsRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON, its times RFC 3339 times"})
	}
	if req.From == nil || req.To == nil || req.To.Before(*req.From) {
		return c.Status(400).JSON(fiber.Map{"error": "from and to must be times, from before to"})
	}
	if req.Run != "" && req.Run != storage.AgentSessionsOfRuns && req.Run != storage.AgentSessionsOfNoRun {
		return c.Status(400).JSON(fiber.Map{"error": `run must be "run", "none" or absent`})
	}
	if req.Limit < 0 || req.Limit > agentSessionsLimit {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be from 1 to 500"})
	}
	limit := req.Limit
	if limit == 0 {
		limit = agentSessionsLimit
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadAgentSessions), agentSessionsTimeout)
	defer cancel()
	sessions, err := h.sessions.AgentSessionList(ctx, projectID, storage.AgentSessionFilter{
		From: *req.From, To: *req.To, UserID: req.UserID, Repository: req.Repository, Project: req.Project, Run: req.Run,
	})
	if err != nil {
		logger.L.Error("Reading the Agent sessions failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	repositories, err := h.sessions.AgentSessionRepositories(ctx, projectID, *req.From)
	if err != nil {
		logger.L.Error("Reading the Agent sessions' repositories failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}

	answer := agentSessionsAnswer{AgentSessions: []agentSessionRow{}, Repositories: repositories, More: len(sessions) > limit}
	var cost float64
	for i, s := range sessions {
		t := &answer.Totals
		t.AgentSessions++
		t.Input, t.Output, t.CacheRead, t.CacheWrite = t.Input+s.Input, t.Output+s.Output, t.CacheRead+s.CacheRead, t.CacheWrite+s.CacheWrite
		if s.CostUSD == nil {
			t.NoCost++
		} else {
			cost += *s.CostUSD
			t.CostUSD = &cost
		}
		if i < limit {
			answer.AgentSessions = append(answer.AgentSessions, agentSessionRowOf(s))
		}
	}
	return c.JSON(answer)
}

func agentSessionRowOf(s storage.AgentSessionTotals) agentSessionRow {
	return agentSessionRow{
		ID: s.SessionID.String(), First: s.First.UTC(), Last: s.Last.UTC(), UserID: s.UserID, Harness: s.Harness,
		Repository: s.Repository, Folder: s.Folder, RunID: s.RunID, Project: s.Project, ParentSessionID: s.ParentSessionID,
		Model: s.Model, Input: s.Input, Output: s.Output, CacheRead: s.CacheRead, CacheWrite: s.CacheWrite,
		CostUSD: s.CostUSD, PersonMessages: s.PersonMessages, Turns: s.Turns, ToolCalls: s.ToolCalls,
		Subagents: s.Subagents, Flags: s.Flags,
	}
}
