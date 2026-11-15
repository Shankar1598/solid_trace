package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// agentTimelineLimit is the most steps that a page of the timeline
	// gives; agentTimelineDefault, the steps when the request sets no
	// limit.
	agentTimelineLimit   = 500
	agentTimelineDefault = 200
)

type agentSessionRequest struct {
	// Path is the subagent transcript whose timeline the page gives; ""
	// gives the main transcript, the .jsonl files that are not a
	// subagent's.
	Path   string `json:"path"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

type (
	agentSessionAnswer struct {
		AgentSession agentSessionRow `json:"agent_session"`
		// Subagents are the subagent transcripts of the Agent session,
		// each with the tool call that started it when a result names it.
		Subagents []agentSubagent `json:"subagents"`
		Steps     []agentStep     `json:"steps"`
		// NextCursor gives the page after this one; "" when this is the
		// last.
		NextCursor string `json:"next_cursor"`
	}
	agentSubagent struct {
		Path      string    `json:"path"`
		AgentID   string    `json:"agent_id"`
		ToolUseID string    `json:"tool_use_id"`
		First     time.Time `json:"first"`
		Last      time.Time `json:"last"`
	}
	agentStep struct {
		ID         string           `json:"id"`
		Time       time.Time        `json:"time"`
		Path       string           `json:"path"`
		Version    uint32           `json:"version"`
		Offset     uint64           `json:"offset"`
		Line       string           `json:"line"`
		Role       string           `json:"role"`
		Text       string           `json:"text"`
		ToolName   string           `json:"tool_name,omitempty"`
		ToolUseID  string           `json:"tool_use_id,omitempty"`
		SubagentID string           `json:"subagent_id,omitempty"`
		Model      string           `json:"model,omitempty"`
		Level      string           `json:"level,omitempty"`
		Tokens     *agentStepTokens `json:"tokens"`
		CostUSD    *float64         `json:"cost_usd"`
		Cut        bool             `json:"cut"`
	}
	agentStepTokens struct {
		Input      uint64 `json:"input"`
		Output     uint64 `json:"output"`
		CacheRead  uint64 `json:"cache_read"`
		CacheWrite uint64 `json:"cache_write"`
	}
)

// Show answers POST /api/:project_id/agent-sessions/:session_id: the
// header of one Agent session of the Agent Project, its subagents, and
// one page of the timeline of its main transcript or of one subagent's.
// 404 when the Project has no such Agent session or transcript.
func (h *AgentSessionsHandler) Show(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	sessionID, err := uuid.Parse(c.Params("session_id"))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "no such Agent session"})
	}
	var req agentSessionRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON"})
		}
	}
	if req.Limit < 0 || req.Limit > agentTimelineLimit {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be from 1 to 500"})
	}
	limit := req.Limit
	if limit == 0 {
		limit = agentTimelineDefault
	}
	var after *storage.AgentTimelineCursor
	if req.Cursor != "" {
		after = &storage.AgentTimelineCursor{}
		raw, err := base64.RawURLEncoding.DecodeString(req.Cursor)
		if err != nil || json.Unmarshal(raw, after) != nil {
			return c.Status(400).JSON(fiber.Map{"error": "the cursor is not one that a page gave"})
		}
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadAgentSession), agentSessionsTimeout)
	defer cancel()
	fail := func(what string, err error) error {
		logger.L.Error("Reading the Agent session failed", "project_id", projectID, "session_id", sessionID, "read", what, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	found, err := h.sessions.AgentSessionTotals(ctx, projectID, sessionID)
	if err != nil {
		return fail("totals", err)
	}
	if len(found) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "no such Agent session"})
	}
	session := found[0]

	var paths []string
	for _, f := range session.Files {
		if !strings.HasSuffix(f.Path, ".jsonl") {
			continue
		}
		if req.Path == "" && !storage.IsSubagentFile(f.Path) || req.Path == f.Path {
			paths = append(paths, f.Path)
		}
	}
	if req.Path != "" && len(paths) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "the Agent session has no such transcript"})
	}
	steps, err := h.sessions.AgentSessionTimeline(ctx, projectID, session, storage.AgentTimelinePage{Paths: paths, After: after, Limit: limit})
	if err != nil {
		return fail("timeline", err)
	}
	calls, err := h.sessions.AgentSubagentCalls(ctx, projectID, session)
	if err != nil {
		return fail("subagents", err)
	}

	answer := agentSessionAnswer{AgentSession: agentSessionRowOf(session), Subagents: []agentSubagent{}, Steps: []agentStep{}}
	for _, f := range session.Files {
		if !strings.HasSuffix(f.Path, ".jsonl") || !storage.IsSubagentFile(f.Path) {
			continue
		}
		id := subagentID(f.Path)
		answer.Subagents = append(answer.Subagents, agentSubagent{Path: f.Path, AgentID: id, ToolUseID: calls[id], First: f.First.UTC(), Last: f.Last.UTC()})
	}
	if len(steps) > limit {
		steps = steps[:limit]
		raw, _ := json.Marshal(steps[limit-1].CursorOf())
		answer.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	for _, s := range steps {
		step := agentStep{
			ID: s.ID.String(), Time: s.Time, Path: s.Path, Version: s.Version, Offset: s.Offset, Line: s.Line, Role: s.Role,
			Text: s.Text, ToolName: s.ToolName, ToolUseID: s.ToolUseID, SubagentID: s.SubagentID, Model: s.Model,
			Level: s.Level, CostUSD: s.CostUSD, Cut: s.Cut,
		}
		if t := s.Tokens; t != nil {
			step.Tokens = &agentStepTokens{Input: t.Input, Output: t.Output, CacheRead: t.CacheRead, CacheWrite: t.CacheWrite}
		}
		answer.Steps = append(answer.Steps, step)
	}
	return c.JSON(answer)
}

// subagentID is the agent id of a subagent's transcript, which Claude
// Code names subagents/agent-<id>.jsonl.
func subagentID(path string) string {
	name := path[strings.LastIndex(path, "/")+1:]
	return strings.TrimPrefix(strings.TrimSuffix(name, ".jsonl"), "agent-")
}
