package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// ReviewRecordReader reads the Review records of an Agent Project.
// *storage.ClickHouseLogs is the production adapter.
type ReviewRecordReader interface {
	ReviewRecords(ctx context.Context, projectID uint32) ([]storage.ReviewRecord, error)
	ReviewRecordJSON(ctx context.Context, projectID uint32, id uuid.UUID) (string, bool, error)
}

// ReviewRecordsHandler answers Console's reads of the Review records that
// the parse found in the Agent sessions of an Agent Project (ADR 0024).
type ReviewRecordsHandler struct {
	records ReviewRecordReader
}

func NewReviewRecordsHandler(records ReviewRecordReader) *ReviewRecordsHandler {
	return &ReviewRecordsHandler{records: records}
}

// RegisterReviewRecordRoutes mounts the Query API's routes for the Review
// records pages, on the loopback query listener as the Agent sessions'
// are.
func RegisterReviewRecordRoutes(app *fiber.App, h *ReviewRecordsHandler) {
	app.Post("/api/:project_id/review-records", h.List)
	app.Post("/api/:project_id/review-records/record", h.Show)
	app.Post("/api/:project_id/review-records/sessions", h.OfSessions)
}

// reviewRecordsLimit is the most PRs that the list gives, and the PRs that
// it gives when the request sets no limit.
const reviewRecordsLimit = 500

type (
	// reviewRecordRow is a Review record with its place and its summary.
	reviewRecordRow struct {
		// Key is the PR, or the Agent session id of a record with no PR:
		// the records of one key are the rounds of one PR.
		Key       string    `json:"key"`
		ID        string    `json:"id"`
		SessionID string    `json:"session_id"`
		Path      string    `json:"path"`
		Version   uint32    `json:"version"`
		Offset    uint64    `json:"offset"`
		StoredAt  time.Time `json:"stored_at"`
		// PR is "" when no PR opened.
		PR    string `json:"pr"`
		Round uint32 `json:"round"`
		// Title is the title of the record's first session, "" for none.
		Title    string `json:"title"`
		Findings uint32 `json:"findings"`
		Fixed    uint32 `json:"fixed"`
		// Missed are the findings that a person, an agent or a test run
		// found, not a reviewer.
		Missed                uint32 `json:"missed"`
		ConfirmationsNotGiven uint32 `json:"confirmations_not_given"`
	}
	reviewRecordsAnswer struct {
		// ReviewRecords are the newest record of each PR, the PR stored
		// last first, each with how many records of the PR are older.
		ReviewRecords []listedReviewRecord `json:"review_records"`
		More          bool                 `json:"more"`
	}
	listedReviewRecord struct {
		reviewRecordRow
		Earlier int `json:"earlier"`
	}
	reviewRecordRequest struct {
		// Key gives the newest record of a PR, and ID one record; one of
		// them, not both.
		Key string `json:"key"`
		ID  string `json:"id"`
	}
	reviewRecordAnswer struct {
		ReviewRecord reviewRecordRow `json:"review_record"`
		// Record is the JSON of the record as its block has it.
		Record string `json:"record"`
		// Rounds are all the records of the PR, newest first, this one
		// too.
		Rounds []reviewRound `json:"rounds"`
	}
	reviewRound struct {
		ID        string    `json:"id"`
		SessionID string    `json:"session_id"`
		Path      string    `json:"path"`
		Version   uint32    `json:"version"`
		Offset    uint64    `json:"offset"`
		Round     uint32    `json:"round"`
		StoredAt  time.Time `json:"stored_at"`
	}
)

