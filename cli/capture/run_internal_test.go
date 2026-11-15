package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A Run's upload reads files that the agent makes. The walk skips links
// and files that are not regular, but the agent can swap a file between
// the walk and the open: these tests call the open itself.

func runUpload(t *testing.T) (*upload, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dir.Close() })
	return &upload{root: root, dir: dir, forRun: &RunUpload{ProjectDir: root, Dir: dir}}, root
}

func TestARunsUploadOpensNoFileOutsideTheProjectDirectory(t *testing.T) {
	u, root := runUpload(t)
	secret := filepath.Join(filepath.Dir(root), "forge-token")
	if err := os.WriteFile(secret, []byte("ghp_secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "evidence", "a.txt")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"evidence/a.txt", "../forge-token", "evidence/../../forge-token"} {
		if f, err := u.open(path); err == nil {
			f.Close()
			t.Errorf("a Run's upload opened %s, which is outside the project directory", path)
		}
	}

	// A person's capture follows the link: Claude Code's files are theirs.
	u.forRun = nil
	f, err := u.open("evidence/a.txt")
	if err != nil {
		t.Fatalf("a person's capture did not open the link: %v", err)
	}
	f.Close()
}

func TestARunsUploadDoesNotWaitOnAFIFO(t *testing.T) {
	u, root := runUpload(t)
	if err := syscall.Mkfifo(filepath.Join(root, "evidence", "a.txt"), 0o600); err != nil {
		t.Skip("no FIFO here:", err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := u.open("evidence/a.txt")
		if err == nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errNotRegular) {
			t.Errorf("the open of a FIFO gave %v, want errNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the open of a FIFO with no writer did not return")
	}
}

func TestTheSearchForTheEndOfALongLineStopsWithTheContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// A sparse file of zeros with no newline: the search reads to its end.
	if err := f.Truncate(1 << 40); err != nil {
		t.Skip("no sparse file here:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := nextLines(ctx, f, 0, 1<<40); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("nextLines gave %v, want the context's error", err)
	}
	if _, err := hashOf(ctx, f); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("hashOf gave %v, want the context's error", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the reads went on for %s after the context ended", took)
	}
}
