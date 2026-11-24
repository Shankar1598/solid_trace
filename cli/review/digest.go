// Package review reads a Claude Code transcript as a short timeline of its
// reviews: the task, then from the first review on, each review's start,
// the agent's texts, the findings blocks, the subagents' reports and
// prompts, and the person's messages, the edits and the commits. A small agent reads the timeline, not the transcript, to record
// what happened to each finding.
package review

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Digest is the timeline of one Agent session.
type Digest struct {
	Session string  `json:"session"`
	Reviews int     `json:"reviews"`
	Events  []Entry `json:"events"`
}

// Kind is what an Entry records.
type Kind string

const (
	KindTask        Kind = "task"
	KindReviewStart Kind = "review_start"
	KindAgentText   Kind = "agent_text"
	KindSubagent    Kind = "subagent_report"
	KindDelegate    Kind = "delegate"
	KindFindings    Kind = "findings"
	KindPerson      Kind = "person"
	KindEdit        Kind = "edit"
	KindCommit      Kind = "commit"
)

// Trigger is who started a review.
type Trigger string

const (
	TriggerPerson Trigger = "person"
	TriggerAgent  Trigger = "agent"
)

// Entry is one step of the timeline. Each Kind sets only its own fields:
// task, person, agent_text and subagent_report set Text; review_start sets
// Skill, Trigger and Args; delegate sets Text (the subagent's description
// and prompt); findings sets Findings, and Error when the block is not
// valid JSON (Findings is then null); edit sets Tool and File; commit sets
// Command.
type Entry struct {
	Seq      int             `json:"seq"`
	UUID     string          `json:"uuid"`
	Time     string          `json:"time"`
	Kind     Kind            `json:"kind"`
	Text     string          `json:"text,omitempty"`
	Skill    string          `json:"skill,omitempty"`
	Trigger  Trigger         `json:"trigger,omitempty"`
	Args     string          `json:"args,omitempty"`
	Findings json.RawMessage `json:"findings,omitempty"`
	Error    string          `json:"error,omitempty"`
	Tool     string          `json:"tool,omitempty"`
	File     string          `json:"file,omitempty"`
	Command  string          `json:"command,omitempty"`
}

// DefaultSkills matches the names of the usual review skills, such as
// code-review, pstack:interrogate and a team's <team>-review.
const DefaultSkills = "review|interrogate"

// The most characters of a text, of a subagent's report, of a subagent's
// prompt, and of a commit command. A review often runs in subagents, so
// their reports hold the findings and get more.
const (
	maxText     = 4000
	maxSubagent = 8000
	maxPrompt   = 1500
	maxCommand  = 300
)

// agentTools are the tools that start a subagent.
var agentTools = map[string]bool{"Agent": true, "Task": true}

// A subagent that runs in the background reports in a later message: a
// hand-back message, or a task notification with its result.
const (
	handBack        = "<agent-message"
	asyncLaunched   = "Async agent launched"
	minNoticeResult = 300
)

const findingsFence = "```solidtrace-findings"

// editTools are the tools that change a file, with the input field that
// names the file.
var editTools = map[string]string{
	"Edit":         "file_path",
	"Write":        "file_path",
	"MultiEdit":    "file_path",
	"NotebookEdit": "notebook_path",
}

// notFromThePerson are the starts of user text that Claude Code writes
// itself: the notice of a background task's end, and a slash command's
// output and caveat.
var notFromThePerson = []string{
	"<task-notification>",
	"<local-command-stdout>",
	"<local-command-stderr>",
	"<local-command-caveat>",
}

