package wrapper

import (
	"slices"
	"strings"
	"testing"

	"github.com/solidtrace/cli/agentsocket"
)

// The harness gets none of the wrapper's or the Runner's names, no Claude
// credential, and only the agent's socket that the wrapper names.
func TestTheHarnessEnvironmentHasNoWrapperRunnerOrCredentialName(t *testing.T) {
	environ := []string{"PATH=/usr/bin", "HOME=/root", "WRAPPER_DIR=/alloc", "WRAPPER_IDENTITY=/id", "NOMAD_TOKEN=t",
		EnvAccountToken + "=account", "ANTHROPIC_API_KEY=k", "ANTHROPIC_AUTH_TOKEN=a", agentsocket.Env + "=/old.sock"}

	got := harnessEnv(environ, "/new.sock")

	want := []string{"PATH=/usr/bin", "HOME=/root", agentsocket.Env + "=/new.sock"}
	if !slices.Equal(got, want) {
		t.Errorf("the harness's environment is %v, want %v", got, want)
	}
	if got := harnessEnv(environ, ""); slices.ContainsFunc(got, func(kv string) bool { return strings.HasPrefix(kv, agentsocket.Env) }) {
		t.Errorf("with no socket, the environment has %v", got)
	}
}
