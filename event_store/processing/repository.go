package processing

import "github.com/solidtrace/event_store/models"

// IssueRepository is the seam between Event processing and Issue persistence.
// Event processing owns this interface; storage adapters satisfy it.
//
// The domain decisions (e.g. "resolved Issues reopen on new Events") live in
// Event processing, not the repository.
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

	// RecordSeenEvents adds a batch's counted Events to their Issues'
	// times_seen, first_seen_at and last_seen_at, from each Event's IssueID
	// and Timestamp. events are in UUID order, and throughUUID is the last
	// Event of the batch, counted or not.
	//
	// It must be idempotent against a replayed batch, which can hold more
	// Events than before: an Event at or before the throughUUID of an earlier
	// call for the Project is not counted again. The guard is committed with
	// the counts.
	RecordSeenEvents(projectID uint32, events []models.Event, throughUUID string) error
}

// Issue status constants — these match the enum values stored by Console.
const (
	IssueStatusOpen     = 0
	IssueStatusResolved = 1
)
