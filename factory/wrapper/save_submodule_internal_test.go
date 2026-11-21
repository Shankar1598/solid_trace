package wrapper

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A submodule with changes saves to the same ref name in its own
// repository.

// submoduleExecution is an Execution after its setup, on a clone of a
// remote whose main has the submodule vendor/lib (../lib.git, on the
// forge's git URL). It gives the Execution, the remote and lib's remote.
func submoduleExecution(t *testing.T) (e *execution, remote, lib string) {
	t.Helper()
	dir := t.TempDir()
	lib = filepath.Join(dir, "lib.git")
	libSeed := filepath.Join(dir, "lib-seed")
	gitRun(t, "init", "-q", "--bare", "-b", "main", lib)
	gitRun(t, "init", "-q", "-b", "main", libSeed)
	commitFiles(t, libSeed, map[string]string{"lib.txt": "one\n"}, "The lib")
	gitRun(t, "-C", libSeed, "push", "-q", lib, "main")
	libCommit := gitRun(t, "-C", libSeed, "rev-parse", "HEAD")

	remote, seed := seedRemote(t, dir)
	writeFiles(t, seed, map[string]string{".gitmodules": "[submodule \"lib\"]\n\tpath = vendor/lib\n\turl = ../lib.git\n"})
	gitRun(t, "-C", seed, "add", ".gitmodules")
	gitRun(t, "-C", seed, "update-index", "--add", "--cacheinfo", "160000,"+libCommit+",vendor/lib")
	gitRun(t, "-C", seed, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "Add the lib")
	gitRun(t, "-C", seed, "push", "-q", remote, "main")

	cfg := testConfig(dir)
	task := dispatcherapi.Task{RunID: saveRunID, ExecutionID: "ex1", Repository: "bitbucket.org/example/shop",
		CloneURL: "file://" + remote, Branch:dispatcherapi.RunBranch(saveRunID), BaseBranch: "main"}
	var log bytes.Buffer
	e = setupExecution(t, cfg, &log, task)
	// git clones a submodule over file:// only when it is permitted.
	e.clock, e.environ = realClock{}, append(os.Environ(), "GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=protocol.file.allow", "GIT_CONFIG_VALUE_0=always")
	e.saver = newWIPSaver(e)
	if err := e.setup(context.Background(), e.environ); err != nil {
		t.Fatalf("setup: %v\n%s", err, log.String())
	}
	if _, err := os.Stat(filepath.Join(e.checkout, "vendor", "lib", "lib.txt")); err != nil {
		t.Fatalf("the setup did not check out the submodule: %v\n%s", err, log.String())
	}
	// The forge's git URL: the setup takes only HTTPS, the save any URL.
	e.task.GitURL = "file://" + dir
	return e, remote, lib
}

func TestAChangedSubmoduleSavesToTheSameRefNameInItsRepository(t *testing.T) {
	e, remote, lib := submoduleExecution(t)
	sub := filepath.Join(e.checkout, "vendor", "lib")
	subHead := gitRun(t, "-C", sub, "rev-parse", "HEAD")
	writeFiles(t, sub, map[string]string{"lib.txt": "two\n", "added.txt": "added\n"})
	writeFiles(t, e.checkout, map[string]string{"app.txt": "app\n"})
	before := agentState(t, sub)

	saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
	if err != nil {
		t.Fatalf("the save: %v", err)
	}

	ref := "refs/agent-wip/" + saveRunID
	if got := gitRun(t, "--git-dir", lib, "rev-parse", ref+"^"); got != subHead {
		t.Errorf("the submodule's WIP commit's parent is %s, not its HEAD %s", got, subHead)
	}
	for path, want := range map[string]string{"lib.txt": "two", "added.txt": "added"} {
		if got := gitRun(t, "--git-dir", lib, "show", ref+":"+path); got != want {
			t.Errorf("%s in the submodule's save: %q, want %q", path, got, want)
		}
	}
	if got := gitRun(t, "--git-dir", remote, "show", ref+":app.txt"); got != "app" {
		t.Errorf("app.txt in the checkout's save: %q", got)
	}
	if after := agentState(t, sub); after != before {
		t.Errorf("the save changed the submodule's checkout:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if len(saved.Skipped) != 0 {
		t.Errorf("the save left out %v", saved.Skipped)
	}
}

func TestAnUnchangedSubmoduleIsNotPushed(t *testing.T) {
	e, remote, lib := submoduleExecution(t)
	writeFiles(t, e.checkout, map[string]string{"app.txt": "app\n"})

	if _, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted); err != nil {
		t.Fatalf("the save: %v", err)
	}

	if refs := gitRun(t, "--git-dir", lib, "for-each-ref"); strings.Contains(refs, "agent-wip") {
		t.Errorf("the save pushed the submodule with no change: %s", refs)
	}
	if refs := gitRun(t, "--git-dir", remote, "for-each-ref"); !strings.Contains(refs, "refs/agent-wip/"+saveRunID) {
		t.Errorf("the checkout is not saved: %s", refs)
	}
}

