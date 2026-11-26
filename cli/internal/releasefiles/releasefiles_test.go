package main

import (
	"encoding/json"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	repoDir   = "../../.."
	cliDir    = "../.."
	pluginDir = "../../plugin"
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
	err := write(options{Dist: dist, Root: pluginRoot(t, `{"name":"solidtrace","version":"0.19.0"}`),
		InstallScript: "../../install.sh", Tag: "v0.19.0", Repository: "example/solid_trace"})
	if err != nil {
		t.Fatal(err)
	}
	return dist
}

// pluginRoot is a cli folder whose plugin folder has the manifest.
func pluginRoot(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plugin", ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin", ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func TestTheReleaseHasTheBinariesTheInstallScriptAndOneChecksumFile(t *testing.T) {
	dist := release(t)

	entries, _ := os.ReadDir(dist)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := append(slices.Clone(binaries), "SHA256SUMS", "install.sh")
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

func TestTheReleaseRefusesATagThatIsNotAVersion(t *testing.T) {
	root := pluginRoot(t, `{"name":"solidtrace","version":"0.19.0"}`)
	for _, tag := range []string{"", "latest", "0.19.0", "v0.19", "v0.19.0/../x"} {
		err := write(options{Dist: t.TempDir(), Root: root, InstallScript: "../../install.sh",
			Tag: tag, Repository: "example/solid_trace"})
		if err == nil {
			t.Errorf("tag %q: no error", tag)
		}
	}
}

func TestTheReleaseRefusesARepositoryThatIsNotOwnerName(t *testing.T) {
	root := pluginRoot(t, `{"name":"solidtrace","version":"0.19.0"}`)
	for _, repository := range []string{"", "owner", "a/b/c", "a/b/../../x", "a b/c", "o/n\nx"} {
		err := write(options{Dist: t.TempDir(), Root: root, InstallScript: "../../install.sh",
			Tag: "v0.19.0", Repository: repository})
		if err == nil {
			t.Errorf("repository %q: no error", repository)
		}
	}
}

// Claude Code installs the plugin of the marketplace at the release's tag,
// and updates it only when plugin.json's version changes. A release commit
// that does not set the version gives the plugin of the last release a new
// tag, and installed plugins then do not update.
func TestTheReleaseRefusesATagThatIsNotThePluginVersion(t *testing.T) {
	for _, c := range []struct{ name, manifest, tag string }{
		{"an older version", `{"name":"solidtrace","version":"0.18.0"}`, "v0.19.0"},
		{"no version", `{"name":"solidtrace"}`, "v0.19.0"},
		{"the version without the pre-release suffix", `{"name":"solidtrace","version":"0.20.0"}`, "v0.20.0-rc.1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dist := t.TempDir()
			err := write(options{Dist: dist, Root: pluginRoot(t, c.manifest), InstallScript: "../../install.sh",
				Tag: c.tag, Repository: "example/solid_trace"})
			if err == nil {
				t.Fatal("no error")
			}
			if entries, _ := os.ReadDir(dist); len(entries) != 0 {
				t.Fatalf("the release wrote files: %v", entries)
			}
		})
	}
}

var changelogName = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)\.md$`)

// The release commit adds docs/changelogs/<tag>.md and sets plugin.json's
// version. This test fails in CI when it does only the first.
func TestThePluginVersionIsTheVersionOfTheNewestChangelog(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repoDir, "docs", "changelogs"))
	if err != nil {
		t.Fatal(err)
	}
	var newest []int
	for _, e := range entries {
		m := changelogName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v := make([]int, 3)
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+1])
		}
		if slices.Compare(v, newest) > 0 {
			newest = v
		}
	}
	if newest == nil {
		t.Fatal("docs/changelogs has no v*.md")
	}
	want := strconv.Itoa(newest[0]) + "." + strconv.Itoa(newest[1]) + "." + strconv.Itoa(newest[2])
	got, err := pluginVersion(filepath.Join(cliDir, pluginManifest))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cli/plugin/.claude-plugin/plugin.json has the version %q; the newest changelog is v%s", got, want)
	}
}

// Claude Code installs <plugin>@<marketplace>. Console, the setup script and
// `solidtrace capture status` use solidtrace@solidtrace.
func TestTheMarketplaceAtTheRepositoryRootGivesThePluginOfCliAsSolidtraceAtSolidtrace(t *testing.T) {
	m := readJSON(t, filepath.Join(repoDir, ".claude-plugin", "marketplace.json"))
	plugins, _ := m["plugins"].([]any)
	if m["name"] != "solidtrace" || len(plugins) != 1 {
		t.Fatalf("marketplace: %v", m)
	}
	entry, _ := plugins[0].(map[string]any)
	if entry["name"] != "solidtrace" || entry["source"] != "./cli/plugin" {
		t.Fatalf("plugin entry: %v", entry)
	}
	manifest := readJSON(t, filepath.Join(repoDir, "cli", "plugin", ".claude-plugin", "plugin.json"))
	if manifest["name"] != entry["name"] {
		t.Fatalf("plugin.json has the name %v, not the entry's name %v", manifest["name"], entry["name"])
	}
	// The entry must not set a version: Claude Code then takes plugin.json's
	// without a word.
	if _, ok := entry["version"]; ok {
		t.Fatal("the entry sets a version")
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

func TestThePluginsHoldNoInstanceURLAndNoSecret(t *testing.T) {
	// One plugin serves every install: `login` gives the CLI its
	// instance. The homepage is the public repository, and a skill can
	// name a public forge's site or API.
	url := regexp.MustCompile(`[a-z]+://[^\s"]+`)
	secret := regexp.MustCompile(`(?i)token|secret|password|bearer|authorization|api[_-]?key`)
	checkNoInstanceURLAndNoSecret(t, pluginDir, url, secret)
}

// publicHosts are the public forges, with their subdomains. A host that
// cannot be an instance (localhost, or a name of the reserved example and
// test domains) and a file URL are allowed too.
var publicHosts = []string{"github.com", "bitbucket.org", "bytebucket.org", "gitlab.com"}

func publicURL(u string) bool {
	// A URL in a shell string can run into an escape, such as \n.
	u, _, _ = strings.Cut(u, `\`)
	if strings.HasPrefix(u, "file:") {
		return true
	}
	parsed, err := neturl.Parse(strings.TrimRight(u, ").,;'`\\"))
	if err != nil {
		return false
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
		`http://localhost:1\n'`:                         true,
		"file://$tmp/api":                               true,
		"file://}":                                      true,
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
