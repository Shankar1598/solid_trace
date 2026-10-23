package main_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// listLatest lists Project 123's latest Logs through the query app without
// failing the test, so that it can run in its own goroutine.
func listLatest(env *TestEnv) ([]string, error) {
	raw, err := json.Marshal(queryRequest{From: allTime.from, To: allTime.to, Limit: 100})
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("POST", "/api/123/logs/query", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.QueryApp.Test(req, -1)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var page queryPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return messages(page.Logs), nil
}

// startLongRead starts an SQL query of Project 123 that runs until it is
// cancelled, and waits a moment for it to begin. It returns the cancel, and
// where the read's error arrives.
func startLongRead(t *testing.T, env *TestEnv) (context.CancelFunc, <-chan error) {
	t.Helper()
	setSQLQueries(t, env, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := env.LogStore.RunSQL(ctx, 123, allTime.from, allTime.to,
			"SELECT count(*) FROM range(1000000000000)", 10)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	return cancel, done
}

func TestALongReadHoldsUpNeitherLogProcessingNorOtherReads(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postAndProcess(t, env,
		logJSON(t, "earlier", testNow.Add(-150*time.Minute), nil),
		logJSON(t, "recent", testNow.Add(-10*time.Minute), nil),
	)

	cancel, longRead := startLongRead(t, env)
	defer cancel()

	// Log processing stores new Logs, and another read lists them.
	postLogs(t, env, "application/json", logJSON(t, "while reading", testNow, nil))
	type listing struct {
		messages []string
		err      error
	}
	listed := make(chan listing, 1)
	go func() {
		if err := env.LogProcessor.ProcessUntilCaughtUp(); err != nil {
			listed <- listing{err: fmt.Errorf("process: %w", err)}
			return
		}
		got, err := listLatest(env)
		listed <- listing{got, err}
	}()
	want := []string{"while reading", "recent", "earlier"}
	select {
	case got := <-listed:
		if got.err != nil {
			t.Fatalf("Processing and listing failed: %v", got.err)
		}
		if !slices.Equal(got.messages, want) {
			t.Errorf("Expected %v while the long read runs, got %v", want, got.messages)
		}
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Log processing or another read waited for the long read")
	}

	select {
	case err := <-longRead:
		t.Fatalf("Expected the long read to still be running, it ended: %v", err)
	default:
	}

	// Cancelling the read stops its query.
	cancel()
	select {
	case err := <-longRead:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected the cancelled read to fail as cancelled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Expected the cancelled read to stop")
	}
}