// A submodule that the save cannot push, or whose URL is not on the
// forge, is listed as not saved. The checkout is saved all the same.
func TestASubmoduleThatCannotBePushedIsListedAsNotSaved(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, e *execution, lib string)
		reason string
	}{
		{"the remote refuses the push", func(t *testing.T, e *execution, lib string) {
			writeFiles(t, lib, map[string]string{"hooks/pre-receive": "#!/bin/sh\necho refused >&2\nexit 1\n"})
			if err := os.Chmod(filepath.Join(lib, "hooks", "pre-receive"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "the push to refs/agent-wip/" + saveRunID + " failed"},
		{"a URL on another host", func(t *testing.T, e *execution, lib string) {
			writeFiles(t, e.checkout, map[string]string{".gitmodules": "[submodule \"lib\"]\n\tpath = vendor/lib\n" +
				"\turl = https://elsewhere.example/lib.git\n"})
		}, "is not on the forge"},
		{"a local path", func(t *testing.T, e *execution, lib string) {
			writeFiles(t, e.checkout, map[string]string{".gitmodules": "[submodule \"lib\"]\n\tpath = vendor/lib\n" +
				"\turl = " + lib + "\n"})
		}, "is not on the forge"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, remote, lib := submoduleExecution(t)
			writeFiles(t, filepath.Join(e.checkout, "vendor", "lib"), map[string]string{"lib.txt": "two\n"})
			c.change(t, e, lib)

			saved, err := e.saver.save(context.Background(), dispatcherapi.EndPreempted)
			if err != nil {
				t.Fatalf("the save: %v", err)
			}

			if len(saved.Skipped) != 1 || !strings.HasPrefix(saved.Skipped[0], "vendor/lib (a submodule that is not saved: ") ||
				!strings.Contains(saved.Skipped[0], c.reason) {
				t.Errorf("the save names as left out %q, want vendor/lib as not saved (%s)", saved.Skipped, c.reason)
			}
			if refs := gitRun(t, "--git-dir", lib, "for-each-ref"); strings.Contains(refs, "agent-wip") {
				t.Errorf("the submodule's remote has a WIP ref: %s", refs)
			}
			if refs := gitRun(t, "--git-dir", remote, "for-each-ref"); !strings.Contains(refs, "refs/agent-wip/"+saveRunID) {
				t.Errorf("the checkout is not saved: %s", refs)
			}
		})
	}
}

// A submodule's URL is the forge's, as the run user's git configuration
// rewrites it; a URL that is not on the forge gets no push.
func TestASubmodulesURLMustBeOnTheForge(t *testing.T) {
	e := &execution{task: dispatcherapi.Task{Repository: "bitbucket.org/example/shop",
		GitURL: "https://git.example.test/git/"}}
	parent := "https://git.example.test/git/example/shop.git"
	for raw, want := range map[string]string{
		"git@bitbucket.org:example/lib.git":                "https://git.example.test/git/example/lib.git",
		"ssh://git@bitbucket.org/example/lib.git":          "https://git.example.test/git/example/lib.git",
		"../lib.git":                                       "https://git.example.test/git/example/lib.git",
		"./nested.git":                                     "https://git.example.test/git/example/shop.git/nested.git",
		"https://git.example.test/git/other/lib.git":       "https://git.example.test/git/other/lib.git",
		"https://elsewhere.example/git/example/lib.git":    "",
		"https://git.example.test/gitx/example/lib.git":    "",
		"https://user@git.example.test/git/example/x.git":  "",
		"https://git.example.test/git/example/../../x.git": "",
		"../../../../x.git":                                "",
		"/srv/lib.git":                                     "",
		"ext::sh -c touch% /tmp/x":                         "",
		"git@elsewhere.example:example/lib.git":            "",
	} {
		got, err := e.submoduleURL(raw, parent)
		if want == "" && err == nil {
			t.Errorf("%q gives %q, want a refusal", raw, got)
		}
		if want != "" && (err != nil || got != want) {
			t.Errorf("%q gives %q (%v), want %q", raw, got, err, want)
		}
	}
}
