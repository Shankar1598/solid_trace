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
	CustomTitle      string          `json:"customTitle"`
	Origin           *lineOrigin     `json:"origin"`
	// ToolUseResult is Claude Code's own record of a tool's result. For
	// the Agent tool it names the subagent that the call started.
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	// A queue-operation line.
	Operation   string `json:"operation"`
	Reason      string `json:"reason"`
	CommandUUID string `json:"commandUuid"`
	// A cost-state line: Claude Code's own total of the Agent session.
	TotalCostUSD *float64 `json:"totalCostUSD"`
	// Effort is the effort of an assistant line's reply, as low or high.
	// Claude Code writes it from version 2.1.296, next to perTurnEffort,
	// which the parse does not read.
	Effort string `json:"effort"`
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
	Type        string          `json:"type"`
	Prompt      json.RawMessage `json:"prompt"`
	SourceUUID  string          `json:"source_uuid"`
	CommandMode string          `json:"commandMode"`
	IsMeta      bool            `json:"isMeta"`
	Origin      *lineOrigin     `json:"origin"`
}

// lineOrigin is who gave Claude Code a message: "human" for a person, or
// another kind, as "task-notification" for the end of a background task or
// "peer" for a subagent's hand-back. Older versions of Claude Code do not
// write it.
type lineOrigin struct {
	Kind string `json:"kind"`
}

// fromAnotherOrigin is true for an origin that is not a person.
func fromAnotherOrigin(o *lineOrigin) bool {
	return o != nil && o.Kind != "" && o.Kind != "human"
}

// taskNotification is the start of the text of the notice that Claude Code
// gives the agent when a background task ends.
const taskNotification = "<task-notification>"

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
	// A cost-state line: its totalCostUSD.
	costState *float64
	// A line that can give the Agent session's title: its kind
	// (storage.AgentTitle) and its text, "" for none.
	titleKind uint8
	title     string
	// An assistant line of the main transcript that ends a PR's final
	// reply: its Review record, with no place yet.
	reviewRecord *storage.ReviewRecord
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
// no Logs. It reads the parse state that the range before left, and gives
// the state after this range.
//
// Some rules read more than one line, so the parse reads the whole range
// before it makes a Log:
//   - the tokens and the cost of a reply go on the last line of its
//     message.id in the range, as each line of the reply repeats them;
//   - a message delivered in the middle of a turn has the time of its
//     remove line, which comes after its queued_command attachment.
//
// Claude Code writes the lines of one reply up to some seconds apart, so a
// reply can continue in the next range. The state keeps the tokens that
// the last reply of each file has had, and a line of that reply in a later
// range gets only the tokens that its reply did not have yet: usually
// none, as each line repeats the same usage. So the sum of the tokens of a
// reply's Logs is always the usage of its last line.
//
// A remove line follows its attachment at once, so the mid-turn rule needs
// an upload cut between them at the end of a range, and then the message
// has the time of its attachment.
func parseUnits(units []storage.AgentFileUnit, retentionDays int, state parseState) ([][]models.Log, []storage.AgentSessionDay, []storage.ReviewRecord, parseState) {
	state = state.clone()
	totals := newSessionTotals(units)
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
	var records []storage.ReviewRecord
	var previous json.RawMessage
	for i, line := range lines {
		unit := units[line.unit]
		receipt := normalise.Receipt{At: unit.StoredAt.UTC(), RetentionDays: retentionDays}
		if i == 0 || lines[i-1].unit != line.unit {
			// A line with no timestamp has, for its Log, the time of the
			// line before it, and the first lines the time that the server
			// stored the unit. That time is not activity: the totals read
			// only the lines with a time of their own.
			previous = json.RawMessage(strconv.Quote(receipt.At.Format(time.RFC3339Nano)))
		}
		object, f := line.object, line.facts
		file := fmt.Sprintf("%s/%d/%s", unit.SessionID, unit.Version, unit.Path)
		if f.usage != nil && f.messageID == "" {
			t := f.usage.tokens()
			cost, hasCost := estimatedCost(f.model, t)
			addTokens(object, t, cost, hasCost)
		} else if f.usage != nil && lastLineOfReply[file+"/"+f.messageID] == i {
			state.addReply(object, file, f, unit.StoredAt)
		}
		if f.queued != "" {
			if at, ok := deliveredAt[unit.SessionID.String()+"/"+f.queued]; ok {
				object["time"] = at
			}
		}
		_, timed := object["time"]
		if timed {
			previous = object["time"]
		} else {
			object["time"] = previous
		}
		totals.addLine(unit, line.offset+uint64(line.length), object, timed, f, receipt)
		keepOnStoredDay(object, receipt.At)
		addSource(object, unit, line.offset, line.length)
		id := uuid.NewSHA1(agentLogIDs, fmt.Appendf(nil, "%s/%d/%d/%s", unit.SessionID, unit.Version, line.offset, unit.Path))
		if r := f.reviewRecord; r != nil {
			r.ID, r.SessionID, r.Path, r.Version, r.Offset, r.StoredAt = id, unit.SessionID, unit.Path, unit.Version, line.offset, unit.StoredAt.UTC()
			records = append(records, *r)
		}
		log, outcome := normalise.JSON(object, receipt)
		if outcome == normalise.Dropped {
			// Not possible with a cut message and these few Attributes; the
			// raw layer has the line.
			continue
		}
		log.ID = id
		logs[line.unit] = append(logs[line.unit], log)
	}
	state.dropOld()
	return logs, totals.days(), records, state
}

