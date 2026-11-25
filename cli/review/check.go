package review

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The values that a record (open-pr/record.md, version 2) can give.
var (
	outcomes     = []string{"fixed", "ignored_by_person", "disputed", "deferred", "ignored_trivial", "open"}
	eventKind    = []string{"found", "refound", "triage", "verify", "person", "agent", "fix"}
	decisions    = []string{"fix", "dispute", "trivial", "defer", "ask"}
	foundBy      = []string{"reviewer", "person", "agent", "test"}
	eventSources = []string{"block", "record"}
	storyKeys    = []string{"started_from", "changes", "reviews", "sessions"}
)

// outcomeDecision is the last triage decision that an outcome needs.
var outcomeDecision = map[string]string{"ignored_trivial": "trivial", "disputed": "dispute", "deferred": "defer"}

// CheckResult is what Check found in a record.
type CheckResult struct {
	Runs       int            `json:"runs"`
	Outcomes   map[string]int `json:"outcomes"`
	FoundBy    map[string]int `json:"found_by"`
	PushedBack int            `json:"pushed_back"`
	Problems   []string       `json:"problems"`
	// Warnings are what the record cannot hold, such as a block of the
	// digest that cannot be read.
	Warnings []string `json:"warnings"`
}

// checkedRecord is the part of a record that Check reads.
type checkedRecord struct {
	Sessions []SessionTitle `json:"sessions"`
	// Round is raw, so that a round of null or 1.5 is a problem and not 0.
	Round   json.RawMessage `json:"round"`
	Story   map[string]any  `json:"story"`
	Reviews []struct {
		Run      int    `json:"run"`
		BlockRun int    `json:"block_run"`
		Session  string `json:"session"`
		Writer   string `json:"writer"`
		Start    string `json:"start"`
	} `json:"reviews"`
	Findings      []checkedFinding `json:"findings"`
	Confirmations []struct {
		ID    string `json:"id"`
		Given any    `json:"given"`
	} `json:"confirmations"`
}

type checkedFinding struct {
	ID      string `json:"id"`
	BlockID string `json:"block_id"`
	Run     *int   `json:"run"`
	FoundBy *struct {
		Kind string `json:"kind"`
	} `json:"found_by"`
	Outcome string         `json:"outcome"`
	Events  []checkedEvent `json:"events"`
}

type checkedEvent struct {
	Type string `json:"type"`
	// Run is the run of a refound event.
	Run        *int     `json:"run"`
	Decision   string   `json:"decision"`
	Asks       string   `json:"asks"`
	PushedBack bool     `json:"pushed_back"`
	Source     string   `json:"source"`
	Evidence   []string `json:"evidence"`
}

func (f checkedFinding) has(eventType string) bool {
	return slices.ContainsFunc(f.Events, func(e checkedEvent) bool { return e.Type == eventType })
}

// fixStands is true when a fix event comes after the last refound event:
// record.md counts a fix only when no later run reports the finding again.
func (f checkedFinding) fixStands() bool {
	types := make([]string, len(f.Events))
	for i, e := range f.Events {
		types[i] = e.Type
	}
	return fixAfterRefound(types)
}

// placedEvent is the type of an event and its place: the seq of its first
// evidence in the digest.
type placedEvent struct {
	kind  string
	place int
}

// fixCovers is true when a fix event and the commit at the place commit
// have no refound event between them. Then a Review-Findings trailer of the
// commit needs no fix of its own; a fix before a later run found the
// finding again does not cover a commit after that run.
func fixCovers(events []placedEvent, commit int) bool {
	events = slices.Clone(events)
	slices.SortStableFunc(events, func(a, b placedEvent) int { return cmp.Compare(a.place, b.place) })
	covered := false
	for _, e := range events {
		switch {
		case e.kind == "refound" && e.place > commit:
			return covered
		case e.kind == "refound":
			covered = false
		case e.kind == "fix":
			covered = true
		}
	}
	return covered
}

func fixAfterRefound(types []string) bool {
	fix, refound := -1, -1
	for i, t := range types {
		switch t {
		case "fix":
			fix = i
		case "refound":
			refound = i
		}
	}
	return fix > refound
}

// inRun is true when f is a finding of run: its own run, or a run that
// found it again.
func (f checkedFinding) inRun(run int) bool {
	if f.Run != nil && *f.Run == run {
		return true
	}
	return slices.ContainsFunc(f.Events, func(e checkedEvent) bool { return e.Type == "refound" && e.Run != nil && *e.Run == run })
}

// checkedBlock is the part of a solidtrace-findings block that Check reads.
type checkedBlock struct {
	Update   any `json:"update"`
	Findings []struct {
		ID     string            `json:"id"`
		Events []json.RawMessage `json:"events"`
	} `json:"findings"`
}

var roundPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// Check checks a record against the digest that it was made from: that each
// value is known, that each claim cites events of the digest, that each
// finding of each block is in the record, and that each outcome agrees with
// its events. An error is a digest or a record that is not valid JSON of
// their shape.
func Check(digestJSON, recordJSON []byte) (CheckResult, error) {
	var d Digest
	if err := json.Unmarshal(digestJSON, &d); err != nil {
		return CheckResult{}, fmt.Errorf("digest: %w", err)
	}
	var r checkedRecord
	if err := json.Unmarshal(recordJSON, &r); err != nil {
		return CheckResult{}, fmt.Errorf("record: %w", err)
	}
	c := checker{d: d, r: r, seq: map[string]int{}, res: CheckResult{
		Runs: len(r.Reviews), Outcomes: map[string]int{}, FoundBy: map[string]int{}, Problems: []string{}, Warnings: []string{},
	}}
	for i, e := range d.Events {
		if _, ok := c.seq[e.UUID]; !ok {
			c.seq[e.UUID] = i
		}
	}
	c.top()
	for _, f := range r.Findings {
		c.finding(f)
	}
	c.blocks()
	c.commits()
	for _, conf := range r.Confirmations {
		if _, ok := conf.Given.(bool); !ok {
			c.problem("%s: given is not true or false", conf.ID)
		}
	}
	return c.res, nil
}

type checker struct {
	d Digest
	r checkedRecord
	// seq is the place of each uuid in the digest.
	seq map[string]int
	res CheckResult
}

func (c *checker) problem(format string, args ...any) {
	c.res.Problems = append(c.res.Problems, fmt.Sprintf(format, args...))
}

func (c *checker) warn(format string, args ...any) {
	c.res.Warnings = append(c.res.Warnings, fmt.Sprintf(format, args...))
}

func (c *checker) top() {
	if c.r.Round != nil && !roundPattern.Match(c.r.Round) {
		c.problem("round %s is not an integer of 1 or more", c.r.Round)
	}
	for _, key := range storyKeys {
		if empty(c.r.Story[key]) {
			c.problem("story.%s is empty", key)
		}
	}
	want, got := sessionIDs(c.d.Sessions), sessionIDs(c.r.Sessions)
	if len(want) > 0 && !slices.Equal(want, got) {
		c.problem("the sessions of the record %v are not the sessions of the digest %v", got, want)
	}
	runs := 0
	for _, e := range c.d.Events {
		if e.Kind == KindReviewStart {
			runs++
		}
		if e.Kind == KindFindings {
			// A block that is not valid JSON is null in the digest, and is
			// still a run.
			var b checkedBlock
			_ = json.Unmarshal(e.Findings, &b)
			if empty(b.Update) {
				runs++
			}
		}
	}
	if runs > 0 && len(c.r.Reviews) == 0 {
		c.problem("the digest has %d runs, and the record has no reviews", runs)
	}
}

func (c *checker) finding(f checkedFinding) {
	id := f.ID
	c.res.Outcomes[f.Outcome]++
	if !slices.Contains(outcomes, f.Outcome) {
		c.problem("%s: the outcome %q is not known", id, f.Outcome)
	}
	kind := ""
	if f.FoundBy != nil {
		kind = f.FoundBy.Kind
	}
	c.res.FoundBy[kind]++
	if !slices.Contains(foundBy, kind) {
		c.problem("%s: the found_by kind %q is not known", id, kind)
	}
	if len(f.Events) == 0 {
		c.problem("%s: no events", id)
	}
	last := ""
	for _, e := range f.Events {
		if e.Type == "triage" {
			last = e.Decision
		}
	}
	fixed := f.fixStands()
	switch {
	case f.Outcome == "fixed" && !f.has("fix"):
		c.problem("%s: the outcome is fixed, and no event is a fix", id)
	case f.Outcome == "fixed" && !fixed:
		c.problem("%s: the outcome is fixed, and a later run found it again", id)
	}
	if need, ok := outcomeDecision[f.Outcome]; ok {
		// record.md: a finding is also deferred when the person said to do
		// it later.
		personDefers := f.Outcome == "deferred" &&
			slices.ContainsFunc(f.Events, func(e checkedEvent) bool { return e.Type == "person" && e.Asks == "defer" })
		switch {
		case fixed:
			c.problem("%s: the outcome is %s, and an event is a fix", id, f.Outcome)
		case last != need && !personDefers:
			c.problem("%s: the outcome is %s, and the last triage decision is %q, not %q", id, f.Outcome, last, need)
		}
	}
	for _, e := range f.Events {
		if !slices.Contains(eventKind, e.Type) {
			c.problem("%s: the event type %q is not known", id, e.Type)
		}
		if e.Type == "triage" && !slices.Contains(decisions, e.Decision) {
			c.problem("%s: the decision %q is not known", id, e.Decision)
		}
		if e.Type == "verify" && e.PushedBack {
			c.res.PushedBack++
		}
		if !slices.Contains(eventSources, e.Source) {
			c.problem("%s: the %s event has the source %q, not block or record", id, e.Type, e.Source)
		}
		if len(e.Evidence) == 0 {
			c.problem("%s: the %s event has no evidence", id, e.Type)
		}
		for _, u := range e.Evidence {
			if _, ok := c.seq[u]; !ok {
				c.problem("%s: the evidence %s is not an event of the digest", id, u)
			}
		}
	}
}

