package wrapper_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/wrapper"
)

// The end's container step kills each running container of the machine.
// A wrapper with no WRAPPER_DOCKER (a test, or a wrapper on a dev box) must
// skip it, also when a docker program is on its PATH: on 2026-11-21 a test
// with no WRAPPER_DOCKER killed each container of the dev box.
func TestWithNoDockerProgramSetTheEndRunsNoDocker(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "--quiet", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "--allow-empty", "-m", "start"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
	}
	// A docker program first on the PATH, which only records that it ran.
	bin := filepath.Join(dir, "bin")
	ran := filepath.Join(dir, "docker-ran")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\necho \"$@\" >>"+ran+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	identity := filepath.Join(dir, "identity")
	if err := os.WriteFile(identity, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A task with no account token: the Execution ends before its setup,
	// and the end procedure runs.
	ends := make(chan dispatcherapi.ExecutionEnd, 1)
	dispatcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case dispatcherapi.Path + "/executions/x1/task":
			json.NewEncoder(w).Encode(dispatcherapi.Task{RunID: "r1", ExecutionID: "x1", Prompt: "Fix the cart",
				Repository: "example.com/shop/shop", CloneURL: repo, SessionID: "00000000-0000-4000-8000-000000000001"})
		case dispatcherapi.Path + "/executions/x1/end":
			var end dispatcherapi.ExecutionEnd
			json.NewDecoder(r.Body).Decode(&end)
			ends <- end
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer dispatcher.Close()

	self, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	// No wrapper.EnvDocker.
	values := map[string]string{wrapper.EnvDispatcherURL: dispatcher.URL, wrapper.EnvExecution: "x1",
		wrapper.EnvIdentityFile: identity, wrapper.EnvDir: filepath.Join(dir, "execution"),
		wrapper.EnvHarness: filepath.Join(dir, "no-harness"), wrapper.EnvRunUser: self.Username}
	var stderr strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if code := wrapper.Main(ctx, func(name string) string { return values[name] }, os.Environ(), &stderr); code != 0 {
		t.Fatalf("the wrapper exits %d: %s", code, stderr.String())
	}
	<-ends
	if b, err := os.ReadFile(ran); err == nil {
		t.Errorf("the wrapper ran docker with no WRAPPER_DOCKER: %s", b)
	}
	if !strings.Contains(stderr.String(), "skips the container step") {
		t.Errorf("the wrapper did not say that it skipped the container step:\n%s", stderr.String())
	}
}
