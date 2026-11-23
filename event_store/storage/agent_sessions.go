package storage

import (
	"cmp"
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

// agentSessionTitlesMigration adds the title of each file (AgentTitle) as
// a (kind, order, text) tuple, so that a part merge keeps the title that
// wins. A row from before it has the default tuple, (0, 0, ”), which is
// "no title". An install that has agent_sessions gets the column at its
// next start; the titles of its old rows stay empty.
var agentSessionTitlesMigration = clickHouseMigration{version: 11202, statements: []string{
	`ALTER TABLE agent_sessions ADD COLUMN IF NOT EXISTS title SimpleAggregateFunction(max, Tuple(UInt8, UInt64, String))`,
}}

// The kinds of an AgentTitle: a higher kind wins over a lower one.
const (
	// AgentTitleFirstMessage is the start of the first person message.
	AgentTitleFirstMessage uint8 = 0
	// AgentTitleAI is the title that Claude Code makes (an ai-title line).
	AgentTitleAI uint8 = 1
	// AgentTitleCustom is the title that a person gives with /rename (a
	// custom-title line).
	AgentTitleCustom uint8 = 2
)

// AgentTitle is the title of an Agent session's file, as the lines of a
// range give it: the max, as a tuple, of the candidates of its lines. A
// custom-title wins over an ai-title, and an ai-title over the first
// person message. Of two lines of one kind, the order decides: for a
// custom-title or an ai-title it is the line's end offset, so that the
// last one wins; for a person message it is the max offset less the
// line's end offset, so that the first one wins. The zero AgentTitle is
// "no title".
type AgentTitle struct {
	Kind  uint8
	Order uint64
	Text  string
}

// After is true when t wins over u, as ClickHouse's max of the tuples
// decides.
func (t AgentTitle) After(u AgentTitle) bool {
	if t.Kind != u.Kind {
		return t.Kind > u.Kind
	}
	if t.Order != u.Order {
		return t.Order > u.Order
	}
	return t.Text > u.Text
}

// NoActivity is the last time of a transcript's row with no line that has
// a timestamp of its own (spec, Storage): the parse puts it in last_time,
// and the time that the server stored the units in first_time. The max of
// last_time never takes it over a real time, so the row is never in a
// range, and the stored time is never before the lines that were stored.
var NoActivity = time.Unix(0, 0).UTC()

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
	// Title is the title that the lines of the range give the file. Only
	// the main transcript has one.
	Title AgentTitle
}

// IsSubagentFile is true for the transcript of a subagent, which Claude
// Code writes under subagents/ in the Agent session's folder.
func IsSubagentFile(path string) bool {
	return strings.HasPrefix(path, "subagents/") || strings.Contains(path, "/subagents/")
}

// isTranscript is true for a .jsonl file: it has lines, and so it has the
// times of its lines.
func isTranscript(path string) bool {
	return strings.HasSuffix(path, ".jsonl")
}

// IsMainTranscript is true for a .jsonl file at the top of the Agent
// session's folder: the transcript of the person's conversation, with its
// model and its cost-state lines.
func IsMainTranscript(path string) bool {
	return isTranscript(path) && !strings.Contains(path, "/")
}

// insertAgentSessions inserts the rows of one range of the parse, in the
// order given, with the range's dedup token in ctx.
func (s *ClickHouseLogs) insertAgentSessions(ctx context.Context, projectID uint32, days []AgentSessionDay) error {
	block, err := s.conn.PrepareBatch(ctx, `INSERT INTO agent_sessions (project_id, day, session_id, path, version,
		first_time, last_time, user_id, harness, repository, folder, run_id, factory_project, parent_session_id,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, price_cost_usd, replies, unpriced_replies,
		last_reply, last_cost_state, person_messages, turns, tool_calls, has_start, flags, title)`)
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
			d.PersonMessages, d.Turns, d.ToolCalls, hasStart, d.Flags, []any{d.Title.Kind, d.Title.Order, d.Title.Text}); err != nil {
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
	Model string
	// Title is the title of the main transcript (AgentTitle), "" for none.
	Title                                string
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
	// FirstDay and LastDay are the first and the last day on which the
	// server stored a unit of the Agent session: its derived Logs are on
	// those days and the days between.
	FirstDay, LastDay time.Time
	// Files are the highest version of each path, in path order.
	Files []AgentSessionFile
}

// AgentSessionFile is the highest version of one path of an Agent
// session, with the times of its first and last lines.
type AgentSessionFile struct {
	Path        string
	Version     uint32
	First, Last time.Time
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
	firstDay, lastDay                    time.Time
	title                                string
}

