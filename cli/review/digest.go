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
	// A Digest from Read also keeps all the events of its session, and the
	// times of its start and of its first review, for Merge.
	all             []Entry
	start, reviewed time.Time
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
// valid JSON (Findings is then null), and Writer for a block of a
// subagent's transcript; edit sets Tool and File; commit sets Command, and
// ReviewFindings from the message's Review-Findings trailers.
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
	// Writer is the agent id of the subagent whose transcript has the
	// block, and "" for the main transcript.
	Writer  string `json:"writer,omitempty"`
	Tool    string `json:"tool,omitempty"`
	File    string `json:"file,omitempty"`
	Command string `json:"command,omitempty"`
	// ReviewFindings are kept whole, because Command is cut.
	ReviewFindings []string `json:"review_findings,omitempty"`
	// gated is true for an agent's text, an edit or a subagent's report
	// from before the session's first review.
	gated bool
	// fromCall is true for a block from the output of solidtrace review
	// block or solidtrace review fixed.
	fromCall bool
}

// DefaultSkills matches the names of the usual review skills: a name whose
// last part (after the last ":") is review or interrogate, or ends in
// -review, such as code-review, pstack:interrogate and a team's
// <team>-review. It does not match preview-deploy.
const DefaultSkills = `(^|:)(review|interrogate)$|-review$`

