package agentprocessing

import (
	"bytes"
	"cmp"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/storage"
)

// sessionTotals is what one range of the parse adds to agent_sessions: a
// row for each (Agent session, UTC day on which the server stored the
// units, file). It reads each line's Log as the parse made it, so the
// totals count what the Logs show: the tokens of a reply once, and who
// sent each message.
type sessionTotals struct {
	rows map[sessionDayKey]*sessionDay
}

type sessionDayKey struct {
	session uuid.UUID
	day     time.Time
	path    string
	version uint32
}

type sessionDay struct {
	storage.AgentSessionDay
	// hasTime is true when a line of the row has a time of its own.
	hasTime bool
	// storedFirst and storedLast are the times that the server stored the
	// units, for a row with no such line.
	storedFirst, storedLast time.Time
}

// newSessionTotals starts the totals of a range with what each unit gives
// with no lines read: its start at offset 0, its flags and its parent.
func newSessionTotals(units []storage.AgentFileUnit) *sessionTotals {
	t := &sessionTotals{rows: map[sessionDayKey]*sessionDay{}}
	for _, unit := range units {
		row := t.row(unit)
		if unit.Offset == 0 {
			row.HasStart = true
		}
		row.Flags |= unit.Flags
		// An uploader can learn the parent after the first upload.
		row.Session.ParentSessionID = max(row.Session.ParentSessionID, unit.Session.ParentSessionID)
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
	key := sessionDayKey{session: unit.SessionID, day: storage.LogDay(unit.StoredAt), path: unit.Path, version: unit.Version}
	row, ok := t.rows[key]
	if !ok {
		row = &sessionDay{AgentSessionDay: storage.AgentSessionDay{
			SessionID: unit.SessionID, Day: key.day, Path: unit.Path, Version: unit.Version, Session: unit.Session,
		}}
		t.rows[key] = row
	}
	return row
}

// addLine adds one line's Log, before its time is kept on the stored day,
// to its file's row. timed is true when the line has a time of its own:
// only such a line is activity, and sets the row's first and last times.
// A line time that is not believable for the unit's receipt, as Log
// ingest decides, gives the stored time. The line ends at end, its offset
// after its last byte.
func (t *sessionTotals) addLine(unit storage.AgentFileUnit, end uint64, object map[string]json.RawMessage, timed bool, f lineFacts, receipt normalise.Receipt) {
	row := t.row(unit)
	if at, ok := timeOf(object["time"]); timed {
		if !ok || !receipt.InWindow(at) {
			at = receipt.At
		}
		if !row.hasTime || at.Before(row.First) {
			row.First = at
		}
		if !row.hasTime || at.After(row.Last) {
			row.Last = at
		}
		row.hasTime = true
	}

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
		row.Replies++
		if cost, ok := object["agent.cost_usd"]; ok {
			var c float64
			_ = json.Unmarshal(cost, &c)
			row.PriceCost += c
		} else {
			row.UnpricedReplies++
		}
		if end > row.LastReplyEnd {
			row.LastReplyEnd, row.LastModel = end, stringOf(object["agent.model"])
		}
	}
	if f.costState != nil && end > row.LastCostStateEnd {
		row.LastCostStateEnd, row.TotalCostUSD = end, *f.costState
	}
	if f.title != "" && storage.IsMainTranscript(unit.Path) {
		title := storage.AgentTitle{Kind: f.titleKind, Order: end, Text: f.title}
		if f.titleKind == storage.AgentTitleFirstMessage {
			title.Order = math.MaxUint64 - end
		}
		// Only a row with the start of the file knows each person message
		// before this one. A later range of an Agent session parsed before
		// titles, which has its first person message in an earlier range,
		// must not give a later message as the title.
		firstUnknown := f.titleKind == storage.AgentTitleFirstMessage && !row.HasStart
		if !firstUnknown && title.After(row.Title) {
			row.Title = title
		}
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
		if c := a.day.Compare(b.day); c != 0 {
			return c
		}
		if c := strings.Compare(a.path, b.path); c != 0 {
			return c
		}
		return cmp.Compare(a.version, b.version)
	})
	days := make([]storage.AgentSessionDay, len(keys))
	for i, key := range keys {
		row := t.rows[key]
		switch {
		case row.hasTime:
		case strings.HasSuffix(row.Path, ".jsonl"):
			// A transcript's lines with no time of their own are not
			// activity (storage.NoActivity).
			row.First, row.Last = row.storedFirst, storage.NoActivity
		default:
			// A side file has no lines: its times are the stored times.
			row.First, row.Last = row.storedFirst, row.storedLast
		}
		days[i] = row.AgentSessionDay
	}
	return days
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
