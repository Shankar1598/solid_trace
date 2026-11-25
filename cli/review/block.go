package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The values that a solidtrace-findings block (code-review/findings.md,
// version 2) can give.
var (
	blockSkills     = []string{"code-review", "deep-review", "update-pr"}
	blockTriggers   = []string{"person", "agent", "open-pr", "update-pr"}
	blockStages     = []string{"step", "final", "pr"}
	findingKinds    = []string{"hard", "judgement"}
	severities      = []string{"high", "medium", "low"}
	categories      = []string{"correctness", "spec", "security", "availability", "performance", "data-loss", "test-gap", "design", "other"}
	verdicts        = []string{"CONFIRMED", "PLAUSIBLE", "REFUTED"}
	blockFoundBy    = []string{"person", "reviewer", "test"} // not agent: only the record's writer finds an agent finding
	runEventTypes   = []string{"found", "refound", "triage", "verify", "fix"}
	updateEventKind = []string{"triage", "fix"}
)

var (
	findingID      = regexp.MustCompile(`^F[1-9][0-9]*$`)
	confirmationID = regexp.MustCompile(`^C[1-9][0-9]*$`)
	commitHash     = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// CheckBlock checks one solidtrace-findings block, a run block or an
// update block, and gives it as compact JSON in its own key order. A field
// that the format does not have is a problem, so that a typo fails at
// once. With problems, the JSON is nil.
func CheckBlock(data []byte) (json.RawMessage, []string) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if err == io.EOF {
			return nil, []string{"no JSON"}
		}
		return nil, []string{"not valid JSON: " + err.Error()}
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, []string{"text after the JSON"}
	}
	c := blockChecker{}
	c.block(v)
	if len(c.problems) > 0 {
		return nil, c.problems
	}
	var compact bytes.Buffer
	_ = json.Compact(&compact, data)
	return bytes.TrimSpace(compact.Bytes()), nil
}

// FenceBlock is compact block JSON as one fenced solidtrace-findings
// block, with a new line at the end.
func FenceBlock(compact []byte) string {
	return findingsFence + "\n" + string(compact) + "\n```\n"
}

// blockCommand is true when a Bash command runs solidtrace review block or
// solidtrace review fixed, which print checked blocks. The binary can be
// called by its path. bodies are the blocks that the command gives to
// review block on stdin, when each review block of the command reads a
// heredoc or a here-string and no review fixed is in it: Claude Code keeps
// only the start of a long output. A review command whose output goes to
// a pipe or a file, and that gives no block on stdin, is not a call: the
// tool result does not hold what it printed. block is true when the
// command runs review block, which always gives a block.
func blockCommand(command string) (bodies []string, ok, block bool) {
	fromStdin := true
	for _, c := range shellCommands(command) {
		w := commandWords(c)
		if len(w) < 3 || filepath.Base(w[0]) != "solidtrace" || w[1] != "review" {
			continue
		}
		switch {
		case w[2] == "block" && len(c.stdin) > 0:
			ok, block = true, true
			// As in Bash, the last of more redirections is the stdin.
			bodies = append(bodies, c.stdin[len(c.stdin)-1])
		case (w[2] == "block" || w[2] == "fixed") && !c.outputAway:
			ok, fromStdin = true, false
			block = block || w[2] == "block"
		}
	}
	if !fromStdin {
		bodies = nil
	}
	return bodies, ok, block
}

type blockChecker struct {
	problems []string
}

func (c *blockChecker) problem(path, format string, args ...any) {
	c.problems = append(c.problems, path+": "+fmt.Sprintf(format, args...))
}

// object gives v as an object when it is one with all of required and no
// field that is not in required or optional.
func (c *blockChecker) object(path string, v any, required, optional []string) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		c.problem(path, "is not an object")
		return nil, false
	}
	var unknown []string
	for k := range m {
		if !slices.Contains(required, k) && !slices.Contains(optional, k) {
			unknown = append(unknown, k)
		}
	}
	slices.Sort(unknown)
	for _, k := range unknown {
		c.problem(path, "the field %q is not known", k)
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			c.problem(path, "no %q", k)
		}
	}
	return m, true
}

// The checks of a field take the object and the key, and skip a field
// that is not there: object already named it.

func (c *blockChecker) text(m map[string]any, path, key string, nullable bool) {
	v, ok := m[key]
	if !ok {
		return
	}
	s, isText := v.(string)
	switch {
	case v == nil && nullable:
	case !isText:
		c.problem(path+key, "is not a string")
	case s == "":
		c.problem(path+key, "is empty")
	}
}