// The most characters of a text, of a subagent's report, of a subagent's
// prompt, and of a commit command. A review often runs in subagents, so
// their reports hold the findings and get more.
const (
	maxText     = 4000
	maxSubagent = 8000
	maxPrompt   = 1500
	maxCommand  = 300
	// reviewTrailer names the findings that a commit fixes.
	reviewTrailer = "Review-Findings:"
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

// Subagent is the transcript of one subagent of an Agent session, with
// the subagent's agent id.
type Subagent struct {
	ID         string
	Transcript io.Reader
}

// Read reads the main transcript of one Agent session into its Digest,
// and from the transcripts of its subagents the blocks that solidtrace
// review block and solidtrace review fixed printed. The session's id is
// the transcript's sessionId, else id.
func Read(r io.Reader, id string, skills *regexp.Regexp, subagents ...Subagent) (Digest, error) {
	b := builder{
		skills:     skills,
		subagents:  map[string]bool{},
		asks:       map[string]bool{},
		blockCalls: map[string]blockCall{},
		edits:      map[string]int{},
		rejected:   map[int]bool{},
		paths:      paths{tops: topLevels{}},
	}
	if err := eachLine(r, b.add); err != nil {
		return Digest{}, err
	}
	if b.session != "" {
		id = b.session
	}
	var main []Entry
	for i, e := range b.events {
		if !b.rejected[i] {
			main = append(main, e)
		}
	}
	sources := [][]Entry{main}
	reviewed := b.reviewedAt
	var subagentReview time.Time
	for _, s := range subagents {
		found, err := subagentBlocks(s.Transcript, s.ID)
		if err != nil {
			return Digest{}, err
		}
		if times := eventTimes(found); len(found) > 0 && (subagentReview.IsZero() || times[0].Before(subagentReview)) {
			subagentReview = times[0]
		}
		sources = append(sources, found)
	}
	events := withoutCopies(mergeByTime(sources))
	// A review that ran only in a subagent starts the review of the
	// session at the time of its first block.
	if !subagentReview.IsZero() && (reviewed.IsZero() || subagentReview.Before(reviewed)) {
		reviewed = subagentReview
		for i, t := range eventTimes(events) {
			if events[i].gated && !t.Before(reviewed) {
				events[i].gated = false
			}
		}
	}
	for i := range events {
		events[i].Session = id
	}
	if events == nil {
		events = []Entry{}
	}
	d := Digest{
		Sessions: []SessionTitle{{ID: id, Title: b.titles.title()}},
		Reviews:  b.reviews,
		all:      events,
		start:    b.start,
		reviewed: reviewed,
	}
	d.Events = Merge(Digest{Sessions: d.Sessions, Events: d.timeline(false)}).Events
	return d, nil
}

// subagentBlocks are the findings entries of the solidtrace review block
// and solidtrace review fixed commands in a subagent's transcript, with
// the subagent's agent id as their writer.
func subagentBlocks(r io.Reader, id string) ([]Entry, error) {
	calls := map[string]blockCall{}
	var out []Entry
	err := eachLine(r, func(l line) {
		var m message
		if json.Unmarshal(l.Message, &m) != nil {
			return
		}
		writer := id
		if l.AgentID != "" {
			writer = l.AgentID
		}
		for _, bl := range blocksOf(m.Content) {
			if l.Type == "assistant" && bl.Type == "tool_use" {
				if call, ok := blockCallOf(bl); ok {
					calls[bl.ID] = call
				}
			}
			if call, ok := calls[bl.ToolUseID]; ok && l.Type == "user" && bl.Type == "tool_result" && !bl.IsError {
				for _, e := range call.entries(l, bl) {
					e.Writer = writer
					out = append(out, e)
				}
			}
		}
	})
	return out, err
}

// blockCall is a Bash tool use that runs solidtrace review block or
// solidtrace review fixed, with the blocks that it gives on stdin (see
// blockCommand).
type blockCall struct {
	bodies []string
	block  bool
}

func blockCallOf(bl block) (blockCall, bool) {
	if bl.Name != "Bash" {
		return blockCall{}, false
	}
	var input struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(bl.Input, &input)
	bodies, ok, isBlock := blockCommand(input.Command)
	return blockCall{bodies: bodies, block: isBlock}, ok
}

// entries are the findings entries of a block call that did not fail, at
// the line of its result: the blocks of its stdin, else those of its
// output. A review block call that gives no block is an entry with an
// error, so that the record does not lose its run with no word. review
// fixed prints nothing for a commit with no trailers.
func (c blockCall) entries(l line, result block) []Entry {
	var found []Entry
	for _, body := range c.bodies {
		found = append(found, blockEntry(l, []byte(body)))
	}
	if len(c.bodies) == 0 {
		text, _ := contentText(result.Content)
		found = findings(l, text)
	}
	if len(found) == 0 && c.block {
		e := newEntry(l, KindFindings)
		e.Findings, e.Error = json.RawMessage("null"), "the output of the solidtrace review command has no solidtrace-findings block"
		found = []Entry{e}
	}
	for i := range found {
		found[i].fromCall = true
	}
	return found
}

// withoutCopies is events without each findings entry from a text or a
// report whose block is the same JSON as the block of a call: older
// skills also put a subagent's block in its report.
func withoutCopies(events []Entry) []Entry {
	called := map[string]bool{}
	for _, e := range events {
		if e.Kind == KindFindings && e.fromCall && e.Error == "" {
			called[string(e.Findings)] = true
		}
	}
	out := make([]Entry, 0, len(events))
	for _, e := range events {
		if e.Kind == KindFindings && !e.fromCall && e.Error == "" && called[string(e.Findings)] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Merge puts the Digests in one: their events in time order, each
// session's own events in the order of its transcript, and the events of
// an earlier Digest first when their times are equal. A session that
// starts after the first review of a Digest keeps all its events, also
// those from before a review of its own.
func Merge(digests ...Digest) Digest {
	var first time.Time
	for _, d := range digests {
		if !d.reviewed.IsZero() && (first.IsZero() || d.reviewed.Before(first)) {
			first = d.reviewed
		}
	}
	all := Digest{Sessions: []SessionTitle{}, Events: []Entry{}}
	sources := make([][]Entry, len(digests))
	for i, d := range digests {
		all.Sessions = append(all.Sessions, d.Sessions...)
		all.Reviews += d.Reviews
		sources[i] = d.timeline(!first.IsZero() && d.start.After(first))
	}
	for _, e := range mergeByTime(sources) {
		e.Seq = len(all.Events)
		all.Events = append(all.Events, e)
	}
	return all
}

// mergeByTime puts the events of sources in one list in time order: each
// source's own events in their order, and the events of an earlier source
// first when their times are equal.
func mergeByTime(sources [][]Entry) []Entry {
	heads := make([]int, len(sources))
	times := make([][]time.Time, len(sources))
	for i, events := range sources {
		times[i] = eventTimes(events)
	}
	var out []Entry
	for {
		next := -1
		for i, events := range sources {
			if heads[i] < len(events) && (next < 0 || times[i][heads[i]].Before(times[next][heads[next]])) {
				next = i
			}
		}
		if next < 0 {
			return out
		}
		out = append(out, sources[next][heads[next]])
		heads[next]++
	}
}

// timeline is the events of d: all of them when keepGated is true, else
// those from the session's first review on and the story before it.
func (d Digest) timeline(keepGated bool) []Entry {
	if d.all == nil {
		return d.Events
	}
	var events []Entry
	for _, e := range d.all {
		if keepGated || !e.gated {
			events = append(events, e)
		}
	}
	return events
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
	AgentID          string          `json:"agentId"`
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
	IsError   bool            `json:"is_error"`
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
	// subagents' reports go in the timeline. The events from before it
	// are gated.
	reviewed bool
	// start is the time of the first line, now of the last line with a
	// time, and reviewedAt of the line of the first review.
	start, now, reviewedAt time.Time
	// subagents are the ids of the tool uses that started a subagent, asks
	// those of AskUserQuestion, and blockCalls those of solidtrace review
	// block and solidtrace review fixed.
	subagents  map[string]bool
	asks       map[string]bool
	blockCalls map[string]blockCall
	// edits has the index in events of each edit, by its tool use id, and
	// rejected the edits whose tool result is an error.
	edits    map[string]int
	rejected map[int]bool
	paths    paths
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
	if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
		if b.start.IsZero() {
			b.start = t
		}
		b.now = t
	}
	b.paths.see(l.Cwd)
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
			b.toolResults(l)
		}
	case "attachment":
		if text, ok := queuedPersonText(l); ok {
			b.personMessage(l, text)
		} else if text, ok := queuedNotice(l); ok {
			b.notice(l, text)
		}
	}
}

