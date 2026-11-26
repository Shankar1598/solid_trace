package command_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/solidtrace/cli/internal/fake"
)

const (
	pngHead  = "\x89PNG\r\n\x1a\n"
	webmHead = "\x1a\x45\xdf\xa3\x42\x82\x84webm"
)

// evidenceFile writes a file of size bytes that starts with head.
func (w *world) evidenceFile(name, head string, size int) string {
	w.t.Helper()
	path := filepath.Join(w.t.TempDir(), name)
	data := []byte(head + strings.Repeat("x", size-len(head)))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		w.t.Fatal(err)
	}
	return path
}

func (w *world) lines() []string {
	return strings.Split(strings.TrimSpace(w.stdout.String()), "\n")
}

var evidenceLine = regexp.MustCompile(`^https://console\.example\.com/acme/evidence/[0-9a-f]{32} (\S+) (.+)$`)

func TestEvidenceAddUploadsEachFileToTheSessionOfClaudeCodeAndPrintsItsLink(t *testing.T) {
	w := newWorld(t, acme)
	w.agent.ConsoleURL = "https://console.example.com"
	w.signIn()
	w.stdout = &syncBuffer{}
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"
	shot := w.evidenceFile("shot.png", pngHead, 86000)
	video := w.evidenceFile("flow.webm", webmHead, 3_500_000)

	if code := w.run("evidence", "add", shot, video, "--caption", "Review records list"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	lines := w.lines()
	if len(lines) != 2 {
		t.Fatalf("stdout: %q, want 2 lines", lines)
	}
	for i, want := range [][2]string{{"image/png", "84 KB"}, {"video/webm", "3.3 MB"}} {
		m := evidenceLine.FindStringSubmatch(lines[i])
		if m == nil || m[1] != want[0] || m[2] != want[1] {
			t.Errorf("line %d: %q, want the link, %s and %s", i+1, lines[i], want[0], want[1])
		}
	}
	stored := w.agent.StoredEvidence()
	if len(stored) != 2 {
		t.Fatalf("stored: %d files, want 2", len(stored))
	}
	for _, file := range stored {
		if file.SessionID != w.claudeSession || file.Caption != "Review records list" {
			t.Errorf("stored in %s with the caption %q, want %s and the caption", file.SessionID, file.Caption, w.claudeSession)
		}
	}
	if !strings.Contains(lines[0], stored[0].ID) {
		t.Errorf("line 1 %q does not link to the first file %s", lines[0], stored[0].ID)
	}
}

func TestEvidenceAddSendsToTheSessionOfTheFlagBeforeTheOneOfClaudeCode(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"
	given := "7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f"

	if code := w.run("evidence", "add", "--session", given, w.evidenceFile("shot.png", pngHead, 100)); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if stored := w.agent.StoredEvidence(); len(stored) != 1 || stored[0].SessionID != given {
		t.Fatalf("stored: %+v, want one file in %s", stored, given)
	}
}

func TestEvidenceAddWithNoSessionStopsBeforeItUploads(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()

	if code := w.run("evidence", "add", w.evidenceFile("shot.png", pngHead, 100)); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := "no Agent session: run this from Claude Code, or give --session"; !strings.Contains(w.stderr.String(), want) {
		t.Errorf("stderr: %q, want %q", w.stderr, want)
	}
	if stored := w.agent.StoredEvidence(); len(stored) != 0 {
		t.Errorf("stored: %d files, want none", len(stored))
	}
}

func TestEvidenceAddJoinsALinkWithNoHostToTheConsoleURLOfTheSignIn(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.stdout = &syncBuffer{}
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"

	if code := w.run("evidence", "add", w.evidenceFile("shot.png", pngHead, 100)); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	want := w.oauth.URL() + "/acme/evidence/" + w.agent.StoredEvidence()[0].ID + " image/png 100 B"
	if got := strings.TrimSpace(w.stdout.String()); got != want {
		t.Errorf("stdout: %q, want %q", got, want)
	}
}

func TestEvidenceAddGoesOnAfterAFileThatFailsAndExits1(t *testing.T) {
	w := newWorld(t, acme)
	w.agent.ConsoleURL = "https://console.example.com"
	w.signIn()
	w.stdout = &syncBuffer{}
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"
	page := w.evidenceFile("page.svg", "<svg xmlns='http://www.w3.org/2000/svg'/>", 60)
	missing := filepath.Join(t.TempDir(), "gone.png")
	shot := w.evidenceFile("shot.png", pngHead, 100)

	if code := w.run("evidence", "add", page, missing, shot); code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", code, w.stderr)
	}
	if lines := w.lines(); len(lines) != 1 || !evidenceLine.MatchString(lines[0]) {
		t.Errorf("stdout: %q, want one link", lines)
	}
	stderr := w.stderr.String()
	for _, want := range []string{
		page + ": SolidTrace keeps only PNG, JPEG, WebP and GIF images and MP4 and WebM videos as Evidence files",
		missing + ": ",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr: %q, want %q", stderr, want)
		}
	}
}

func TestEvidenceAddRefusesAFileOver32MiBBeforeItUploads(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"
	big := w.evidenceFile("long.webm", webmHead, fake.EvidenceLimit+1)

	if code := w.run("evidence", "add", big); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := big + ": 32.0 MB: an Evidence file is at most 32 MiB"; !strings.Contains(w.stderr.String(), want) {
		t.Errorf("stderr: %q, want %q", w.stderr, want)
	}
	if stored := w.agent.StoredEvidence(); len(stored) != 0 {
		t.Errorf("stored: %d files, want none", len(stored))
	}
}

func TestEvidenceAddToAnOlderServerSaysToUpgradeIt(t *testing.T) {
	w := newWorld(t, acme)
	w.agent.NoEvidence = true
	w.signIn()
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"

	if code := w.run("evidence", "add", w.evidenceFile("shot.png", pngHead, 100)); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := "this SolidTrace server is too old for Evidence files: ask the operator to upgrade it"; !strings.Contains(w.stderr.String(), want) {
		t.Errorf("stderr: %q, want %q", w.stderr, want)
	}
}

func TestEvidenceAddToAnotherPersonsSessionSaysThatSolidTraceRefusedIt(t *testing.T) {
	w := newWorld(t, acme, beta)
	session := "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"
	w.agent.Sessions = map[string]*fake.Session{session: {Organization: "beta"}}
	w.signIn()
	w.claudeSession = session

	if code := w.run("evidence", "add", w.evidenceFile("shot.png", pngHead, 100)); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := "SolidTrace refused the upload to this Agent session (Agent session " + session; !strings.Contains(w.stderr.String(), want) {
		t.Errorf("stderr: %q, want %q", w.stderr, want)
	}
}

func TestEvidenceAddOfTheSameFileAgainGivesTheSameLink(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.stdout = &syncBuffer{}
	w.claudeSession = "0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f"
	shot := w.evidenceFile("shot.png", pngHead, 100)

	for range 2 {
		if code := w.run("evidence", "add", shot); code != 0 {
			t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
		}
	}
	if lines := w.lines(); len(lines) != 2 || lines[0] != lines[1] {
		t.Errorf("stdout: %q, want the same line twice", lines)
	}
	if stored := w.agent.StoredEvidence(); len(stored) != 1 {
		t.Errorf("stored: %d files, want 1", len(stored))
	}
}
