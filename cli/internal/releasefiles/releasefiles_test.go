package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const pluginDir = "../../plugin"

var binaries = []string{"solidtrace-darwin-amd64", "solidtrace-darwin-arm64", "solidtrace-linux-amd64", "solidtrace-linux-arm64"}

// release makes the release files of v0.19.0 in a dist directory that holds
// the four binaries, as cli/build leaves it.
func release(t *testing.T) string {
	t.Helper()
	dist := t.TempDir()
	for _, b := range binaries {
		if err := os.WriteFile(filepath.Join(dist, b), []byte("binary "+b), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	err := write(options{Dist: dist, Plugin: pluginDir, InstallScript: "../../install.sh",
		Tag: "v0.19.0", Repository: "example/solid_trace"})
	if err != nil {
		t.Fatal(err)
	}
	return dist
}

func readJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	return v
}

func TestTheReleaseHasTheBinariesOneChecksumFileThePluginZipAndTheMarketplaceFile(t *testing.T) {
	dist := release(t)

	entries, _ := os.ReadDir(dist)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := append(slices.Clone(binaries), "SHA256SUMS", "install.sh", "marketplace.json", "solidtrace-plugin.zip")
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("release files:\n%v\nwant\n%v", names, want)
	}

	// Each other file is in the checksum file, in sha256sum's format.
	sums, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if n := strings.Count(string(sums), "\n"); n != len(want)-1 {
		t.Fatalf("SHA256SUMS has %d lines, want %d:\n%s", n, len(want)-1, sums)
	}
	check := exec.Command("sha256sum", "--check", "--strict", "SHA256SUMS")
	check.Dir = dist
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("sha256sum --check: %v\n%s", err, out)
	}
}

func TestThePluginZipHoldsTheHooksAndAManifestWithTheReleaseVersion(t *testing.T) {
	dist := release(t)

	z, err := zip.OpenReader(filepath.Join(dist, "solidtrace-plugin.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	files := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], _ = io.ReadAll(r)
		r.Close()
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{".claude-plugin/plugin.json", "README.md", "hooks/hooks.json"}) {
		t.Fatalf("zip entries: %v", names)
	}
	manifest := readJSON(t, files[".claude-plugin/plugin.json"])
	if manifest["name"] != "solidtrace" || manifest["version"] != "0.19.0" {
		t.Fatalf("plugin.json: %v", manifest)
	}
	source, _ := os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json"))
	if string(files["hooks/hooks.json"]) != string(source) {
		t.Fatal("the zip's hooks.json differs from cli/plugin/hooks/hooks.json")
	}
}

func TestTheMarketplaceFilePinsThePluginToTheReleaseZip(t *testing.T) {
	dist := release(t)

	data, _ := os.ReadFile(filepath.Join(dist, "marketplace.json"))
	m := readJSON(t, data)
	owner, _ := m["owner"].(map[string]any)
	if m["name"] != "solidtrace" || owner["name"] == "" || owner["name"] == nil {
		t.Fatalf("marketplace: %s", data)
	}
	plugins, _ := m["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("plugins: %v", plugins)
	}
	entry := plugins[0].(map[string]any)
	if entry["name"] != "solidtrace" {
		t.Fatalf("entry name %v: it must equal plugin.json's name", entry["name"])
	}
	source, _ := entry["source"].(map[string]any)
	zipped, _ := os.ReadFile(filepath.Join(dist, "solidtrace-plugin.zip"))
	sum := sha256.Sum256(zipped)
	wantSource := map[string]any{
		"source": "archive",
		"url":    "https://github.com/example/solid_trace/releases/download/v0.19.0/solidtrace-plugin.zip",
		"sha256": hex.EncodeToString(sum[:]),
	}
	if len(source) != len(wantSource) {
		t.Fatalf("source: %v, want %v", source, wantSource)
	}
	for k, v := range wantSource {
		if source[k] != v {
			t.Fatalf("source.%s = %v, want %v", k, source[k], v)
		}
	}
	// plugin.json in the zip holds the version; the entry must not also
	// set one (Claude Code then takes plugin.json's without a word).
	if _, ok := entry["version"]; ok {
		t.Fatal("the entry sets a version")
	}
}

func TestTheReleaseRefusesATagThatIsNotAVersion(t *testing.T) {
	for _, tag := range []string{"", "latest", "0.19.0", "v0.19", "v0.19.0/../x"} {
		err := write(options{Dist: t.TempDir(), Plugin: pluginDir, InstallScript: "../../install.sh",
			Tag: tag, Repository: "example/solid_trace"})
		if err == nil {
			t.Errorf("tag %q: no error", tag)
		}
	}
}

func TestEachHookOfThePluginStartsCaptureInTheBackground(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	var events []string
	for event, groups := range file.Hooks {
		events = append(events, event)
		if len(groups) != 1 || len(groups[0].Hooks) != 1 || groups[0].Matcher != "" {
			t.Fatalf("%s: want one hook for each source: %+v", event, groups)
		}
		h := groups[0].Hooks[0]
		if h.Type != "command" || h.Command != "solidtrace capture --detach" || h.Timeout <= 0 || h.Timeout > 10 {
			t.Errorf("%s: %+v", event, h)
		}
	}
	slices.Sort(events)
	if !slices.Equal(events, []string{"SessionEnd", "SessionStart", "Stop", "SubagentStop"}) {
		t.Fatalf("events: %v", events)
	}
}

func TestThePluginHoldsNoInstanceURLAndNoSecret(t *testing.T) {
	// One plugin serves every install: `login` gives the CLI its
	// instance. The homepage is the public repository.
	url := regexp.MustCompile(`[a-z]+://[^\s"]+`)
	secret := regexp.MustCompile(`(?i)token|secret|password|bearer|authorization|api[_-]?key`)
	err := filepath.WalkDir(pluginDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, u := range url.FindAllString(string(data), -1) {
			if !strings.HasPrefix(u, "https://github.com/") {
				t.Errorf("%s holds the URL %s", path, u)
			}
		}
		// The README is prose for people; the JSON files are what runs.
		if s := secret.FindString(string(data)); s != "" && filepath.Ext(path) == ".json" {
			t.Errorf("%s holds %q", path, s)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