func newEntry(l line, kind Kind) Entry {
	return Entry{UUID: l.UUID, Time: l.Timestamp, Kind: kind}
}

func (b *builder) markReviewed() {
	if !b.reviewed {
		b.reviewed, b.reviewedAt = true, b.now
	}
}

// addGated adds an event that is in the timeline only from a review on,
// and gives its index.
func (b *builder) addGated(e Entry) int {
	e.gated = !b.reviewed
	b.events = append(b.events, e)
	return len(b.events) - 1
}

func (b *builder) assistantBlock(l line, bl block) {
	switch bl.Type {
	case "text":
		if strings.TrimSpace(bl.Text) == "" {
			return
		}
		// A findings block marks a review, also with no review start before it.
		found := findings(l, bl.Text)
		if len(found) > 0 {
			b.markReviewed()
		}
		e := newEntry(l, KindAgentText)
		e.Text = cut(bl.Text, maxText)
		b.addGated(e)
		b.events = append(b.events, found...)
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
		if bl.Name == "AskUserQuestion" {
			b.asks[bl.ID] = true
		}
		if call, ok := blockCallOf(bl); ok {
			b.blockCalls[bl.ID] = call
		}
		if agentTools[bl.Name] {
			b.subagents[bl.ID] = true
			e := newEntry(l, KindDelegate)
			e.Text = cut(strings.TrimSpace(input.Description+"\n\n"+input.Prompt), maxPrompt)
			b.events = append(b.events, e)
		}
		if field, ok := editTools[bl.Name]; ok {
			path := input.FilePath
			if field == "notebook_path" {
				path = input.NotebookPath
			}
			e := newEntry(l, KindEdit)
			e.Tool, e.File = bl.Name, b.paths.relative(l.Cwd, path)
			b.edits[bl.ID] = b.addGated(e)
		}
		// A subagent's work often lands with a git merge -m command.
		if bl.Name == "Bash" {
			if messages := commitMessages(input.Command); len(messages) > 0 {
				e := newEntry(l, KindCommit)
				e.Command = cut(input.Command, maxCommand)
				e.ReviewFindings = reviewTrailers(messages)
				b.events = append(b.events, e)
			}
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
	isReview := isCommand && name != "" && b.skills.MatchString(name)
	// A command such as /clear or /model is not the task, but a skill such
	// as /tdd or /open-pr is the work.
	isTask := !b.hasTask && (!isCommand || name != "" && !housekeeping[name])
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
	b.person(l, text)
}

// housekeeping are the slash commands of Claude Code that do no work.
var housekeeping = map[string]bool{
	"add-dir": true, "agents": true, "bashes": true, "clear": true, "compact": true, "config": true,
	"context": true, "cost": true, "doctor": true, "effort": true, "exit": true, "export": true,
	"fast": true, "help": true, "hooks": true, "ide": true, "keybindings": true, "login": true,
	"logout": true, "mcp": true, "memory": true, "model": true, "output-style": true,
	"permissions": true, "plugin": true, "privacy-settings": true, "quit": true,
	"release-notes": true, "rename": true, "resume": true, "rewind": true, "status": true,
	"statusline": true, "tasks": true, "terminal-setup": true, "theme": true, "todos": true,
	"upgrade": true, "usage": true, "vim": true,
}

func (b *builder) person(l line, text string) {
	e := newEntry(l, KindPerson)
	e.Text = cut(strings.TrimSpace(text), maxText)
	b.events = append(b.events, e)
}

func (b *builder) reviewStart(l line, skill string, trigger Trigger, args string) {
	b.markReviewed()
	b.reviews++
	e := newEntry(l, KindReviewStart)
	e.Skill, e.Trigger, e.Args = skill, trigger, strings.TrimSpace(args)
	b.events = append(b.events, e)
}

// What Claude Code writes in the tool result of a question to the person,
// and of a tool use that the person rejected.
const (
	answered        = "User has answered your questions:"
	answeredEnd     = "You can now continue with the user's answers in mind."
	rejected        = "The user doesn't want to proceed with this tool use."
	rejectedBecause = "the user said:"
)

// toolResults reads a user line that is not the person's: the tool
// results, a hand-back message, or a task notification. A tool result has
// the person's answer to a question, the person's words when they rejected
// a tool use, or a subagent's report.
func (b *builder) toolResults(l line) {
	var m message
	if json.Unmarshal(l.Message, &m) != nil {
		return
	}
	var s string
	if json.Unmarshal(m.Content, &s) != nil {
		var parts []string
		for _, bl := range blocksOf(m.Content) {
			switch bl.Type {
			case "tool_result":
				b.toolResult(l, bl)
			case "text":
				parts = append(parts, bl.Text)
			}
		}
		s = strings.Join(parts, "\n")
	}
	if strings.Contains(s, handBack) {
		b.report(l, s)
	} else {
		b.notice(l, s)
	}
}

func (b *builder) toolResult(l line, bl block) {
	text, _ := contentText(bl.Content)
	if bl.IsError {
		// The tool did not change the file.
		if i, ok := b.edits[bl.ToolUseID]; ok {
			b.rejected[i] = true
		}
		_, words, said := strings.Cut(text, rejectedBecause)
		if said && strings.TrimSpace(words) != "" {
			b.person(l, words)
		}
		if said || strings.HasPrefix(strings.TrimSpace(text), rejected) {
			return
		}
	}
	if call, ok := b.blockCalls[bl.ToolUseID]; ok {
		// A call that failed printed no block.
		if !bl.IsError {
			b.markReviewed()
			b.events = append(b.events, call.entries(l, bl)...)
		}
		return
	}
	if answers, ok := strings.CutPrefix(strings.TrimSpace(text), answered); ok && b.asks[bl.ToolUseID] {
		b.person(l, strings.TrimSuffix(strings.TrimSpace(answers), answeredEnd))
		return
	}
	if b.subagents[bl.ToolUseID] && !strings.HasPrefix(text, asyncLaunched) {
		b.report(l, text)
	}
}

// notice adds the result of a task notification as a subagent's report,
// when it is long or holds a findings block. A short result with no block
// is only a note that a task ended.
func (b *builder) notice(l line, s string) {
	if !strings.HasPrefix(strings.TrimSpace(s), "<task-notification>") {
		return
	}
	if result, ok := noticeResult(s); ok && (len(result) >= minNoticeResult || len(findings(l, result)) > 0) {
		b.report(l, result)
	}
}

// noticeResult is the text of a task notification's <result>: up to the
// last </result> before </task-notification>, because a report can quote
// </result>.
func noticeResult(s string) (string, bool) {
	_, after, ok := strings.Cut(s, "<result>")
	if !ok {
		return "", false
	}
	if i := strings.LastIndex(after, "</task-notification>"); i >= 0 {
		after = after[:i]
	}
	i := strings.LastIndex(after, "</result>")
	if i < 0 {
		return "", false
	}
	return strings.TrimSpace(after[:i]), true
}

// report adds a subagent's report. A report with a findings block is a
// review that a subagent ran, so it counts from before the first review
// start too.
func (b *builder) report(l line, text string) {
	found := findings(l, text)
	if len(found) > 0 {
		b.markReviewed()
	}
	e := newEntry(l, KindSubagent)
	e.Text = cut(strings.TrimSpace(text), maxSubagent)
	b.addGated(e)
	b.events = append(b.events, found...)
}

// findings are the findings entries of a text, one for each
// ```solidtrace-findings block in it, in order.
func findings(l line, text string) []Entry {
	var out []Entry
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != findingsFence {
			continue
		}
		start, end := i+1, -1
		for j := start; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				end = j
				break
			}
		}
		if end < 0 {
			e := newEntry(l, KindFindings)
			e.Findings, e.Error = json.RawMessage("null"), "the solidtrace-findings block has no closing ```"
			return append(out, e)
		}
		i = end
		out = append(out, blockEntry(l, []byte(strings.Join(lines[start:end], "\n"))))
	}
	return out
}

