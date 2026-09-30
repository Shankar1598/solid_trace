package ingest

import "github.com/solidtrace/event_store/models"

// IssueRepository is the seam between Event ingest and Issue persistence.
// Event ingest owns this interface; storage adapters satisfy it.
//
// The interface covers the three operations Event ingest needs when
type IssueRepository interface {
	// FindIssueByFingerprint looks up an existing Issue by its fingerprint
	// within a Project. Returns the issueID, fingerprintID, issue status,
	// and whether a match was found.
	//
	// This is a pure read — it must not mutate Issue state.
	FindIssueByFingerprint(projectID uint32, fingerprint string) (issueID int64, fingerprintID int64, issueStatus int, found bool, err error)

	// FindOrCreateIssue returns the Issue for a fingerprint, creating the
	// Issue and its first fingerprint atomically (with its Issue number) if
	// none exists. It must be safe against concurrent calls for the same new
	// fingerprint: exactly one reports created=true, and the rest get that
	// Issue back.
	FindOrCreateIssue(projectID uint32, fingerprint, title, culprit, kind string) (issueID int64, fingerprintID int64, issueStatus int, created bool, err error)

	// ReopenIssue transitions a resolved Issue back to open.
	ReopenIssue(issueID int64) error
}

// EventWriter is the seam between Event ingest and Event storage.
// storage.PebbleWriter is the production adapter.
type EventWriter interface {
	// WriteEvent durably stores the Event's raw payload and sets its
	// EventUUID. Once the writer is closed it returns an error that the
	// caller must not retry.
	WriteEvent(event *models.Event) error
}

// Issue status constants — these match the enum values stored by Console.
const (
	IssueStatusOpen     = 0
	IssueStatusResolved = 1
)
