package ingest

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

	// CreateIssueWithFingerprint creates a new Issue and its first
	// fingerprint record atomically, including Issue number generation.
	CreateIssueWithFingerprint(projectID uint32, fingerprint, title, culprit, kind string) (issueID int64, fingerprintID int64, err error)

	// ReopenIssue transitions a resolved Issue back to open.
	ReopenIssue(issueID int64) error
}

// Issue status constants — these match the enum values stored by Console.
const (
	IssueStatusOpen     = 0
	IssueStatusResolved = 1
)

