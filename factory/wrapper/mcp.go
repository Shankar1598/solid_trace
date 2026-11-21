package wrapper

import (
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The MCP token of an Issue or a Monitor Run. The Dispatcher makes it
// for each Execution: mcp:read for the Run's Project only, with the
// Execution's expiry. The run user can reach it (the spec's list of what
// the run user reaches): the wrapper writes it to a file of the secrets
// directory, outside the checkout, which the run user reads and cannot
// change. The harness gets an MCP configuration (--mcp-config) whose
// server 'solidtrace' reads the token from that file for each request,
// so the token is in no environment and in no file of the checkout.

// mcpServerName is the name of SolidTrace's MCP server in the harness.
const mcpServerName = "solidtrace"

// mcpTokenPattern is the form of an API token: the token goes into a
// shell command and a JSON header, with no quoting.
var mcpTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,512}$`)

func (c config) mcpToken() string  { return filepath.Join(c.secrets(), "mcp-token") }
func (c config) mcpConfig() string { return filepath.Join(c.secrets(), "mcp.json") }

// writeMCP writes the Run's MCP token and the harness's MCP configuration
// to the secrets directory, when the task has an MCP token.
func (e *execution) writeMCP() error {
	mcp := e.task.MCP
	if mcp == nil {
		return nil
	}
	if !mcpTokenPattern.MatchString(mcp.Token) {
		return errors.New("the task's MCP token is not an API token")
	}
	u, err := url.Parse(mcp.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("the task's MCP server URL is not an HTTPS URL")
	}
	if err := writeSecret(e.runUser, e.cfg.mcpToken(), []byte(mcp.Token), 0o440); err != nil {
		return err
	}
	// The secrets directory's path has no quote (setup checks it).
	helper := `printf '{"Authorization":"Bearer %s"}' "$(cat '` + e.cfg.mcpToken() + `')"`
	config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		mcpServerName: map[string]any{"type": "http", "url": mcp.URL, "headersHelper": helper},
	}})
	if err != nil {
		return err
	}
	return writeSecret(e.runUser, e.cfg.mcpConfig(), config, 0o440)
}

// mcpEnv is what the harness's environment gets for the MCP token: the
// path of its file and the MCP server's URL (none for a Run with no MCP
// token).
func (e *execution) mcpEnv() []string {
	if e.task.MCP == nil {
		return nil
	}
	return []string{dispatcherapi.EnvMCPTokenFile + "=" + e.cfg.mcpToken(), dispatcherapi.EnvMCPURL + "=" + e.task.MCP.URL}
}
