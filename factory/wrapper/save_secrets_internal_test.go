package wrapper

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// The wrapper never reads the agent's .git/info/exclude: git reads it as
// the run user, with the agent's rights. A link at .git/info or at
// info/exclude to a file of root's gives no copy of that file in the
// save's folder, and the agent's own patterns still apply.
func TestTheWrapperCopiesNoFileThatTheAgentsInfoExcludeNames(t *testing.T) {
	for _, link := range []string{"info", "info/exclude"} {
		t.Run(link, func(t *testing.T) {
			e, remote := savingExecution(t, map[string]string{"app.txt": "one\n"}, func(task *dispatcherapi.Task) {
				task.SaveExclude = []string{"factory.txt"}
			})
			outside := t.TempDir()
			writeFiles(t, outside, map[string]string{"exclude": "local.txt\n# the secret of root\n"})
			gitDir := filepath.Join(e.checkout, ".git")
			if err := os.RemoveAll(filepath.Join(gitDir, "info")); err != nil {
				t.Fatal(err)
			}
			if link == "info" {
				if err := os.Symlink(outside, filepath.Join(gitDir, "info")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(gitDir, "info"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "exclude"), filepath.Join(gitDir, "info", "exclude")); err != nil {
					t.Fatal(err)
				}
			}

			r := &repoSave{work: e.checkout, gitDir: gitDir}
			defer r.remove()
			if err := newWIPSaver(e).prepare(r); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			err := filepath.WalkDir(r.dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, err := os.ReadFile(path)
				if bytes.Contains(b, []byte("the secret of root")) {
					t.Errorf("the wrapper copied the agent's exclude file to %s", path)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}

			writeFiles(t, e.checkout, map[string]string{"factory.txt": "x\n", "local.txt": "x\n", "new.txt": "new\n"})
			saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
			if err != nil {
				t.Fatalf("the save: %v", err)
			}
			if got := gitRun(t, "--git-dir", remote, "show", "refs/agent-wip/"+saveRunID+":new.txt"); got != "new" {
				t.Errorf("new.txt in the save: %q", got)
			}
			slices.Sort(saved.Skipped)
			if want := []string{"factory.txt", "local.txt"}; !slices.Equal(saved.Skipped, want) {
				t.Errorf("the save names as left out %q, want %q", saved.Skipped, want)
			}
		})
	}
}
