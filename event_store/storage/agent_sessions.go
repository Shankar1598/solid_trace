package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// agentSessionsMigration makes agent_sessions, the totals of each Agent
// session (spec, Storage): one row for each (Agent session, day of
// activity, file), which a read sums by Agent session id. The day is the
// UTC day on which the server stored the units, as in agent_session_files,
// so that Retention, Project deletion and the disk limit drop the rows of
// the same days as the raw table. Each column is a simple aggregate, so a
// part merge keeps one row for each key and a read gets the same totals
// with the same functions.
//
// A file is a path and its version. After a trim, the new version of a
// path repeats most of the old one, so a read counts the highest version
// of each path only.
//
// last_reply and last_cost_state are (end offset, value) tuples of the
// latest line in the file, with its model or its totalCostUSD. A line
// ends after offset 0, so the default tuple, at 0, is "none". The order
// of the lines in the file is the order in which Claude Code wrote them;
// the times of the lines are not always in order, and a cost-state line
// has no time.
//
// has_start is 1 when the file's unit at offset 0 is in the day: a file
// whose start is not kept any more is partial.
var agentSessionsMigration = clickHouseMigration{version: 11201, statements: []string{
	`CREATE TABLE IF NOT EXISTS agent_sessions (
		project_id UInt32,
		day Date,
		session_id UUID,
		path String,
		version UInt32,
		first_time SimpleAggregateFunction(min, DateTime64(6, 'UTC')),
		last_time SimpleAggregateFunction(max, DateTime64(6, 'UTC')),
		user_id SimpleAggregateFunction(any, UInt64),
		harness SimpleAggregateFunction(any, String),
		repository SimpleAggregateFunction(any, String),
		folder SimpleAggregateFunction(any, String),
		run_id SimpleAggregateFunction(any, String),
		factory_project SimpleAggregateFunction(any, String),
		parent_session_id SimpleAggregateFunction(max, String),
		input_tokens SimpleAggregateFunction(sum, UInt64),
		output_tokens SimpleAggregateFunction(sum, UInt64),
		cache_read_tokens SimpleAggregateFunction(sum, UInt64),
		cache_write_tokens SimpleAggregateFunction(sum, UInt64),
		price_cost_usd SimpleAggregateFunction(sum, Float64),
		replies SimpleAggregateFunction(sum, UInt64),
		unpriced_replies SimpleAggregateFunction(sum, UInt64),
		last_reply SimpleAggregateFunction(max, Tuple(UInt64, String)),
		last_cost_state SimpleAggregateFunction(max, Tuple(UInt64, Float64)),
		person_messages SimpleAggregateFunction(sum, UInt64),
		turns SimpleAggregateFunction(sum, UInt64),
		tool_calls SimpleAggregateFunction(sum, UInt64),
		has_start SimpleAggregateFunction(max, UInt8),
		flags SimpleAggregateFunction(groupBitOr, UInt8)
	) ENGINE = AggregatingMergeTree
	PARTITION BY (project_id, day)
	ORDER BY (project_id, session_id, day, path, version)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(agentFilesDedupWindow),
}}

// AgentSessionDay is what one range of the parse adds to the totals of
// one file of one Agent session on one day: a row of agent_sessions.
type AgentSessionDay struct {
	SessionID  uuid.UUID
	Day        time.Time
	Path       string
	Version    uint32
	First      time.Time
	Last       time.Time
	Session    AgentSession
	Input      uint64
	Output     uint64
	CacheRead  uint64
	CacheWrite uint64
	// PriceCost is the sum of the estimated cost of the replies whose
	// model has a price; UnpricedReplies counts the others, of Replies.
	PriceCost       float64
	Replies         uint64
	UnpricedReplies uint64
	// LastReplyEnd and LastCostStateEnd are the end offsets of the latest
	// reply and cost-state line in the file; 0 is none.
	LastReplyEnd     uint64
	LastModel        string
	LastCostStateEnd uint64
	TotalCostUSD     float64
	PersonMessages   uint64
	Turns            uint64
	ToolCalls        uint64
	HasStart         bool
	Flags            uint8
}

// IsSubagentFile is true for the transcript of a subagent, which Claude
// Code writes under subagents/ in the Agent session's folder.
func IsSubagentFile(path string) bool {
	return strings.HasPrefix(path, "subagents/") || strings.Contains(path, "/subagents/")
}

// isMainTranscript is true for a .jsonl file at the top of the Agent
// session's folder: the transcript of the person's conversation, with its
// model and its cost-state lines.
func isMainTranscript(path string) bool {
	return strings.HasSuffix(path, ".jsonl") && !strings.Contains(path, "/")
}

// insertAgentSessions inserts the rows of one range of the parse, in the
// order given, with the range's dedup token in ctx.
func (s *ClickHouseLogs) insertAgentSessions(ctx context.Context, projectID uint32, days []AgentSessionDay) error {
	block, err := s.conn.PrepareBatch(ctx, `INSERT INTO agent_sessions (project_id, day, session_id, path, version,
		first_time, last_time, user_id, harness, repository, folder, run_id, factory_project, parent_session_id,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, price_cost_usd, replies, unpriced_replies,
		last_reply, last_cost_state, person_messages, turns, tool_calls, has_start, flags)`)
	if err != nil {
		return err
	}
	defer block.Abort()
	for _, d := range days {
		var hasStart uint8
		if d.HasStart {
			hasStart = 1
		}
		if err := block.Append(projectID, LogDay(d.Day), d.SessionID, d.Path, d.Version, d.First.UTC(), d.Last.UTC(),
			uint64(d.Session.UserID), d.Session.Harness, d.Session.Repository, d.Session.Folder, d.Session.RunID,
			d.Session.Project, d.Session.ParentSessionID, d.Input, d.Output, d.CacheRead, d.CacheWrite, d.PriceCost,
			d.Replies, d.UnpricedReplies, []any{d.LastReplyEnd, d.LastModel}, []any{d.LastCostStateEnd, d.TotalCostUSD},
			d.PersonMessages, d.Turns, d.ToolCalls, hasStart, d.Flags); err != nil {
			return err
		}
	}
	return block.Send()
}

// AgentSessionTotals are the totals of one Agent session: its rows of
// agent_sessions summed over its days that are kept, of the highest
// version of each path.
type AgentSessionTotals struct {
	SessionID       uuid.UUID
	First, Last     time.Time
	UserID          int64
	Harness         string
	Repository      string
	Folder          string
	RunID           string
	Project         string
	ParentSessionID string
	// Model is the model of the latest reply in the main transcript, not
	// in a subagent's.
	Model                                string
	Input, Output, CacheRead, CacheWrite uint64
	// CostUSD is the estimated cost: the main transcript's last
	// cost-state line's totalCostUSD when it comes after the last reply
	// in the file, else the sum of the replies at list prices. Nil is "no
	// cost": a reply whose model has no price, or no reply.
	CostUSD                          *float64
	PersonMessages, Turns, ToolCalls uint64
	Subagents                        int
	// Flags are cut, partial and trimmed, in that order, when set.
	Flags []string
}

// agentFileTotals are the totals of one file of an Agent session.
type agentFileTotals struct {
	path                                 string
	version                              uint32
	first, last                          time.Time
	input, output, cacheRead, cacheWrite uint64
	priceCost                            float64
	replies, unpriced                    uint64
	replyEnd                             uint64
	model                                string
	costEnd                              uint64
	stateCost                            float64
	person, turns, toolCalls             uint64
	hasStart                             bool
}

// AgentSessionTotals reads the totals of a Project's Agent sessions,
// latest activity first: of the sessions given, or of all when none are.
func (s *ClickHouseLogs) AgentSessionTotals(ctx context.Context, projectID uint32, sessionIDs ...uuid.UUID) ([]AgentSessionTotals, error) {
	if len(sessionIDs) == 0 {
		return s.agentSessionTotals(ctx, projectID, "")
	}
	return s.agentSessionTotals(ctx, projectID, ` AND session_id IN ?`, sessionIDs)
}

// AgentSessionFilter selects the Agent sessions of a list. An Agent
// session is in the range [From, To] when it has activity in it: one of
// its files has a line in the range on one day. The other fields select
// nothing when they are zero.
type AgentSessionFilter struct {
	From, To time.Time
	// UserID is the person whose token uploaded the Agent session.
	UserID int64
	// Repository is the repository, as host/workspace/name.
	Repository string
	// Project is the factory Project of a Run's Agent session: it selects
	// the Agent sessions of Runs only.
	Project string
	// Run is AgentSessionsOfRuns, AgentSessionsOfNoRun, or "" for both.
	Run string
}

// The values of AgentSessionFilter.Run.
const (
	AgentSessionsOfRuns  = "run"
	AgentSessionsOfNoRun = "none"
)

// AgentSessionList reads the totals of the Agent sessions of a Project
// that the filter selects, latest activity first. The totals of each
// Agent session are of all its days that are kept, not only of the range.
func (s *ClickHouseLogs) AgentSessionList(ctx context.Context, projectID uint32, f AgentSessionFilter) ([]AgentSessionTotals, error) {
	// A line's time is at most one hour after the time when the server
	// stored it (normalise.Receipt), so a line in the range is on a day
	// from the day of From less one hour. The read gets only those days.
	selected := `SELECT session_id FROM agent_sessions WHERE project_id = ? AND day >= toDate(?)`
	args := []any{projectID, f.From.Add(-time.Hour)}
	if f.UserID != 0 {
		selected += ` AND user_id = ?`
		args = append(args, uint64(f.UserID))
	}
	if f.Repository != "" {
		selected += ` AND repository = ?`
		args = append(args, f.Repository)
	}
	if f.Project != "" {
		selected += ` AND factory_project = ? AND run_id != ''`
		args = append(args, f.Project)
	}
	switch f.Run {
	case AgentSessionsOfRuns:
		selected += ` AND run_id != ''`
	case AgentSessionsOfNoRun:
		selected += ` AND run_id = ''`
	}
	// Grouped by the key of the table, so that the times of a row are its
	// merged times, when its parts are not merged yet too.
	selected += ` GROUP BY session_id, day, path, version HAVING max(last_time) >= ? AND min(first_time) <= ?`
	args = append(args, f.From, f.To)
	return s.agentSessionTotals(ctx, projectID, ` AND session_id IN (`+selected+`)`, args...)
}

// AgentSessionRepositories are the repositories of the Agent sessions of
// a Project that the server stored from the day of from, in name order.
func (s *ClickHouseLogs) AgentSessionRepositories(ctx context.Context, projectID uint32, from time.Time) ([]string, error) {
	rows, err := s.conn.Query(ctx, `SELECT DISTINCT repository FROM agent_sessions
		WHERE project_id = ? AND day >= toDate(?) AND repository != '' ORDER BY repository`, projectID, from.Add(-time.Hour))
	if err != nil {
		return nil, fmt.Errorf("read agent session repositories: %w", err)
	}
	defer rows.Close()
	repositories := []string{}
	for rows.Next() {
		var repository string
		if err := rows.Scan(&repository); err != nil {
			return nil, fmt.Errorf("read agent session repositories: %w", err)
		}
		repositories = append(repositories, repository)
	}
	return repositories, rows.Err()
}

// agentSessionTotals reads the totals of the Agent sessions of a Project
// that the condition selects. The condition's args follow the Project.
func (s *ClickHouseLogs) agentSessionTotals(ctx context.Context, projectID uint32, condition string, conditionArgs ...any) ([]AgentSessionTotals, error) {
	query := `SELECT session_id, path, version, min(first_time), max(last_time), any(user_id), any(harness),
		any(repository), any(folder), any(run_id), any(factory_project), max(parent_session_id), sum(input_tokens),
		sum(output_tokens), sum(cache_read_tokens), sum(cache_write_tokens), sum(price_cost_usd), sum(replies),
		sum(unpriced_replies), tupleElement(max(last_reply), 1), tupleElement(max(last_reply), 2),
		tupleElement(max(last_cost_state), 1), tupleElement(max(last_cost_state), 2),
		sum(person_messages), sum(turns), sum(tool_calls), max(has_start), groupBitOr(flags)
		FROM agent_sessions WHERE project_id = ?` + condition + ` GROUP BY session_id, path, version ORDER BY session_id, path, version`
	rows, err := s.conn.Query(ctx, query, append([]any{projectID}, conditionArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("read agent sessions: %w", err)
	}
	defer rows.Close()
	var (
		all   []AgentSessionTotals
		files [][]agentFileTotals
		flags []uint8
	)
	for rows.Next() {
		var (
			t        AgentSessionTotals
			f        agentFileTotals
			userID   uint64
			hasStart uint8
			bits     uint8
		)
		if err := rows.Scan(&t.SessionID, &f.path, &f.version, &f.first, &f.last, &userID, &t.Harness, &t.Repository,
			&t.Folder, &t.RunID, &t.Project, &t.ParentSessionID, &f.input, &f.output, &f.cacheRead, &f.cacheWrite,
			&f.priceCost, &f.replies, &f.unpriced, &f.replyEnd, &f.model, &f.costEnd, &f.stateCost,
			&f.person, &f.turns, &f.toolCalls, &hasStart, &bits); err != nil {
			return nil, fmt.Errorf("read agent sessions: %w", err)
		}
		t.UserID, f.hasStart = int64(userID), hasStart != 0
		n := len(all) - 1
		if n < 0 || all[n].SessionID != t.SessionID {
			all, files, flags = append(all, t), append(files, nil), append(flags, 0)
			n++
		} else if all[n].ParentSessionID < t.ParentSessionID {
			all[n].ParentSessionID = t.ParentSessionID
		}
		files[n], flags[n] = append(files[n], f), flags[n]|bits
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read agent sessions: %w", err)
	}
	for i := range all {
		sumAgentFiles(&all[i], files[i], flags[i])
	}
	slices.SortStableFunc(all, func(a, b AgentSessionTotals) int {
		if c := b.Last.Compare(a.Last); c != 0 {
			return c
		}
		return strings.Compare(a.SessionID.String(), b.SessionID.String())
	})
	return all, nil
}

// sumAgentFiles sums the totals of an Agent session's files into t: the
// times of all the files, and the counts of the highest version of each
// path.
func sumAgentFiles(t *AgentSessionTotals, files []agentFileTotals, flags uint8) {
	highest := map[string]uint32{}
	subagents := map[string]bool{}
	for i, f := range files {
		if i == 0 || f.first.Before(t.First) {
			t.First = f.first
		}
		if f.last.After(t.Last) {
			t.Last = f.last
		}
		highest[f.path] = max(highest[f.path], f.version)
		if strings.HasSuffix(f.path, ".jsonl") && IsSubagentFile(f.path) {
			subagents[f.path] = true
		}
	}
	var (
		priceCost         float64
		replies, unpriced uint64
		partial           bool
		main              *agentFileTotals
	)
	for i := range files {
		f := &files[i]
		if f.version != highest[f.path] {
			continue
		}
		t.Input, t.Output, t.CacheRead, t.CacheWrite = t.Input+f.input, t.Output+f.output, t.CacheRead+f.cacheRead, t.CacheWrite+f.cacheWrite
		t.PersonMessages, t.Turns, t.ToolCalls = t.PersonMessages+f.person, t.Turns+f.turns, t.ToolCalls+f.toolCalls
		priceCost, replies, unpriced = priceCost+f.priceCost, replies+f.replies, unpriced+f.unpriced
		partial = partial || !f.hasStart
		if isMainTranscript(f.path) && (main == nil || f.last.After(main.last)) {
			main = f
		}
	}
	t.Subagents = len(subagents)
	switch {
	case main != nil && main.costEnd > main.replyEnd:
		t.CostUSD = &main.stateCost
	case unpriced == 0 && replies > 0:
		t.CostUSD = &priceCost
	}
	if main != nil {
		t.Model = main.model
	}
	t.Flags = agentSessionFlags(flags, partial)
}

func agentSessionFlags(flags uint8, partial bool) []string {
	names := []string{}
	if flags&AgentFlagCut != 0 {
		names = append(names, "cut")
	}
	if partial {
		names = append(names, "partial")
	}
	if flags&AgentFlagTrimmed != 0 {
		names = append(names, "trimmed")
	}
	return names
}
