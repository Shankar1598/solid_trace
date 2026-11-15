package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The wrapper asks for the commands after the last one that it got, so
// that it does not act on a command again. A command of a kind that the
// wrapper does not know does not end the poll: the next call must give
// its seq as after.
func TestTheCommandsCallAsksForTheCommandsAfterTheLastOneThatTheWrapperGot(t *testing.T) {
	var mu sync.Mutex
	var afters []int64
	dispatcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req dispatcherapi.CommandsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		afters = append(afters, req.After)
		calls := len(afters)
		mu.Unlock()
		// After the first command, or after 4 calls: the Execution ended.
		if req.After >= 1 || calls >= 4 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(dispatcherapi.Commands{Commands: []dispatcherapi.Command{{Seq: 1, Kind: "pause"}}})
	}))
	t.Cleanup(dispatcher.Close)
	identity := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(identity, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	e := &execution{log: &log, client: newClient(config{dispatcherURL: dispatcher.URL, execution: "e-1", identityFile: identity})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	e.pollCommands(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(afters) != 2 || afters[0] != 0 || afters[1] != 1 {
		t.Errorf("the commands calls asked after %v, want [0 1]", afters)
	}
	if n := strings.Count(log.String(), `the command "pause"`); n != 1 {
		t.Errorf("the wrapper logged the unknown command %d times, want 1:\n%s", n, log.String())
	}
}
