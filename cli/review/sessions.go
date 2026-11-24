package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Change is what a PR changes, as git gives it: the facts that link an
// Agent session to the PR.
type Change struct {
	Branch string
	// Subjects has the subject of each of the PR's commits.
	Subjects []string
	Files    []string
	// Roots are the folders of the repository: its top level and each of
	// its worktrees.
	Roots []string
}

// Session is an Agent session on this machine with its links to a PR.
type Session struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	First   time.Time `json:"first,omitzero"`
	Last    time.Time `json:"last,omitzero"`
	Current bool      `json:"current"`
	// Strong is true when the session made a commit of the PR.
	Strong bool  `json:"strong"`
	Links  Links `json:"links"`
}

// Links are what link an Agent session to a PR.
type Links struct {
	// Commits is how many of the PR's commits have a subject that a git
	// commit or git merge command of the session holds.
	Commits int `json:"commits"`
	// BranchLines is how many lines of the transcripts the PR's branch
	// was checked out for.
	BranchLines int `json:"branch_lines"`
	// Files is how many of the PR's files the session changed.
	Files int `json:"files"`
}

// Any is true when the session has a link to the PR.
func (l Links) Any() bool { return l.Commits > 0 || l.BranchLines > 0 || l.Files > 0 }

// SessionList is the Agent sessions that can be the ones that made a PR.
type SessionList struct {
	Branch   string    `json:"branch"`
	Base     string    `json:"base"`
	Commits  int       `json:"commits"`
	Files    int       `json:"files"`
	Sessions []Session `json:"sessions"`
}

// Link reads the transcripts of the Agent session id, its main transcript
// and its subagents' transcripts, for its links to the PR of c. inRepo is
// true when a line has a cwd in one of c's Roots.
func Link(id, main string, subagents []string, c Change) (s Session, inRepo bool, err error) {
	k := linker{
		c:        c,
		subjects: make([]string, len(c.Subjects)),
		files:    map[string]bool{},
		made:     map[int]bool{},
		changed:  map[string]bool{},
		tops:     topLevels{},
	}
	for i, subject := range c.Subjects {
		k.subjects[i] = collapse(subject)
	}
	for _, f := range c.Files {
		k.files[f] = true
	}
	for i, path := range append([]string{main}, subagents...) {
		if err := k.read(path, i == 0); err != nil {
			return Session{}, false, err
		}
	}
	s = Session{
		ID:    id,
		Title: k.titles.title(),
		First: k.first,
		Last:  k.last,
		Links: Links{Commits: len(k.made), BranchLines: k.branchLines, Files: len(k.changed)},
	}
	s.Strong = s.Links.Commits > 0
	return s, k.inRepo, nil
}

type linker struct {
	c        Change
	subjects []string
	files    map[string]bool
	// made has the index in c.Subjects of each commit that the session made.
	made        map[int]bool
	changed     map[string]bool
	branchLines int
	inRepo      bool
	first, last time.Time
	titles      titles
	tops        topLevels
	// start is the first cwd of the transcript that the linker reads.
	start string
}

func (k *linker) read(path string, isMain bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	k.start = ""
	return eachLine(f, func(l line) {
		if isMain {
			k.titles.add(l)
		}
		k.add(l)
	})
}

func (k *linker) add(l line) {
	if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
		if k.first.IsZero() || t.Before(k.first) {
			k.first = t
		}
		if t.After(k.last) {
			k.last = t
		}
	}
	if k.start == "" {
		k.start = l.Cwd
	}
	if !k.inRepo && l.Cwd != "" && inside(l.Cwd, k.c.Roots) {
		k.inRepo = true
	}
	if k.c.Branch != "" && l.GitBranch == k.c.Branch {
		k.branchLines++
	}
	if l.Type != "assistant" {
		return
	}
	var m message
	if json.Unmarshal(l.Message, &m) != nil {
		return
	}
	for _, bl := range blocksOf(m.Content) {
		if bl.Type != "tool_use" {
			continue
		}
		var input struct {
			Command      string `json:"command"`
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		}
		_ = json.Unmarshal(bl.Input, &input)
		// A merge commit's subject is in its git merge -m command.
		if bl.Name == "Bash" && (strings.Contains(input.Command, "git commit") || strings.Contains(input.Command, "git merge")) {
			command := collapse(input.Command)
			for i, subject := range k.subjects {
				if subject != "" && !k.made[i] && strings.Contains(command, subject) {
					k.made[i] = true
				}
			}
		}
		if field, ok := editTools[bl.Name]; ok {
			path := input.FilePath
			if field == "notebook_path" {
				path = input.NotebookPath
			}
			if rel := k.relative(l.Cwd, path); k.files[rel] {
				k.changed[rel] = true
			}
		}
	}
}

// relative is path relative to the git top level of cwd. A subagent's
// worktree is often removed when it ends, so for a cwd that is not there
// now, the top level is the transcript's first cwd: the worktree's top
// level, where the subagent started.
func (k *linker) relative(cwd, path string) string {
	if _, err := os.Stat(cwd); err != nil && filepath.IsAbs(path) && k.start != "" && inside(cwd, []string{k.start}) {
		if rel, err := filepath.Rel(k.start, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return k.tops.relative(cwd, path)
}

// inside is true when dir is one of roots or in one of them.
func inside(dir string, roots []string) bool {
	dir = filepath.Clean(dir)
	for _, root := range roots {
		if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// collapse is s with each run of white space as one space, so that a
// subject matches a command that wraps it or puts it in a heredoc.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

const maxTitle = 80

// titles are the names of an Agent session in its main transcript.
type titles struct {
	custom, ai, first string
}

func (t *titles) add(l line) {
	switch {
	case l.Type == "custom-title" && l.CustomTitle != "":
		t.custom = l.CustomTitle
	case l.Type == "ai-title" && l.AITitle != "":
		t.ai = l.AITitle
	case t.first == "" && l.Type == "user" && !l.IsSidechain:
		// A slash command such as /clear does not name the session.
		if text, ok := personText(l); ok && !strings.Contains(text, "<command-name>") {
			t.first = cut(strings.TrimSpace(text), maxTitle)
		}
	}
}

// title is the last name that a person gave the session, else the last
// title that Claude Code made for it, else the start of its first person
// message.
func (t titles) title() string {
	switch {
	case t.custom != "":
		return t.custom
	case t.ai != "":
		return t.ai
	}
	return t.first
}