// AgentSessionTotals reads the totals of a Project's Agent sessions,
// latest activity first: of the sessions given, or of all when none are.
func (s *ClickHouseLogs) AgentSessionTotals(ctx context.Context, projectID uint32, sessionIDs ...uuid.UUID) ([]AgentSessionTotals, error) {
	if len(sessionIDs) == 0 {
		return s.agentSessionTotals(ctx, projectID, AgentSessionOrder{}, "")
	}
	return s.agentSessionTotals(ctx, projectID, AgentSessionOrder{}, ` AND session_id IN ?`, sessionIDs)
}

// AgentSessionFilter selects the Agent sessions of a list. An Agent
// session is in the range [From, To] when it has activity in it: one of
// its transcripts has a line with a timestamp of its own in the range on
// one day. The other fields select
// nothing when they are zero.
type AgentSessionFilter struct {
	From, To time.Time
	// UserID is the person whose token uploaded the Agent session.
	UserID int64
	// Repository is the repository, as host/workspace/name.
	Repository string
	// Project is the factory Project of a Run's Agent session: it selects
	// the Agent sessions of Runs only, as only a token of a Run sends a
	// Project, with its run_id (the upload, handler/agent_files.go).
	Project string
	// Run is AgentSessionsOfRuns, AgentSessionsOfNoRun, or "" for both.
	Run string
	// Order is the order of the list. It selects nothing.
	Order AgentSessionOrder
}

// The values of AgentSessionFilter.Run.
const (
	AgentSessionsOfRuns  = "run"
	AgentSessionsOfNoRun = "none"
)

// AgentSessionList reads the totals of the Agent sessions of a Project
// that the filter selects, in the filter's order. The totals of each
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
		selected += ` AND factory_project = ?`
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
	// Only a transcript has the times of its lines: a side file has the
	// time when the server stored it, which is not activity. A transcript's
	// row with no timed line has NoActivity as its last time, so it is in no
	// range.
	selected += ` AND endsWith(path, '.jsonl') GROUP BY session_id, day, path, version HAVING max(last_time) >= ? AND min(first_time) <= ?`
	args = append(args, f.From, f.To)
	return s.agentSessionTotals(ctx, projectID, f.Order, ` AND session_id IN (`+selected+`)`, args...)
}

// AgentSessionRepositories are the repositories of the Agent sessions of
// a Project that the server stored from the day of from, in name order.
func (s *ClickHouseLogs) AgentSessionRepositories(ctx context.Context, projectID uint32, from time.Time) ([]string, error) {
	return s.agentSessionValues(ctx, "repository", projectID, from)
}

// AgentSessionFactoryProjects are the slugs of the Projects of the Runs
// whose Agent sessions the server stored in a Project from the day of
// from, in name order: the choices of the Project (Runs) filter.
func (s *ClickHouseLogs) AgentSessionFactoryProjects(ctx context.Context, projectID uint32, from time.Time) ([]string, error) {
	return s.agentSessionValues(ctx, "factory_project", projectID, from)
}

