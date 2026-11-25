package storage

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// reviewRecordsMigration makes review_records, the Review records that the
// parse finds in Agent sessions (ADR 0024): one row for each assistant line
// of a main transcript whose last solidtrace-pr-record block is a record.
// A row has the place of its line, as the line's Log has, what the Review
// records page lists of the record, and the record as the block has it,
// all of it: a Log cuts a string at 64 KiB. day is the UTC day on which the
// server stored the line's unit, as in agent_session_files, so that
// Retention, Project deletion and the disk limit drop a Review record with
// its Agent session. A parse again of the same line replaces its row.
// After the Agent session steps.
var reviewRecordsMigration = clickHouseMigration{version: 11205, statements: []string{
	`CREATE TABLE IF NOT EXISTS review_records (
		project_id UInt32,
		day Date,
		stored_at DateTime64(6, 'UTC'),
		session_id UUID,
		path String,
		version UInt32,
		offset UInt64,
		id UUID,
		pr String,
		round UInt32,
		title String,
		findings UInt32,
		fixed UInt32,
		missed UInt32,
		confirmations_not_given UInt32,
		record String CODEC(ZSTD(1))
	) ENGINE = ReplacingMergeTree
	PARTITION BY (project_id, day)
	ORDER BY (project_id, session_id, path, version, offset)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(agentFilesDedupWindow),
}}

// ReviewRecord is the Review record of one line of an Agent session's main
// transcript: a row of review_records.
type ReviewRecord struct {
	// ID is the id of the Log of its line.
	ID        uuid.UUID
	SessionID uuid.UUID
	Path      string
	Version   uint32
	Offset    uint64
	StoredAt  time.Time
	// PR is the record's pr: the PR's URL, local:<path> for a PR file in
	// local mode, or "" when no PR opened.
	PR    string
	Round uint32
	// Title is the title of the record's first session, "" for none.
	Title   string
	Summary ReviewSummary
	// Record is the JSON of the record as its block has it. A read of the
	// list leaves it empty.
	Record string
}

// ReviewSummary counts what the Review records page lists of a record.
type ReviewSummary struct {
	Findings uint32
	// Fixed are the findings whose outcome is fixed.
	Fixed uint32
	// Missed are the findings that the reviews missed: a person, an agent
	// or a test run found them.
	Missed uint32
	// NotGiven are the confirmations that no event shows the developer
	// gave.
	NotGiven uint32
}

// Key is the PR of the record, or the Agent session id of a record with no
// PR: the records of one key are the rounds of one PR.
func (r ReviewRecord) Key() string {
	if r.PR != "" {
		return r.PR
	}
	return r.SessionID.String()
}

// compareRounds orders two records of one key: the higher round is the
// later, then the later store, then the later line.
func compareRounds(a, b ReviewRecord) int {
	return cmp.Or(cmp.Compare(a.Round, b.Round), compareStores(a, b))
}

// compareStores orders two records by the time that the server stored
// them, then by the place of their line.
func compareStores(a, b ReviewRecord) int {
	return cmp.Or(a.StoredAt.Compare(b.StoredAt), cmp.Compare(a.SessionID.String(), b.SessionID.String()),
		cmp.Compare(a.Path, b.Path), cmp.Compare(a.Version, b.Version), cmp.Compare(a.Offset, b.Offset))
}

// ReviewRecordPR is the Review records of one key, newest first.
type ReviewRecordPR struct {
	Key     string
	Records []ReviewRecord
}

// ReviewRecordsByPR groups records by their key, each key's newest record
// first, and the keys by the time that the server stored their newest
// record, the latest first (compareStores). Of the records of one path of an Agent
// session, only those of its highest version count, as a read of the
// session reads only that version: after a trim, the new version repeats
// the lines of the old one.
func ReviewRecordsByPR(records []ReviewRecord) []ReviewRecordPR {
	type file struct {
		session uuid.UUID
		path    string
	}
	highest := map[file]uint32{}
	for _, r := range records {
		f := file{r.SessionID, r.Path}
		highest[f] = max(highest[f], r.Version)
	}
	index := map[string]int{}
	var prs []ReviewRecordPR
	for _, r := range records {
		if r.Version != highest[file{r.SessionID, r.Path}] {
			continue
		}
		i, ok := index[r.Key()]
		if !ok {
			i = len(prs)
			index[r.Key()] = i
			prs = append(prs, ReviewRecordPR{Key: r.Key()})
		}
		prs[i].Records = append(prs[i].Records, r)
	}
	for i := range prs {
		slices.SortFunc(prs[i].Records, func(a, b ReviewRecord) int { return compareRounds(b, a) })
	}
	slices.SortFunc(prs, func(a, b ReviewRecordPR) int { return compareStores(b.Records[0], a.Records[0]) })
	return prs
}

// insertReviewRecords inserts the records of one range of the parse, with
// the range's dedup token in ctx.
func (s *ClickHouseLogs) insertReviewRecords(ctx context.Context, projectID uint32, records []ReviewRecord) error {
	block, err := s.conn.PrepareBatch(ctx, `INSERT INTO review_records (project_id, day, stored_at, session_id, path,
		version, offset, id, pr, round, title, findings, fixed, missed, confirmations_not_given, record)`)
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, r := range records {
		stored := r.StoredAt.UTC()
		if err := block.Append(projectID, LogDay(stored), stored, r.SessionID, r.Path, r.Version, r.Offset, r.ID, r.PR,
			r.Round, r.Title, r.Summary.Findings, r.Summary.Fixed, r.Summary.Missed, r.Summary.NotGiven, r.Record); err != nil {
			return err
		}
	}
	return block.Send()
}

// ReviewRecords reads every Review record of a Project, each once, with no
// record JSON.
func (s *ClickHouseLogs) ReviewRecords(ctx context.Context, projectID uint32) ([]ReviewRecord, error) {
	rows, err := s.conn.Query(ctx, `SELECT id, session_id, path, version, offset, stored_at, pr, round, title,
		findings, fixed, missed, confirmations_not_given FROM review_records FINAL WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read review records: %w", err)
	}
	defer rows.Close()
	var records []ReviewRecord
	for rows.Next() {
		var r ReviewRecord
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Path, &r.Version, &r.Offset, &r.StoredAt, &r.PR, &r.Round, &r.Title,
			&r.Summary.Findings, &r.Summary.Fixed, &r.Summary.Missed, &r.Summary.NotGiven); err != nil {
			return nil, fmt.Errorf("read review records: %w", err)
		}
		r.StoredAt = r.StoredAt.UTC()
		records = append(records, r)
	}
	return records, rows.Err()
}

// ReviewRecordJSON reads the record JSON of a Project's Review record with
// id; false when the Project has none with id.
func (s *ClickHouseLogs) ReviewRecordJSON(ctx context.Context, projectID uint32, id uuid.UUID) (string, bool, error) {
	rows, err := s.conn.Query(ctx, `SELECT record FROM review_records WHERE project_id = ? AND id = ? LIMIT 1`, projectID, id)
	if err != nil {
		return "", false, fmt.Errorf("read review record: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", false, rows.Err()
	}
	var record string
	if err := rows.Scan(&record); err != nil {
		return "", false, fmt.Errorf("read review record: %w", err)
	}
	return record, true, nil
}
