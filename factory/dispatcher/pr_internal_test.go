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
	return prRunWith(t, d, f, org, project, runID, executionID, true)
}

// prRunWith is prRun; when start is false, the Execution stays
// dispatched (its wrapper did not take its task).
func prRunWith(t *testing.T, d *Dispatcher, f *fakeforge.Forge, org, project, runID, executionID string,
	start bool) storedExecution {
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
	if start {
		if _, err := d.store.startExecution(executionID, now, defaultLimits); err != nil {
			t.Fatal(err)
		}
	}
	f.Push("example/shop", dispatcherapi.RunBranch(runID), map[string]string{"fix.txt": runID + "\n"}, "Fix")
	e, err := d.store.execution(executionID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func prRequest(ctx context.Context, title string) *http.Request {
	return prTextRequest(ctx, dispatcherapi.PRText{Title: title})
}

func prTextRequest(ctx context.Context, text dispatcherapi.PRText) *http.Request {
	body, _ := json.Marshal(text)
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

// callPR makes the pr call of the Execution e and gives its answer.
func callPR(d *Dispatcher, e storedExecution, text dispatcherapi.PRText) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	d.pr(rec, prTextRequest(context.Background(), text), e)
	return rec
}

// noPR fails the test when the forge has a PR or the Run has a PR
// opened event.
func noPR(t *testing.T, d *Dispatcher, f *fakeforge.Forge) {
	t.Helper()
	if prs := f.PRs("example/shop"); len(prs) != 0 {
		t.Errorf("the forge has PRs %+v, want none", prs)
	}
	if n := count(t, d.store, `SELECT count(*) FROM outbox WHERE body LIKE '%pr_opened%'`); n != 0 {
		t.Errorf("%d PR opened events, want none", n)
	}
}

func TestCheckPRText(t *testing.T) {
	title255 := "é" + strings.Repeat("a", maxPRTitle-1)
	for _, c := range []struct {
		name string
		text dispatcherapi.PRText
		ok   bool
	}{
		{"a title of 255 runes", dispatcherapi.PRText{Title: title255}, true},
		{"a title of 256 runes", dispatcherapi.PRText{Title: title255 + "a"}, false},
		{"a title of 255 multibyte runes", dispatcherapi.PRText{Title: strings.Repeat("é", maxPRTitle)}, true},
		{"a description only", dispatcherapi.PRText{Description: "Why"}, true},
		{"no title and no description", dispatcherapi.PRText{}, false},
		{"a title with a line feed", dispatcherapi.PRText{Title: "a\nb"}, false},
		{"a title with a carriage return", dispatcherapi.PRText{Title: "a\rb"}, false},
		{"a description with line breaks", dispatcherapi.PRText{Title: "a", Description: "b\nc\r\nd"}, true},
		{"a description of 64 KiB", dispatcherapi.PRText{Title: "a", Description: strings.Repeat("d", maxReportText)}, true},
		{"a description over 64 KiB", dispatcherapi.PRText{Title: "a", Description: strings.Repeat("d", maxReportText+1)}, false},
		{"a title that is not UTF-8", dispatcherapi.PRText{Title: "a\xffb"}, false},
		{"a description that is not UTF-8", dispatcherapi.PRText{Title: "a", Description: "\xff"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			problem := checkPRText(c.text)
			if c.ok && problem != "" {
				t.Errorf("refused: %s", problem)
			}
			if !c.ok && problem == "" {
				t.Error("accepted")
			}
		})
	}
}

// A title with a line break is refused before the forge.
func TestAPRTitleWithALineBreakIsRefused(t *testing.T) {
	d, f := prWorld(t)
	e := prRun(t, d, f, "7", "42", "r-1", "e-1")
	rec := callPR(d, e, dispatcherapi.PRText{Title: "Fix\nthe cart"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d %s, want 400", rec.Code, rec.Body)
	}
	noPR(t, d, f)
}

// Only a running Execution can open or update its Run's PR.
func TestAPRCallOfAnExecutionThatIsNotRunningIsRefused(t *testing.T) {
	d, f := prWorld(t)
	ended := prRun(t, d, f, "7", "42", "r-1", "e-1")
	if err := d.store.endExecution("e-1", dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndNoReport},
		time.Now()); err != nil {
		t.Fatal(err)
	}
	ended, err := d.store.execution("e-1")
	if err != nil {
		t.Fatal(err)
	}
	dispatched := prRunWith(t, d, f, "8", "43", "r-2", "e-2", false)
	for name, e := range map[string]storedExecution{"ended": ended, "dispatched": dispatched} {
		if rec := callPR(d, e, dispatcherapi.PRText{Title: "Fix the cart"}); rec.Code != http.StatusConflict {
			t.Errorf("%s Execution: got %d %s, want 409", name, rec.Code, rec.Body)
		}
	}
	noPR(t, d, f)
}

// The first pr call of a Run opens the PR, so it needs a title. A later
// call can change only the description.
func TestTheFirstPRCallNeedsATitle(t *testing.T) {
	d, f := prWorld(t)
	e := prRun(t, d, f, "7", "42", "r-1", "e-1")
	if rec := callPR(d, e, dispatcherapi.PRText{Description: "Why"}); rec.Code != http.StatusBadRequest {
		t.Errorf("first call with a description only: got %d %s, want 400", rec.Code, rec.Body)
	}
	noPR(t, d, f)

	if rec := callPR(d, e, dispatcherapi.PRText{Title: "Fix the cart"}); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body)
	}
	if rec := callPR(d, e, dispatcherapi.PRText{Description: "Why now"}); rec.Code != http.StatusOK {
		t.Fatalf("update with a description only: %d %s", rec.Code, rec.Body)
	}
	prs := f.PRs("example/shop")
	if len(prs) != 1 || prs[0].Title != "Fix the cart" || prs[0].Description != "Why now" {
		t.Errorf("want one PR 'Fix the cart' with the new description, got %+v", prs)
	}
}

