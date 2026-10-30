package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// The kinds of Log read, as the Overview's Health names them.
const (
	ReadNewestPage = "newest page"
	// ReadTable is a Query that returns a table: the Logs explorer's tables
	// and Charts.
	ReadTable     = "table"
	ReadMatch     = "Match monitor run"
	ReadHistogram = "histogram"
	ReadLogByID   = "Log by id"
	ReadFields    = "Fields"
	ReadSQLQuery  = "SQL query"
	// ReadTrace reads a trace across Projects, tagged with the first the
	// read was asked for.
	ReadTrace = "trace"
	// ReadTransactions reads a Project's Transactions by name, and
	// ReadTransaction one Transaction's breakdown.
	ReadTransactions = "transactions"
	ReadTransaction  = "transaction"
)

// readTag is a Log read's log_comment: its Project, its kind, the run its
// statements share, and whether it was forced, so that ClickHouse's
// query_log can be read back by read, as the Overview's read times do.
type readTag struct {
	ProjectID uint32 `json:"project_id"`
	Read      string `json:"read"`
	Run       string `json:"run"`
	Forced    bool   `json:"forced,omitempty"`
}

// The ClickHouse priority of tagged reads: a forced read's is lower, so
// that it waits while other Log reads run. ClickHouse weighs only the
// statements that have a priority, so every tagged read has one.
const (
	readPriority       = 1
	forcedReadPriority = 10
)

// TagRead returns a context whose ClickHouse statements carry a log_comment
// naming projectID and the read's kind, with a run of their own. It's set
// where a read is issued, once: the run of a Query, histogram or Match
// monitor, a Log by id, Fields. Tagging again, as a Query found to return a
// table is, keeps the run and changes the kind.
//
// It sets the context's whole ClickHouse settings, which no tagged read sets
// otherwise: the log_comment, and the read's priority. SQL queries aren't
// tagged: their Project's SQL user names the Project in query_log, and its
// profile allows no log_comment.
func TagRead(ctx context.Context, projectID uint32, kind string) context.Context {
	tag, _ := ctx.Value(readTagKey{}).(readTag)
	tag.ProjectID, tag.Read = projectID, kind
	return withTag(ctx, tag)
}

// ForceRead marks a tagged read as forced, run as written past the read
// budget: its statements, as tagged now and again, run at a lower ClickHouse
// priority than other Log reads, and its log_comment says forced.
func ForceRead(ctx context.Context) context.Context {
	tag, ok := ctx.Value(readTagKey{}).(readTag)
	if !ok {
		return ctx
	}
	tag.Forced = true
	return withTag(ctx, tag)
}

// withTag tags ctx's statements with tag, in a run of their own if it has
// none yet.
func withTag(ctx context.Context, tag readTag) context.Context {
	if tag.Run == "" {
		run := make([]byte, 8)
		_, _ = rand.Read(run)
		tag.Run = hex.EncodeToString(run)
	}
	comment, _ := json.Marshal(tag)
	priority := readPriority
	if tag.Forced {
		priority = forcedReadPriority
	}
	ctx = context.WithValue(ctx, readTagKey{}, tag)
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"log_comment": string(comment), "priority": priority}))
}

type readTagKey struct{}
