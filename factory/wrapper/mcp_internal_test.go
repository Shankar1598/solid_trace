package wrapper

import (
	"bytes"
	"os"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The MCP token goes into a shell command and a JSON header with no
// quoting, so a token that is not an API token (a quote, a space, a shell
// word) is refused, and no file of the secrets directory is written.
func TestAnMCPTokenThatIsNotAnAPITokenIsRefusedAndNoFileIsWritten(t *testing.T) {
	for _, token := range []string{"st_abc'; touch pwned; '", "st_abc def", "st_abc$(id)", "st_abc\"", ""} {
		cfg := testConfig(t.TempDir())
		e := setupExecution(t, cfg, &bytes.Buffer{}, dispatcherapi.Task{
			MCP: &dispatcherapi.MCPToken{Token: token, URL: "https://solidtrace.example/mcp"}})
		if err := os.MkdirAll(cfg.secrets(), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := e.writeMCP(); err == nil {
			t.Errorf("the token %q is accepted", token)
		}
		for _, path := range []string{cfg.mcpToken(), cfg.mcpConfig()} {
			if _, err := os.Stat(path); err == nil {
				t.Errorf("the token %q: %s is written", token, path)
			}
		}
	}
}

// An API token is written to a file that only the wrapper changes (0440).
func TestAnAPITokenIsWrittenToAFileOfTheSecretsDirectory(t *testing.T) {
	cfg := testConfig(t.TempDir())
	e := setupExecution(t, cfg, &bytes.Buffer{}, dispatcherapi.Task{
		MCP: &dispatcherapi.MCPToken{Token: "st_Ab1-2.3_x", URL: "https://solidtrace.example/mcp"}})
	if err := os.MkdirAll(cfg.secrets(), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := e.writeMCP(); err != nil {
		t.Fatal(err)
	}

	if b, err := os.ReadFile(cfg.mcpToken()); err != nil || string(b) != "st_Ab1-2.3_x" {
		t.Errorf("the token file: %q, %v", b, err)
	}
	if info, err := os.Stat(cfg.mcpToken()); err != nil || info.Mode().Perm() != 0o440 {
		t.Errorf("the token file's mode: %v, %v, want 440", info, err)
	}
}
