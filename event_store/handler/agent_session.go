package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

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
	// agentSearchLimit is the most characters of a search inside an
	// Agent session.
	agentSearchLimit = 1000
)

type agentSessionRequest struct {
	// Path is the subagent transcript whose timeline the page gives; ""
	// gives the main transcript, the .jsonl files that are not a
	// subagent's.
	Path   string `json:"path"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
	// Search, if set, keeps the steps whose text holds it, with any case.
	// With no path, it looks in all the Agent session's transcripts, the
	// subagents' too.
	Search string `json:"search"`
	// Order is "oldest" (also when empty), the oldest step first, or
	// "newest", the newest step first: then the first page is the end of
	// the timeline, and a cursor gives the older steps before it.
	Order string `json:"order"`
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
		// Author is who said a person message of a Run's Agent session; nil
		// on any other step, and on each step of another Agent session.
		Author *storage.AgentAuthor `json:"author,omitempty"`
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
	if req.Order != "" && req.Order != "oldest" && req.Order != "newest" {
		return c.Status(400).JSON(fiber.Map{"error": "order must be oldest or newest"})
	}
	if utf8.RuneCountInString(req.Search) > agentSearchLimit {
		return c.Status(400).JSON(fiber.Map{"error": "a search must have at most 1000 characters"})
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
		all := req.Path == "" && (req.Search != "" || !storage.IsSubagentFile(f.Path))
		if all || req.Path == f.Path {
			paths = append(paths, f.Path)
		}
	}
	if req.Path != "" && len(paths) == 0 {
		return c.Status(404).JSON(fiber.Map{"error": "the Agent session has no such transcript"})
	}
	steps, err := h.sessions.AgentSessionTimeline(ctx, projectID, session, storage.AgentTimelinePage{Paths: paths, After: after, Limit: limit, Search: req.Search,
		Newest: req.Order == "newest"})
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
	var authors map[string]storage.AgentAuthor
	if session.RunID != "" {
		if authors, err = h.runAuthors(ctx, projectID, session); err != nil {
			return fail("authors", err)
		}
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
		if authors != nil && s.Role == "person" {
			author, ok := authors[s.ID.String()]
			if !ok {
				// A person message of a subagent's transcript is the
				// agent's own prompt.
				author = storage.AgentAuthor{Kind: storage.AuthorNotFromAPerson}
			}
			step.Author = &author
		}
		answer.Steps = append(answer.Steps, step)
	}
	return c.JSON(answer)
}

// agentPeopleLimit is the most person messages of one Agent session that
// the join of authors reads.
const agentPeopleLimit = 5000

// runAuthors joins the person messages of a Run's Agent session to the
// Run's message events (storage.AgentAuthors). It reads all the person
// messages of the main transcripts, not only the page's, because the
// fallback matches them in order. The join is at read time: an event can
// arrive after the parse.
func (h *AgentSessionsHandler) runAuthors(ctx context.Context, projectID uint32, session storage.AgentSessionTotals) (map[string]storage.AgentAuthor, error) {
	var paths []string
	for _, f := range session.Files {
		if strings.HasSuffix(f.Path, ".jsonl") && !storage.IsSubagentFile(f.Path) {
			paths = append(paths, f.Path)
		}
	}
	people, err := h.sessions.AgentSessionTimeline(ctx, projectID, session, storage.AgentTimelinePage{Paths: paths, Limit: agentPeopleLimit, Role: "person"})
	if err != nil {
		return nil, err
	}
	events, err := h.sessions.RunEvents(ctx, projectID, session.RunID)
	if err != nil {
		return nil, err
	}
	return storage.AgentAuthors(events, people), nil
}

// subagentID is the agent id of a subagent's transcript, which Claude
// Code names subagents/agent-<id>.jsonl.
func subagentID(path string) string {
	name := path[strings.LastIndex(path, "/")+1:]
	return strings.TrimPrefix(strings.TrimSuffix(name, ".jsonl"), "agent-")
}

type agentRawLineRequest struct {
	Path    string `json:"path"`
	Version uint32 `json:"version"`
	Offset  uint64 `json:"offset"`
}

type agentRawLineAnswer struct {
	// Line is the raw line, redacted as stored, without its newline.
	Line string `json:"line"`
	// Cut is true when the raw layer does not hold all of the line.
	Cut bool `json:"cut"`
}

// RawLine answers POST /api/:project_id/agent-sessions/:session_id/line,
// 'Show full' on a cut value: the raw line of a step, by its path, version
// and offset, from the Agent Project's raw layer. 404 when the Project has
// no such Agent session or the raw layer has no line there.
func (h *AgentSessionsHandler) RawLine(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	sessionID, err := uuid.Parse(c.Params("session_id"))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "no such Agent session"})
	}
	var req agentRawLineRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON"})
	}
	if req.Path == "" {
		return c.Status(400).JSON(fiber.Map{"error": "path is required"})
	}
	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadAgentSession), agentSessionsTimeout)
	defer cancel()
	line, found, err := h.sessions.ReadAgentRawLine(ctx, projectID, sessionID, req.Path, req.Version, req.Offset)
	if err != nil {
		logger.L.Error("Reading the raw line failed", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	if !found {
		return c.Status(404).JSON(fiber.Map{"error": "the Agent session has no such line"})
	}
	return c.JSON(agentRawLineAnswer{Line: line.Line, Cut: line.Cut})
}
