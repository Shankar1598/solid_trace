package command_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	openPRSkill   = "../plugin-skills/skills/open-pr/SKILL.md"
	updatePRSkill = "../plugin-skills/skills/update-pr/SKILL.md"
)

var (
	skillExample = regexp.MustCompile("for example `([^`]+)`")
	oldServer    = regexp.MustCompile("an old SolidTrace server says `([^`]+)`")
	evidencePart = regexp.MustCompile("`open-pr/SKILL\\.md` › Write the PR › \\*\\*([^*]+)\\*\\*")
)

func skillEvidenceCommand(t *testing.T, home string) ([]string, string) {
	t.Helper()
	data, err := os.ReadFile(openPRSkill)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	var commands []string
	inBash := false
	for _, l := range strings.Split(text, "\n") {
		switch trimmed := strings.TrimSpace(l); {
		case trimmed == "```bash":
			inBash = true
		case trimmed == "```":
			inBash = false
		case inBash && strings.HasPrefix(trimmed, "solidtrace evidence add "):
			commands = append(commands, trimmed)
		}
	}
	if len(commands) != 1 {
		t.Fatalf("%s has %d lines that start `solidtrace evidence add ` in a ```bash block, want 1: the test runs that line", openPRSkill, len(commands))
	}
	return shellWords(t, commands[0], home), text
}

func shellWords(t *testing.T, line, home string) []string {
	t.Helper()
	var words []string
	var word strings.Builder
	inWord := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == ' ':
			if inWord {
				words, inWord = append(words, word.String()), false
				word.Reset()
			}
		case c == '"' || c == '\'':
			end := strings.IndexByte(line[i+1:], c)
			if end < 0 {
				t.Fatalf("the command %q has a quote that does not close", line)
			}
			quoted := line[i+1 : i+1+end]
			if c == '"' && strings.ContainsAny(quoted, "$`\\") {
				t.Fatalf("the command %q has a double-quoted word that the shell changes: %q", line, quoted)
			}
			word.WriteString(quoted)
			inWord, i = true, i+1+end
		case c == '~' && !inWord && strings.HasPrefix(line[i+1:], "/"):
			word.WriteString(home)
			inWord = true
		case strings.IndexByte("$`\\|&;<>(){}*?", c) >= 0:
			t.Fatalf("the command %q has %q, which the shell changes", line, c)
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

func writeNamedFiles(t *testing.T, words []string) {
	t.Helper()
	for _, word := range words {
		if filepath.Ext(word) != ".png" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(word), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(word, []byte(pngHead+strings.Repeat("x", 86000)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheEvidenceCommandOfTheOpenPRSkillUploadsAndPrintsTheLineThatTheSkillReads(t *testing.T) {
	words, text := skillEvidenceCommand(t, t.TempDir())
	if !slices.Equal(words[:3], []string{"solidtrace", "evidence", "add"}) {
		t.Fatalf("the command is %q, want solidtrace evidence add", words)
	}
	t.Chdir(t.TempDir())
	writeNamedFiles(t, words)
	w := newWorld(t, acme)
	w.agent.ConsoleURL = "https://console.example.com"
	w.signIn()
	w.stdout = &syncBuffer{}
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"

	if code := w.run(words[1:]...); code != 0 {
		t.Fatalf("the skill's command %q: exit %d\nstderr: %s", words, code, w.stderr)
	}
	if lines := w.lines(); len(lines) != 1 || !evidenceLine.MatchString(lines[0]) {
		t.Errorf("stdout: %q, want one line with the link, the type and the size", lines)
	}
	caption := words[slices.Index(words, "--caption")+1]
	if stored := w.agent.StoredEvidence(); len(stored) != 1 || stored[0].Caption != caption || stored[0].SessionID != w.claudeSession {
		t.Errorf("stored: %+v, want one file of the Claude Code session with the caption %q", stored, caption)
	}
	var examples []string
	for _, m := range skillExample.FindAllStringSubmatch(text, -1) {
		if strings.Contains(m[1], "/evidence/") {
			examples = append(examples, m[1])
		}
	}
	if len(examples) != 1 || !evidenceLine.MatchString(examples[0]) {
		t.Errorf("%s: the example lines of the output are %q, want one with the form of the real line, in a sentence of the form \"for example `<link> <type> <size>`\": the test reads that sentence", openPRSkill, examples)
	}
}

func TestTheEvidenceCommandOfTheOpenPRSkillToAnOldServerPrintsNoLinkAndTheMessageThatTheSkillNames(t *testing.T) {
	words, text := skillEvidenceCommand(t, t.TempDir())
	m := oldServer.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s has no sentence of the form \"an old SolidTrace server says `<message>`\": the test reads the message from that sentence", openPRSkill)
	}
	t.Chdir(t.TempDir())
	writeNamedFiles(t, words)
	w := newWorld(t, acme)
	w.agent.NoEvidence = true
	w.signIn()
	w.stdout = &syncBuffer{}
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"

	if code := w.run(words[1:]...); code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", code, w.stderr)
	}
	if out := w.stdout.String(); out != "" {
		t.Errorf("stdout: %q, want no link", out)
	}
	if !strings.Contains(w.stderr.String(), m[1]) {
		t.Errorf("stderr: %q, want the skill's message %q", w.stderr, m[1])
	}
}

func skillStep(t *testing.T, path, heading string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?ms)^## [0-9]+\. ` + regexp.QuoteMeta(heading) + `\n(.*?)(?:^## |\z)`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("%s has no step heading of the form \"## <n>. %s\"", path, heading)
	}
	return m[1]
}

func TestTheUpdatePRSkillKeepsOnlyTheEarlierEvidenceLinksThatStillProveThePRAndUploadsOnlyTheFilesOfItsRound(t *testing.T) {
	step := skillStep(t, updatePRSkill, "Update the PR")
	m := evidencePart.FindStringSubmatch(step)
	if m == nil {
		t.Fatalf("%s › Update the PR names no part of open-pr in the form \"`open-pr/SKILL.md` › Write the PR › **<part>**\": the test reads that form", updatePRSkill)
	}
	if !strings.Contains(skillStep(t, openPRSkill, "Write the PR"), "\n**"+m[1]+".**") {
		t.Errorf("%s › Write the PR has no part that starts \"**%s.**\", which %s › Update the PR names", openPRSkill, m[1], updatePRSkill)
	}
	for _, sentence := range []string{
		"Keep the link of an Evidence file of the earlier body only when its file still shows how the PR works now.",
		"When the fixes of this round change what a file shows, remove its link, or replace it with the link of a new file.",
		"Upload only the files that prove the fixes of this round, not the files of the earlier links",
	} {
		if !strings.Contains(step, sentence) {
			t.Errorf("%s › Update the PR does not say %q", updatePRSkill, sentence)
		}
	}
}
