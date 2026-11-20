package wrapper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// A stop command sets the stopping state before the end starts. The
// Runner's SIGTERM after the stop is then not the machine's stop: no
// preemption handler starts, and the stop command ends the Execution.
func TestASIGTERMAfterAStopCommandStartsNoPreemptionHandler(t *testing.T) {
	dispatcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req dispatcherapi.CommandsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.After >= 1 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(dispatcherapi.Commands{Commands: []dispatcherapi.Command{{Seq: 1, Kind: dispatcherapi.CommandStop}}})
	}))
	t.Cleanup(dispatcher.Close)
	identity := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(identity, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config{dispatcherURL: dispatcher.URL, execution: "e-1", identityFile: identity}
	e := newExecution(context.Background(), cfg, newClient(cfg), dispatcherapi.Task{}, io.Discard, nil, runUser{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	e.pollCommands(ctx)
	if !e.stopping.Load() {
		t.Fatal("the stop command did not set the stopping state")
	}
	if end, ending := e.endedEarly(); !ending || end.Reason != dispatcherapi.EndStopped {
		t.Fatalf("the end after the stop command: %+v, %v, want the reason stopped", end, ending)
	}

	signals := make(chan os.Signal, 1)
	watching, stopWatching := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.watch(watching, noNotice{}, signals); close(done) }()
	signals <- syscall.SIGTERM
	time.Sleep(200 * time.Millisecond)
	stopWatching()
	<-done
	if owner := e.owner.Load(); owner != ownerNone {
		t.Errorf("the end's owner after the SIGTERM is %d, want none: the preemption handler started", owner)
	}
	if e.isMarked() {
		t.Error("the SIGTERM after the stop command set the preempting marker")
	}
}
