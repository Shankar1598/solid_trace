package dispatcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
)

// An Admin can change the Factory settings while a Run waits. The task
// call of a later Execution then names the Run's own repository, folder
// and base branch, as its start kept them: never the repository of one
// settings version and the folder of another.
func TestTheTaskNamesTheRunsRepositoryAndFolderAfterTheSettingsChange(t *testing.T) {
	w := newAdminWorld(t)
	d, now := w.d, w.clock()
	run := storedRun{Run: dispatcherapi.Run{ID: "r1", Organization: "example", Project: "shop", Prompt: "Fix the cart",
		State: dispatcherapi.StateQueued, CreatedAt: now}, organizationID: "7", projectID: "42",
		repository: "bitbucket.org/example/mono", folder: "apps/shop", baseBranch: "develop"}
	if err := d.store.insertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := d.store.saveSettings("7", "42", "shop", dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/shop2", BaseBranch: "main"}, "", nil, person{id: "1", name: "Ana"}, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.store.addExecution(run.ID, "e-1", now); !ok || err != nil {
		t.Fatalf("add the Execution: %v %v", ok, err)
	}
	e, err := d.store.execution("e-1")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	d.task(rec, httptest.NewRequest("POST", "/task", nil), e)
	if rec.Code != http.StatusOK {
		t.Fatalf("the task call: %d %s", rec.Code, rec.Body)
	}
	var task dispatcherapi.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Repository != "bitbucket.org/example/mono" || task.CloneURL != "https://bitbucket.org/example/mono.git" ||
		task.Folder != "apps/shop" || task.BaseBranch != "develop" {
		t.Errorf("the task: repository %q, clone URL %q, folder %q, base branch %q; want the Run's",
			task.Repository, task.CloneURL, task.Folder, task.BaseBranch)
	}
}
