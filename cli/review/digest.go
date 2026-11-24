// Package review reads Claude Code transcripts as a short timeline of
// their reviews: the task, the person's messages, the commits and the
// subagents' prompts, and from the first review on also each review's
// start, the agent's texts, the findings blocks, the subagents' reports
// and the edits. A small agent reads the timeline, not the transcripts, to
// record what happened to each finding. It also finds the Agent sessions
// that a PR's branch links to.
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
	"time"
)

// Digest is the timeline of one or more Agent sessions.
type Digest struct {
	Sessions []SessionTitle `json:"sessions"`
	Reviews  int            `json:"reviews"`
	Events   []Entry        `json:"events"`
}

// SessionTitle is an Agent session of a Digest.
type SessionTitle struct {
	ID    string `json:"id"`
	Title string `json:"title"`
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
	Session  string          `json:"session"`
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

// Read reads the main transcript of one Agent session into its Digest.
// The session's id is the transcript's sessionId, else id.
func Read(r io.Reader, id string, skills *regexp.Regexp) (Digest, error) {
	b := builder{skills: skills, subagents: map[string]bool{}, tops: topLevels{}}
	if err := eachLine(r, b.add); err != nil {
		return Digest{}, err
	}
	if b.session != "" {
		id = b.session
	}
	for i := range b.events {
		b.events[i].Session = id
	}
	return Merge(Digest{Sessions: []SessionTitle{{ID: id, Title: b.titles.title()}}, Reviews: b.reviews, Events: b.events}), nil
}

// Merge puts the Digests in one: their events in time order, each
// session's own events in the order of its transcript, and the events of
// an earlier Digest first when their times are equal.
func Merge(digests ...Digest) Digest {
	all := Digest{Sessions: []SessionTitle{}, Events: []Entry{}}
	heads := make([]int, len(digests))
	times := make([][]time.Time, len(digests))
	for i, d := range digests {
		all.Sessions = append(all.Sessions, d.Sessions...)
		all.Reviews += d.Reviews
		times[i] = eventTimes(d.Events)
	}
	for {
		next := -1
		for i, d := range digests {
			if heads[i] < len(d.Events) && (next < 0 || times[i][heads[i]].Before(times[next][heads[next]])) {
				next = i
			}
		}
		if next < 0 {
			return all
		}
		e := digests[next].Events[heads[next]]
		e.Seq = len(all.Events)
		all.Events = append(all.Events, e)
		heads[next]++
	}
}

// eventTimes are the times of events. An event whose time is not valid
// takes the time of the event before it, so that it stays in its place.
func eventTimes(events []Entry) []time.Time {
	times := make([]time.Time, len(events))
	var last time.Time
	for i, e := range events {
		if t, err := time.Parse(time.RFC3339Nano, e.Time); err == nil {
			last = t
		}
		times[i] = last
	}
	return times
}

// eachLine gives each line of a transcript to add. A line that is not
// valid JSON is skipped: a live transcript can end in a part of a line.
func eachLine(r io.Reader, add func(line)) error {
	in := bufio.NewReader(r)
	for {
		raw, err := in.ReadBytes('\n')
		if len(raw) > 0 {
			var l line
			if json.Unmarshal(raw, &l) == nil {
				add(l)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type line struct {
	Type             string          `json:"type"`
	UUID             string          `json:"uuid"`
	Timestamp        string          `json:"timestamp"`
	SessionID        string          `json:"sessionId"`
	Cwd              string          `json:"cwd"`
	GitBranch        string          `json:"gitBranch"`
	CustomTitle      string          `json:"customTitle"`
	AITitle          string          `json:"aiTitle"`
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
	session string
	titles  titles
	reviews int
	events  []Entry
	hasTask bool
	// reviewed is true from the first review on, a review start or a
	// findings block: only then do the agent's texts, the edits and the
	// subagents' reports go in the timeline.
	reviewed bool
	// subagents are the ids of the tool uses that started a subagent.
	subagents map[string]bool
	tops      topLevels
}

func (b *builder) add(l line) {
	b.titles.add(l)
	// A compact summary is Claude Code's own retelling of the session, and it
	// can quote a findings block or a hand-back.
	if l.IsSidechain || l.IsCompactSummary {
		return
	}
	if b.session == "" {
		b.session = l.SessionID
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
		} else {
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
		// A findings block marks a review, also with no review start before it.
		found, ok := findings(l, bl.Text)
		if strings.TrimSpace(bl.Text) == "" || (!ok && !b.reviewed) {
			return
		}
		b.reviewed = true
		e := newEntry(l, KindAgentText)
		e.Text = cut(bl.Text, maxText)
		b.events = append(b.events, e)
		if ok {
			b.events = append(b.events, found)
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
		if agentTools[bl.Name] {
			b.subagents[bl.ID] = true
			e := newEntry(l, KindDelegate)
			e.Text = cut(strings.TrimSpace(input.Description+"\n\n"+input.Prompt), maxPrompt)
			b.events = append(b.events, e)
		}
		if field, ok := editTools[bl.Name]; ok && b.reviewed {
			path := input.FilePath
			if field == "notebook_path" {
				path = input.NotebookPath
			}
			e := newEntry(l, KindEdit)
			e.Tool, e.File = bl.Name, b.tops.relative(l.Cwd, path)
			b.events = append(b.events, e)
		}
		// A subagent's work often lands with a git merge -m command.
		if bl.Name == "Bash" && (strings.Contains(input.Command, "git commit") || strings.Contains(input.Command, "git merge")) {
			e := newEntry(l, KindCommit)
			e.Command = cut(input.Command, maxCommand)
			b.events = append(b.events, e)
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
		b.events = append(b.events, e)
	}
	if isReview {
		b.reviewStart(l, name, TriggerPerson, args)
		return
	}
	// A command before the task, such as the /clear that starts the
	// session, is not a step of the work.
	if isTask || !b.hasTask {
		return
	}
	e := newEntry(l, KindPerson)
	e.Text = cut(text, maxText)
	b.events = append(b.events, e)
}

func (b *builder) reviewStart(l line, skill string, trigger Trigger, args string) {
	b.reviewed = true
	b.reviews++
	e := newEntry(l, KindReviewStart)
	e.Skill, e.Trigger, e.Args = skill, trigger, strings.TrimSpace(args)
	b.events = append(b.events, e)
}

// subagentReports adds the reports in a user line that is not the
// person's: a subagent's tool result, a hand-back message, or a task
// notification's result. A report with a findings block is a review that a
// subagent ran, so it counts from before the first review start too.
func (b *builder) subagentReports(l line) {
	var m message
	if json.Unmarshal(l.Message, &m) != nil {
		return
	}
	add := func(text string) {
		found, ok := findings(l, text)
		if !ok && !b.reviewed {
			return
		}
		b.reviewed = true
		e := newEntry(l, KindSubagent)
		e.Text = cut(strings.TrimSpace(text), maxSubagent)
		b.events = append(b.events, e)
		if ok {
			b.events = append(b.events, found)
		}
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

// topLevels keeps the git top level of each cwd that relative looked up.
type topLevels map[string]string

// relative is path relative to the git top level of cwd, when path is in
// it. Else it is path.
func (tops topLevels) relative(cwd, path string) string {
	if cwd == "" || !filepath.IsAbs(path) {
		return path
	}
	top, ok := tops[cwd]
	if !ok {
		top = gitTopLevel(cwd)
		tops[cwd] = top
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
