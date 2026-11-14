package agentprocessing

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// sessionTotals is what one range of the parse adds to agent_sessions: a
// row for each (Agent session, UTC day on which the server stored the
// units). It reads each line's Log as the parse made it, so the totals
// count what the Logs show: the tokens of a reply once, and who sent each
// message.
type sessionTotals struct {
	rows map[sessionDayKey]*sessionDay
}

type sessionDayKey struct {
	session uuid.UUID
	day     time.Time
}

type sessionDay struct {
	storage.AgentSessionDay
	hasLine bool
	// storedFirst and storedLast are the times that the server stored the
	// units, for a day with no lines.
	storedFirst, storedLast time.Time
	subagents, files, starts map[string]bool
}

// newSessionTotals starts the totals of a range with what each unit gives
// with no lines read: its file, its start at offset 0 and its flags.
func newSessionTotals(units []storage.AgentFileUnit) *sessionTotals {
	t := &sessionTotals{rows: map[sessionDayKey]*sessionDay{}}
	for _, unit := range units {
		row := t.row(unit)
		file := storage.AgentFileName(unit.Version, unit.Path)
		row.files[file] = true
		if unit.Offset == 0 {
			row.starts[file] = true
		}
		row.Flags |= unit.Flags
		if strings.HasSuffix(unit.Path, ".jsonl") && isSubagentFile(unit.Path) {
			row.subagents[unit.Path] = true
		}
		stored := unit.StoredAt.UTC()
		if row.storedFirst.IsZero() || stored.Before(row.storedFirst) {
			row.storedFirst = stored
		}
		if stored.After(row.storedLast) {
			row.storedLast = stored
		}
	}
	return t
}

func (t *sessionTotals) row(unit storage.AgentFileUnit) *sessionDay {
	key := sessionDayKey{session: unit.SessionID, day: storage.LogDay(unit.StoredAt)}
	row, ok := t.rows[key]
	if !ok {
		row = &sessionDay{
			AgentSessionDay: storage.AgentSessionDay{SessionID: unit.SessionID, Day: key.day, Session: unit.Session},
			subagents:       map[string]bool{}, files: map[string]bool{}, starts: map[string]bool{},
		}
		t.rows[key] = row
	}
	return row
}

// addLine adds one line's Log, before its time is kept on the stored day,
// to its Agent session's row.
func (t *sessionTotals) addLine(unit storage.AgentFileUnit, offset uint64, object map[string]json.RawMessage, f lineFacts) {
	row := t.row(unit)
	at, ok := timeOf(object["time"])
	if !ok {
		at = unit.StoredAt.UTC()
	}
	if !row.hasLine || at.Before(row.First) {
		row.First = at
	}
	if !row.hasLine || at.After(row.Last) {
		row.Last = at
	}
	row.hasLine = true
	place := storage.AgentLinePlace{Time: at, Offset: offset}

	role, line := stringOf(object["agent.role"]), stringOf(object["agent.line"])
	if role == rolePerson {
		row.PersonMessages++
		// A message delivered in the middle of a turn is an attachment
		// line: it does not start a turn.
		if strings.HasPrefix(line, "user") {
			row.Turns++
		}
	}
	if line == "assistant/tool_use" || line == "assistant/server_tool_use" {
		row.ToolCalls++
	}
	if _, ok := object["agent.tokens.output"]; ok {
		row.Input += uintOf(object["agent.tokens.input"])
		row.Output += uintOf(object["agent.tokens.output"])
		row.CacheRead += uintOf(object["agent.tokens.cache_read"])
		row.CacheWrite += uintOf(object["agent.tokens.cache_write"])
		if cost, ok := object["agent.cost_usd"]; ok {
			var c float64
			_ = json.Unmarshal(cost, &c)
			row.PriceCost += c
		} else {
			row.UnpricedReplies++
		}
		if later(place, row.LastReply) {
			row.LastReply, row.LastModel = place, stringOf(object["agent.model"])
		}
	}
	if f.costState != nil && later(place, row.LastCostState) {
		row.LastCostState, row.TotalCostUSD = place, *f.costState
	}
}

// days are the rows of the range, in a fixed order, so that a range
// parsed again makes the same insert.
func (t *sessionTotals) days() []storage.AgentSessionDay {
	keys := make([]sessionDayKey, 0, len(t.rows))
	for key := range t.rows {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b sessionDayKey) int {
		if c := bytes.Compare(a.session[:], b.session[:]); c != 0 {
			return c
		}
		return a.day.Compare(b.day)
	})
	days := make([]storage.AgentSessionDay, len(keys))
	for i, key := range keys {
		row := t.rows[key]
		if !row.hasLine {
			row.First, row.Last = row.storedFirst, row.storedLast
		}
		row.Subagents, row.Files, row.FileStarts = sortedKeys(row.subagents), sortedKeys(row.files), sortedKeys(row.starts)
		days[i] = row.AgentSessionDay
	}
	return days
}

func later(a, b storage.AgentLinePlace) bool {
	if b.Time.IsZero() {
		return true
	}
	return a.Time.After(b.Time) || (a.Time.Equal(b.Time) && a.Offset > b.Offset)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func timeOf(raw json.RawMessage) (time.Time, bool) {
	s := stringOf(raw)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t.UTC(), err == nil
}

func stringOf(raw json.RawMessage) string {
	s, _ := stringValue(raw)
	return s
}

func uintOf(raw json.RawMessage) uint64 {
	var n int64
	if json.Unmarshal(raw, &n) != nil || n < 0 {
		return 0
	}
	return uint64(n)
}
