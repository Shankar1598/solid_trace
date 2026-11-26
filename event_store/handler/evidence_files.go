package handler

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type EvidenceFiles interface {
	Add(ctx context.Context, upload storage.EvidenceUpload) (storage.EvidenceFile, bool, error)
	EvidenceFile(ctx context.Context, id uuid.UUID) (storage.EvidenceFile, bool, error)
	OpenEvidenceFile(ctx context.Context, file storage.EvidenceFile, offset, length int64) (io.ReadCloser, error)
}

const evidenceCaptionLimit = 200

const evidenceTimeout = 10 * time.Minute

// UploadEvidenceFile stores one Evidence file of an Agent session: the body
// is its bytes, and the query can give a caption. The type is found from
// the first bytes: PNG, JPEG, WebP, GIF, MP4 or WebM, else 415. A file over
// 32 MiB gets 413. An Agent session that another person uploaded gets 403,
// as UploadAgentFile answers. The file can come before its Agent session.
//
// 201 answers the new file's id, its Console URL, its type and its size.
// The same bytes again to the same Agent session answer 200 with the first
// upload's row, and store nothing.
func (h *AgentHandler) UploadEvidenceFile(c *fiber.Ctx) error {
	token := agentToken(c)
	projectID, sessionID, ok, err := h.agentSessionOf(c, token)
	if !ok {
		return err
	}
	session, known, _, err := h.files.AgentSessionFiles(projectID, sessionID)
	if err != nil {
		logger.L.Error("Failed to read an Agent session", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent session could not be read. Try again later."})
	}
	if known && anotherPersonsSession(token, session) {
		return anotherPersons(c, sessionID)
	}
	caption := strings.TrimSpace(c.Query("caption"))
	if !utf8.ValidString(caption) || utf8.RuneCountInString(caption) > evidenceCaptionLimit {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": fmt.Sprintf("Send a caption of at most %d characters, in UTF-8.", evidenceCaptionLimit)})
	}
	body := c.Request().Body()
	if len(body) > storage.MaxEvidenceFileBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{
			"error": fmt.Sprintf("An Evidence file is at most %d MiB; this one has %d bytes.", storage.MaxEvidenceFileBytes>>20, len(body))})
	}
	if len(body) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "The body is empty: send the bytes of the file."})
	}
	contentType, known := storage.EvidenceContentType(body)
	if coding, err := contentCoding(c.Get(fiber.HeaderContentEncoding)); err != nil || coding != "" || !known {
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(fiber.Map{
			"error": "An Evidence file is a PNG, JPEG, WebP or GIF image, or an MP4 or WebM video, sent as its bytes with no Content-Encoding."})
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), evidenceTimeout)
	defer cancel()
	file, created, err := h.evidence.Add(ctx, storage.EvidenceUpload{
		OrganizationID: token.Organization.ID,
		AgentSessionID: sessionID,
		UserID:         token.UserID,
		Caption:        caption,
		ContentType:    contentType,
		Bytes:          body,
		At:             h.clock.Now(),
	})
	if err != nil {
		logger.L.Error("Failed to store an Evidence file", "organization_id", token.Organization.ID, "session_id", sessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Evidence file could not be stored. Try again later."})
	}
	consoleURL, err := h.settings.ConsoleURL()
	if err != nil {
		logger.L.Error("Failed to read the Console URL", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Console URL could not be read. Try again later."})
	}
	if consoleURL == "" {
		consoleURL = h.consoleURL
	}
	status := fiber.StatusOK
	if created {
		status = fiber.StatusCreated
	}
	return c.Status(status).JSON(fiber.Map{
		"id":           file.ID.String(),
		"url":          fmt.Sprintf("%s/%s/evidence/%s", consoleURL, token.Organization.Slug, file.ID),
		"content_type": file.ContentType,
		"byte_size":    file.ByteSize,
	})
}

// EvidenceFilesHandler serves Console's reads of Evidence files on the
// loopback query listener. Console checks that the person is a member of
// the file's Organization.
type EvidenceFilesHandler struct {
	files EvidenceFiles
}

func NewEvidenceFilesHandler(files EvidenceFiles) *EvidenceFilesHandler {
	return &EvidenceFilesHandler{files: files}
}

func RegisterEvidenceQueryRoutes(app *fiber.App, h *EvidenceFilesHandler) {
	app.Get("/evidence/:id", h.Show)
	app.Get("/evidence/:id/content", h.Content)
}

func (h *EvidenceFilesHandler) find(c *fiber.Ctx) (storage.EvidenceFile, bool, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return storage.EvidenceFile{}, false, nil
	}
	file, found, err := h.files.EvidenceFile(c.UserContext(), id)
	if err != nil {
		logger.L.Error("Failed to read an Evidence file", "id", id, "error", err)
	}
	return file, found, err
}