// Read reads a transcript into its Digest. The session is the transcript's
// sessionId. A line that is not valid JSON is skipped: a live transcript
// can end in a part of a line.
func Read(r io.Reader, skills *regexp.Regexp) (Digest, error) {
	b := builder{skills: skills, subagents: map[string]bool{}, tops: map[string]string{}}
	in := bufio.NewReader(r)
	for {
		raw, err := in.ReadBytes('\n')
		if len(raw) > 0 {
			var l line
			if json.Unmarshal(raw, &l) == nil {
				b.add(l)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Digest{}, err
		}
	}
	for i := range b.digest.Events {
		b.digest.Events[i].Seq = i
	}
	if b.digest.Events == nil {
		b.digest.Events = []Entry{}
	}
	return b.digest, nil
}

type line struct {
	Type             string          `json:"type"`
	UUID             string          `json:"uuid"`
	Timestamp        string          `json:"timestamp"`
	SessionID        string          `json:"sessionId"`
	Cwd              string          `json:"cwd"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Origin           *origin         `json:"origin"`
	Message          json.RawMessage `json:"message"`
	Attachment       json.RawMessage `json:"attachment"`
}

// origin is who gave Claude Code a message: "human" for a person. Older
// versions of Claude Code do not write it.
type origin struct {
	Kind string `json:"kind"`
}

func (o *origin) notAPerson() bool { return o != nil && o.Kind != "" && o.Kind != "human" }

type message struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type queuedCommand struct {
	Type        string          `json:"type"`
	Prompt      json.RawMessage `json:"prompt"`
	CommandMode string          `json:"commandMode"`
	IsMeta      bool            `json:"isMeta"`
	Origin      *origin         `json:"origin"`
}

type builder struct {
	skills  *regexp.Regexp
	digest  Digest
	hasTask bool
	// reviewed is true from the first review start on: only then do the
	// other kinds go in the timeline.
	reviewed bool
	// subagents are the ids of the tool uses that started a subagent.
	subagents map[string]bool
	tops      map[string]string
}

func (b *builder) add(l line) {
	if l.IsSidechain {
		return
	}
	if b.digest.Session == "" {
		b.digest.Session = l.SessionID
	}
	switch l.Type {
	case "assistant":
		var m message
		// A <synthetic> message is Claude Code's own, such as an API error.
		if json.Unmarshal(l.Message, &m) != nil || m.Model == "<synthetic>" {
			return
		}
		for _, bl := range blocksOf(m.Content) {
			b.assistantBlock(l, bl)
		}
	case "user":
		if text, ok := personText(l); ok {
			b.personMessage(l, text)
		} else if b.reviewed {
			b.subagentReports(l)
		}
	case "attachment":
		if text, ok := queuedPersonText(l); ok {
			b.personMessage(l, text)
		}
	}
}

func newEntry(l line, kind Kind) Entry {
	return Entry{UUID: l.UUID, Time: l.Timestamp, Kind: kind}
}

func (b *builder) assistantBlock(l line, bl block) {
	switch bl.Type {
	case "text":
		if !b.reviewed || strings.TrimSpace(bl.Text) == "" {
			return
		}
		e := newEntry(l, KindAgentText)
		e.Text = cut(bl.Text, maxText)
		b.digest.Events = append(b.digest.Events, e)
		if e, ok := findings(l, bl.Text); ok {
			b.digest.Events = append(b.digest.Events, e)
		}
	case "tool_use":
		var input struct {
			Skill        string `json:"skill"`
			Args         string `json:"args"`
			Command      string `json:"command"`
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
			Description  string `json:"description"`
			Prompt       string `json:"prompt"`
		}
		_ = json.Unmarshal(bl.Input, &input)
		if bl.Name == "Skill" && input.Skill != "" && b.skills.MatchString(input.Skill) {
			b.reviewStart(l, input.Skill, TriggerAgent, input.Args)
			return
		}
		if !b.reviewed {
			return
		}
		if agentTools[bl.Name] {
			b.subagents[bl.ID] = true
			e := newEntry(l, KindDelegate)
			e.Text = cut(strings.TrimSpace(input.Description+"\n\n"+input.Prompt), maxPrompt)
			b.digest.Events = append(b.digest.Events, e)
		}
		if field, ok := editTools[bl.Name]; ok {
			path := input.FilePath
			if field == "notebook_path" {
				path = input.NotebookPath
			}
			e := newEntry(l, KindEdit)
			e.Tool, e.File = bl.Name, b.relative(l.Cwd, path)
			b.digest.Events = append(b.digest.Events, e)
		}
		if bl.Name == "Bash" && strings.Contains(input.Command, "git commit") {
			e := newEntry(l, KindCommit)
			e.Command = cut(input.Command, maxCommand)
			b.digest.Events = append(b.digest.Events, e)
		}
	}
}

func (b *builder) personMessage(l line, text string) {
	name, isCommand := tagText(text, "command-name")
	args, _ := tagText(text, "command-args")
	name = strings.TrimPrefix(name, "/")
	if isCommand {
		text = strings.TrimSpace("/" + name + " " + args)
	}
	// A command such as /clear or /model is not the task.
	isReview := isCommand && name != "" && b.skills.MatchString(name)
	isTask := !b.hasTask && (!isCommand || isReview)
	if isTask {
		b.hasTask = true
		e := newEntry(l, KindTask)
		e.Text = cut(text, maxText)
		b.digest.Events = append(b.digest.Events, e)
	}
	if isReview {
		b.reviewStart(l, name, TriggerPerson, args)
		return
	}
	if isTask || !b.reviewed {
		return
	}
	e := newEntry(l, KindPerson)
	e.Text = cut(text, maxText)
	b.digest.Events = append(b.digest.Events, e)
}

func (b *builder) reviewStart(l line, skill string, trigger Trigger, args string) {
	b.reviewed = true
	b.digest.Reviews++
	e := newEntry(l, KindReviewStart)
	e.Skill, e.Trigger, e.Args = skill, trigger, strings.TrimSpace(args)
	b.digest.Events = append(b.digest.Events, e)
}

// subagentReports adds the reports in a user line that is not the
// person's: a subagent's tool result, a hand-back message, or a task
// notification's result.
func (b *builder) subagentReports(l line) {
	var m message
	if json.Unmarshal(l.Message, &m) != nil {
		return
	}
	add := func(text string) {
		e := newEntry(l, KindSubagent)
		e.Text = cut(strings.TrimSpace(text), maxSubagent)
		b.digest.Events = append(b.digest.Events, e)
	}
	var s string
	if json.Unmarshal(m.Content, &s) != nil {
		var parts []string
		for _, bl := range blocksOf(m.Content) {
			switch {
			case bl.Type == "tool_result" && b.subagents[bl.ToolUseID]:
				if text, _ := contentText(bl.Content); !strings.HasPrefix(text, asyncLaunched) {
					add(text)
				}
			case bl.Type == "text":
				parts = append(parts, bl.Text)
			}
		}
		s = strings.Join(parts, "\n")
	}
	if strings.Contains(s, handBack) {
		add(s)
	} else if result, ok := tagText(s, "result"); ok && strings.HasPrefix(strings.TrimSpace(s), "<task-notification>") && len(result) >= minNoticeResult {
		add(result)
	}
}

// findings is the findings entry of an assistant text that holds a
// ```solidtrace-findings block.
func findings(l line, text string) (Entry, bool) {
	start := -1
	lines := strings.Split(text, "\n")
	for i, s := range lines {
		if strings.TrimSpace(s) == findingsFence {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return Entry{}, false
	}
	e := newEntry(l, KindFindings)
	e.Findings = json.RawMessage("null")
	end := -1
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "```" {
			end = i
			break
		}
	}
	if end < 0 {
		e.Error = "the solidtrace-findings block has no closing ```"
		return e, true
	}
	body := []byte(strings.Join(lines[start:end], "\n"))
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		e.Error = err.Error()
		return e, true
	}
	// json.Compact keeps the block's own key order and its <, > and &.
	var compact bytes.Buffer
	_ = json.Compact(&compact, body)
	e.Findings = compact.Bytes()
	return e, true
}

// personText is the text of a user line that a person wrote. Hook
// feedback, a skill's text, a compaction's summary, a subagent's line, a
// tool's result and the text that Claude Code writes itself are not.
func personText(l line) (string, bool) {
	if l.IsMeta || l.IsCompactSummary || l.Origin.notAPerson() {
		return "", false
	}
	var m message
	if json.Unmarshal(l.Message, &m) != nil {
		return "", false
	}
	text, ok := contentText(m.Content)
	return text, ok && fromThePerson(text)
}

// queuedPersonText is the text of a message that a person sent while the
// agent worked, which Claude Code writes as a queued_command attachment.
// Claude Code queues a background task's notice in the same way.
func queuedPersonText(l line) (string, bool) {
	var a queuedCommand
	if json.Unmarshal(l.Attachment, &a) != nil || a.Type != "queued_command" {
		return "", false
	}
	if a.IsMeta || a.CommandMode == "task-notification" || a.Origin.notAPerson() || l.Origin.notAPerson() {
		return "", false
	}
	text, ok := contentText(a.Prompt)
	return text, ok && fromThePerson(text)
}

func fromThePerson(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	for _, start := range notFromThePerson {
		if strings.HasPrefix(trimmed, start) {
			return false
		}
	}
	return true
}

// contentText is the text of a user content: a string, or text blocks. A
// content with a tool result is not a message.
func contentText(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var parts []string
	for _, bl := range blocksOf(raw) {
		switch bl.Type {
		case "tool_result":
			return "", false
		case "text":
			parts = append(parts, bl.Text)
		default:
			parts = append(parts, "["+bl.Type+"]")
		}
	}
	return strings.Join(parts, "\n"), len(parts) > 0
}

func blocksOf(raw json.RawMessage) []block {
	var blocks []block
	_ = json.Unmarshal(raw, &blocks)
	return blocks
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

// cut is text cut to at most limit characters, with "…" at the end.
func cut(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// relative is path relative to the git top level of cwd, when path is in
// it. Else it is path.
func (b *builder) relative(cwd, path string) string {
	if cwd == "" || !filepath.IsAbs(path) {
		return path
	}
	top, ok := b.tops[cwd]
	if !ok {
		top = gitTopLevel(cwd)
		b.tops[cwd] = top
	}
	if top == "" {
		return path
	}
	rel, err := filepath.Rel(top, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

// gitTopLevel is the nearest folder at or above dir that holds .git (a
// folder, or a worktree's file), or "" for none.
func gitTopLevel(dir string) string {
	for dir = filepath.Clean(dir); ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
