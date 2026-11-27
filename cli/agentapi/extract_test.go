package agentapi_test

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
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

// extractIn extracts archive into dir, which it makes, and also gives the
// paths that Extract gave.
func extractIn(t *testing.T, archive io.Reader, dir string) ([]agentapi.ExtractedFile, []string, error) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var given []string
	files, err := agentapi.Extract(archive, root, nil, func(name string) error {
		given = append(given, filepath.ToSlash(name))
		return nil
	})
	return files, given, err
}

func TestExtractGivesEachFileAndFolderThatItMakes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "evidence"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, given, err := extractIn(t, archiveOf(t, tarFile{name: "s/subagents/a.jsonl", body: "{}\n"},
		tarFile{name: "evidence/report.md", body: "Done."}), dir)
	if err != nil {
		t.Fatal(err)
	}
	// evidence/ was there before the extract.
	want := []string{"s", "s/subagents", "s/subagents/a.jsonl", "evidence/report.md"}
	if strings.Join(given, " ") != strings.Join(want, " ") {
		t.Errorf("given %v, want %v", given, want)
	}
}

func TestExtractWritesEachCompleteFileAndLeavesOutAPartialOrCutFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects", "run")
	const id = "6f1d1c3e-8a4b-4c2d-9e5f-0a1b2c3d4e5f"
	files, _, err := extractIn(t, archiveOf(t,
		tarFile{name: id + ".jsonl", body: "{\"uuid\":\"a\"}\n"},
		tarFile{name: id + "/subagents/agent-1.jsonl", body: "{}\n"},
		tarFile{name: "evidence/report.md", body: "Done."},
		tarFile{name: id + "/tool-results/old.txt", body: "late part", flags: "partial"},
		tarFile{name: id + "/tool-results/cut.txt", body: "x", flags: "cut"},
		tarFile{name: "folder/", kind: tar.TypeDir},
	), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []agentapi.ExtractedFile{{Name: id + ".jsonl"}, {Name: id + "/subagents/agent-1.jsonl"},
		{Name: "evidence/report.md"}, {Name: id + "/tool-results/old.txt", Flags: "partial"},
		{Name: id + "/tool-results/cut.txt", Flags: "cut"}}
	if !slices.Equal(files, want) {
		t.Errorf("Extract gave %v, want %v", files, want)
	}
	for name, want := range map[string]string{id + ".jsonl": "{\"uuid\":\"a\"}\n", id + "/subagents/agent-1.jsonl": "{}\n",
		"evidence/report.md": "Done."} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v, want %q", name, b, err, want)
		}
	}
	for _, name := range []string{id + "/tool-results/old.txt", id + "/tool-results/cut.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s is written, but SolidTrace does not have all of it", name)
		}
	}
}

func TestExtractRefusesAPathOutOfTheAgentSessionDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "projects", "run")
	for _, name := range []string{"../outside.txt", "/etc/outside.txt", "a/../../outside.txt", "./a.txt", `a\..\b`} {
		_, _, err := extractIn(t, archiveOf(t, tarFile{name: name, body: "x"}), dir)
		if err == nil || !strings.Contains(err.Error(), "not in the Agent session directory") {
			t.Errorf("the path %q: %v, want a refusal", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "projects", "outside.txt")); err == nil {
		t.Errorf("Extract wrote out of its directory")
	}
}

func TestExtractReplacesAFileThatWasThere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("a longer older file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := extractIn(t, archiveOf(t, tarFile{name: "a.jsonl", body: "new\n"}), dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.jsonl")); string(b) != "new\n" {
		t.Errorf("the file is %q, want the download's bytes", b)
	}
}

func TestExtractWithNoGiveWritesTheFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := agentapi.Extract(archiveOf(t, tarFile{name: "s/subagents/a.jsonl", body: "{}\n"},
		tarFile{name: "s.jsonl", body: "{\"uuid\":\"a\"}\n"}), root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []agentapi.ExtractedFile{{Name: "s/subagents/a.jsonl"}, {Name: "s.jsonl"}}; !slices.Equal(files, want) {
		t.Errorf("Extract gave %v, want %v", files, want)
	}
	for name, want := range map[string]string{"s/subagents/a.jsonl": "{}\n", "s.jsonl": "{\"uuid\":\"a\"}\n"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s: %q %v, want %q", name, b, err, want)
		}
	}
}
