package models

import "time"

type Event struct {
	ProjectID uint32
	EventUUID string // 32 hex chars (no dashes)
	Timestamp time.Time
	RawJSON   []byte // Original event payload
	Tags      map[string]string
}
