package dispatcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// prRun makes the Run runID of the Organization org with the running
// Execution executionID, whose Project has Factory settings for the fake
// forge's example/shop, and pushes the Run's branch.
func prRun(t *testing.T, d *Dispatcher, f *fakeforge.Forge, org, project, runID, executionID string) storedExecution {
	t.Helper()
	now := time.Now().UTC()
	dana := person{id: "1", name: "dana@example.com"}
	if _, err := d.store.saveConnection(org, "bitbucket", "bitbucket", f.Token, dana, now); err != nil {
		t.Fatal(err)
	}
	connection, err := d.store.connectionID(org, "bitbucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.store.saveSettings(org, project, "shop", dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/shop", BaseBranch: "main"}, connection, dana, now); err != nil {
		t.Fatal(err)
	}
	run := storedRun{Run: dispatcherapi.Run{ID: runID, Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: now}, organizationID: org, projectID: project,
		repository: "bitbucket.org/example/shop", baseBranch: "main"}
	if err := d.store.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.store.addExecution(runID, executionID, now); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	if _, err := d.store.startExecution(executionID, now, defaultLimits); err != nil {
		t.Fatal(err)
	}
	f.Push("example/shop", dispatcherapi.RunBranch(runID), map[string]string{"fix.txt": runID + "\n"}, "Fix")
	e, err := d.store.execution(executionID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func prRequest(ctx context.Context, title string) *http.Request {
	body, _ := json.Marshal(dispatcherapi.PRText{Title: title})
	return httptest.NewRequest("POST", "/pr", strings.NewReader(string(body))).WithContext(ctx)
}

func prWorld(t *testing.T) (*Dispatcher, *fakeforge.Forge) {
	d, f := forgeWorld(t, "")
	d.now = time.Now
	return d, f
}

// The wrapper's pr call can go while the forge opens the PR: its client
// times out, or the agent's process is killed. The forge still opens the
// PR, so the Dispatcher waits for it and records it on the Run.
func TestAPRThatTheForgeOpensAfterTheWrapperWentIsRecorded(t *testing.T) {
	d, f := prWorld(t)
	e := prRun(t, d, f, "7", "42", "r-1", "e-1")
	reached, release := f.HoldNextAPI()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.pr(httptest.NewRecorder(), prRequest(ctx, "Fix the cart"), e)
	}()
	<-reached
	cancel()
	release()
	<-done

	prs := f.PRs("example/shop")
	if len(prs) != 1 {
		t.Fatalf("want one PR on the forge, got %+v", prs)
	}
	run, err := d.store.run("7", "r-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.PRURL != prs[0].Link {
		t.Errorf("the Run shows the PR %q, not the forge's %q", run.PRURL, prs[0].Link)
	}
	if n := count(t, d.store, `SELECT count(*) FROM outbox WHERE body LIKE '%pr_opened%'`); n != 1 {
		t.Errorf("%d PR opened events, want 1", n)
	}
}

// A slow forge call of one Run does not make the pr calls of other Runs
// wait.
func TestAPRCallOfOneRunDoesNotWaitOnTheForgeCallOfAnother(t *testing.T) {
	d, f := prWorld(t)
	slow := prRun(t, d, f, "7", "42", "r-1", "e-1")
	other := prRun(t, d, f, "8", "43", "r-2", "e-2")
	reached, release := f.HoldNextAPI()
	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		d.pr(httptest.NewRecorder(), prRequest(context.Background(), "Fix the cart"), slow)
	}()
	<-reached

	otherDone := make(chan *httptest.ResponseRecorder)
	go func() {
		rec := httptest.NewRecorder()
		d.pr(rec, prRequest(context.Background(), "Fix the basket"), other)
		otherDone <- rec
	}()
	select {
	case rec := <-otherDone:
		if rec.Code != http.StatusOK {
			t.Errorf("the other Run's pr call: %d %s", rec.Code, rec.Body)
		}
	case <-time.After(10 * time.Second):
		t.Error("the other Run's pr call waits on the slow forge call")
		release()
		<-otherDone
	}
	release()
	<-slowDone
	if prs := f.PRs("example/shop"); len(prs) != 2 {
		t.Errorf("want two PRs, got %+v", prs)
	}
}