func (c *blockChecker) oneOf(m map[string]any, path, key string, values []string) {
	v, ok := m[key]
	if !ok {
		return
	}
	if s, isText := v.(string); !isText || !slices.Contains(values, s) {
		c.problem(path+key, "%s is not one of %s", show(v), strings.Join(values, ", "))
	}
}

// integer checks that a field is an integer of at least least.
func (c *blockChecker) integer(m map[string]any, path, key string, least int64, nullable bool) {
	v, ok := m[key]
	if !ok || v == nil && nullable {
		return
	}
	n, isNumber := v.(json.Number)
	i, err := n.Int64()
	if !isNumber || err != nil || i < least {
		c.problem(path+key, "%s is not an integer of %d or more", show(v), least)
	}
}

func (c *blockChecker) boolean(m map[string]any, path, key string) {
	if v, ok := m[key]; ok {
		if _, isBool := v.(bool); !isBool {
			c.problem(path+key, "is not true or false")
		}
	}
}

func (c *blockChecker) list(m map[string]any, path, key string) []any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	l, isList := v.([]any)
	if !isList {
		c.problem(path+key, "is not a list")
	}
	return l
}

func (c *blockChecker) texts(m map[string]any, path, key string) {
	for i, s := range c.list(m, path, key) {
		c.text(map[string]any{"": s}, fmt.Sprintf("%s%s[%d]", path, key, i), "", false)
	}
}

func (c *blockChecker) block(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		c.problem("the block", "is not an object")
		return
	}
	_, isUpdate := m["update"]
	if _, isRun := m["run"]; isRun && isUpdate {
		c.problem("the block", "has run and update: a block is a run block or an update block")
		return
	}
	if isUpdate {
		c.updateBlock(m)
	} else {
		c.runBlock(m)
	}
}

func (c *blockChecker) version(m map[string]any) {
	if v, ok := m["version"]; ok {
		if n, isNumber := v.(json.Number); !isNumber || n.String() != "2" {
			c.problem("version", "%s is not 2", show(v))
		}
	}
}

func (c *blockChecker) runBlock(v map[string]any) {
	m, _ := c.object("the block", v,
		[]string{"version", "run", "skill", "trigger", "stage", "step", "base", "head", "reviewers", "verifier", "findings", "confirmations", "dropped"},
		[]string{"skipped"})
	c.version(m)
	c.integer(m, "", "run", 1, false)
	c.oneOf(m, "", "skill", blockSkills)
	c.oneOf(m, "", "trigger", blockTriggers)
	c.oneOf(m, "", "stage", blockStages)
	c.text(m, "", "step", true)
	c.text(m, "", "base", false)
	c.text(m, "", "head", false)
	for i, r := range c.list(m, "", "reviewers") {
		path := fmt.Sprintf("reviewers[%d]", i)
		r, _ := c.object(path, r, []string{"name", "model", "effort"}, nil)
		for _, k := range []string{"name", "model", "effort"} {
			c.text(r, path+".", k, false)
		}
	}
	if verifier := m["verifier"]; verifier != nil {
		r, _ := c.object("verifier", verifier, []string{"model", "effort"}, nil)
		c.text(r, "verifier.", "model", false)
		c.text(r, "verifier.", "effort", false)
	}
	if skipped, ok := m["skipped"]; ok {
		s, _ := skipped.(string)
		if s != "no code" && s != "person" && !strings.HasPrefix(s, "covered by ") {
			c.problem("skipped", "%s is not \"no code\", \"person\" or \"covered by <head>\"", show(skipped))
		}
	}
	seen := map[string]bool{}
	for i, f := range c.list(m, "", "findings") {
		c.finding(fmt.Sprintf("findings[%d]", i), f, seen)
	}
	seen = map[string]bool{}
	for i, conf := range c.list(m, "", "confirmations") {
		path := fmt.Sprintf("confirmations[%d]", i)
		r, _ := c.object(path, conf, []string{"id", "reviewer", "ask"}, nil)
		c.id(r, path+".", confirmationID, "C<n>", seen)
		c.text(r, path+".", "reviewer", false)
		c.text(r, path+".", "ask", false)
	}
	if dropped, ok := m["dropped"]; ok {
		d, isObject := dropped.(map[string]any)
		if !isObject {
			c.problem("dropped", "is not an object")
		}
		names := make([]string, 0, len(d))
		for name := range d {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			c.integer(d, "dropped.", name, 0, false)
		}
	}
}

