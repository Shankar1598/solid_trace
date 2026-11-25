package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// AgentTimelineStep is one step of an Agent session's timeline: the Log of
// one .jsonl line that has a role, such as a person message, an agent
// reply, a tool call or a tool's output.
type AgentTimelineStep struct {
	ID uuid.UUID
	// Time is the line's own time: the time that the Log keeps in
	// original.time when the server stored the line on another day.
	Time    time.Time
	Path    string
	Version uint32
	Offset  uint64
	// Line is the kind of the line, as assistant/tool_use.
	Line, Role string
	// Text is the Log's message, but for a tool call its input as JSON
	// only, without the tool name before it.
	Text                            string
	ToolName, ToolUseID, SubagentID string
	// Model and Effort are set on a reply only.
	Model, Effort, Level string
	// UUID is the uuid of the line, and SourceUUID the uuid that the
	// sender gave a message that the agent got in the middle of a turn (a
	// queued_command attachment). A Run's wrapper gives each message its
	// own uuid, so SourceUUID, or UUID if it is empty, names the message.
	UUID, SourceUUID string
	// Tokens are set on the last line of a reply only.
	Tokens  *AgentStepTokens
	CostUSD *float64
	// Cut is true when the Log's message or an Attribute was cut at the
	// Log limits: the raw line has the full text.
	Cut bool
}

// AgentStepTokens are the tokens of one request, by kind.
type AgentStepTokens struct {
	Input, Output, CacheRead, CacheWrite uint64
}

// AgentTimelineCursor is the place of a step in the timeline's order:
// time, then path, version and offset.
type AgentTimelineCursor struct {
	Time    time.Time `json:"t"`
	Path    string    `json:"p"`
	Version uint32    `json:"v"`
	Offset  uint64    `json:"o"`
}

// CursorOf is the place of the step, for the page after it.
func (s AgentTimelineStep) CursorOf() AgentTimelineCursor {
	return AgentTimelineCursor{Time: s.Time, Path: s.Path, Version: s.Version, Offset: s.Offset}
}

// AgentTimelinePage asks for one page of an Agent session's timeline.
type AgentTimelinePage struct {
	// Paths are the files whose steps the page gives, each at its version
	// in the Agent session's Files. No paths give no steps.
	Paths []string
	// After, if set, is the place of the last step of the page before.
	After *AgentTimelineCursor
	// Newest gives the steps in the reverse order, the newest first, so
	// that the first page is the end of the timeline and each page after
	// it is older.
	Newest bool
	Limit  int
	// Search, if set, keeps the steps whose text holds it, with any case.
	Search string
	// Role, if set, keeps the steps of that role only.
	Role string
}

// agentSessionLogs is the condition on the derived Logs of the Agent
// session's files given, at their highest versions, in the days that
// the server stored them. No paths select no Logs. Only a transcript has
// derived Logs, so a file of another kind, such as one named otel,
// selects none.
func agentSessionLogs(projectID uint32, session AgentSessionTotals, paths []string) string {
	c := ClickHouseColumns{}
	var files []string
	for _, f := range session.Files {
		if !isTranscript(f.Path) || !slices.Contains(paths, f.Path) {
			continue
		}
		files = append(files, fmt.Sprintf("(file = %s AND %s = %d)", chsql.String(f.Path), c.Attribute(NumberAttribute, "agent.raw.version"), f.Version))
	}
	if len(files) == 0 {
		files = []string{"0"}
	}
	return fmt.Sprintf("project_id = %d AND service = %s AND toDate(time) >= toDate(%s) AND toDate(time) <= toDate(%s) AND %s = %s AND (%s)",
		projectID, chsql.String(session.Repository), chsql.Time(session.FirstDay), chsql.Time(session.LastDay),
		c.Attribute(StringAttribute, "agent.session_id"), chsql.String(session.SessionID.String()), strings.Join(files, " OR "))
}

