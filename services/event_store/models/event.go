package models

import "time"

type Event struct {
	ProjectID          uint32            `json:"project_id"`
	EventUUID          string            `json:"uuid"` // 32 hex chars (no dashes)
	Timestamp          time.Time         `json:"timestamp"`
	RawJSON            []byte            `json:"-"` // Original event payload
	Tags               map[string]string `json:"tags"`
	IssueFingerprintID int64             `json:"issue_fingerprint_id"`
	IssueID            int64             `json:"issue_id"`
	IsNewIssue         bool              `json:"is_new_issue"`
}
