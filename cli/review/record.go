package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Record is the review record of a PR (open-pr/record.md, version 2).
type Record struct {
	Version       int                  `json:"version"`
	Sessions      []SessionTitle       `json:"sessions"`
	PR            *string              `json:"pr"`
	Round         int                  `json:"round"`
	Story         Story                `json:"story"`
	Reviews       []RecordReview       `json:"reviews"`
	Findings      []RecordFinding      `json:"findings"`
	Confirmations []RecordConfirmation `json:"confirmations"`
}

// Story is what the record tells in words. Only the notes give it.
type Story struct {
	StartedFrom string   `json:"started_from"`
	Changes     []string `json:"changes"`
	Reviews     []string `json:"reviews"`
	AfterReview []string `json:"after_review"`
	Sessions    []string `json:"sessions"`
}

// RecordReview is one run of a review. A run from a review start with no
// block has no block_run, stage, step, reviewers or verifier.
type RecordReview struct {
	Run       int             `json:"run"`
	BlockRun  *int            `json:"block_run"`
	Session   string          `json:"session"`
	Writer    string          `json:"writer"`
	Skill     *string         `json:"skill"`
	Trigger   *string         `json:"trigger"`
	Stage     *string         `json:"stage"`
	Step      *string         `json:"step"`
	Skipped   *string         `json:"skipped"`
	Start     string          `json:"start"`
	Reviewers json.RawMessage `json:"reviewers"`
	Verifier  json.RawMessage `json:"verifier"`
}

// RecordFinding is one finding of the record, with all its events.
type RecordFinding struct {
	ID         string        `json:"id"`
	BlockID    *string       `json:"block_id"`
	Run        *int          `json:"run"`
	AfterRun   *int          `json:"after_run,omitempty"`
	FoundBy    FoundBy       `json:"found_by"`
	Also       []string      `json:"also,omitempty"`
	Kind       *string       `json:"kind"`
	Severity   *string       `json:"severity"`
	Category   *string       `json:"category"`
	File       *string       `json:"file"`
	Line       *int          `json:"line"`
	Title      *string       `json:"title"`
	Scenario   *string       `json:"scenario"`
	Events     []RecordEvent `json:"events"`
	Outcome    string        `json:"outcome"`
	Confidence string        `json:"confidence"`
}

