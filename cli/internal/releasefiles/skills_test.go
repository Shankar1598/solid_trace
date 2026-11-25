package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	headingLine   = regexp.MustCompile(`^## [0-9]+\. (.+?)\s*$`)
	markdownLink  = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	skillDirPath  = regexp.MustCompile("\\$\\{CLAUDE_SKILL_DIR\\}([^\\s`\"'()<>]*)")
	stepReference = regexp.MustCompile("`([^`]*)SKILL\\.md` › ")
	codeSpan      = regexp.MustCompile("`[^`]*`")
	skillFileSpan = regexp.MustCompile("`(?:[^`]*/)?([a-z0-9-]+)/SKILL\\.md`")
	quoted        = regexp.MustCompile(`"([^"]+)"`)
	tomlTable     = regexp.MustCompile(`^\[\[?[A-Za-z0-9_.-]+\]\]?$`)
	tomlKey       = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*\s*=(.*)$`)
	tomlScalar    = regexp.MustCompile(`^(true|false|[+-]?[0-9][0-9_]*(\.[0-9_]+)?([eE][+-]?[0-9]+)?)`)
)

// skillProblems are the problems of the skill files in each root that is
// there: each root holds one folder for each skill.
func skillProblems(roots ...string) ([]string, error) {
	var problems []string
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		headings, err := skillHeadings(root)
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			skill, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
			for _, p := range fileProblems(path, skill, string(data), headings) {
				problems = append(problems, path+": "+p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return problems, nil
}

// skillHeadings are the step headings (## <n>. <Heading>) of the SKILL.md
// of each skill in root, by the skill's folder name.
func skillHeadings(root string) (map[string][]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	headings := map[string][]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(root, e.Name(), "SKILL.md"))
		if !e.IsDir() || os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		headings[e.Name()] = []string{}
		for _, l := range strings.Split(string(data), "\n") {
			if m := headingLine.FindStringSubmatch(l); m != nil {
				headings[e.Name()] = append(headings[e.Name()], m[1])
			}
		}
	}
	return headings, nil
}

func fileProblems(path, skill, text string, headings map[string][]string) []string {
	var problems []string
	add := func(n int, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("line %d: ", n)+fmt.Sprintf(format, args...))
	}
	dir := filepath.Dir(path)
	lines := strings.Split(text, "\n")
	if filepath.Base(path) == "SKILL.md" {
		for _, p := range frontmatterProblems(lines, skill) {
			add(1, "%s", p)
		}
	}
	for i, l := range lines {
		n := i + 1
		if strings.ContainsRune(l, '—') {
			add(n, "has an em dash (U+2014)")
		}
		for _, m := range markdownLink.FindAllStringSubmatch(l, -1) {
			target, _, _ := strings.Cut(m[1], "#")
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
				add(n, "the link %s does not resolve", m[1])
			}
		}
		for _, m := range skillDirPath.FindAllStringSubmatch(l, -1) {
			target := strings.TrimRight(m[1], ".,")
			if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
				add(n, "${CLAUDE_SKILL_DIR}%s does not resolve", target)
			}
		}
		for _, loc := range stepReference.FindAllStringSubmatchIndex(l, -1) {
			named := skill
			if folder := strings.TrimSuffix(l[loc[2]:loc[3]], "/"); folder != "" {
				named = filepath.Base(folder)
			}
			after := l[loc[1]:]
			if !slices.ContainsFunc(headings[named], func(h string) bool { return strings.HasPrefix(after, h) }) {
				add(n, "%s/SKILL.md has no step heading that starts %q", named, cutText(after, 40))
			}
		}
		// A quoted text in a line that names another skill's SKILL.md is
		// the name of one of that skill's steps.
		var named []string
		for _, m := range skillFileSpan.FindAllStringSubmatch(l, -1) {
			named = append(named, m[1])
		}
		if len(named) > 0 {
			for _, m := range quoted.FindAllStringSubmatch(codeSpan.ReplaceAllString(l, ""), -1) {
				if !slices.ContainsFunc(named, func(s string) bool { return slices.Contains(headings[s], m[1]) }) {
					add(n, "%q is not a step heading of %s", m[1], strings.Join(named, " or "))
				}
			}
		}
	}
	for _, b := range codeBlocks(lines) {
		if b.end < 0 {
			add(b.start, "the ```%s block has no closing ```", b.lang)
			continue
		}
		body := strings.Join(lines[b.start:b.end], "\n")
		var v any
		switch b.lang {
		case "json":
			if err := json.Unmarshal([]byte(strings.ReplaceAll(body, `"..."`, `"x"`)), &v); err != nil {
				add(b.start, "the ```json block is not valid JSON: %v", err)
			}
		case "solidtrace-findings":
			if err := json.Unmarshal([]byte(body), &v); err != nil {
				add(b.start, "the ```solidtrace-findings block is not valid JSON: %v", err)
			}
		case "toml":
			for j, tl := range lines[b.start:b.end] {
				if !tomlLine(tl) {
					add(b.start+j+1, "the ```toml line %q is not a table, a key = value or a comment", tl)
				}
			}
		}
	}
	return problems
}

