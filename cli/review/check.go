package review

import (
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
}

type record struct {
	Sessions []SessionTitle `json:"sessions"`
	// Round is raw, so that a round of null or 1.5 is a problem and not 0.
	Round   json.RawMessage `json:"round"`
	Story   map[string]any  `json:"story"`
	Reviews []struct {
		Run      int    `json:"run"`
		BlockRun int    `json:"block_run"`
		Session  string `json:"session"`
	} `json:"reviews"`
	Findings      []recordFinding `json:"findings"`
	Confirmations []struct {
		ID    string `json:"id"`
		Given any    `json:"given"`
	} `json:"confirmations"`
}

type recordFinding struct {
	ID      string `json:"id"`
	BlockID string `json:"block_id"`
	Run     *int   `json:"run"`
	FoundBy *struct {
		Kind string `json:"kind"`
	} `json:"found_by"`
	Outcome string        `json:"outcome"`
	Events  []recordEvent `json:"events"`
}

type recordEvent struct {
	Type       string   `json:"type"`
	Decision   string   `json:"decision"`
	Asks       string   `json:"asks"`
	PushedBack bool     `json:"pushed_back"`
	Source     string   `json:"source"`
	Evidence   []string `json:"evidence"`
}

func (f recordFinding) has(eventType string) bool {
	return slices.ContainsFunc(f.Events, func(e recordEvent) bool { return e.Type == eventType })
}

// findingsBlock is the part of a solidtrace-findings block that Check reads.
type findingsBlock struct {
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
	var r record
	if err := json.Unmarshal(recordJSON, &r); err != nil {
		return CheckResult{}, fmt.Errorf("record: %w", err)
	}
	c := checker{d: d, r: r, res: CheckResult{
		Runs: len(r.Reviews), Outcomes: map[string]int{}, FoundBy: map[string]int{}, Problems: []string{},
	}}
	c.uuids = map[string]bool{}
	for _, e := range d.Events {
		c.uuids[e.UUID] = true
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
	d     Digest
	r     record
	uuids map[string]bool
	res   CheckResult
}

func (c *checker) problem(format string, args ...any) {
	c.res.Problems = append(c.res.Problems, fmt.Sprintf(format, args...))
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
			var b findingsBlock
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

func (c *checker) finding(f recordFinding) {
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
	fixed := f.has("fix")
	if f.Outcome == "fixed" && !fixed {
		c.problem("%s: the outcome is fixed, and no event is a fix", id)
	}
	if need, ok := outcomeDecision[f.Outcome]; ok {
		// record.md: a finding is also deferred when the person said to do
		// it later.
		personDefers := f.Outcome == "deferred" &&
			slices.ContainsFunc(f.Events, func(e recordEvent) bool { return e.Type == "person" && e.Asks == "defer" })
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
			if !c.uuids[u] {
				c.problem("%s: the evidence %s is not an event of the digest", id, u)
			}
		}
	}
}

// blocks checks that each finding of each block, update blocks too, is in
// the record, with at least as many block events that cite the block as
// the block gives.
func (c *checker) blocks() {
	for _, e := range c.d.Events {
		if e.Kind != KindFindings || len(e.Findings) == 0 || string(e.Findings) == "null" {
			continue
		}
		var b findingsBlock
		if err := json.Unmarshal(e.Findings, &b); err != nil {
			c.problem("the block %s cannot be read: %v", e.UUID, err)
			continue
		}
		for _, bf := range b.Findings {
			cited := 0
			for _, f := range c.r.Findings {
				if f.BlockID != bf.ID {
					continue
				}
				for _, ev := range f.Events {
					if ev.Source == "block" && slices.Contains(ev.Evidence, e.UUID) {
						cited++
					}
				}
			}
			switch {
			case cited == 0:
				c.problem("%s of the block %s is not in the record", bf.ID, e.UUID)
			case cited < len(bf.Events):
				c.problem("%s of the block %s: the record has %d block events, and the block has %d", bf.ID, e.UUID, cited, len(bf.Events))
			}
		}
	}
}

// commits checks that each finding that a commit's Review-Findings trailer
// names has a fix event in the record. The trailer's run is the block's
// run in the commit's session.
func (c *checker) commits() {
	type key struct {
		session  string
		blockRun int
	}
	runs := map[key]int{}
	for _, rv := range c.r.Reviews {
		runs[key{rv.Session, rv.BlockRun}] = rv.Run
	}
	for _, e := range c.d.Events {
		for _, value := range e.ReviewFindings {
			blockRun, ids, err := ParseReviewFindings(value)
			if err != nil {
				c.problem("the commit %s: %v", e.UUID, err)
				continue
			}
			run, hasRun := runs[key{e.Session, blockRun}]
			for _, id := range ids {
				i := slices.IndexFunc(c.r.Findings, func(f recordFinding) bool {
					return hasRun && f.Run != nil && *f.Run == run && f.BlockID == id
				})
				if i < 0 || !c.r.Findings[i].has("fix") {
					c.problem("the commit %s: %q names %s, which has no fix event in the record", e.UUID, value, id)
				}
			}
		}
	}
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