// agentSessionValues are the values of a column of the Agent sessions of
// a Project that the server stored from the day of from, but the empty one, in name
// order. column is one of this file's names, never a caller's text.
func (s *ClickHouseLogs) agentSessionValues(ctx context.Context, column string, projectID uint32, from time.Time) ([]string, error) {
	rows, err := s.conn.Query(ctx, `SELECT DISTINCT `+column+` FROM agent_sessions
		WHERE project_id = ? AND day >= toDate(?) AND `+column+` != '' ORDER BY `+column, projectID, from.Add(-time.Hour))
	if err != nil {
		return nil, fmt.Errorf("read agent session %s values: %w", column, err)
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("read agent session %s values: %w", column, err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// agentSessionTotals reads the totals of the Agent sessions of a Project
// that the condition selects, in the order. The condition's args follow
// the Project.
func (s *ClickHouseLogs) agentSessionTotals(ctx context.Context, projectID uint32, order AgentSessionOrder, condition string, conditionArgs ...any) ([]AgentSessionTotals, error) {
	query := `SELECT session_id, path, version, min(first_time), max(last_time), any(user_id), any(harness),
		any(repository), any(folder), any(run_id), any(factory_project), max(parent_session_id), sum(input_tokens),
		sum(output_tokens), sum(cache_read_tokens), sum(cache_write_tokens), sum(price_cost_usd), sum(replies),
		sum(unpriced_replies), tupleElement(max(last_reply), 1), tupleElement(max(last_reply), 2),
		tupleElement(max(last_cost_state), 1), tupleElement(max(last_cost_state), 2),
		sum(person_messages), sum(turns), sum(tool_calls), max(has_start), groupBitOr(flags), min(day), max(day),
		tupleElement(max(title), 3)
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
			&f.person, &f.turns, &f.toolCalls, &hasStart, &bits, &f.firstDay, &f.lastDay, &f.title); err != nil {
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
	order.sortSessions(all)
	return all, nil
}

// The sorts of a list of Agent sessions (AgentSessionOrder.Sort).
const (
	AgentSortLast   = "last"
	AgentSortStart  = "start"
	AgentSortTokens = "tokens"
	AgentSortCost   = "cost"
)

// IsAgentSort is true for each of the sorts of a list of Agent sessions.
func IsAgentSort(sort string) bool {
	return sort == AgentSortLast || sort == AgentSortStart || sort == AgentSortTokens || sort == AgentSortCost
}

// AgentSessionOrder is the order of a list of Agent sessions: by the last
// activity (Sort "" or AgentSortLast), the start, the tokens or the
// estimated cost; the latest or the largest first, unless Ascending. The
// zero value is the latest activity first.
type AgentSessionOrder struct {
	Sort      string
	Ascending bool
}

// sortSessions puts sessions in the order. This is the one place of the
// order of the Agent sessions list: a limit takes the first rows after
// it. Agent sessions that tie go by id, in both directions, so that a
// limit always gives the same rows. An Agent session with no cost comes
// after each with a cost, in both directions: no cost is not a cost of 0,
// and the cheapest Agent sessions must come first when the cost is
// ascending.
func (o AgentSessionOrder) sortSessions(sessions []AgentSessionTotals) {
	sign := -1
	if o.Ascending {
		sign = 1
	}
	slices.SortFunc(sessions, func(a, b AgentSessionTotals) int {
		var c int
		switch o.Sort {
		case AgentSortStart:
			c = sign * a.First.Compare(b.First)
		case AgentSortTokens:
			c = sign * cmp.Compare(a.Tokens(), b.Tokens())
		case AgentSortCost:
			switch {
			case a.CostUSD == nil && b.CostUSD == nil:
			case a.CostUSD == nil:
				c = 1
			case b.CostUSD == nil:
				c = -1
			default:
				c = sign * cmp.Compare(*a.CostUSD, *b.CostUSD)
			}
		default:
			c = sign * a.Last.Compare(b.Last)
		}
		if c != 0 {
			return c
		}
		return strings.Compare(a.SessionID.String(), b.SessionID.String())
	})
}

// Tokens are the tokens of each kind of the Agent session, added: the
// tokens of the Agent sessions page.
func (t AgentSessionTotals) Tokens() uint64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// sumAgentFiles sums the totals of an Agent session's files into t: the
// times of all the files, and the counts of the highest version of each
// path.
func sumAgentFiles(t *AgentSessionTotals, files []agentFileTotals, flags uint8) {
	highest := map[string]uint32{}
	subagents := map[string]bool{}
	// A file that is not .jsonl has no lines, so its times are the times
	// when the server stored it. A transcript whose lines have no time of
	// their own has no activity (NoActivity): its times are the first time
	// when the server stored it. The Agent session's times are those of its
	// transcripts with activity, unless it has none.
	active := make([]bool, len(files))
	for i := range files {
		f := &files[i]
		active[i] = isTranscript(f.path) && f.last.After(NoActivity)
		if !f.last.After(NoActivity) {
			f.last = f.first
		}
	}
	transcripts := slices.Contains(active, true)
	for i, f := range files {
		if !transcripts || active[i] {
			if t.First.IsZero() || f.first.Before(t.First) {
				t.First = f.first
			}
			if f.last.After(t.Last) {
				t.Last = f.last
			}
		}
		if i == 0 || f.firstDay.Before(t.FirstDay) {
			t.FirstDay = f.firstDay
		}
		if f.lastDay.After(t.LastDay) {
			t.LastDay = f.lastDay
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
		t.Files = append(t.Files, AgentSessionFile{Path: f.path, Version: f.version, First: f.first, Last: f.last})
		t.Input, t.Output, t.CacheRead, t.CacheWrite = t.Input+f.input, t.Output+f.output, t.CacheRead+f.cacheRead, t.CacheWrite+f.cacheWrite
		t.PersonMessages, t.Turns, t.ToolCalls = t.PersonMessages+f.person, t.Turns+f.turns, t.ToolCalls+f.toolCalls
		priceCost, replies, unpriced = priceCost+f.priceCost, replies+f.replies, unpriced+f.unpriced
		partial = partial || !f.hasStart
		if IsMainTranscript(f.path) && (main == nil || f.last.After(main.last)) {
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
		t.Model, t.Title = main.model, main.title
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