func answerNoEvidenceFile(c *fiber.Ctx, err error) error {
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal error"})
	}
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "No such Evidence file"})
}

func (h *EvidenceFilesHandler) Show(c *fiber.Ctx) error {
	file, found, err := h.find(c)
	if err != nil || !found {
		return answerNoEvidenceFile(c, err)
	}
	answer := fiber.Map{
		"id":                  file.ID.String(),
		"organization_id":     file.OrganizationID,
		"agent_session_id":    file.AgentSessionID.String(),
		"sha256":              file.SHA256,
		"caption":             nil,
		"content_type":        file.ContentType,
		"byte_size":           file.ByteSize,
		"uploaded_by_user_id": nil,
		"created_at":          file.CreatedAt,
		"deleted_at":          file.DeletedAt,
	}
	if file.Caption != "" {
		answer["caption"] = file.Caption
	}
	if file.UploadedByUserID != 0 {
		answer["uploaded_by_user_id"] = file.UploadedByUserID
	}
	return c.JSON(answer)
}

// Content answers GET /evidence/:id/content: the file's bytes, 200 for all
// of them, or 206 for the one range of a Range header. 410 when Evidence
// Retention deleted them.
func (h *EvidenceFilesHandler) Content(c *fiber.Ctx) error {
	file, found, err := h.find(c)
	if err != nil || !found {
		return answerNoEvidenceFile(c, err)
	}
	if file.DeletedAt != nil {
		return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": "Evidence Retention deleted this Evidence file", "deleted_at": file.DeletedAt})
	}
	offset, length, status := int64(0), file.ByteSize, fiber.StatusOK
	if header := c.Get(fiber.HeaderRange); header != "" {
		switch r := byteRange(header, file.ByteSize); r.kind {
		case outsideFile:
			c.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes */%d", file.ByteSize))
			return c.Status(fiber.StatusRequestedRangeNotSatisfiable).JSON(fiber.Map{"error": "The range is outside the file"})
		case partOfFile:
			offset, length, status = r.start, r.end-r.start+1, fiber.StatusPartialContent
			c.Set(fiber.HeaderContentRange, fmt.Sprintf("bytes %d-%d/%d", r.start, r.end, file.ByteSize))
		}
	}
	// The body streams after this returns, so the read's context ends when
	// the stream closes.
	ctx, cancel := context.WithTimeout(context.Background(), evidenceTimeout)
	reader, err := h.files.OpenEvidenceFile(ctx, file, offset, length)
	if err != nil {
		cancel()
		logger.L.Error("Failed to read an Evidence file's bytes", "id", file.ID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal error"})
	}
	c.Set(fiber.HeaderContentType, file.ContentType)
	c.Set(fiber.HeaderAcceptRanges, "bytes")
	return c.Status(status).SendStream(cancelOnClose{reader, cancel}, int(length))
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r cancelOnClose) Close() error {
	defer r.cancel()
	return r.ReadCloser.Close()
}

type rangeKind int

const (
	wholeFile rangeKind = iota
	partOfFile
	outsideFile
)

type fileRange struct {
	kind       rangeKind
	start, end int64
}

func byteRange(header string, size int64) fileRange {
	spec, found := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	first, last, dash := strings.Cut(spec, "-")
	if !found || !dash || strings.Contains(spec, ",") {
		return fileRange{kind: wholeFile}
	}
	if first == "" {
		suffix, err := strconv.ParseInt(last, 10, 64)
		if err != nil || suffix < 0 {
			return fileRange{kind: wholeFile}
		}
		if suffix == 0 {
			return fileRange{kind: outsideFile}
		}
		return fileRange{kind: partOfFile, start: max(size-suffix, 0), end: size - 1}
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 {
		return fileRange{kind: wholeFile}
	}
	end := size - 1
	if last != "" {
		if end, err = strconv.ParseInt(last, 10, 64); err != nil || end < start {
			return fileRange{kind: wholeFile}
		}
	}
	if start >= size {
		return fileRange{kind: outsideFile}
	}
	return fileRange{kind: partOfFile, start: start, end: min(end, size-1)}
}
