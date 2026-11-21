package wrapper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The take-over's terminal opens Claude Code at its prompt: harness-env
// puts the image's template keys (the onboarding done) and the trust of
// the checkout in $CLAUDE_CONFIG_DIR/.claude.json, and keeps what the
// headless Executions before wrote there.
func TestTheTakeOverPutsTheTemplateAndTheTrustInClaudeJSON(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "claude.json")
	os.WriteFile(template, []byte(`{"hasCompletedOnboarding": true, "projects": {}}`), 0o644)
	config := filepath.Join(dir, "config")
	checkout := "/workspace/bitbucket.org/example/repo"

	t.Run("a new config directory", func(t *testing.T) {
		if err := prepareClaudeJSON(config, template, checkout); err != nil {
			t.Fatal(err)
		}
		got := readClaudeJSON(t, config)
		if got["hasCompletedOnboarding"] != true {
			t.Errorf("hasCompletedOnboarding is %v, want true", got["hasCompletedOnboarding"])
		}
		if trust := projectOf(t, got, checkout)["hasTrustDialogAccepted"]; trust != true {
			t.Errorf("the checkout's hasTrustDialogAccepted is %v, want true", trust)
		}
		if info, err := os.Stat(filepath.Join(config, ".claude.json")); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("the file's mode: %v %v, want 0600", info, err)
		}
	})

	t.Run("a file that a headless Execution wrote", func(t *testing.T) {
		os.WriteFile(filepath.Join(config, ".claude.json"),
			[]byte(`{"firstStartTime": "x", "projects": {"`+checkout+`": {"allowedTools": ["Bash"]}}}`), 0o600)
		if err := prepareClaudeJSON(config, template, checkout); err != nil {
			t.Fatal(err)
		}
		got := readClaudeJSON(t, config)
		if got["hasCompletedOnboarding"] != true || got["firstStartTime"] != "x" {
			t.Errorf("got %v, want the template's key and the file's key", got)
		}
		project := projectOf(t, got, checkout)
		if project["hasTrustDialogAccepted"] != true || project["allowedTools"] == nil {
			t.Errorf("the checkout's entry is %v, want the trust and the entry's keys", project)
		}
	})

	t.Run("no template: the trust only", func(t *testing.T) {
		other := filepath.Join(dir, "other")
		if err := prepareClaudeJSON(other, filepath.Join(dir, "missing.json"), checkout); err == nil {
			t.Error("no error for a missing template")
		}
		if trust := projectOf(t, readClaudeJSON(t, other), checkout)["hasTrustDialogAccepted"]; trust != true {
			t.Errorf("the checkout's hasTrustDialogAccepted is %v, want true", trust)
		}
	})
}

func readClaudeJSON(t *testing.T, config string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(config, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func projectOf(t *testing.T, got map[string]any, checkout string) map[string]any {
	t.Helper()
	projects, _ := got["projects"].(map[string]any)
	project, _ := projects[checkout].(map[string]any)
	return project
}