// blocks checks that each finding of each block, update blocks too, is in
// the record, with at least as many block events that cite the block as
// the block gives. The findings of an update block must be findings of
// the run that it names, from the same writer.
func (c *checker) blocks() {
	runs := c.runs()
	for _, e := range c.d.Events {
		if e.Kind == KindFindings && e.Error != "" {
			c.warn("the block %s cannot be read (%s): the record has none of its findings", e.UUID, e.Error)
		}
		if e.Kind != KindFindings || len(e.Findings) == 0 || string(e.Findings) == "null" {
			continue
		}
		var b checkedBlock
		if err := json.Unmarshal(e.Findings, &b); err != nil {
			c.problem("the block %s cannot be read: %v", e.UUID, err)
			continue
		}
		update, isUpdate := b.Update.(float64)
		var updateRuns []int
		if isUpdate {
			updateRuns = runs[runKey{e.Session, e.Writer, int(update)}]
			if len(updateRuns) == 0 {
				c.problem("the update block %s names run %v, and the record has no review of that block_run from its session and writer", e.UUID, update)
			}
		}
		for _, bf := range b.Findings {
			cited := 0
			inRun := false
			for _, f := range c.r.Findings {
				if f.BlockID != bf.ID {
					continue
				}
				citing := false
				for _, ev := range f.Events {
					if ev.Source == "block" && slices.Contains(ev.Evidence, e.UUID) {
						cited++
						citing = true
					}
				}
				inRun = inRun || citing && slices.ContainsFunc(updateRuns, f.inRun)
			}
			switch {
			case cited == 0:
				c.problem("%s of the block %s is not in the record", bf.ID, e.UUID)
			case cited < len(bf.Events):
				c.problem("%s of the block %s: the record has %d block events, and the block has %d", bf.ID, e.UUID, cited, len(bf.Events))
			case len(updateRuns) > 0 && !inRun:
				c.problem("%s of the update block %s is not on a finding of run %v", bf.ID, e.UUID, updateRuns)
			}
		}
	}
}

// runKey is a block's run in the transcript that wrote it.
type runKey struct {
	session, writer string
	blockRun        int
}

// runs are the record's runs of each block run. Older records give a
// subagent's block the main writer, so one key can have more runs.
func (c *checker) runs() map[runKey][]int {
	runs := map[runKey][]int{}
	for _, rv := range c.r.Reviews {
		k := runKey{rv.Session, rv.Writer, rv.BlockRun}
		runs[k] = append(runs[k], rv.Run)
	}
	return runs
}

// commits checks that each finding that a commit's Review-Findings trailer
// names has a fix event that covers the commit (see fixCovers). The
// trailer's run is the latest block of that run before the commit, in the
// commit's session and writer: the main transcript's.
func (c *checker) commits() {
	for _, e := range c.d.Events {
		for _, value := range e.ReviewFindings {
			blockRun, ids, err := ParseReviewFindings(value)
			if err != nil {
				c.problem("the commit %s: %v", e.UUID, err)
				continue
			}
			at := c.seq[e.UUID]
			run := c.runAt(runKey{e.Session, e.Writer, blockRun}, at)
			for _, id := range ids {
				i := slices.IndexFunc(c.r.Findings, func(f checkedFinding) bool {
					return run > 0 && f.BlockID == id && f.inRun(run)
				})
				if i < 0 || !fixCovers(c.placed(c.r.Findings[i]), at) {
					c.problem("the commit %s: %q names %s, which has no fix event in the record", e.UUID, value, id)
				}
			}
		}
	}
}

// runAt is the record's run of the latest block of the block run k before
// the place at, or 0 for none. A writer can give a block run again, as an
// older subagent with no writer of its own or a count that started again.
func (c *checker) runAt(k runKey, at int) int {
	run := 0
	for _, rv := range c.r.Reviews {
		if start, ok := c.seq[rv.Start]; ok && start < at && (runKey{rv.Session, rv.Writer, rv.BlockRun}) == k {
			run = rv.Run
		}
	}
	return run
}

func (c *checker) placed(f checkedFinding) []placedEvent {
	events := make([]placedEvent, len(f.Events))
	for i, e := range f.Events {
		events[i] = placedEvent{e.Type, len(c.d.Events)}
		if len(e.Evidence) > 0 {
			if s, ok := c.seq[e.Evidence[0]]; ok {
				events[i].place = s
			}
		}
	}
	return events
}

// ParseReviewFindings reads the value of a Review-Findings trailer, such as
// "run 1: F2, F5": the block's run and the ids of its findings.
func ParseReviewFindings(value string) (run int, ids []string, err error) {
	head, list, _ := strings.Cut(value, ":")
	words := strings.Fields(head)
	if len(words) > 0 {
		run, err = strconv.Atoi(words[len(words)-1])
	}
	if len(words) == 0 || err != nil || run < 1 {
		return 0, nil, fmt.Errorf("the trailer value %q has no run", value)
	}
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return run, ids, nil
}

func sessionIDs(sessions []SessionTitle) []string {
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// empty is true for a JSON value that is missing, null, false, 0, "", []
// or {}.
func empty(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case bool:
		return !v
	case float64:
		return v == 0
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}
