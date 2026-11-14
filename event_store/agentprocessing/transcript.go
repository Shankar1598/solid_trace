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
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	Level       string          `json:"level"`
	Message     json.RawMessage `json:"message"`
	Content     json.RawMessage `json:"content"`
	Attachment  json.RawMessage `json:"attachment"`
	AiTitle     string          `json:"aiTitle"`
}

type transcriptMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
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
	Type   string          `json:"type"`
	Prompt json.RawMessage `json:"prompt"`
}

// agentLogIDs is the namespace of the Log ids that the parse makes: a
// line's id comes from where its raw line is, so a parse again makes the
// same Log.
var agentLogIDs = uuid.MustParse("5b0f3c1e-8f53-4d0e-9a52-6c1d2b7e4a90")

// parseUnit makes one Log for each line of a unit of a .jsonl file, by Log
// ingest's normalise, at the Agent Project's Retention. A unit of another
// file has no Logs.
func parseUnit(unit storage.AgentFileUnit, retentionDays int) []models.Log {
	if !strings.HasSuffix(unit.Path, ".jsonl") {
		return nil
	}
	receipt := normalise.Receipt{At: unit.StoredAt.UTC(), RetentionDays: retentionDays}
	// A line with no timestamp has the time of the line before it, and the
	// first lines the time that the server stored the unit.
	previous := json.RawMessage(strconv.Quote(receipt.At.Format(time.RFC3339Nano)))
	toolNames := map[string]string{}
	var logs []models.Log
	data := unit.Bytes
	for start := 0; start < len(data); {
		end := bytes.IndexByte(data[start:], '\n')
		if end < 0 {
			end = len(data)
		} else {
			end += start
		}
		raw := data[start:end]
		offset := unit.Offset + uint64(start)
		start = end + 1
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		object := lineObject(raw, toolNames)
		if t, ok := object["time"]; ok {
			previous = t
		} else {
			object["time"] = previous
		}
		addSource(object, unit, offset, len(raw))
		log, outcome := normalise.JSON(object, receipt)
		if outcome == normalise.Dropped {
			// Not possible with a cut message and these few Attributes; the
			// raw layer has the line.
			continue
		}
		log.ID = uuid.NewSHA1(agentLogIDs, fmt.Appendf(nil, "%s/%d/%d/%s", unit.SessionID, unit.Version, offset, unit.Path))
		logs = append(logs, log)
	}
	return logs
}

// lineObject is the Log of one line as a JSON object for normalise: the
// text of the line as its message, and what the line is under agent. It
// notes the tool of each tool call in toolNames, by its id, for the result.
func lineObject(raw []byte, toolNames map[string]string) map[string]json.RawMessage {
	object := map[string]json.RawMessage{}
	var line transcriptLine
	if err := json.Unmarshal(raw, &line); err != nil {
		object["message"] = quote(string(raw))
		object["agent.line"] = quote("unreadable")
		return object
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
		var msg transcriptMessage
		if s, ok := stringValue(line.Message); ok {
			text.WriteString(s)
		} else if json.Unmarshal(line.Message, &msg) == nil {
			set("agent.message_id", msg.ID)
			set("agent.model", msg.Model)
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
				s, _ := contentOf(a.Prompt)
				text.WriteString(s)
			}
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
	return object
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
