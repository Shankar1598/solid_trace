package agentprocessing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

// The shape of a Claude Code transcript line that the parse reads. Other
// fields stay in the raw layer only.
type transcriptLine struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	ParentUUID       string          `json:"parentUuid"`
	Timestamp        string          `json:"timestamp"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Level            string          `json:"level"`
	Message          json.RawMessage `json:"message"`
	Content          json.RawMessage `json:"content"`
	Attachment       json.RawMessage `json:"attachment"`
	AiTitle          string          `json:"aiTitle"`
	// A queue-operation line.
	Operation   string `json:"operation"`
	Reason      string `json:"reason"`
	CommandUUID string `json:"commandUuid"`
}

type transcriptMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *messageUsage   `json:"usage"`
}

// messageUsage is the usage of one request. Claude Code repeats it on each
// line of the reply.
type messageUsage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	Speed                    string `json:"speed"`
	CacheCreation            *struct {
		Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (u messageUsage) tokens() tokens {
	t := tokens{input: u.InputTokens, output: u.OutputTokens, cacheRead: u.CacheReadInputTokens, speed: u.Speed}
	if c := u.CacheCreation; c != nil && c.Ephemeral5m+c.Ephemeral1h == u.CacheCreationInputTokens {
		t.cacheWrite5m, t.cacheWrite1h = c.Ephemeral5m, c.Ephemeral1h
	} else {
		t.cacheWrite5m = u.CacheCreationInputTokens
	}
	return t
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type attachment struct {
	Type       string          `json:"type"`
	Prompt     json.RawMessage `json:"prompt"`
	SourceUUID string          `json:"source_uuid"`
}

// The roles of user lines (spec, Storage): a message that a person sent,
// and a user line that Claude Code or an agent wrote, as hook feedback, a
// subagent's prompt or a summary of a compaction.
const (
	rolePerson    = "person"
	roleNotPerson = "not from a person"
)

// lineFacts is what the parse of a line gives to the other lines of the
// range.
type lineFacts struct {
	// An assistant line: its reply and the tokens of the reply's request.
	messageID, model string
	usage            *messageUsage
	// A queued_command attachment: the uuid that the sender gave it.
	queued string
	// A remove line of a message delivered in the middle of a turn: the
	// message's uuid and the time of its delivery.
	delivered   string
	deliveredAt json.RawMessage
}

type parsedLine struct {
	unit   int
	offset uint64
	length int
	object map[string]json.RawMessage
	facts  lineFacts
}

// agentLogIDs is the namespace of the Log ids that the parse makes: a
// line's id comes from where its raw line is, so a parse again makes the
// same Log.
var agentLogIDs = uuid.MustParse("5b0f3c1e-8f53-4d0e-9a52-6c1d2b7e4a90")

// parseUnits makes one Log for each line of the units of .jsonl files in a
// range of the parse, by Log ingest's normalise, at the Agent Project's
// Retention: the Logs of units[i] are logs[i]. A unit of another file has
// no Logs.
//
// Some rules read more than one line, so the parse reads the whole range
// before it makes a Log:
//   - the tokens and the cost of a reply go on the last line of its
//     message.id in the range, as each line of the reply repeats them;
//   - a message delivered in the middle of a turn has the time of its
//     remove line, which comes after its queued_command attachment.
//
// A rule cannot see a line in another range. The lines of one reply are
// written together, and a remove line follows its attachment at once, so
// that needs an upload cut between them at the end of a range.
func parseUnits(units []storage.AgentFileUnit, retentionDays int) [][]models.Log {
	var lines []parsedLine
	lastLineOfReply := map[string]int{}
	deliveredAt := map[string]json.RawMessage{}
	toolNames := map[string]map[string]string{}
	for i, unit := range units {
		if !strings.HasSuffix(unit.Path, ".jsonl") {
			continue
		}
		file := fmt.Sprintf("%s/%d/%s", unit.SessionID, unit.Version, unit.Path)
		if toolNames[file] == nil {
			toolNames[file] = map[string]string{}
		}
		subagent := isSubagentFile(unit.Path)
		data := unit.Bytes
		for start := 0; start < len(data); {
			end := bytes.IndexByte(data[start:], '\n')
			if end < 0 {
				end = len(data)
			} else {
				end += start
			}
			raw := data[start:end]
			line := parsedLine{unit: i, offset: unit.Offset + uint64(start), length: len(raw)}
			start = end + 1
			if len(bytes.TrimSpace(raw)) == 0 {
				continue
			}
			line.object, line.facts = lineObject(raw, toolNames[file], subagent)
			if f := line.facts; f.usage != nil && f.messageID != "" {
				lastLineOfReply[file+"/"+f.messageID] = len(lines)
			}
			if f := line.facts; f.delivered != "" {
				deliveredAt[unit.SessionID.String()+"/"+f.delivered] = f.deliveredAt
			}
			lines = append(lines, line)
		}
	}

	logs := make([][]models.Log, len(units))
	var previous json.RawMessage
	for i, line := range lines {
		unit := units[line.unit]
		receipt := normalise.Receipt{At: unit.StoredAt.UTC(), RetentionDays: retentionDays}
		if i == 0 || lines[i-1].unit != line.unit {
			// A line with no timestamp has the time of the line before it,
			// and the first lines the time that the server stored the unit.
			previous = json.RawMessage(strconv.Quote(receipt.At.Format(time.RFC3339Nano)))
		}
		object, f := line.object, line.facts
		file := fmt.Sprintf("%s/%d/%s", unit.SessionID, unit.Version, unit.Path)
		if f.usage != nil && (f.messageID == "" || lastLineOfReply[file+"/"+f.messageID] == i) {
			addTokens(object, f.model, f.usage.tokens())
		}
		if f.queued != "" {
			if at, ok := deliveredAt[unit.SessionID.String()+"/"+f.queued]; ok {
				object["time"] = at
			}
		}
		if t, ok := object["time"]; ok {
			previous = t
		} else {
			object["time"] = previous
		}
		keepOnStoredDay(object, receipt.At)
		addSource(object, unit, line.offset, line.length)
		log, outcome := normalise.JSON(object, receipt)
		if outcome == normalise.Dropped {
			// Not possible with a cut message and these few Attributes; the
			// raw layer has the line.
			continue
		}
		log.ID = uuid.NewSHA1(agentLogIDs, fmt.Appendf(nil, "%s/%d/%d/%s", unit.SessionID, unit.Version, line.offset, unit.Path))
		logs[line.unit] = append(logs[line.unit], log)
	}
	return logs
}

// isSubagentFile is true for the transcript of a subagent, which Claude
// Code writes under subagents/ in the Agent session's folder.
func isSubagentFile(path string) bool {
	return strings.HasPrefix(path, "subagents/") || strings.Contains(path, "/subagents/")
}

// addTokens adds the tokens of a request by kind, and its estimated cost
// when its model has a price.
func addTokens(object map[string]json.RawMessage, model string, t tokens) {
	number := func(n int64) json.RawMessage { return json.RawMessage(strconv.FormatInt(n, 10)) }
	object["agent.tokens.input"] = number(t.input)
	object["agent.tokens.output"] = number(t.output)
	object["agent.tokens.cache_read"] = number(t.cacheRead)
	object["agent.tokens.cache_write"] = number(t.cacheWrite5m + t.cacheWrite1h)
	if cost, ok := estimatedCost(model, t); ok {
		object["agent.cost_usd"] = json.RawMessage(strconv.FormatFloat(cost, 'g', -1, 64))
	}
}

// keepOnStoredDay keeps a Log on the UTC day on which the server stored
// its unit, which is the day of its raw row, so that Retention and the disk
// limit drop the raw record and its Logs together (spec, Storage). A line
// whose own time is on a different day, as in an old Agent session uploaded
// late, gets the stored time, and its own time stays in original.time, as
// normalise keeps a time that it does not use.
func keepOnStoredDay(object map[string]json.RawMessage, storedAt time.Time) {
	raw, ok := object["time"]
	if !ok {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return
	}
	if t.UTC().Format(time.DateOnly) == storedAt.UTC().Format(time.DateOnly) {
		return
	}
	delete(object, "time")
	object["original.time"] = raw
}

// lineObject is the Log of one line as a JSON object for normalise: the
// text of the line as its message, and what the line is under agent, with
// the facts that other lines need. It notes the tool of each tool call in
// toolNames, by its id, for the result. A user line of a subagent's file is
// not from a person.
func lineObject(raw []byte, toolNames map[string]string, subagent bool) (map[string]json.RawMessage, lineFacts) {
	object := map[string]json.RawMessage{}
	var facts lineFacts
	var line transcriptLine
	if err := json.Unmarshal(raw, &line); err != nil {
		object["message"] = quote(string(raw))
		object["agent.line"] = quote("unreadable")
		return object, facts
	}
	set := func(name, value string) {
		if value != "" {
			object[name] = quote(value)
		}
	}
	if line.Timestamp != "" {
		object["time"] = quote(line.Timestamp)
	}
	set("agent.uuid", line.UUID)
	set("agent.parent_uuid", line.ParentUUID)
	if line.IsSidechain {
		object["agent.sidechain"] = json.RawMessage("true")
	}
	kind := line.Type
	if line.Subtype != "" {
		kind += "/" + line.Subtype
	}
	var text strings.Builder
	role, level := "", ""
	switch line.Type {
	case "assistant", "user":
		role = line.Type
		if line.Type == "user" {
			role = rolePerson
			if line.IsMeta || line.IsCompactSummary || line.IsSidechain || subagent {
				role = roleNotPerson
			}
		}
		var msg transcriptMessage
		if s, ok := stringValue(line.Message); ok {
			text.WriteString(s)
		} else if json.Unmarshal(line.Message, &msg) == nil {
			set("agent.message_id", msg.ID)
			set("agent.model", msg.Model)
			// A synthetic message is Claude Code's own, not a request.
			if line.Type == "assistant" && msg.Usage != nil && msg.Model != "<synthetic>" {
				facts.messageID, facts.model, facts.usage = msg.ID, msg.Model, msg.Usage
			}
			s, blocks := contentOf(msg.Content)
			text.WriteString(s)
			for i, b := range blocks {
				if i > 0 || s != "" {
					text.WriteByte('\n')
				}
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
				case "thinking":
					text.WriteString(b.Thinking)
				case "tool_use", "server_tool_use":
					text.WriteString(b.Name)
					text.WriteByte(' ')
					text.Write(b.Input)
					set("agent.tool_name", b.Name)
					set("agent.tool_use_id", b.ID)
					toolNames[b.ID] = b.Name
				case "tool_result":
					role = "tool"
					rs, rb := contentOf(b.Content)
					text.WriteString(rs)
					for _, x := range rb {
						if x.Type == "text" {
							text.WriteString(x.Text)
						} else {
							text.WriteString("[" + x.Type + "]")
						}
					}
					set("agent.tool_use_id", b.ToolUseID)
					set("agent.tool_name", toolNames[b.ToolUseID])
					if b.IsError {
						level = "error"
					}
				default:
					text.WriteString("[" + b.Type + "]")
				}
				if i == 0 {
					kind = line.Type + "/" + b.Type
				}
			}
		}
	case "attachment":
		var a attachment
		if json.Unmarshal(line.Attachment, &a) == nil {
			kind = "attachment/" + a.Type
			if a.Type == "queued_command" {
				// A message that was queued while the agent worked: a
				// person's message, at the time of its delivery.
				s, _ := contentOf(a.Prompt)
				text.WriteString(s)
				role = rolePerson
				if line.IsSidechain || subagent {
					role = roleNotPerson
				}
				set("agent.source_uuid", a.SourceUUID)
				facts.queued = a.SourceUUID
			}
		}
	case "queue-operation":
		if line.Operation == "remove" && line.Reason == "absorbed_mid_turn" && line.CommandUUID != "" && line.Timestamp != "" {
			facts.delivered, facts.deliveredAt = line.CommandUUID, quote(line.Timestamp)
		}
	case "system":
		role = "system"
		s, _ := contentOf(line.Content)
		text.WriteString(s)
		if line.Level == "error" {
			level = "error"
		}
	case "ai-title":
		text.WriteString(line.AiTitle)
	}
	set("agent.line", kind)
	set("agent.role", role)
	set("level", level)
	object["message"] = quote(text.String())
	return object, facts
}

// addSource adds where the Log comes from: the repository as its service,
// the path as its file, the Agent session, the place of its raw line, and
// for a Run's Agent session the Run's Project, repository and folder.
func addSource(object map[string]json.RawMessage, unit storage.AgentFileUnit, offset uint64, length int) {
	object["service"] = quote(unit.Session.Repository)
	object["file"] = quote(unit.Path)
	object["agent.session_id"] = quote(unit.SessionID.String())
	object["agent.raw.version"] = json.RawMessage(strconv.FormatUint(uint64(unit.Version), 10))
	object["agent.raw.offset"] = json.RawMessage(strconv.FormatUint(offset, 10))
	object["agent.raw.bytes"] = json.RawMessage(strconv.Itoa(length))
	if unit.Session.RunID != "" {
		object["factory.project"] = quote(unit.Session.Project)
		object["repository"] = quote(unit.Session.Repository)
		object["folder"] = quote(unit.Session.Folder)
	}
}

// contentOf reads a content value: a string, or an array of blocks.
func contentOf(raw json.RawMessage) (string, []contentBlock) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", nil
	}
	switch raw[0] {
	case '"':
		s, _ := stringValue(raw)
		return s, nil
	case '[':
		var blocks []contentBlock
		_ = json.Unmarshal(raw, &blocks)
		return "", blocks
	}
	return "", nil
}

func stringValue(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	return s, json.Unmarshal(raw, &s) == nil
}

func quote(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
