package handler

import (
	"bytes"
	"context"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// AgentEvidenceHandler answers Console's read of one Evidence file of an
// Agent session: a file that the agent put under evidence/ in the Agent
// session directory, as the Run page shows it. Console sends the
// Organization's Agent Project, and checks that the Run's report names the
// file.
type AgentEvidenceHandler struct {
	files AgentDownloads
}

func NewAgentEvidenceHandler(files AgentDownloads) *AgentEvidenceHandler {
	return &AgentEvidenceHandler{files: files}
}

// RegisterAgentEvidenceRoute mounts the read of one Evidence file on the
// loopback query listener.
func RegisterAgentEvidenceRoute(app *fiber.App, h *AgentEvidenceHandler) {
	app.Get("/api/:project_id/agent-sessions/:session_id/evidence", h.Show)
}

const (
	agentEvidenceTimeout = 30 * time.Second
	// agentEvidenceDir is the folder of the Agent session directory that
	// holds the Evidence files.
	agentEvidenceDir = "evidence/"
)

// Show answers GET /api/:project_id/agent-sessions/:session_id/evidence
// ?path=evidence/<name>: the stored bytes of the highest version of that
// path, with the media type of its name. 400 for a path that is not a
// plain path under evidence/; 404 when the Agent session or the file is
// not in the Project.
func (h *AgentEvidenceHandler) Show(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	sessionID, err := uuid.Parse(c.Params("session_id"))
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "No such Agent session"})
	}
	name := c.Query("path")
	if !evidencePath(name) {
		return c.Status(400).JSON(fiber.Map{"error": "path must be a file under evidence/"})
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadAgentSession), agentEvidenceTimeout)
	defer cancel()
	files, err := h.files.AgentDownloadFiles(ctx, projectID, sessionID)
	if err != nil {
		logger.L.Error("Reading an Evidence file failed", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	var file []storage.AgentDownloadFile
	for _, f := range files {
		if f.Path == name {
			file = []storage.AgentDownloadFile{f}
		}
	}
	if file == nil {
		return c.Status(404).JSON(fiber.Map{"error": "The Agent session has no such Evidence file"})
	}
	var body bytes.Buffer
	if err := h.files.ReadAgentDownload(ctx, projectID, sessionID, file, func(_ int, b []byte) error {
		_, err := body.Write(b)
		return err
	}); err != nil {
		logger.L.Error("Reading an Evidence file failed", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	c.Set(fiber.HeaderContentType, evidenceType(name))
	return c.Send(body.Bytes())
}

// evidencePath reports whether name is a clean relative path of a file
// under evidence/.
func evidencePath(name string) bool {
	return strings.HasPrefix(name, agentEvidenceDir) && len(name) > len(agentEvidenceDir) &&
		path.Clean(name) == name && !strings.Contains(name, "\x00")
}

// evidenceType is the media type of an Evidence file by its extension,
// application/octet-stream when it has none that is known.
func evidenceType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".txt", ".log":
		return "text/plain; charset=utf-8"
	}
	if kind := mime.TypeByExtension(path.Ext(name)); kind != "" {
		return kind
	}
	return "application/octet-stream"
}