func frontmatterProblems(lines []string, skill string) []string {
	if len(lines) == 0 || lines[0] != "---" {
		return []string{"SKILL.md has no frontmatter"}
	}
	fields := map[string]string{}
	closed := false
	for _, l := range lines[1:] {
		if l == "---" {
			closed = true
			break
		}
		if key, value, ok := strings.Cut(l, ":"); ok {
			fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	var problems []string
	if !closed {
		problems = append(problems, "the frontmatter has no closing ---")
	}
	if fields["name"] != skill {
		problems = append(problems, fmt.Sprintf("the frontmatter name is %q, not the folder name %q", fields["name"], skill))
	}
	if fields["description"] == "" {
		problems = append(problems, "the frontmatter has no description")
	}
	return problems
}

// codeBlock is a fenced block of a language that the test parses: its
// body is lines[start:end], and end is -1 when the block does not close.
type codeBlock struct {
	lang       string
	start, end int
}

// codeBlocks finds the blocks by their fence lines only, so that a block
// inside a ```` fence (an example of a reply) is found too.
func codeBlocks(lines []string) []codeBlock {
	var blocks []codeBlock
	for i := 0; i < len(lines); i++ {
		lang, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "```")
		if !ok || (lang != "json" && lang != "solidtrace-findings" && lang != "toml") {
			continue
		}
		b := codeBlock{lang: lang, start: i + 1, end: -1}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				b.end, i = j, j
				break
			}
		}
		blocks = append(blocks, b)
		if b.end < 0 {
			break
		}
	}
	return blocks
}

// tomlLine is true for a line that TOML can read in a small file: a blank
// line, a comment, a [table], or key = value with a string, a number, a
// boolean or an array of them on one line.
func tomlLine(l string) bool {
	l = strings.TrimSpace(l)
	if l == "" || strings.HasPrefix(l, "#") || tomlTable.MatchString(l) {
		return true
	}
	m := tomlKey.FindStringSubmatch(l)
	if m == nil {
		return false
	}
	rest, ok := tomlValue(m[2])
	rest = strings.TrimSpace(rest)
	return ok && (rest == "" || strings.HasPrefix(rest, "#"))
}

// tomlValue reads one value at the start of s, and gives the text after it.
func tomlValue(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t")
	switch {
	case strings.HasPrefix(s, `"`):
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				return s[i+1:], true
			}
		}
		return "", false
	case strings.HasPrefix(s, "'"):
		i := strings.IndexByte(s[1:], '\'')
		if i < 0 {
			return "", false
		}
		return s[i+2:], true
	case strings.HasPrefix(s, "["):
		s = strings.TrimLeft(s[1:], " \t")
		for !strings.HasPrefix(s, "]") {
			var ok bool
			if s, ok = tomlValue(s); !ok {
				return "", false
			}
			s = strings.TrimLeft(s, " \t")
			if after, comma := strings.CutPrefix(s, ","); comma {
				s = strings.TrimLeft(after, " \t")
			} else if !strings.HasPrefix(s, "]") {
				return "", false
			}
		}
		return s[1:], true
	}
	m := tomlScalar.FindString(s)
	return s[len(m):], m != ""
}

func cutText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func TestTheSkillFilesHaveTheirNamesAndNoBrokenLinkReferenceOrBlock(t *testing.T) {
	problems, err := skillProblems(filepath.Join(skillsDir, "skills"), filepath.Join(pluginDir, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("the skill files have %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

func TestTheSkillCheckNamesTheFileAndTheProblemOfEachBreak(t *testing.T) {
	cases := []struct {
		name, file, text, want string
	}{
		{"a broken link", "code-review/SKILL.md", "See [the list](missing.md).",
			"the link missing.md does not resolve"},
		{"a broken skill folder path", "open-pr/SKILL.md", "Read `${CLAUDE_SKILL_DIR}/../code-review/missing.md`.",
			"${CLAUDE_SKILL_DIR}/../code-review/missing.md does not resolve"},
		{"a step reference with no heading", "update-pr/SKILL.md", "Fix as `code-review/SKILL.md` › Fix it all says.",
			`code-review/SKILL.md has no step heading that starts "Fix it all says."`},
		{"a reference to a skill that is not there", "update-pr/SKILL.md", "As `other/SKILL.md` › Report says.",
			`other/SKILL.md has no step heading that starts "Report says."`},
		{"a quoted step that is not a heading", "update-pr/SKILL.md", "Follow `open-pr/SKILL.md`: \"Find the PR fast\".",
			`"Find the PR fast" is not a step heading of open-pr`},
		{"a json block that does not parse", "open-pr/record.md", "```json\n{\"a\": }\n```",
			"the ```json block is not valid JSON"},
		{"a findings block that does not parse", "code-review/findings.md", "```solidtrace-findings\n{\"version\": 2,}\n```",
			"the ```solidtrace-findings block is not valid JSON"},
		{"a toml block that does not parse", "code-review/models.md", "```toml\n[models]\ntests = opus\n```",
			"the ```toml line \"tests = opus\" is not a table, a key = value or a comment"},
		{"an em dash", "deep-review/SKILL.md", "A fault — a big one.", "has an em dash (U+2014)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "skills")
			if err := os.CopyFS(root, os.DirFS(filepath.Join(skillsDir, "skills"))); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, filepath.FromSlash(c.file))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\n"+c.text+"\n")...), 0o644); err != nil {
				t.Fatal(err)
			}

			problems, err := skillProblems(root)

			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 || !strings.HasPrefix(problems[0], path+": line ") || !strings.Contains(problems[0], c.want) {
				t.Fatalf("problems:\n%s\nwant one problem of %s with %q", strings.Join(problems, "\n"), path, c.want)
			}
		})
	}
}

func TestTheSkillCheckNamesASkillWhoseNameIsNotItsFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	if err := os.CopyFS(root, os.DirFS(filepath.Join(skillsDir, "skills"))); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "deep-review"), filepath.Join(root, "deeper-review")); err != nil {
		t.Fatal(err)
	}

	problems, err := skillProblems(root)

	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "deeper-review", "SKILL.md") + `: line 1: the frontmatter name is "deep-review", not the folder name "deeper-review"`
	if !slices.Contains(problems, want) {
		t.Fatalf("problems:\n%s\nwant %s", strings.Join(problems, "\n"), want)
	}
}