// FoundBy is who found a finding.
type FoundBy struct {
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Access string `json:"access,omitempty"`
	Self   bool   `json:"self,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

// RecordEvent is one event of a finding. Each type sets only its own
// fields: refound sets Run; triage sets Decision, Why, Tradeoffs and
// ApprovedBy; verify sets Verdict, PushedBack and Why; person sets Said and
// Asks; agent sets Found and Changed; fix sets Files and Commit.
type RecordEvent struct {
	Type       string          `json:"type"`
	Run        *int            `json:"run,omitempty"`
	Decision   string          `json:"decision,omitempty"`
	Why        string          `json:"why,omitempty"`
	Tradeoffs  string          `json:"tradeoffs,omitempty"`
	ApprovedBy string          `json:"approved_by,omitempty"`
	Verdict    string          `json:"verdict,omitempty"`
	PushedBack *bool           `json:"pushed_back,omitempty"`
	Said       string          `json:"said,omitempty"`
	Asks       string          `json:"asks,omitempty"`
	Found      string          `json:"found,omitempty"`
	Changed    *[]string       `json:"changed,omitempty"`
	Files      *[]string       `json:"files,omitempty"`
	Commit     json.RawMessage `json:"commit,omitempty"`
	Source     string          `json:"source"`
	Evidence   []string        `json:"evidence"`
}

// RecordConfirmation is a check that a reviewer asked the developer to
// run. Given is null until the notes say.
type RecordConfirmation struct {
	ID       string   `json:"id"`
	Run      int      `json:"run"`
	Reviewer string   `json:"reviewer"`
	Ask      string   `json:"ask"`
	Given    *bool    `json:"given"`
	Evidence []string `json:"evidence"`
}

// NotesError is a notes file that BuildRecord refuses, with each problem.
type NotesError struct {
	Problems []string
}

func (e *NotesError) Error() string { return strings.Join(e.Problems, "\n") }

// RecordOptions are what BuildRecord takes besides the digest.
type RecordOptions struct {
	// Notes is the JSON of the notes, or nil for the mechanical part only.
	Notes []byte
	PR    *string
	// Round is 1 or more.
	Round int
}

// BuildRecord makes the record of a digest: its runs, each block finding
// with its events, the events of update blocks, a fix for each finding
// that a Review-Findings trailer names when no block gave one, the
// confirmations and the outcomes. With notes, it adds the story, the
// events and findings that need reading, the confirmations' given and the
// doubt, and works out the outcomes again. The warnings are blocks that
// the record cannot place. An error is a digest that is not valid JSON, or
// a *NotesError.
func BuildRecord(digestJSON []byte, opts RecordOptions) (Record, []string, error) {
	var d Digest
	if err := json.Unmarshal(digestJSON, &d); err != nil {
		return Record{}, nil, fmt.Errorf("digest: %w", err)
	}
	b := newRecordBuilder(d)
	b.r.PR, b.r.Round = opts.PR, opts.Round
	b.build()
	if opts.Notes != nil {
		if problems := b.notes(opts.Notes); len(problems) > 0 {
			return Record{}, nil, &NotesError{Problems: problems}
		}
	}
	b.finish()
	return b.r, b.warnings, nil
}

type recordBuilder struct {
	d        Digest
	r        Record
	warnings []string
	// seq is the place of each uuid in the digest.
	seq map[string]int
	// runs has the record's run of each block run, and members the
	// finding of each block id in each run, refound ones too.
	runs    map[runKey]int
	members map[memberKey]int
	// last is the latest finding of each block id of a writer, for refound.
	last map[writerID]int
	// noBlock are the runs that a review start with no block gave.
	noBlock map[int]bool
}

type memberKey struct {
	run int
	id  string
}

type writerID struct {
	session, writer, id string
}

func newRecordBuilder(d Digest) *recordBuilder {
	b := &recordBuilder{
		d:       d,
		seq:     map[string]int{},
		runs:    map[runKey]int{},
		members: map[memberKey]int{},
		last:    map[writerID]int{},
		noBlock: map[int]bool{},
	}
	for i, e := range d.Events {
		if _, ok := b.seq[e.UUID]; !ok {
			b.seq[e.UUID] = i
		}
	}
	sessions := d.Sessions
	if sessions == nil {
		sessions = []SessionTitle{}
	}
	b.r = Record{
		Version:  2,
		Sessions: sessions,
		Round:    1,
		Story:    Story{Changes: []string{}, Reviews: []string{}, AfterReview: []string{}, Sessions: []string{}},
		Reviews:  []RecordReview{}, Findings: []RecordFinding{}, Confirmations: []RecordConfirmation{},
	}
	return b
}

func (b *recordBuilder) warn(format string, args ...any) {
	b.warnings = append(b.warnings, fmt.Sprintf(format, args...))
}

// recordBlock is the part of a findings block that the record copies. It
// is read leniently: a block from a text was not checked.
type recordBlock struct {
	Run       *int            `json:"run"`
	Update    *int            `json:"update"`
	Skill     *string         `json:"skill"`
	Trigger   *string         `json:"trigger"`
	Stage     *string         `json:"stage"`
	Step      *string         `json:"step"`
	Skipped   *string         `json:"skipped"`
	Reviewers json.RawMessage `json:"reviewers"`
	Verifier  json.RawMessage `json:"verifier"`
	Findings  []struct {
		ID       string   `json:"id"`
		Reviewer string   `json:"reviewer"`
		Kind     *string  `json:"kind"`
		Severity *string  `json:"severity"`
		Category *string  `json:"category"`
		File     *string  `json:"file"`
		Line     *int     `json:"line"`
		Title    *string  `json:"title"`
		Scenario *string  `json:"scenario"`
		Also     []string `json:"also"`
		FoundBy  *FoundBy `json:"found_by"`
		Events   []struct {
			Type       string          `json:"type"`
			Decision   string          `json:"decision"`
			Why        string          `json:"why"`
			Tradeoffs  string          `json:"tradeoffs"`
			ApprovedBy string          `json:"approved_by"`
			Verdict    string          `json:"verdict"`
			PushedBack *bool           `json:"pushed_back"`
			Commit     json.RawMessage `json:"commit"`
			Files      []string        `json:"files"`
		} `json:"events"`
	} `json:"findings"`
	Confirmations []struct {
		ID       string `json:"id"`
		Reviewer string `json:"reviewer"`
		Ask      string `json:"ask"`
	} `json:"confirmations"`
}

func readBlock(raw json.RawMessage) recordBlock {
	var rb recordBlock
	// A field of the wrong type stays empty, and the others are read.
	_ = json.Unmarshal(raw, &rb)
	return rb
}

func (b *recordBuilder) build() {
	withBlock := b.startsWithBlock()
	for i, e := range b.d.Events {
		switch {
		case e.Kind == KindReviewStart && !withBlock[i]:
			run := b.newRun(RecordReview{Session: e.Session, Writer: "", Skill: textOrNil(e.Skill),
				Trigger: textOrNil(string(e.Trigger)), Start: e.UUID})
			b.noBlock[run] = true
		case e.Kind == KindFindings:
			rb := readBlock(e.Findings)
			if rb.Update != nil {
				b.update(e, rb)
			} else {
				b.runBlock(e, rb)
			}
		}
	}
	for _, e := range b.d.Events {
		for _, value := range e.ReviewFindings {
			b.trailer(e, value)
		}
	}
}

// startsWithBlock marks each review start that a run block of its session
// and skill follows before the session's next review start. Such a start
// is not a run of its own: its blocks are. A block with no skill is of
// any skill, and open-pr runs deep-review with no review start.
func (b *recordBuilder) startsWithBlock() map[int]bool {
	open := map[string]int{}
	marked := map[int]bool{}
	for i, e := range b.d.Events {
		switch e.Kind {
		case KindReviewStart:
			open[e.Session] = i
		case KindFindings:
			rb := readBlock(e.Findings)
			start, ok := open[e.Session]
			if !ok || rb.Update != nil {
				continue
			}
			skill := b.d.Events[start].Skill
			if rb.Skill == nil || *rb.Skill == skill[strings.LastIndex(skill, ":")+1:] {
				marked[start] = true
			}
		}
	}
	return marked
}

func (b *recordBuilder) newRun(rv RecordReview) int {
	rv.Run = len(b.r.Reviews) + 1
	b.r.Reviews = append(b.r.Reviews, rv)
	return rv.Run
}

func (b *recordBuilder) runBlock(e Entry, rb recordBlock) {
	run := b.newRun(RecordReview{BlockRun: rb.Run, Session: e.Session, Writer: e.Writer, Skill: rb.Skill,
		Trigger: rb.Trigger, Stage: rb.Stage, Step: rb.Step, Skipped: rb.Skipped, Start: e.UUID,
		Reviewers: nullIfEmpty(rb.Reviewers), Verifier: nullIfEmpty(rb.Verifier)})
	if rb.Run != nil {
		k := runKey{e.Session, e.Writer, *rb.Run}
		if _, ok := b.runs[k]; ok {
			b.warn("the block %s has the run %d of an earlier block of its writer: update blocks and trailers name the later one", e.UUID, *rb.Run)
		}
		b.runs[k] = run
	}
	for _, bf := range rb.Findings {
		events := make([]RecordEvent, 0, len(bf.Events))
		refound := false
		for _, be := range bf.Events {
			ev := RecordEvent{Type: be.Type, Decision: be.Decision, Why: be.Why, Tradeoffs: be.Tradeoffs,
				ApprovedBy: be.ApprovedBy, Verdict: be.Verdict, Source: "block", Evidence: []string{e.UUID}}
			switch be.Type {
			case "refound":
				refound = true
				ev.Run = intPtr(run)
			case "verify":
				ev.PushedBack = boolPtr(be.PushedBack != nil && *be.PushedBack)
			case "fix":
				ev.Files, ev.Commit = filesOf(be.Files), nullIfEmpty(be.Commit)
			}
			events = append(events, ev)
		}
		owner := writerID{e.Session, e.Writer, bf.ID}
		if i, ok := b.last[owner]; ok && refound {
			b.r.Findings[i].Events = append(b.r.Findings[i].Events, events...)
			b.members[memberKey{run, bf.ID}] = i
			continue
		}
		foundBy := FoundBy{Kind: "reviewer", Name: bf.Reviewer}
		if bf.FoundBy != nil {
			foundBy = *bf.FoundBy
		}
		b.r.Findings = append(b.r.Findings, RecordFinding{
			ID: fmt.Sprintf("%d.%s", run, bf.ID), BlockID: textOrNil(bf.ID), Run: intPtr(run), FoundBy: foundBy,
			Also: bf.Also, Kind: bf.Kind, Severity: bf.Severity, Category: bf.Category, File: bf.File,
			Line: bf.Line, Title: bf.Title, Scenario: bf.Scenario, Events: events,
		})
		i := len(b.r.Findings) - 1
		b.last[owner] = i
		b.members[memberKey{run, bf.ID}] = i
	}
	for _, c := range rb.Confirmations {
		b.r.Confirmations = append(b.r.Confirmations, RecordConfirmation{
			ID: fmt.Sprintf("%d.%s", run, c.ID), Run: run, Reviewer: c.Reviewer, Ask: c.Ask, Evidence: []string{},
		})
	}
}

func (b *recordBuilder) update(e Entry, rb recordBlock) {
	run, ok := b.runs[runKey{e.Session, e.Writer, *rb.Update}]
	if !ok {
		b.warn("the update block %s names run %d, and its session and writer have no block of that run", e.UUID, *rb.Update)
		return
	}
	for _, bf := range rb.Findings {
		i, ok := b.members[memberKey{run, bf.ID}]
		if !ok {
			b.warn("the update block %s names %s, which run %d does not have", e.UUID, bf.ID, run)
			continue
		}
		for _, be := range bf.Events {
			ev := RecordEvent{Type: be.Type, Decision: be.Decision, Why: be.Why, Tradeoffs: be.Tradeoffs,
				ApprovedBy: be.ApprovedBy, Verdict: be.Verdict, Source: "block", Evidence: []string{e.UUID}}
			switch be.Type {
			case "verify":
				ev.PushedBack = boolPtr(be.PushedBack != nil && *be.PushedBack)
			case "fix":
				ev.Files, ev.Commit = filesOf(be.Files), nullIfEmpty(be.Commit)
			}
			b.r.Findings[i].Events = append(b.r.Findings[i].Events, ev)
		}
	}
}

// trailer adds a fix event to each finding that a commit's
// Review-Findings trailer names, when no block gave the finding a fix.
// The trailer's run is a run of the main transcript of the commit's
// session.
func (b *recordBuilder) trailer(e Entry, value string) {
	blockRun, ids, err := ParseReviewFindings(value)
	if err != nil {
		b.warn("the commit %s: %v", e.UUID, err)
		return
	}
	run, ok := b.runs[runKey{e.Session, e.Writer, blockRun}]
	if !ok {
		b.warn("the commit %s: %q names run %d, and its session has no block of that run", e.UUID, value, blockRun)
		return
	}
	for _, id := range ids {
		i, ok := b.members[memberKey{run, id}]
		if !ok {
			b.warn("the commit %s: %q names %s, which run %d does not have", e.UUID, value, id, run)
			continue
		}
		f := &b.r.Findings[i]
		if slices.ContainsFunc(f.Events, func(ev RecordEvent) bool { return ev.Type == "fix" }) {
			continue
		}
		f.Events = append(f.Events, RecordEvent{Type: "fix", Files: filesOf(nil), Commit: json.RawMessage("null"),
			Source: "record", Evidence: []string{e.UUID}})
	}
}

// finish puts the events of each finding in time order, by their first
// evidence, and works out each outcome.
func (b *recordBuilder) finish() {
	for i := range b.r.Findings {
		f := &b.r.Findings[i]
		sort.SliceStable(f.Events, func(x, y int) bool { return b.place(f.Events[x]) < b.place(f.Events[y]) })
		f.Outcome = outcome(f.Events)
		if f.Confidence == "" {
			f.Confidence = "high"
		}
	}
}

func (b *recordBuilder) place(e RecordEvent) int {
	if len(e.Evidence) == 0 {
		return len(b.d.Events)
	}
	if s, ok := b.seq[e.Evidence[0]]; ok {
		return s
	}
	return len(b.d.Events)
}

// outcome is the first outcome of record.md that the events prove.
func outcome(events []RecordEvent) string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	if fixAfterRefound(types) {
		return "fixed"
	}
	if slices.ContainsFunc(events, func(e RecordEvent) bool { return e.Type == "person" && e.Asks == "ignore" }) {
		return "ignored_by_person"
	}
	last, lastAt, pushedBack := "", -1, false
	for i, e := range events {
		if e.Type == "triage" {
			last, lastAt = e.Decision, i
		}
	}
	// A push back counts only when no decision came after it.
	for _, e := range events[lastAt+1:] {
		pushedBack = pushedBack || e.Type == "verify" && e.PushedBack != nil && *e.PushedBack
	}
	personDefers := slices.ContainsFunc(events, func(e RecordEvent) bool { return e.Type == "person" && e.Asks == "defer" })
	switch {
	case last == "dispute" && !pushedBack:
		return "disputed"
	case last == "defer" || personDefers:
		return "deferred"
	case last == "trivial" && !pushedBack:
		return "ignored_trivial"
	}
	return "open"
}

// The values that the notes can give.
var (
	noteEventTypes   = []string{"person", "agent", "fix", "triage"}
	noteFindingTypes = []string{"found", "person", "agent", "fix", "triage"}
	asks             = []string{"fix", "ignore", "defer", "dig", "question"}
	noteFindingID    = regexp.MustCompile(`^(P|A|X)[1-9][0-9]*$|^T([1-9][0-9]*)-[1-9][0-9]*$`)
	idFoundBy        = map[string]string{"P": "person", "A": "agent", "X": "test"}
)

// maxSaid is the most characters of a person's words in a person event.
const maxSaid = 200

type notesFile struct {
	Story         *Story            `json:"story"`
	Events        []json.RawMessage `json:"events"`
	Findings      []json.RawMessage `json:"findings"`
	Confirmations []json.RawMessage `json:"confirmations"`
	Confidence    map[string]string `json:"confidence"`
}

type noteFinding struct {
	ID       string            `json:"id"`
	BlockID  *string           `json:"block_id"`
	Run      *int              `json:"run"`
	AfterRun *int              `json:"after_run"`
	FoundBy  *FoundBy          `json:"found_by"`
	Also     []string          `json:"also"`
	Kind     *string           `json:"kind"`
	Severity *string           `json:"severity"`
	Category *string           `json:"category"`
	File     *string           `json:"file"`
	Line     *int              `json:"line"`
	Title    *string           `json:"title"`
	Scenario *string           `json:"scenario"`
	Events   []json.RawMessage `json:"events"`
}

type noteEvent struct {
	Finding  string    `json:"finding"`
	Type     string    `json:"type"`
	Decision string    `json:"decision"`
	Why      string    `json:"why"`
	Said     string    `json:"said"`
	Asks     string    `json:"asks"`
	Found    string    `json:"found"`
	Changed  *[]string `json:"changed"`
	Files    *[]string `json:"files"`
	Commit   *string   `json:"commit"`
	Source   string    `json:"source"`
	Evidence []string  `json:"evidence"`
}

// noteEventFields are the fields of each type of note event, besides
// type, source and evidence.
var noteEventFields = map[string][]string{
	"found":  {},
	"person": {"said", "asks"},
	"agent":  {"found", "changed"},
	"fix":    {"files", "commit"},
	"triage": {"decision", "why"},
}

type notesChecker struct {
	b        *recordBuilder
	problems []string
}

func (c *notesChecker) problem(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

// strict reads data into v, and refuses a field that v does not have.
func strict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("text after the JSON")
	}
	return nil
}

// notes merges the notes into the record, or gives each problem of them.
func (b *recordBuilder) notes(data []byte) []string {
	var n notesFile
	if err := strict(data, &n); err != nil {
		return []string{"the notes: " + err.Error()}
	}
	c := notesChecker{b: b}
	if n.Story != nil {
		b.r.Story = *n.Story
		for _, l := range []*[]string{&b.r.Story.Changes, &b.r.Story.Reviews, &b.r.Story.AfterReview, &b.r.Story.Sessions} {
			if *l == nil {
				*l = []string{}
			}
		}
	}
	for i, raw := range n.Findings {
		c.finding(fmt.Sprintf("findings[%d]", i), raw)
	}
	for i, raw := range n.Events {
		path := fmt.Sprintf("events[%d]", i)
		ev, finding, ok := c.event(path, raw, noteEventTypes, true)
		if !ok {
			continue
		}
		j := b.findingIndex(finding)
		if j < 0 {
			c.problem("%s: the finding %q is not in the record", path, finding)
			continue
		}
		b.r.Findings[j].Events = append(b.r.Findings[j].Events, ev)
	}
	for i, raw := range n.Confirmations {
		c.confirmation(fmt.Sprintf("confirmations[%d]", i), raw)
	}
	ids := make([]string, 0, len(n.Confidence))
	for id := range n.Confidence {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		j := b.findingIndex(id)
		switch level := n.Confidence[id]; {
		case j < 0:
			c.problem("confidence: the finding %q is not in the record", id)
		case level != "low" && level != "high":
			c.problem("confidence.%s: %q is not low or high", id, level)
		default:
			b.r.Findings[j].Confidence = level
		}
	}
	return c.problems
}

func (b *recordBuilder) findingIndex(id string) int {
	return slices.IndexFunc(b.r.Findings, func(f RecordFinding) bool { return f.ID == id })
}

func (c *notesChecker) finding(path string, raw json.RawMessage) {
	var nf noteFinding
	if err := strict(raw, &nf); err != nil {
		c.problem("%s: %v", path, err)
		return
	}
	m := noteFindingID.FindStringSubmatch(nf.ID)
	switch {
	case m == nil:
		c.problem("%s: the id %q is not P<n>, A<n>, X<n> or T<run>-<n>", path, nf.ID)
		return
	case c.b.findingIndex(nf.ID) >= 0:
		c.problem("%s: the id %s is in the record already", path, nf.ID)
		return
	}
	if nf.BlockID != nil {
		c.problem("%s: block_id is not null: only a block gives a finding a block_id", path)
	}
	if nf.FoundBy == nil || !slices.Contains(foundBy, nf.FoundBy.Kind) {
		c.problem("%s: found_by has no kind of %s", path, strings.Join(foundBy, ", "))
	}
	if m[2] != "" {
		run, _ := strconv.Atoi(m[2])
		if !c.b.noBlock[run] || nf.Run == nil || *nf.Run != run {
			c.problem("%s: a T<run>-<n> finding has the run of a review start with no block, and %s names run %s", path, nf.ID, m[2])
		}
	} else {
		if nf.Run != nil {
			c.problem("%s: run is not null: a finding from outside the reviews has after_run", path)
		}
		if want := idFoundBy[m[1]]; nf.FoundBy != nil && nf.FoundBy.Kind != want {
			c.problem("%s: the found_by kind of %s is %s, not %q", path, nf.ID, want, nf.FoundBy.Kind)
		}
	}
	if nf.AfterRun != nil && (*nf.AfterRun < 1 || *nf.AfterRun > len(c.b.r.Reviews)) {
		c.problem("%s: after_run %d is not a run of the record", path, *nf.AfterRun)
	}
	if len(nf.Events) == 0 {
		c.problem("%s: no events", path)
	}
	f := RecordFinding{ID: nf.ID, Run: nf.Run, AfterRun: nf.AfterRun, Also: nf.Also,
		Kind: nf.Kind, Severity: nf.Severity, Category: nf.Category, File: nf.File, Line: nf.Line,
		Title: nf.Title, Scenario: nf.Scenario, Events: []RecordEvent{}}
	if nf.FoundBy != nil {
		f.FoundBy = *nf.FoundBy
	}
	for i, raw := range nf.Events {
		if ev, _, ok := c.event(fmt.Sprintf("%s.events[%d]", path, i), raw, noteFindingTypes, false); ok {
			f.Events = append(f.Events, ev)
		}
	}
	c.b.r.Findings = append(c.b.r.Findings, f)
}

// event reads one note event of a type in types. With named, the event
// names its finding.
func (c *notesChecker) event(path string, raw json.RawMessage, types []string, named bool) (RecordEvent, string, bool) {
	var fields map[string]json.RawMessage
	var ne noteEvent
	if err := json.Unmarshal(raw, &fields); err != nil {
		c.problem("%s: %v", path, err)
		return RecordEvent{}, "", false
	}
	if err := json.Unmarshal(raw, &ne); err != nil {
		c.problem("%s: %v", path, err)
		return RecordEvent{}, "", false
	}
	if !slices.Contains(types, ne.Type) {
		c.problem("%s: the type %q is not one of %s", path, ne.Type, strings.Join(types, ", "))
		return RecordEvent{}, "", false
	}
	allowed := append([]string{"type", "source", "evidence"}, noteEventFields[ne.Type]...)
	if named {
		allowed = append(allowed, "finding")
	}
	var unknown []string
	for k := range fields {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	slices.Sort(unknown)
	for _, k := range unknown {
		c.problem("%s: a %s event has no field %q", path, ne.Type, k)
	}
	ok := len(unknown) == 0
	need := func(field string, given bool) {
		if !given {
			c.problem("%s: a %s event needs %q", path, ne.Type, field)
			ok = false
		}
	}
	if named {
		need("finding", ne.Finding != "")
	}
	ev := RecordEvent{Type: ne.Type, Source: ne.Source, Evidence: ne.Evidence}
	switch ne.Type {
	case "person":
		need("said", ne.Said != "")
		if !slices.Contains(asks, ne.Asks) {
			c.problem("%s: asks %q is not one of %s", path, ne.Asks, strings.Join(asks, ", "))
			ok = false
		}
		ev.Said, ev.Asks = cutSaid(ne.Said), ne.Asks
	case "agent":
		need("found", ne.Found != "")
		ev.Found, ev.Changed = ne.Found, ne.Changed
		if ev.Changed == nil {
			ev.Changed = filesOf(nil)
		}
	case "fix":
		need("files", ne.Files != nil)
		ev.Files, ev.Commit = ne.Files, json.RawMessage("null")
		if ne.Commit != nil {
			ev.Commit, _ = json.Marshal(*ne.Commit)
		}
	case "triage":
		if !slices.Contains(decisions, ne.Decision) {
			c.problem("%s: the decision %q is not one of %s", path, ne.Decision, strings.Join(decisions, ", "))
			ok = false
		}
		if ne.Decision != "fix" {
			need("why", ne.Why != "")
		}
		ev.Decision, ev.Why = ne.Decision, ne.Why
	}
	if ne.Source != "record" {
		c.problem("%s: the source is %q, not record", path, ne.Source)
		ok = false
	}
	need("evidence", len(ne.Evidence) > 0)
	ok = c.evidence(path, ne.Evidence) && ok
	return ev, ne.Finding, ok
}

func (c *notesChecker) evidence(path string, uuids []string) bool {
	ok := true
	for _, u := range uuids {
		if _, found := c.b.seq[u]; !found {
			c.problem("%s: the evidence %q is not an event of the digest", path, u)
			ok = false
		}
	}
	return ok
}

func (c *notesChecker) confirmation(path string, raw json.RawMessage) {
	var nc struct {
		ID       string   `json:"id"`
		Given    *bool    `json:"given"`
		Evidence []string `json:"evidence"`
	}
	if err := strict(raw, &nc); err != nil {
		c.problem("%s: %v", path, err)
		return
	}
	i := slices.IndexFunc(c.b.r.Confirmations, func(rc RecordConfirmation) bool { return rc.ID == nc.ID })
	switch {
	case i < 0:
		c.problem("%s: the confirmation %q is not in the record", path, nc.ID)
	case nc.Given == nil:
		c.problem("%s: given is not true or false", path)
	case c.evidence(path, nc.Evidence):
		c.b.r.Confirmations[i].Given = nc.Given
		c.b.r.Confirmations[i].Evidence = append([]string{}, nc.Evidence...)
	}
}

// cutSaid cuts a person's words to maxSaid characters, with "…" at the end.
func cutSaid(s string) string {
	runes := []rune(s)
	if len(runes) <= maxSaid {
		return s
	}
	return string(runes[:maxSaid-1]) + "…"
}

func textOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(i int) *int    { return &i }
func boolPtr(v bool) *bool { return &v }
func filesOf(files []string) *[]string {
	out := append([]string{}, files...)
	return &out
}

func nullIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
