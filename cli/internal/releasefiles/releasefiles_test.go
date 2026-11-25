package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	cliDir    = "../.."
	pluginDir = "../../plugin"
	skillsDir = "../../plugin-skills"
)

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
	err := write(options{Dist: dist, Root: cliDir, InstallScript: "../../install.sh",
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
	want := append(slices.Clone(binaries), "SHA256SUMS", "install.sh", "marketplace.json", "solidtrace-plugin.zip",
		"solidtrace-skills.zip")
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

// unzip gives the names, in order, and the contents of the zip's files.
func unzip(t *testing.T, path string) ([]string, map[string][]byte) {
	t.Helper()
	z, err := zip.OpenReader(path)
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
	return names, files
}

func TestTheSkillsZipHoldsTheSkillsAndAManifestWithTheReleaseVersion(t *testing.T) {
	dist := release(t)

	names, files := unzip(t, filepath.Join(dist, "solidtrace-skills.zip"))

	if !slices.Contains(names, ".claude-plugin/plugin.json") || !slices.ContainsFunc(names, func(n string) bool {
		return strings.HasPrefix(n, "skills/")
	}) {
		t.Fatalf("zip entries: %v", names)
	}
	manifest := readJSON(t, files[".claude-plugin/plugin.json"])
	if manifest["name"] != "solidtrace-skills" || manifest["version"] != "0.19.0" || manifest["displayName"] != "SolidTrace skills" {
		t.Fatalf("plugin.json: %v", manifest)
	}
}

func TestThePluginZipHoldsTheHooksAndAManifestWithTheReleaseVersion(t *testing.T) {
	dist := release(t)

	names, files := unzip(t, filepath.Join(dist, "solidtrace-plugin.zip"))
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

func TestTheMarketplaceFilePinsEachPluginToItsReleaseZip(t *testing.T) {
	dist := release(t)

	data, _ := os.ReadFile(filepath.Join(dist, "marketplace.json"))
	m := readJSON(t, data)
	owner, _ := m["owner"].(map[string]any)
	if m["name"] != "solidtrace" || owner["name"] == "" || owner["name"] == nil {
		t.Fatalf("marketplace: %s", data)
	}
	plugins, _ := m["plugins"].([]any)
	wantEntries := []struct{ name, zip string }{
		{"solidtrace", "solidtrace-plugin.zip"},
		{"solidtrace-skills", "solidtrace-skills.zip"},
	}
	if len(plugins) != len(wantEntries) {
		t.Fatalf("plugins: %v", plugins)
	}
	for i, want := range wantEntries {
		entry := plugins[i].(map[string]any)
		// The entry's name must equal plugin.json's name in the zip.
		_, files := unzip(t, filepath.Join(dist, want.zip))
		if entry["name"] != want.name || readJSON(t, files[".claude-plugin/plugin.json"])["name"] != want.name {
			t.Fatalf("entry %d: name %v, want %s, the name in plugin.json", i, entry["name"], want.name)
		}
		if d, _ := entry["description"].(string); d == "" {
			t.Errorf("%s: no description", want.name)
		}
		source, _ := entry["source"].(map[string]any)
		zipped, _ := os.ReadFile(filepath.Join(dist, want.zip))
		sum := sha256.Sum256(zipped)
		wantSource := map[string]any{
			"source": "archive",
			"url":    "https://github.com/example/solid_trace/releases/download/v0.19.0/" + want.zip,
			"sha256": hex.EncodeToString(sum[:]),
		}
		if len(source) != len(wantSource) {
			t.Fatalf("%s source: %v, want %v", want.name, source, wantSource)
		}
		for k, v := range wantSource {
			if source[k] != v {
				t.Fatalf("%s source.%s = %v, want %v", want.name, k, source[k], v)
			}
		}
		// plugin.json in the zip holds the version; the entry must not also
		// set one (Claude Code then takes plugin.json's without a word).
		if _, ok := entry["version"]; ok {
			t.Fatalf("%s: the entry sets a version", want.name)
		}
	}
}

func TestTheReleaseRefusesATagThatIsNotAVersion(t *testing.T) {
	for _, tag := range []string{"", "latest", "0.19.0", "v0.19", "v0.19.0/../x"} {
		err := write(options{Dist: t.TempDir(), Root: cliDir, InstallScript: "../../install.sh",
			Tag: tag, Repository: "example/solid_trace"})
		if err == nil {
			t.Errorf("tag %q: no error", tag)
		}
	}
}

func TestTheReleaseRefusesARepositoryThatIsNotOwnerName(t *testing.T) {
	for _, repository := range []string{"", "owner", "a/b/c", "a/b/../../x", "a b/c", "o/n\nx"} {
		err := write(options{Dist: t.TempDir(), Root: cliDir, InstallScript: "../../install.sh",
			Tag: "v0.19.0", Repository: repository})
		if err == nil {
			t.Errorf("repository %q: no error", repository)
		}
	}
}

// A symbolic link in the plugin directory can point at a file outside the
// tree; the release must not put its target into the public zip.
func TestTheReleaseRefusesAPluginFileThatIsNotARegularFile(t *testing.T) {
	root := pluginRoot(t, `{"name":"solidtrace"}`)
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "plugin", "link")); err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	err := write(options{Dist: dist, Root: root, InstallScript: "../../install.sh",
		Tag: "v0.19.0", Repository: "example/solid_trace"})
	if err == nil {
		t.Fatal("no error for a symbolic link in the plugin")
	}
	if _, err := os.Stat(filepath.Join(dist, PluginZip)); !os.IsNotExist(err) {
		t.Fatalf("the release wrote a plugin zip: %v", err)
	}
}

// Claude Code installs <name>@solidtrace with the entry's name: a manifest
// of another name would install a plugin that the entry does not name.
func TestTheReleaseRefusesAPluginWhoseManifestHasAnotherName(t *testing.T) {
	root := pluginRoot(t, `{"name":"another"}`)

	err := write(options{Dist: t.TempDir(), Root: root, InstallScript: "../../install.sh",
		Tag: "v0.19.0", Repository: "example/solid_trace"})

	if err == nil || !strings.Contains(err.Error(), "another") {
		t.Fatalf("error: %v", err)
	}
}

// pluginRoot is a cli folder whose plugin folder has the manifest, and
// whose skills plugin folder is correct.
func pluginRoot(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	for dir, data := range map[string]string{"plugin": manifest, "plugin-skills": `{"name":"solidtrace-skills"}`} {
		if err := os.MkdirAll(filepath.Join(root, dir, ".claude-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, ".claude-plugin", "plugin.json"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
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

func TestThePluginsHoldNoInstanceURLAndNoSecret(t *testing.T) {
	// One plugin serves every install: `login` gives the CLI its
	// instance. The homepage is the public repository, and a skill can
	// name a public forge's site or API.
	url := regexp.MustCompile(`[a-z]+://[^\s"]+`)
	secret := regexp.MustCompile(`(?i)token|secret|password|bearer|authorization|api[_-]?key`)
	for _, dir := range []string{pluginDir, skillsDir} {
		checkNoInstanceURLAndNoSecret(t, dir, url, secret)
	}
}

// publicHosts are the public forges, with their subdomains. A host that
// cannot be an instance (localhost, or a name of the reserved example and
// test domains) and a file URL are allowed too.
var publicHosts = []string{"github.com", "bitbucket.org", "bytebucket.org", "gitlab.com"}

func publicURL(u string) bool {
	parsed, err := neturl.Parse(strings.TrimRight(u, ").,;'`\\"))
	if err != nil {
		return false
	}
	if parsed.Scheme == "file" {
		return true
	}
	host := parsed.Hostname()
	if host == "localhost" || host == "127.0.0.1" || strings.HasSuffix(host, ".example") ||
		host == "example.com" || strings.HasSuffix(host, ".example.com") || strings.HasSuffix(host, ".test") {
		return true
	}
	return slices.ContainsFunc(publicHosts, func(h string) bool { return host == h || strings.HasSuffix(host, "."+h) })
}

func TestAPublicURLIsAForgeOrCannotBeAnInstance(t *testing.T) {
	for u, want := range map[string]bool{
		"https://api.bitbucket.org/2.0/repositories":    true,
		"https://cli.github.com)":                       true,
		"https://gitlab.com/example-team/app":           true,
		"https://avatar.example/a.png":                  true,
		"http://localhost:1":                            true,
		"file://$tmp/api":                               true,
		"https://solidtrace.acme.dev/":                  false,
		"https://github.com.acme.dev/":                  false,
		"http://10.0.0.3:4000/":                         false,
		"https://console.solidtrace.internal/projects/": false,
	} {
		if got := publicURL(u); got != want {
			t.Errorf("publicURL(%q) = %v, want %v", u, got, want)
		}
	}
}

func checkNoInstanceURLAndNoSecret(t *testing.T, dir string, url, secret *regexp.Regexp) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, u := range url.FindAllString(string(data), -1) {
			if !publicURL(u) {
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
