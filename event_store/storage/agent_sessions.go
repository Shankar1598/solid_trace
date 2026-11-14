package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// agentSessionsMigration makes agent_sessions, the totals of each Agent
// session (spec, Storage): one row for each (Agent session, day of
// activity), which a read sums by Agent session id. The day is the UTC day
// on which the server stored the units, as in agent_session_files, so that
// Retention, Project deletion and the disk limit drop the rows of the same
// days as the raw table. Each column is a simple aggregate, so a part merge
// keeps one row for each key and a read gets the same totals with
// the same functions.
//
// last_reply and last_cost_state are (time, offset, value) tuples: the
// largest is the latest line, with its model or its totalCostUSD. The
// default tuple, at time 0, is "none".
//
// files are the files (version/path) that the day has units of, and
// file_starts those whose unit at offset 0 is in the day: a file whose
// start is not kept any more is partial.
var agentSessionsMigration = clickHouseMigration{version: 11201, statements: []string{
	`CREATE TABLE IF NOT EXISTS agent_sessions (
		project_id UInt32,
		day Date,
		session_id UUID,
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
		unpriced_replies SimpleAggregateFunction(sum, UInt64),
		last_reply SimpleAggregateFunction(max, Tuple(DateTime64(6, 'UTC'), UInt64, String)),
		last_cost_state SimpleAggregateFunction(max, Tuple(DateTime64(6, 'UTC'), UInt64, Float64)),
		person_messages SimpleAggregateFunction(sum, UInt64),
		turns SimpleAggregateFunction(sum, UInt64),
		tool_calls SimpleAggregateFunction(sum, UInt64),
		subagents SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
		files SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
		file_starts SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
		flags SimpleAggregateFunction(groupBitOr, UInt8)
	) ENGINE = AggregatingMergeTree
	PARTITION BY (project_id, day)
	ORDER BY (project_id, session_id, day)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(agentFilesDedupWindow),
}}

// AgentSessionDay is what one range of the parse adds to the totals of
// one Agent session on one day: a row of agent_sessions.
type AgentSessionDay struct {
	SessionID  uuid.UUID
	Day        time.Time
	First      time.Time
	Last       time.Time
	Session    AgentSession
	Input      uint64
	Output     uint64
	CacheRead  uint64
	CacheWrite uint64
	// PriceCost is the sum of the estimated cost of the replies whose
	// model has a price; UnpricedReplies counts the others.
	PriceCost       float64
	UnpricedReplies uint64
	// LastReply and LastCostState are the place of the latest reply and
	// cost-state line; a zero time is none.
	LastReply      AgentLinePlace
	LastModel      string
	LastCostState  AgentLinePlace
	TotalCostUSD   float64
	PersonMessages uint64
	Turns          uint64
	ToolCalls      uint64
	Subagents      []string
	Files          []string
	FileStarts     []string
	Flags          uint8
}

// AgentLinePlace orders the lines of an Agent session: by time, then by
// offset, so a line with no timestamp, which has the time of the line
// before it, comes after that line.
type AgentLinePlace struct {
	Time   time.Time
	Offset uint64
}

// AgentFileName is a file of an Agent session in files and file_starts.
func AgentFileName(version uint32, path string) string {
	return fmt.Sprintf("%d/%s", version, path)
}

// insertAgentSessions inserts the rows of one range of the parse, in the
// order given, with the range's dedup token in ctx.
func (s *ClickHouseLogs) insertAgentSessions(ctx context.Context, projectID uint32, days []AgentSessionDay) error {
	block, err := s.conn.PrepareBatch(ctx, `INSERT INTO agent_sessions (project_id, day, session_id, first_time,
		last_time, user_id, harness, repository, folder, run_id, factory_project, parent_session_id, input_tokens,
		output_tokens, cache_read_tokens, cache_write_tokens, price_cost_usd, unpriced_replies, last_reply,
		last_cost_state, person_messages, turns, tool_calls, subagents, files, file_starts, flags)`)
	if err != nil {
		return err
	}
	defer block.Abort()
	nonNil := func(a []string) []string {
		if a == nil {
			return []string{}
		}
		return a
	}
	for _, d := range days {
		lastReply := []any{placeTime(d.LastReply), d.LastReply.Offset, d.LastModel}
		lastCost := []any{placeTime(d.LastCostState), d.LastCostState.Offset, d.TotalCostUSD}
		if err := block.Append(projectID, LogDay(d.Day), d.SessionID, d.First.UTC(), d.Last.UTC(),
			uint64(d.Session.UserID), d.Session.Harness, d.Session.Repository, d.Session.Folder, d.Session.RunID,
			d.Session.Project, d.Session.ParentSessionID, d.Input, d.Output, d.CacheRead, d.CacheWrite, d.PriceCost,
			d.UnpricedReplies, lastReply, lastCost, d.PersonMessages, d.Turns, d.ToolCalls, nonNil(d.Subagents),
			nonNil(d.Files), nonNil(d.FileStarts), d.Flags); err != nil {
			return err
		}
	}
	return block.Send()
}

func placeTime(p AgentLinePlace) time.Time {
	if p.Time.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return p.Time.UTC()
}

// AgentSessionTotals are the totals of one Agent session: its rows of
// agent_sessions summed over its days that are kept.
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
	// Model is the model of the latest reply.
	Model                                string
	Input, Output, CacheRead, CacheWrite uint64
	// CostUSD is the estimated cost: the last cost-state line's
	// totalCostUSD when it is later than the last reply, else the sum of
	// the replies at list prices. Nil is "no cost": a reply whose model
	// has no price.
	CostUSD                          *float64
	PersonMessages, Turns, ToolCalls uint64
	Subagents                        int
	// Flags are cut, partial and trimmed, in that order, when set.
	Flags []string
}

// AgentSessionTotals reads the totals of a Project's Agent sessions,
// latest activity first: of the sessions given, or of all when none are.
func (s *ClickHouseLogs) AgentSessionTotals(ctx context.Context, projectID uint32, sessionIDs ...uuid.UUID) ([]AgentSessionTotals, error) {
	query := `SELECT session_id, min(first_time), max(last_time), any(user_id), any(harness), any(repository),
		any(folder), any(run_id), any(factory_project), max(parent_session_id), sum(input_tokens), sum(output_tokens),
		sum(cache_read_tokens), sum(cache_write_tokens), sum(price_cost_usd), sum(unpriced_replies),
		tupleElement(max(last_reply), 1), tupleElement(max(last_reply), 2), tupleElement(max(last_reply), 3),
		tupleElement(max(last_cost_state), 1), tupleElement(max(last_cost_state), 2),
		tupleElement(max(last_cost_state), 3),
		sum(person_messages), sum(turns), sum(tool_calls), length(groupUniqArrayArray(subagents)),
		groupUniqArrayArray(files), groupUniqArrayArray(file_starts), groupBitOr(flags)
		FROM agent_sessions WHERE project_id = ?`
	args := []any{projectID}
	if len(sessionIDs) > 0 {
		query += ` AND session_id IN ?`
		args = append(args, sessionIDs)
	}
	query += ` GROUP BY session_id ORDER BY max(last_time) DESC, session_id`
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read agent sessions: %w", err)
	}
	defer rows.Close()
	var all []AgentSessionTotals
	for rows.Next() {
		var (
			t                    AgentSessionTotals
			userID, unpriced     uint64
			priceCost, stateCost float64
			replyAt, costAt      time.Time
			replyOffset, costOff uint64
			subagents            uint64
			files, starts        []string
			flags                uint8
		)
		if err := rows.Scan(&t.SessionID, &t.First, &t.Last, &userID, &t.Harness, &t.Repository, &t.Folder, &t.RunID,
			&t.Project, &t.ParentSessionID, &t.Input, &t.Output, &t.CacheRead, &t.CacheWrite, &priceCost, &unpriced,
			&replyAt, &replyOffset, &t.Model, &costAt, &costOff, &stateCost,
			&t.PersonMessages, &t.Turns, &t.ToolCalls, &subagents, &files, &starts, &flags); err != nil {
			return nil, fmt.Errorf("read agent sessions: %w", err)
		}
		t.UserID, t.Subagents = int64(userID), int(subagents)
		hasCostState := costAt.Unix() > 0
		costLater := costAt.After(replyAt) || (costAt.Equal(replyAt) && costOff > replyOffset)
		switch {
		case hasCostState && costLater:
			t.CostUSD = &stateCost
		case unpriced == 0 && replyAt.Unix() > 0:
			t.CostUSD = &priceCost
		}
		t.Flags = agentSessionFlags(flags, partialFiles(files, starts))
		all = append(all, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read agent sessions: %w", err)
	}
	return all, nil
}

// partialFiles is true when the highest version of a path has no start
// kept: Retention dropped the day of its unit at offset 0.
func partialFiles(files, starts []string) bool {
	highest := map[string]uint64{}
	for _, f := range files {
		version, path, ok := strings.Cut(f, "/")
		v, err := strconv.ParseUint(version, 10, 32)
		if !ok || err != nil {
			continue
		}
		if v > highest[path] {
			highest[path] = v
		}
	}
	started := map[string]bool{}
	for _, f := range starts {
		started[f] = true
	}
	for path, v := range highest {
		if !started[AgentFileName(uint32(v), path)] {
			return true
		}
	}
	return false
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