// List answers POST /api/:project_id/review-records: the newest Review
// record of each PR of the Agent Project, the PR whose newest record the
// server stored last first, at most limit PRs.
func (h *ReviewRecordsHandler) List(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	var req struct {
		Limit int `json:"limit"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON"})
		}
	}
	if req.Limit < 0 || req.Limit > reviewRecordsLimit {
		return c.Status(400).JSON(fiber.Map{"error": "limit must be from 1 to 500"})
	}
	limit := req.Limit
	if limit == 0 {
		limit = reviewRecordsLimit
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadReviewRecords), agentSessionsTimeout)
	defer cancel()
	records, err := h.records.ReviewRecords(ctx, projectID)
	if err != nil {
		logger.L.Error("Reading the Review records failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	prs := storage.ReviewRecordsByPR(records)
	answer := reviewRecordsAnswer{ReviewRecords: []listedReviewRecord{}, More: len(prs) > limit}
	for _, pr := range prs[:min(limit, len(prs))] {
		answer.ReviewRecords = append(answer.ReviewRecords, listedReviewRecord{reviewRecordRowOf(pr.Records[0]), len(pr.Records) - 1})
	}
	return c.JSON(answer)
}

// Show answers POST /api/:project_id/review-records/record: one Review
// record with its JSON, by its key (the newest record of the PR) or by its
// id, and all the rounds of its PR. 404 when the Agent Project has no such
// record.
func (h *ReviewRecordsHandler) Show(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	var req reviewRecordRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON"})
	}
	if (req.Key == "") == (req.ID == "") {
		return c.Status(400).JSON(fiber.Map{"error": "give key or id"})
	}
	var id uuid.UUID
	if req.ID != "" {
		if id, err = uuid.Parse(req.ID); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "no such Review record"})
		}
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadReviewRecords), agentSessionsTimeout)
	defer cancel()
	fail := func(err error) error {
		logger.L.Error("Reading the Review record failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	records, err := h.records.ReviewRecords(ctx, projectID)
	if err != nil {
		return fail(err)
	}
	pr, shown, found := findReviewRecord(storage.ReviewRecordsByPR(records), req.Key, id)
	if !found {
		return c.Status(404).JSON(fiber.Map{"error": "no such Review record"})
	}
	record, found, err := h.records.ReviewRecordJSON(ctx, projectID, shown.ID)
	if err != nil {
		return fail(err)
	}
	if !found {
		return c.Status(404).JSON(fiber.Map{"error": "no such Review record"})
	}
	answer := reviewRecordAnswer{ReviewRecord: reviewRecordRowOf(shown), Record: record, Rounds: []reviewRound{}}
	for _, r := range pr.Records {
		answer.Rounds = append(answer.Rounds, reviewRound{ID: r.ID.String(), SessionID: r.SessionID.String(), Path: r.Path,
			Version: r.Version, Offset: r.Offset, Round: r.Round, StoredAt: r.StoredAt.UTC()})
	}
	return c.JSON(answer)
}

// sessionReviewRecord is the Review record of one Agent session: enough
// for a link to its page.
type sessionReviewRecord struct {
	ID string `json:"id"`
	// Key is the PR, or the Agent session id of a record with no PR.
	Key   string `json:"key"`
	PR    string `json:"pr"`
	Round uint32 `json:"round"`
}

// OfSessions answers POST /api/:project_id/review-records/sessions: for
// each Agent session of session_ids (at most reviewRecordsLimit) that has
// a Review record, the record that the server stored last of its own, by
// the session's id. A session with none is not in the answer.
func (h *ReviewRecordsHandler) OfSessions(c *fiber.Ctx) error {
	projectID, err := parseProjectID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the Project id must be a number"})
	}
	var req struct {
		SessionIDs []string `json:"session_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "the body must be JSON"})
	}
	if len(req.SessionIDs) > reviewRecordsLimit {
		return c.Status(400).JSON(fiber.Map{"error": "give at most 500 session_ids"})
	}
	ids := make([]uuid.UUID, 0, len(req.SessionIDs))
	for _, s := range req.SessionIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "each session_id must be a UUID"})
		}
		ids = append(ids, id)
	}
	answer := map[string]sessionReviewRecord{}
	if len(ids) == 0 {
		return c.JSON(fiber.Map{"sessions": answer})
	}

	ctx, cancel := context.WithTimeout(storage.TagRead(c.UserContext(), projectID, storage.ReadReviewRecords), agentSessionsTimeout)
	defer cancel()
	records, err := h.records.ReviewRecords(ctx, projectID)
	if err != nil {
		logger.L.Error("Reading the Review records failed", "project_id", projectID, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Internal error"})
	}
	for id, r := range storage.SessionReviewRecords(storage.ReviewRecordsByPR(records), ids) {
		answer[id.String()] = sessionReviewRecord{ID: r.ID.String(), Key: r.Key(), PR: r.PR, Round: r.Round}
	}
	return c.JSON(fiber.Map{"sessions": answer})
}

// findReviewRecord finds the PR of key and its newest record, or with no
// key the record with id and its PR.
func findReviewRecord(prs []storage.ReviewRecordPR, key string, id uuid.UUID) (storage.ReviewRecordPR, storage.ReviewRecord, bool) {
	for _, pr := range prs {
		if key != "" && pr.Key == key {
			return pr, pr.Records[0], true
		}
		for _, r := range pr.Records {
			if key == "" && r.ID == id {
				return pr, r, true
			}
		}
	}
	return storage.ReviewRecordPR{}, storage.ReviewRecord{}, false
}

func reviewRecordRowOf(r storage.ReviewRecord) reviewRecordRow {
	return reviewRecordRow{
		Key: r.Key(), ID: r.ID.String(), SessionID: r.SessionID.String(), Path: r.Path, Version: r.Version, Offset: r.Offset,
		StoredAt: r.StoredAt.UTC(), PR: r.PR, Round: r.Round, Title: r.Title, Findings: r.Summary.Findings,
		Fixed: r.Summary.Fixed, Missed: r.Summary.Missed, ConfirmationsNotGiven: r.Summary.NotGiven,
	}
}
