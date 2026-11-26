package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
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

// recordVersion is the version of the record that review record prints
// and review final takes.
const recordVersion = 2

// ReadFinal decodes data as one review record of version 2, the record
// that open-pr and update-pr give to review final. A field that the record
// does not have, no sessions, no findings, or a round below 1 or above
// 4294967295 (the uint32 that EventStore keeps) is an error.
func ReadFinal(data []byte) (Record, error) {
	var v struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(data, &v) == nil && v.Version != recordVersion {
		return Record{}, fmt.Errorf("version %d is not %d", v.Version, recordVersion)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r Record
	if err := dec.Decode(&r); err != nil {
		if err == io.EOF {
			return Record{}, errors.New("no JSON")
		}
		return Record{}, fmt.Errorf("not a review record: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Record{}, errors.New("text after the record")
	}
	switch {
	case r.Sessions == nil:
		return Record{}, errors.New("no sessions")
	case r.Findings == nil:
		return Record{}, errors.New("no findings")
	case r.Round < 1:
		return Record{}, fmt.Errorf("round %d is not 1 or more", r.Round)
	case r.Round > math.MaxUint32:
		return Record{}, fmt.Errorf("round %d is more than %d", r.Round, uint32(math.MaxUint32))
	}
	return r, nil
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
	// runs has the record's run of the latest block so far of each block
	// run, and members the finding of each block id in each run, refound
	// ones too.
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
		Version:  recordVersion,
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
		ID       string             `json:"id"`
		Reviewer string             `json:"reviewer"`
		Kind     *string            `json:"kind"`
		Severity *string            `json:"severity"`
		Category *string            `json:"category"`
		File     *string            `json:"file"`
		Line     *int               `json:"line"`
		Title    *string            `json:"title"`
		Scenario *string            `json:"scenario"`
		Also     []string           `json:"also"`
		FoundBy  *FoundBy           `json:"found_by"`
		Events   []recordBlockEvent `json:"events"`
	} `json:"findings"`
	Confirmations []struct {
		ID       string `json:"id"`
		Reviewer string `json:"reviewer"`
		Ask      string `json:"ask"`
	} `json:"confirmations"`
}

type recordBlockEvent struct {
	Type       string          `json:"type"`
	Decision   string          `json:"decision"`
	Why        string          `json:"why"`
	Tradeoffs  string          `json:"tradeoffs"`
	ApprovedBy string          `json:"approved_by"`
	Verdict    string          `json:"verdict"`
	PushedBack *bool           `json:"pushed_back"`
	Commit     json.RawMessage `json:"commit"`
	Files      []string        `json:"files"`
}

// blockEvent is the record event of the block event be, of the block
// uuid. A refound event also needs its run.
func blockEvent(be recordBlockEvent, uuid string) RecordEvent {
	ev := RecordEvent{Type: be.Type, Decision: be.Decision, Why: be.Why, Tradeoffs: be.Tradeoffs,
		ApprovedBy: be.ApprovedBy, Verdict: be.Verdict, Source: "block", Evidence: []string{uuid}}
	switch be.Type {
	case "verify":
		ev.PushedBack = boolPtr(be.PushedBack != nil && *be.PushedBack)
	case "fix":
		ev.Files, ev.Commit = filesOf(be.Files), nullIfEmpty(be.Commit)
	}
	return ev
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
			if e.Error != "" {
				b.warn("the block %s cannot be read (%s): its run has no findings", e.UUID, e.Error)
			}
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
			b.warn("the block %s has the run %d of an earlier block of its writer: the update blocks and trailers after it name it", e.UUID, *rb.Run)
		}
		b.runs[k] = run
	}
	for _, bf := range rb.Findings {
		events := make([]RecordEvent, 0, len(bf.Events))
		refound := false
		for _, be := range bf.Events {
			ev := blockEvent(be, e.UUID)
			if be.Type == "refound" {
				refound = true
				ev.Run = intPtr(run)
			}
			events = append(events, ev)
		}
		owner := writerID{e.Session, e.Writer, bf.ID}
		if i, ok := b.last[owner]; ok && refound {
			b.r.Findings[i].Events = append(b.r.Findings[i].Events, events...)
			b.members[memberKey{run, bf.ID}] = i
			continue
		}
		by := FoundBy{Kind: "reviewer", Name: bf.Reviewer}
		if bf.FoundBy != nil {
			by = *bf.FoundBy
		}
		b.r.Findings = append(b.r.Findings, RecordFinding{
			ID: fmt.Sprintf("%d.%s", run, bf.ID), BlockID: textOrNil(bf.ID), Run: intPtr(run), FoundBy: by,
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
			b.r.Findings[i].Events = append(b.r.Findings[i].Events, blockEvent(be, e.UUID))
		}
	}
}

// trailer adds a fix event to each finding that a commit's
// Review-Findings trailer names, when no fix covers the commit (see
// fixCovers), such as the fix of the commit's update block from review
// fixed. The
// trailer's run is the latest block of that run before the commit, in the
// main transcript of the commit's session.
func (b *recordBuilder) trailer(e Entry, value string) {
	blockRun, ids, err := ParseReviewFindings(value)
	if err != nil {
		b.warn("the commit %s: %v", e.UUID, err)
		return
	}
	at := b.seq[e.UUID]
	run := b.runAt(runKey{e.Session, e.Writer, blockRun}, at)
	if run == 0 {
		b.warn("the commit %s: %q names run %d, and its session has no block of that run before the commit", e.UUID, value, blockRun)
		return
	}
	for _, id := range ids {
		i, ok := b.members[memberKey{run, id}]
		if !ok {
			b.warn("the commit %s: %q names %s, which run %d does not have", e.UUID, value, id, run)
			continue
		}
		f := &b.r.Findings[i]
		placed := make([]placedEvent, len(f.Events))
		for j, ev := range f.Events {
			placed[j] = placedEvent{ev.Type, b.place(ev)}
		}
		if fixCovers(placed, at) {
			continue
		}
		f.Events = append(f.Events, RecordEvent{Type: "fix", Files: filesOf(nil), Commit: json.RawMessage("null"),
			Source: "record", Evidence: []string{e.UUID}})
	}
}

// runAt is the record's run of the latest block of the block run k before
// the place at, or 0 for none. A writer can give a block run again, as an
// older subagent with no writer of its own or a count that started again.
func (b *recordBuilder) runAt(k runKey, at int) int {
	run := 0
	for _, rv := range b.r.Reviews {
		if rv.BlockRun != nil && (runKey{rv.Session, rv.Writer, *rv.BlockRun}) == k && b.seq[rv.Start] < at {
			run = rv.Run
		}
	}
	return run
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
