package wrapper

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The secret files and the wrapper's state files are never in a save, and
// the save runs no program of the agent's git configuration.

// remoteObjects is each object that the remote has, with its content.
func remoteObjects(t *testing.T, remote string) string {
	t.Helper()
	out, err := exec.Command("git", "--git-dir", remote, "cat-file", "--batch-all-objects", "--batch").CombinedOutput()
	if err != nil {
		t.Fatalf("read the remote's objects: %v %s", err, out)
	}
	return string(out)
}

func TestASaveHasNoSecretFileAndNoStateFileOfTheWrapperAlsoThroughALink(t *testing.T) {
	e, remote := savingExecution(t, nil, nil)
	const secret = "the-forge-token-0123456789"
	if err := os.MkdirAll(e.cfg.secrets(), 0o700); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(e.cfg.secrets(), "forge-token")
	if err := os.WriteFile(token, []byte(secret), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.cfg.runState(), []byte(`{"state":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The agent links to the secret file and to the state file.
	for name, target := range map[string]string{"token": token, "state.json": e.cfg.runState(), "dir": e.cfg.dir} {
		if err := os.Symlink(target, filepath.Join(e.checkout, name)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted); err != nil {
		t.Fatalf("the save: %v", err)
	}

	if objects := remoteObjects(t, remote); strings.Contains(objects, secret) {
		t.Errorf("the remote has the secret after the save:\n%s", objects)
	}
	tree := gitRun(t, "--git-dir", remote, "ls-tree", "refs/agent-wip/"+saveRunID)
	for _, name := range []string{"token", "state.json", "dir"} {
		if !strings.Contains(tree, "120000 blob") || !strings.Contains(tree, "\t"+name) {
			t.Errorf("the save does not keep %s as a link:\n%s", name, tree)
		}
	}
	for _, dir := range []string{e.cfg.secrets(), e.cfg.dir} {
		if strings.HasPrefix(dir+"/", e.checkout+"/") {
			t.Errorf("%s is in the checkout", dir)
		}
	}
}

func TestTheWrapperRefusesAnExecutionDirectoryInTheWorkspace(t *testing.T) {
	for _, dir := range []string{"/workspace/state", "/workspace"} {
		_, err := loadConfig(func(name string) string {
			return map[string]string{EnvDispatcherURL: "http://127.0.0.1:1", EnvExecution: "ex1",
				EnvIdentityFile: "/id", EnvWorkspace: "/workspace", EnvDir: dir}[name]
		})
		if err == nil || !strings.Contains(err.Error(), EnvDir) {
			t.Errorf("%s=%s in the workspace: %v", EnvDir, dir, err)
		}
	}
}

func TestASaveRunsNoProgramOfTheAgentsGitConfiguration(t *testing.T) {
	e, remote := savingExecution(t, map[string]string{"app.txt": "one\n"}, nil)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "agent-program")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"core.fsmonitor", script}, {"filter.agent.clean", script},
		{"filter.agent.process", script}, {"core.hooksPath", filepath.Dir(script)}} {
		gitRun(t, "-C", e.checkout, "config", kv[0], kv[1])
	}
	writeFiles(t, e.checkout, map[string]string{".gitattributes": "* filter=agent\n", "app.txt": "two\n", "new.txt": "new\n"})

	if _, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted); err != nil {
		t.Fatalf("the save: %v", err)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Errorf("the save ran a program of the agent's git configuration")
	}
	if got := gitRun(t, "--git-dir", remote, "show", "refs/agent-wip/"+saveRunID+":app.txt"); got != "two" {
		t.Errorf("app.txt in the save: %q", got)
	}
}