// AgentSessionTimeline reads one page of an Agent session's timeline from
// its derived Logs: the steps of the files given, in the order of their
// lines' own times, then path, version and offset (or in the reverse
// order, with page.Newest). So a message that the
// agent got in the middle of a turn shows at the time it got it, which
// the parse gives its Log. It reads one more step than the limit, so the
// caller knows if there is a page after it.
func (s *ClickHouseLogs) AgentSessionTimeline(ctx context.Context, projectID uint32, session AgentSessionTotals, page AgentTimelinePage) (steps []AgentTimelineStep, err error) {
	c := ClickHouseColumns{}
	str := func(key string) string { return "ifNull(" + c.Attribute(StringAttribute, key) + ", '')" }
	num := func(key string) string { return c.Attribute(NumberAttribute, key) }
	own := "ifNull(parseDateTime64BestEffortOrNull(" + c.Attribute(StringAttribute, "original.time") + ", 6, 'UTC'), time)"
	version, offset := "toUInt32(ifNull("+num("agent.raw.version")+", 0))", "toUInt64(ifNull("+num("agent.raw.offset")+", 0))"
	direction, past := "", ">"
	if page.Newest {
		direction, past = " DESC", "<"
	}
	order := own + direction + ", file" + direction + ", " + version + direction + ", " + offset + direction
	place := own + ", file, " + version + ", " + offset

	where := agentSessionLogs(projectID, session, page.Paths) + " AND " + str("agent.role") + " != ''"
	inner := ""
	if page.Role != "" {
		inner += " AND " + str("agent.role") + " = " + chsql.String(page.Role)
	}
	if page.Search != "" {
		inner += " AND positionCaseInsensitiveUTF8(message, " + chsql.String(page.Search) + ") > 0"
	}
	if page.After != nil {
		a := page.After
		inner += fmt.Sprintf(" AND (%s) %s (%s, %s, %d, %d)", place, past, chsql.Time(a.Time), chsql.String(a.Path), a.Version, a.Offset)
	}
	// The ids of the page first, so that the messages of the other steps
	// are not read.
	query := fmt.Sprintf(`SELECT id, %s, file, %s, %s, %s, %s, message, %s, %s, %s, %s, %s, ifNull(level, ''),
		%s, %s, %s, %s, %s, ifNull(%s, false), %s, %s
		FROM logs WHERE %s AND id IN (SELECT id FROM logs WHERE %s%s ORDER BY %s LIMIT %d)
		ORDER BY %s`,
		own, version, offset, str("agent.line"), str("agent.role"), str("agent.tool_name"), str("agent.tool_use_id"),
		str("agent.subagent_id"), str("agent.model"), str("agent.effort"),
		num("agent.tokens.input"), num("agent.tokens.output"), num("agent.tokens.cache_read"), num("agent.tokens.cache_write"),
		num("agent.cost_usd"), c.Attribute(BoolAttribute, "solidtrace.truncated"),
		str("agent.uuid"), str("agent.source_uuid"), where, where, inner, order, page.Limit+1, order)
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read the agent session timeline: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			step                                 AgentTimelineStep
			input, output, cacheRead, cacheWrite *float64
		)
		if err := rows.Scan(&step.ID, &step.Time, &step.Path, &step.Version, &step.Offset, &step.Line, &step.Role,
			&step.Text, &step.ToolName, &step.ToolUseID, &step.SubagentID, &step.Model, &step.Effort, &step.Level,
			&input, &output, &cacheRead, &cacheWrite, &step.CostUSD, &step.Cut, &step.UUID, &step.SourceUUID); err != nil {
			return nil, fmt.Errorf("read the agent session timeline: %w", err)
		}
		step.Time = step.Time.UTC()
		if strings.HasSuffix(step.Line, "tool_use") && step.ToolName != "" {
			step.Text = strings.TrimPrefix(step.Text, step.ToolName+" ")
		}
		if output != nil {
			step.Tokens = &AgentStepTokens{Input: uintOf(input), Output: uintOf(output), CacheRead: uintOf(cacheRead), CacheWrite: uintOf(cacheWrite)}
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

// Paths are the paths of all the Agent session's files.
func (s AgentSessionTotals) Paths() []string {
	paths := make([]string, 0, len(s.Files))
	for _, f := range s.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

func uintOf(f *float64) uint64 {
	if f == nil || *f < 0 {
		return 0
	}
	return uint64(*f)
}

// AgentSubagentCalls gives, for each subagent of an Agent session that a
// tool call started, the id of that tool call, by the subagent's agent
// id: from the result of the call, which names the subagent.
func (s *ClickHouseLogs) AgentSubagentCalls(ctx context.Context, projectID uint32, session AgentSessionTotals) (calls map[string]string, err error) {
	c := ClickHouseColumns{}
	subagent := c.Attribute(StringAttribute, "agent.subagent_id")
	query := fmt.Sprintf("SELECT %s, any(ifNull(%s, '')) FROM logs WHERE %s AND isNotNull(%s) GROUP BY 1",
		subagent, c.Attribute(StringAttribute, "agent.tool_use_id"), agentSessionLogs(projectID, session, session.Paths()), subagent)
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read the agent session's subagents: %w", err)
	}
	defer rows.Close()
	calls = map[string]string{}
	for rows.Next() {
		var id *string
		var toolUseID string
		if err := rows.Scan(&id, &toolUseID); err != nil {
			return nil, fmt.Errorf("read the agent session's subagents: %w", err)
		}
		if id != nil {
			calls[*id] = toolUseID
		}
	}
	return calls, rows.Err()
}
