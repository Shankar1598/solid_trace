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
)

// readTag is a Log read's log_comment: its Project, its kind, and the run
// its statements share, so that ClickHouse's query_log can be read back by
// read, as the Overview's read times do.
type readTag struct {
	ProjectID uint32 `json:"project_id"`
	Read      string `json:"read"`
	Run       string `json:"run"`
}

// TagRead returns a context whose ClickHouse statements carry a log_comment
// naming projectID and the read's kind, with a run of their own. It's set
// where a read is issued, once: the run of a Query, histogram or Match
// monitor, a Log by id, Fields. Tagging again, as a Query found to return a
// table is, keeps the run and changes the kind.
//
// It sets the context's whole ClickHouse settings, which no tagged read sets
// otherwise. SQL queries aren't tagged: their Project's SQL user names the
// Project in query_log, and its profile allows no log_comment.
func TagRead(ctx context.Context, projectID uint32, kind string) context.Context {
	tag, ok := ctx.Value(readTagKey{}).(readTag)
	if !ok {
		run := make([]byte, 8)
		_, _ = rand.Read(run)
		tag.Run = hex.EncodeToString(run)
	}
	tag.ProjectID, tag.Read = projectID, kind
	comment, _ := json.Marshal(tag)
	ctx = context.WithValue(ctx, readTagKey{}, tag)
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"log_comment": string(comment)}))
}

type readTagKey struct{}
