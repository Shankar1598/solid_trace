package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// AgentTotals reads the totals of Agent sessions from agent_sessions
// (*storage.ClickHouseLogs).
type AgentTotals interface {
	AgentSessionTotals(ctx context.Context, projectID uint32, sessionIDs ...uuid.UUID) ([]storage.AgentSessionTotals, error)
}

// agentTotalsTimeout bounds the read of the totals call.
const agentTotalsTimeout = 30 * time.Second

// maxTotalsSessions is the most Agent session ids of one totals call.
const maxTotalsSessions = 100

// agentTotalsAnswer is the totals call's answer: the sums of the Agent
// sessions, and Complete, false while the parse has not inserted each
// unit stored of them.
type agentTotalsAnswer struct {
	agentSessionsTotals
	Complete bool `json:"complete"`
}

// AgentSessionTotals answers GET /agent-session-totals?agent_session_ids=
// <id>,<id>: the summed agent_sessions rows of the Agent sessions, as the
// Agent sessions list sums them. Only a token with sessions:read whose
// list has each id gets it, so the Dispatcher's child token for a Run
// reads only that Run's totals. An id with no upload adds nothing. The
// answer does not wait for the parse: complete is false while the parse
// has not caught up with each unit stored of the Agent sessions.
func (h *AgentHandler) AgentSessionTotals(c *fiber.Ctx) error {
	token := agentToken(c)
	if token.AgentSessionIDs == nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "The totals call needs a token with sessions:read for a list of Agent session ids, and this token has no list."})
	}
	text := c.Query("agent_session_ids")
	if text == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Send agent_session_ids: the Agent session ids, between commas."})
	}
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, one := range strings.Split(text, ",") {
		id, err := uuid.Parse(one)
		if err != nil || len(one) != 36 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("%q is not an Agent session id: send the UUID that the harness gives it.", one)})
		}
		if !limitHas(token.AgentSessionIDs, id) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": fmt.Sprintf("This token is limited to the Agent sessions of a Run, and %s is not one of them.", id)})
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > maxTotalsSessions {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("Send at most %d Agent session ids.", maxTotalsSessions)})
	}
	projectID, ok, err := h.agentProjectOf(c, token)
	if !ok {
		return err
	}
	fail := func(what string, err error) error {
		logger.L.Error("Failed to read the totals of Agent sessions", "project_id", projectID, "read", what, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The totals could not be read. Try again later."})
	}
	complete := true
	var stored []uuid.UUID
	for _, id := range ids {
		_, known, states, err := h.files.AgentSessionFiles(projectID, id)
		if err != nil {
			return fail("files", err)
		}
		if !known {
			continue
		}
		parsed, err := h.files.AgentFilesParsed(projectID, states)
		if err != nil {
			return fail("the parse's cursor", err)
		}
		complete = complete && parsed
		stored = append(stored, id)
	}
	answer := agentTotalsAnswer{Complete: complete}
	if len(stored) > 0 {
		ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadAgentSessions), agentTotalsTimeout)
		defer cancel()
		sessions, err := h.totals.AgentSessionTotals(ctx, projectID, stored...)
		if err != nil {
			return fail("totals", err)
		}
		answer.agentSessionsTotals = sumAgentSessions(sessions)
	}
	return c.JSON(answer)
}
