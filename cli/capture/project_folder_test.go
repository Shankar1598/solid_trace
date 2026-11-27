package capture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/capture"
)

// The expected names come from Claude Code 2.1.326: a session in a folder
// with ï and ü gave "-na-ve--", and a long path with 😀 gave two "-" for it
// and the same hash as the JavaScript String hash of the path.
func TestClaudeProjectFolderIsTheNameThatClaudeCodeGivesThePath(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/Users/ana/shop", "-Users-ana-shop"},
		{"/home/ana/my_repo/src.v2/new folder", "-home-ana-my-repo-src-v2-new-folder"},
		{"/Users/ana/naïve-ü", "-Users-ana-na-ve--"},
		{"/Users/ana/😀", "-Users-ana---"},
		// 200 characters stay as they are. At 201 the hash comes after the
		// first 200, here of Java's String.hashCode in Python.
		{"/" + strings.Repeat("a", 199), "-" + strings.Repeat("a", 199)},
		{"/" + strings.Repeat("a", 200), "-" + strings.Repeat("a", 199) + "-b6ymvl"},
		// 324 characters, as the prototype's path.
		{"/srv/checkouts/" + strings.Repeat("folder-name/", 25) + "last-more",
			("-srv-checkouts-" + strings.Repeat("folder-name-", 25))[:200] + "-14fxei"},
		{"/Users/zoë/" + strings.Repeat("ü😀x/", 60),
			("-Users-zo--" + strings.Repeat("---x-", 60))[:200] + "-hm0m33"},
	} {
		if got := capture.ClaudeProjectFolder(c.path); got != c.want {
			t.Errorf("ClaudeProjectFolder(%q)\n got %q\nwant %q", c.path, got, c.want)
		}
	}
}

// transcriptIn writes a transcript whose first line has the cwd into the
// Claude Code project folder of folder, and gives its path.
func transcriptIn(t *testing.T, folder, cwd string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "projects", capture.ClaudeProjectFolder(folder))
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "1b2c3d4e-5f60-4718-8a9b-0c1d2e3f4a5b.jsonl")
	line := `{"type":"user","cwd":"` + cwd + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A resume on another machine gives a transcript whose first cwd is the
// folder on the first machine. The start directory is then the folder of
// this machine whose project folder holds the transcript.
func TestStartDirOfAResumedAgentSessionIsTheFolderOfItsProjectFolderOnThisMachine(t *testing.T) {
	target := filepath.Join(t.TempDir(), "shop")
	sub := filepath.Join(target, "web", "app")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := transcriptIn(t, target, "/home/someone-else/checkouts/shop")

	for _, hookCwd := range []string{target, sub} {
		if got := capture.StartDir(transcript, hookCwd); got != target {
			t.Errorf("StartDir with the hook cwd %s = %s, want %s", hookCwd, got, target)
		}
	}
	other := t.TempDir()
	if got := capture.StartDir(transcript, other); got != other {
		t.Errorf("StartDir with the hook cwd %s out of the target = %s, want the hook cwd", other, got)
	}
}

func TestStartDirIsTheFirstCwdOfTheTranscriptWhenItIsOnThisMachine(t *testing.T) {
	start := t.TempDir()
	sub := filepath.Join(start, "web")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := transcriptIn(t, start, start)

	// The agent did cd web. The sweep has no hook cwd.
	for _, hookCwd := range []string{sub, ""} {
		if got := capture.StartDir(transcript, hookCwd); got != start {
			t.Errorf("StartDir with the hook cwd %q = %s, want the first cwd %s", hookCwd, got, start)
		}
	}
}

// The history of a fork can have the cwd of another checkout that is also
// on this machine. Its project folder does not hold the transcript, so it
// is not the start directory.
func TestStartDirIsNotAFirstCwdWhoseProjectFolderDoesNotHoldTheTranscript(t *testing.T) {
	first, target := t.TempDir(), t.TempDir()
	transcript := transcriptIn(t, target, first)

	if got := capture.StartDir(transcript, target); got != target {
		t.Errorf("StartDir = %s, want the folder %s of the project folder", got, target)
	}
}