// Two pr calls of one Run at the same time open one PR: the second waits
// for the first, then updates the PR that the first opened.
func TestTwoPRCallsOfOneRunOpenOnePR(t *testing.T) {
	d, f := prWorld(t)
	e := prRun(t, d, f, "7", "42", "r-1", "e-1")
	reached, release := f.HoldNextAPI()
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- callPR(d, e, dispatcherapi.PRText{Title: "First"}) }()
	<-reached

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { secondDone <- callPR(d, e, dispatcherapi.PRText{Title: "Second"}) }()
	// Release the forge when the second call waits on the Run's lock, or
	// when it ended without a wait.
	var second *httptest.ResponseRecorder
	deadline := time.Now().Add(10 * time.Second)
	for second == nil && !waitsOnLock(d, "r-1") {
		select {
		case second = <-secondDone:
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the second pr call does not wait and does not end")
		}
	}
	release()
	first := <-firstDone
	if second == nil {
		second = <-secondDone
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("pr calls: %d %s, %d %s", first.Code, first.Body, second.Code, second.Body)
	}
	prs := f.PRs("example/shop")
	if len(prs) != 1 || prs[0].Title != "Second" {
		t.Errorf("want one PR, updated to 'Second', got %+v", prs)
	}
	if n := count(t, d.store, `SELECT count(*) FROM outbox WHERE body LIKE '%pr_opened%'`); n != 1 {
		t.Errorf("%d PR opened events, want 1", n)
	}
}

// waitsOnLock is true when two pr calls of the Run hold or wait on its
// lock.
func waitsOnLock(d *Dispatcher, runID string) bool {
	d.prLocks.mu.Lock()
	defer d.prLocks.mu.Unlock()
	rl := d.prLocks.runs[runID]
	return rl != nil && rl.users == 2
}
