package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The values that the notes can give.
var (
	noteEventTypes   = []string{"person", "agent", "fix", "triage"}
	noteFindingTypes = []string{"found", "person", "agent", "fix", "triage"}
	personAsks       = []string{"fix", "ignore", "defer", "dig", "question"}
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
		if !slices.Contains(personAsks, ne.Asks) {
			c.problem("%s: asks %q is not one of %s", path, ne.Asks, strings.Join(personAsks, ", "))
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
