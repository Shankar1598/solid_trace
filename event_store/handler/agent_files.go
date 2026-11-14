package handler

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// The caps on an upload of a unit of an Agent session's file.
const (
	// agentUnitLimit is the most bytes of one unit once decoded: for a
	// .jsonl file, whole lines up to the last newline.
	agentUnitLimit = 32 << 20
	// agentBodyLimit caps the body of an upload as sent: one unit and at
	// most 1 MiB of request overhead. It is also the ingest app's limit.
	agentBodyLimit = agentUnitLimit + 1<<20
	// agentPathLimit is the longest path of a file under the Agent session
	// root, in bytes.
	agentPathLimit = 1024
)

// AgentFiles stores the units of Agent sessions' files and answers what the
// server has of them (*storage.PebbleWriter).
type AgentFiles interface {
	WriteAgentFileUnit(projectID uint32, upload storage.AgentUpload) (storage.AgentFileState, bool, error)
	AgentSessionFiles(projectID uint32, sessionID uuid.UUID) (storage.AgentSession, bool, []storage.AgentFileState, error)
}

// agentSessionOf checks the Agent session id of the path against the
// token's limit, and finds the token's Agent Project. ok is false when it
// has answered.
func (h *AgentHandler) agentSessionOf(c *fiber.Ctx, token auth.Token) (projectID uint32, sessionID uuid.UUID, ok bool, err error) {
	text := c.Params("session_id")
	sessionID, parseErr := uuid.Parse(text)
	if parseErr != nil || len(text) != 36 {
		return 0, uuid.Nil, false, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("%q is not an Agent session id: send the UUID that the harness gives it.", text)})
	}
	if token.AgentSessionIDs != nil && !limitHas(token.AgentSessionIDs, sessionID) {
		return 0, uuid.Nil, false, c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": fmt.Sprintf("This token is limited to the Agent sessions of a Run, and %s is not one of them.", sessionID)})
	}
	projectID, on, err := h.settings.AgentProject(token.Organization.ID)
	if err != nil {
		logger.L.Error("Failed to find the Agent Project", "organization_id", token.Organization.ID, "error", err)
		return 0, uuid.Nil, false, c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent Project could not be read. Try again later."})
	}
	if !on {
		return 0, uuid.Nil, false, c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": fmt.Sprintf("Agent sessions are off in %s. An Admin turns them on in Console on Settings → Agent sessions.",
				token.Organization.Name)})
	}
	return projectID, sessionID, true, nil
}

func limitHas(ids []string, sessionID uuid.UUID) bool {
	for _, id := range ids {
		if strings.EqualFold(id, sessionID.String()) {
			return true
		}
	}
	return false
}

func anotherPersons(c *fiber.Ctx, sessionID uuid.UUID) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"error": fmt.Sprintf("Agent session %s was uploaded by another person. Only its person, or a token of its Run, adds to it.", sessionID)})
}

// AgentSessionFiles answers what the server has of an Agent session: who
// and where it is, and for each path its highest version with its end
// offset and hash. The solidtrace CLI asks before it uploads, and sends
// only what is new. An Agent session with no upload has no files.
func (h *AgentHandler) AgentSessionFiles(c *fiber.Ctx) error {
	token := agentToken(c)
	projectID, sessionID, ok, err := h.agentSessionOf(c, token)
	if !ok {
		return err
	}
	session, known, files, err := h.files.AgentSessionFiles(projectID, sessionID)
	if err != nil {
		logger.L.Error("Failed to read an Agent session's files", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent session could not be read. Try again later."})
	}
	answer := fiber.Map{"agent_session": nil, "files": []fiber.Map{}}
	if !known {
		return c.JSON(answer)
	}
	if token.AgentSessionIDs == nil && session.UserID != token.UserID {
		return anotherPersons(c, sessionID)
	}
	list := make([]fiber.Map, len(files))
	for i, f := range files {
		list[i] = fiber.Map{"path": f.Path, "version": f.Version, "end_offset": f.EndOffset, "content_hash": f.ContentHash}
	}
	answer["agent_session"] = fiber.Map{
		"id":         sessionID.String(),
		"person":     fiber.Map{"user_id": session.UserID},
		"harness":    session.Harness,
		"repository": session.Repository,
		"folder":     session.Folder,
		"run_id":     session.RunID,
		"project":    session.Project,
	}
	answer["files"] = list
	return c.JSON(answer)
}

// UploadAgentFile stores one unit of a file of an Agent session: the body
// is its bytes, from the offset in the query, of the path and version in
// the query. For a .jsonl file a unit is whole lines. The query also names
// the harness, the repository (host/workspace/name) and the folder inside
// it; a token limited to a Run's Agent sessions also names the Run (run_id)
// and its Project (project).
//
// A 200 means the bytes are stored, as for Log ingest; Agent file
// processing inserts them into ClickHouse about one time a second. A
// repeat of a unit stores nothing new. An upload that leaves a gap or
// overlaps the stored end offset gets 409 with that end offset.
//
// Before it is stored, each byte of the unit that a Redaction pattern
// matches becomes '*' (package redaction). So the stored unit has the
// length of the original, and the end offset is the client's.
func (h *AgentHandler) UploadAgentFile(c *fiber.Ctx) error {
	token := agentToken(c)
	projectID, sessionID, ok, err := h.agentSessionOf(c, token)
	if !ok {
		return err
	}
	unit, problem := agentUnitOf(c, token)
	if problem != "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": problem})
	}
	coding, err := contentCoding(c.Get(fiber.HeaderContentEncoding))
	if err != nil {
		return renderBodyError(c, err)
	}
	body, err := decodeBody(c.Request().Body(), coding)
	if err == nil && len(body) > agentUnitLimit {
		err = errBodyTooLarge
	}
	if err != nil {
		return renderBodyError(c, err)
	}
	if len(body) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "The body is empty: send the bytes of the unit."})
	}
	if strings.HasSuffix(unit.Path, ".jsonl") && body[len(body)-1] != '\n' {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "A unit of a .jsonl file is whole lines: send the bytes up to the last newline."})
	}
	// The one Redaction point: after the body is decoded and before any
	// byte is stored. A unit whose patterns cannot be read is not stored,
	// as a secret could stay in it; the client sends it again later.
	patterns, err := h.redaction.Current()
	if err != nil {
		logger.L.Error("Failed to read the Redaction patterns", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "The Redaction patterns could not be read, so nothing was stored. Try again later. " +
				"If this goes on, an Instance admin checks the extra patterns on Settings → Instance."})
	}
	patterns.Unit(body, strings.HasSuffix(unit.Path, ".jsonl"))
	unit.SessionID = sessionID
	unit.Bytes = body
	unit.StoredAt = h.clock.Now().UTC()

	state, stored, err := h.files.WriteAgentFileUnit(projectID, storage.AgentUpload{Unit: unit, AnyPerson: token.AgentSessionIDs != nil})
	var conflict *storage.AgentUploadConflict
	switch {
	case errors.As(err, &conflict):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": conflict.Reason, "path": conflict.State.Path, "version": conflict.State.Version,
			"end_offset": conflict.State.EndOffset,
		})
	case errors.Is(err, storage.ErrAgentSessionOfAnotherPerson):
		return anotherPersons(c, sessionID)
	case errors.Is(err, storage.ErrShuttingDown):
		c.Set(fiber.HeaderRetryAfter, retryAfterSeconds)
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "The server is shutting down. Try again in a few seconds."})
	case err != nil:
		logger.L.Error("Failed to store an Agent session's unit", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The unit could not be stored. Try again later."})
	}
	if stored {
		h.notifyFiles(projectID)
	}
	return c.JSON(fiber.Map{"path": state.Path, "version": state.Version, "end_offset": state.EndOffset, "stored": stored})
}

