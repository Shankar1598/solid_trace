package main_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
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

// startLongRead starts a read of Project 123's Logs of all time that runs
// until it is cancelled, and waits a moment for it to begin. It returns the
// cancel, and where the read's error arrives.
func startLongRead(t *testing.T, env *TestEnv) (context.CancelFunc, <-chan error) {
	t.Helper()
	setSQLQueries(t, env, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := env.SQL.RunSQL(ctx, 123, allTime.from, allTime.to,
			"SELECT count() FROM logs, numbers(1000000000000)", 10)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	return cancel, done
}

func TestALongReadHoldsUpNeitherLogProcessingNorOtherReads(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// The hot window is 11:45 to 13:00, so the Log export moves the first
	// Log to a file.
	postAndProcess(t, env,
		logJSON(t, "exported", testNow.Add(-150*time.Minute), nil),
		logJSON(t, "hot", testNow.Add(-10*time.Minute), nil),
	)

	cancel, longRead := startLongRead(t, env)
	defer cancel()

	// The Log export starts while it runs.
	exported := make(chan error, 1)
	go func() { exported <- env.LogStore.ExportLogs(env.Clock.Now()) }()
	time.Sleep(300 * time.Millisecond)

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
	want := []string{"while reading", "hot", "exported"}
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
	if err := <-exported; err != nil {
		t.Fatalf("Log export failed: %v", err)
	}
	if got, err := listLatest(env); err != nil || !slices.Equal(got, want) {
		t.Errorf("Expected %v after the export, got %v (%v)", want, got, err)
	}
}

func TestTheDiskLimitWaitsForAReadOfTheFilesItWouldDelete(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postAndProcess(t, env, logJSON(t, "exported", testNow.Add(-150*time.Minute), nil))
	env.ExportLogs(t)
	file := hourFile(t, env, 123, testNow.Add(-150*time.Minute))
	cancel, longRead := startLongRead(t, env)
	defer cancel()

	// The file stays while the read may still read it.
	if err := env.LogStore.EnforceDiskLimit(storage.LogDiskLimit{Bytes: 1}); !errors.Is(err, storage.ErrLogFilesInUse) {
		t.Errorf("Expected the disk limit to wait for the read, got %v", err)
	}
	if got, err := listLatest(env); err != nil || !slices.Equal(got, []string{"exported"}) {
		t.Errorf("Expected the Log kept during the read, got %v (%v)", got, err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("Expected the file kept on disk during the read, got %v", err)
	}

	// Its next pass, after the read, deletes it.
	cancel()
	<-longRead
	enforceLogDiskLimit(t, env, 1)
	if got, err := listLatest(env); err != nil || len(got) != 0 {
		t.Errorf("Expected the Log deleted after the read, got %v (%v)", got, err)
	}
	assertGone(t, filepath.Dir(file), "by the disk limit after the read")
}