// isSubagentFile is true for the transcript of a subagent.
var isSubagentFile = storage.IsSubagentFile

// addTokens adds the tokens of a request by kind, and its estimated cost
// when its model has a price.
func addTokens(object map[string]json.RawMessage, t tokens, cost float64, hasCost bool) {
	number := func(n int64) json.RawMessage { return json.RawMessage(strconv.FormatInt(n, 10)) }
	object["agent.tokens.input"] = number(t.input)
	object["agent.tokens.output"] = number(t.output)
	object["agent.tokens.cache_read"] = number(t.cacheRead)
	object["agent.tokens.cache_write"] = number(t.cacheWrite5m + t.cacheWrite1h)
	if hasCost {
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
		if line.Type == "user" && fromAnotherOrigin(line.Origin) {
			role = roleNotPerson
		}
		var msg transcriptMessage
		if s, ok := stringValue(line.Message); ok {
			text.WriteString(s)
		} else if json.Unmarshal(line.Message, &msg) == nil {
			set("agent.message_id", msg.ID)
			set("agent.model", msg.Model)
			if line.Type == "assistant" {
				set("agent.effort", line.Effort)
			}
			// A synthetic message is Claude Code's own, not a request.
			if line.Type == "assistant" && msg.Usage != nil && msg.Model != "<synthetic>" {
				facts.messageID, facts.model, facts.usage = msg.ID, msg.Model, msg.Usage
			}
			s, blocks := contentOf(msg.Content)
			text.WriteString(s)
			var texts []string
			if role == rolePerson && strings.HasPrefix(s, taskNotification) {
				role = roleNotPerson
			}
			for i, b := range blocks {
				if i > 0 || s != "" {
					text.WriteByte('\n')
				}
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
					texts = append(texts, b.Text)
				case "thinking":
					// Claude Code can keep a thinking block with no text.
					if b.Thinking == "" {
						text.WriteString("[thinking]")
					}
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
					set("agent.subagent_id", subagentOf(line.ToolUseResult))
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
			// The skill's own text has an example block, but on a user line.
			if line.Type == "assistant" && !subagent && !line.IsSidechain {
				facts.reviewRecord = reviewRecordOf(texts)
			}
		}
	case "attachment":
		var a attachment
		if json.Unmarshal(line.Attachment, &a) == nil {
			kind = "attachment/" + a.Type
			if a.Type == "queued_command" {
				// A message that was queued while the agent worked: a
				// person's message, at the time of its delivery. Claude
				// Code also queues the notice of a background task's end
				// and a subagent's hand-back in this way.
				s, _ := contentOf(a.Prompt)
				text.WriteString(s)
				role = rolePerson
				if line.IsSidechain || subagent || a.IsMeta || a.CommandMode == "task-notification" ||
					fromAnotherOrigin(a.Origin) || fromAnotherOrigin(line.Origin) || strings.HasPrefix(s, taskNotification) {
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
	case "cost-state":
		facts.costState = line.TotalCostUSD
	case "system":
		role = "system"
		s, _ := contentOf(line.Content)
		text.WriteString(s)
		if line.Level == "error" {
			level = "error"
		}
	case "ai-title":
		text.WriteString(line.AiTitle)
		facts.titleKind, facts.title = storage.AgentTitleAI, oneLine(line.AiTitle, maxTitleRunes)
	case "custom-title":
		text.WriteString(line.CustomTitle)
		facts.titleKind, facts.title = storage.AgentTitleCustom, oneLine(line.CustomTitle, maxTitleRunes)
	}
	if role == rolePerson && isCommandOutput(text.String()) {
		// Claude Code writes the output of a slash command or a shell
		// command as a user line: the output is Claude Code's, not the
		// person's.
		role = roleNotPerson
	}
	if role == rolePerson {
		facts.titleKind, facts.title = storage.AgentTitleFirstMessage, personTitle(text.String())
	}
	// A line with no text, as Claude Code's own records, has its kind as
	// its message, so that it is not an empty Log.
	if text.Len() == 0 && role == "" {
		text.WriteString("[" + kind + "]")
	}
	if level == "" {
		level = "info"
	}
	set("agent.line", kind)
	set("agent.role", role)
	set("level", level)
	object["message"] = quote(text.String())
	return object, facts
}

// maxTitleRunes is the most characters of a title that Claude Code or a
// person gives, and maxFirstMessageRunes of a title from the first person
// message: a short start of it.
const (
	maxTitleRunes        = 200
	maxFirstMessageRunes = 80
)

// personTitle is the title that a person message gives: its start, as the
// Agent session page shows the message, on one line. The tags that Claude
// Code adds to a slash command go, and the command and its arguments stay.
// A shell command (`! ls`) is "!" and the command. The output of a local
// command is Claude Code's, so it gives "".
func personTitle(text string) string {
	if command, ok := tagText(text, "command-name"); ok {
		args, _ := tagText(text, "command-args")
		return oneLine(command+" "+args, maxFirstMessageRunes)
	}
	if command, ok := tagText(text, "bash-input"); ok {
		return oneLine("! "+command, maxFirstMessageRunes)
	}
	if isCommandOutput(text) {
		return ""
	}
	text = strings.NewReplacer("<system-reminder>", "", "</system-reminder>", "").Replace(text)
	return oneLine(text, maxFirstMessageRunes)
}

// isCommandOutput is true for the output of a slash command, which Claude
// Code writes in <local-command-stdout> or <local-command-stderr>, and of a
// shell command (`! ls`), in <bash-stdout> and <bash-stderr>.
func isCommandOutput(text string) bool {
	for _, name := range []string{"local-command-stdout", "local-command-stderr", "bash-stdout", "bash-stderr"} {
		if _, ok := tagText(text, name); ok {
			return true
		}
	}
	return false
}

// tagText is the text in the first <name>…</name> of text, trimmed.
func tagText(text, name string) (string, bool) {
	_, after, ok := strings.Cut(text, "<"+name+">")
	if !ok {
		return "", false
	}
	inside, _, ok := strings.Cut(after, "</"+name+">")
	return strings.TrimSpace(inside), ok
}

// oneLine is text with each run of white space as one space, cut to at
// most limit characters with "…" at the end.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
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

// subagentOf is the agent id of the subagent that a tool's result names,
// "" when it names none. The subagent's transcript is
// subagents/agent-<id>.jsonl.
func subagentOf(result json.RawMessage) string {
	var r struct {
		AgentID string `json:"agentId"`
	}
	if len(result) == 0 || result[0] != '{' || json.Unmarshal(result, &r) != nil {
		return ""
	}
	return r.AgentID
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