// agentUnitOf reads the unit's address and its Agent session's who and
// where from the query, or answers what is wrong with them.
func agentUnitOf(c *fiber.Ctx, token auth.Token) (storage.AgentFileUnit, string) {
	unit := storage.AgentFileUnit{
		Path:        c.Query("path"),
		ContentHash: c.Query("content_hash"),
		Session: storage.AgentSession{
			UserID:     token.UserID,
			Harness:    strings.TrimSpace(c.Query("harness")),
			Repository: strings.TrimSpace(c.Query("repository")),
			Folder:     strings.Trim(strings.TrimSpace(c.Query("folder")), "/"),
			RunID:      strings.TrimSpace(c.Query("run_id")),
			Project:    strings.TrimSpace(c.Query("project")),
		},
	}
	if problem := relativePathProblem("path", unit.Path); problem != "" {
		return unit, problem
	}
	version, err := strconv.ParseUint(c.Query("version"), 10, 32)
	if err != nil || version == 0 {
		return unit, "Send version, the version of the path: a whole number from 1."
	}
	unit.Version = uint32(version)
	if unit.Offset, err = strconv.ParseUint(c.Query("offset"), 10, 64); err != nil {
		return unit, "Send offset, where the unit starts in the file: a whole number from 0."
	}
	if unit.Session.Harness == "" {
		return unit, "Send harness, the name of the coding agent, as claude-code."
	}
	if !repositoryShape(unit.Session.Repository) {
		return unit, "Send repository, the matched remote, as host/workspace/name."
	}
	if unit.Session.Folder != "" {
		if problem := relativePathProblem("folder", unit.Session.Folder); problem != "" {
			return unit, problem
		}
	}
	limited := token.AgentSessionIDs != nil
	if limited && (unit.Session.RunID == "" || unit.Session.Project == "") {
		return unit, "A token of a Run sends run_id and project, the Run and its Project."
	}
	if !limited && (unit.Session.RunID != "" || unit.Session.Project != "") {
		return unit, "Only a token of a Run sends run_id and project."
	}
	return unit, ""
}

// relativePathProblem is what is wrong with a path under a root, or "": it
// must be relative, with no . or .. part, and in UTF-8.
func relativePathProblem(name, p string) string {
	switch {
	case p == "":
		return fmt.Sprintf("Send %s, a path relative to its root.", name)
	case len(p) > agentPathLimit:
		return fmt.Sprintf("The %s is longer than %d bytes.", name, agentPathLimit)
	case !utf8.ValidString(p) || strings.ContainsAny(p, "\\\x00"):
		return fmt.Sprintf("The %s must be UTF-8 with / between its parts.", name)
	case strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../"):
		return fmt.Sprintf("The %s %q must be relative to its root, with no . or .. part.", name, p)
	}
	return ""
}

// repositoryShape reports whether a repository is host/workspace/name, with
// more parts for a workspace in groups.
func repositoryShape(repository string) bool {
	parts := strings.Split(repository, "/")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}
