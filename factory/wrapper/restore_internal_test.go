package wrapper

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/agentapi"
)

type tarFile struct {
	name, body, flags string
	kind              byte
}

func archiveOf(t *testing.T, files ...tarFile) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, f := range files {
		h := &tar.Header{Name: f.name, Size: int64(len(f.body)), Mode: 0o600, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if f.kind != 0 {
			h.Typeflag, h.Size = f.kind, 0
		}
		if f.flags != "" {
			h.PAXRecords = map[string]string{agentapi.PAXFlags: f.flags}
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := w.Write([]byte(f.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b
}

func TestTheRestoreWritesEachCompleteFileAndLeavesOutAPartialOrCutFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects", "run")
	const id = "6f1d1c3e-8a4b-4c2d-9e5f-0a1b2c3d4e5f"
	got, err := extract(archiveOf(t,
		tarFile{name: id + ".jsonl", body: "{\"uuid\":\"a\"}\n"},
		tarFile{name: id + "/subagents/agent-1.jsonl", body: "{}\n"},
		tarFile{name: "evidence/report.md", body: "Done."},
		tarFile{name: id + "/tool-results/old.txt", body: "late part", flags: "partial"},
		tarFile{name: id + "/tool-results/cut.txt", body: "x", flags: "cut"},
		tarFile{name: "folder/", kind: tar.TypeDir},
	), dir, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{id + ".jsonl": "{\"uuid\":\"a\"}\n", id + "/subagents/agent-1.jsonl": "{}\n",
		"evidence/report.md": "Done."} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != want || !got[name] {
			t.Errorf("%s: %q %v (restored %v), want %q", name, b, err, got[name], want)
		}
	}
	for _, name := range []string{id + "/tool-results/old.txt", id + "/tool-results/cut.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil || got[name] {
			t.Errorf("%s is restored, but SolidTrace does not have all of it", name)
		}
	}
}

func TestTheRestoreRefusesAPathOutOfTheAgentSessionDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "projects", "run")
	for _, name := range []string{"../outside.txt", "/etc/outside.txt", "a/../../outside.txt", "./a.txt", `a\..\b`} {
		_, err := extract(archiveOf(t, tarFile{name: name, body: "x"}), dir, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "not in the Agent session directory") {
			t.Errorf("the path %q: %v, want a refusal", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "projects", "outside.txt")); err == nil {
		t.Errorf("the restore wrote out of its directory")
	}
}

func TestTheRestoreReplacesAFileThatAnEarlierExecutionLeft(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("a longer older file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extract(archiveOf(t, tarFile{name: "a.jsonl", body: "new\n"}), dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.jsonl")); string(b) != "new\n" {
		t.Errorf("the file is %q, want the download's bytes", b)
	}
}