func (c *blockChecker) updateBlock(v map[string]any) {
	m, _ := c.object("the block", v, []string{"version", "update", "findings"}, nil)
	c.version(m)
	c.integer(m, "", "update", 1, false)
	findings := c.list(m, "", "findings")
	if _, isList := m["findings"].([]any); isList && len(findings) == 0 {
		c.problem("findings", "is empty: an update block adds events to one or more findings")
	}
	seen := map[string]bool{}
	for i, f := range findings {
		path := fmt.Sprintf("findings[%d]", i)
		r, _ := c.object(path, f, []string{"id", "events"}, nil)
		c.id(r, path+".", findingID, "F<n>", seen)
		c.events(r, path+".", updateEventKind)
	}
}

// id checks the id of m, and that no other object of the block has it.
func (c *blockChecker) id(m map[string]any, path string, pattern *regexp.Regexp, form string, seen map[string]bool) {
	v, ok := m["id"]
	if !ok {
		return
	}
	s, _ := v.(string)
	switch {
	case !pattern.MatchString(s):
		c.problem(path+"id", "%s is not of the form %s", show(v), form)
	case seen[s]:
		c.problem(path+"id", "%s is in the block more than once", s)
	}
	seen[s] = true
}

func (c *blockChecker) finding(path string, v any, seen map[string]bool) {
	m, ok := c.object(path, v,
		[]string{"id", "reviewer", "kind", "severity", "category", "file", "line", "title", "scenario", "events"},
		[]string{"also", "found_by"})
	if !ok {
		return
	}
	path += "."
	c.id(m, path, findingID, "F<n>", seen)
	c.text(m, path, "reviewer", false)
	c.oneOf(m, path, "kind", findingKinds)
	c.oneOf(m, path, "severity", severities)
	c.oneOf(m, path, "category", categories)
	c.text(m, path, "file", true)
	c.integer(m, path, "line", 1, true)
	c.text(m, path, "title", false)
	c.text(m, path, "scenario", false)
	c.texts(m, path, "also")
	if fb, ok := m["found_by"]; ok {
		r, _ := c.object(path+"found_by", fb, []string{"kind", "name"}, []string{"access", "self", "ref"})
		c.oneOf(r, path+"found_by.", "kind", blockFoundBy)
		for _, k := range []string{"name", "access", "ref"} {
			c.text(r, path+"found_by.", k, false)
		}
		c.boolean(r, path+"found_by.", "self")
	}
	events := c.events(m, path, runEventTypes)
	if len(events) > 0 {
		if e, ok := events[0].(map[string]any); ok && e["type"] != "found" && e["type"] != "refound" {
			c.problem(path+"events[0]", "is %s: the first event of a finding is found or refound", show(e["type"]))
		}
	}
}

// events checks the events of a finding, each of a type in types.
func (c *blockChecker) events(m map[string]any, path string, types []string) []any {
	events := c.list(m, path, "events")
	if _, isList := m["events"].([]any); isList && len(events) == 0 {
		c.problem(path+"events", "is empty")
	}
	for i, e := range events {
		c.event(fmt.Sprintf("%sevents[%d]", path, i), e, types)
	}
	return events
}

func (c *blockChecker) event(path string, v any, types []string) {
	m, ok := v.(map[string]any)
	if !ok {
		c.problem(path, "is not an object")
		return
	}
	t, _ := m["type"].(string)
	if !slices.Contains(types, t) {
		c.problem(path+".type", "%s is not one of %s", show(m["type"]), strings.Join(types, ", "))
		return
	}
	in := path + "."
	switch t {
	case "found", "refound":
		c.object(path, m, []string{"type"}, nil)
	case "triage":
		required := []string{"type", "decision"}
		if m["decision"] != "fix" {
			required = append(required, "why")
		}
		c.object(path, m, required, []string{"why", "tradeoffs", "approved_by"})
		c.oneOf(m, in, "decision", decisions)
		c.text(m, in, "why", false)
		c.text(m, in, "tradeoffs", false)
		c.oneOf(m, in, "approved_by", []string{"author"})
	case "verify":
		c.object(path, m, []string{"type", "verdict", "pushed_back", "why"}, nil)
		c.oneOf(m, in, "verdict", verdicts)
		c.boolean(m, in, "pushed_back")
		c.text(m, in, "why", false)
	case "fix":
		c.object(path, m, []string{"type", "commit", "files"}, nil)
		if commit, ok := m["commit"]; ok {
			if s, _ := commit.(string); !commitHash.MatchString(s) {
				c.problem(in+"commit", "%s is not the full hash of a commit", show(commit))
			}
		}
		c.texts(m, in, "files")
	}
}

// show is v as JSON, for a problem.
func show(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