// blockEntry is the findings entry of the JSON of one block.
func blockEntry(l line, body []byte) Entry {
	e := newEntry(l, KindFindings)
	e.Findings = json.RawMessage("null")
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		e.Error = err.Error()
		return e
	}
	if _, ok := v.(map[string]any); !ok {
		e.Error = "the solidtrace-findings block is not a JSON object"
		return e
	}
	// json.Compact keeps the block's own key order and its <, > and &.
	var compact bytes.Buffer
	_ = json.Compact(&compact, body)
	e.Findings = compact.Bytes()
	return e
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

// queuedNotice is the text of a background task's notice that Claude Code
// queued while the agent worked.
func queuedNotice(l line) (string, bool) {
	var a queuedCommand
	if json.Unmarshal(l.Attachment, &a) != nil || a.Type != "queued_command" || a.CommandMode != "task-notification" {
		return "", false
	}
	return contentText(a.Prompt)
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

// paths makes the paths of a transcript's edits relative to their git
// top level.
type paths struct {
	tops topLevels
	// start is the first cwd of the transcript.
	start string
}

func (p *paths) see(cwd string) {
	if p.start == "" {
		p.start = cwd
	}
}

// relative is path relative to the git top level of cwd. A worktree is
// often removed when its work ends, so for a cwd that is not there now the
// top level is the .claude/worktrees/<name> folder that holds cwd. Else,
// when no folder above cwd holds .git, it is the transcript's first cwd
// when that holds cwd: the top level of a worktree outside the
// repository, where the agent started.
func (p *paths) relative(cwd, path string) string {
	if cwd != "" && filepath.IsAbs(path) {
		if _, err := os.Stat(cwd); err != nil {
			tops := []string{claudeWorktree(cwd)}
			if p.tops.top(cwd) == "" {
				tops = append(tops, p.start)
			}
			for _, top := range tops {
				if top == "" || !inside(cwd, []string{top}) {
					continue
				}
				if rel, ok := within(top, path); ok {
					return rel
				}
			}
		}
	}
	return p.tops.relative(cwd, path)
}

// claudeWorktree is the folder of the Claude Code worktree that holds dir,
// <repository>/.claude/worktrees/<name>, or "" for none.
func claudeWorktree(dir string) string {
	sep := string(filepath.Separator)
	marker := sep + ".claude" + sep + "worktrees" + sep
	dir = filepath.Clean(dir)
	i := strings.LastIndex(dir+sep, marker)
	if i < 0 {
		return ""
	}
	name, _, _ := strings.Cut(dir[i+len(marker):], sep)
	if name == "" {
		return ""
	}
	return dir[:i+len(marker)+len(name)]
}

// within is path relative to top, when path is in top.
func within(top, path string) (string, bool) {
	rel, err := filepath.Rel(top, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// topLevels keeps the git top level of each cwd that relative looked up.
type topLevels map[string]string

// relative is path relative to the git top level of cwd, when path is in
// it. Else it is path.
func (tops topLevels) relative(cwd, path string) string {
	if cwd == "" || !filepath.IsAbs(path) {
		return path
	}
	if top := tops.top(cwd); top != "" {
		if rel, ok := within(top, path); ok {
			return rel
		}
	}
	return path
}

func (tops topLevels) top(cwd string) string {
	top, ok := tops[cwd]
	if !ok {
		top = gitTopLevel(cwd)
		tops[cwd] = top
	}
	return top
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
