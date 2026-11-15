package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestTheDispatcherRefusesToStartAndNamesEachAbsentValue(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), nil, env(nil), &stderr)

	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{"DISPATCHER_SOLIDTRACE_URL", "DISPATCHER_SOLIDTRACE_TOKEN", "DISPATCHER_DATABASE", "DISPATCHER_ENCRYPTION_KEY"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the error does not name %s:\n%s", want, stderr.String())
		}
	}
}

func TestTheDispatcherRefusesASolidTraceURLThatIsNotHTTP(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), nil, env(map[string]string{
		"DISPATCHER_SOLIDTRACE_URL":   "console.example.com",
		"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
		"DISPATCHER_DATABASE":         filepath.Join(t.TempDir(), "d.sqlite3"),
	}), &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "http:// or https://") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestTheDispatcherReadsTheFileAndTheEnvironmentWinsOverIt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "dispatcher.env")
	listen := freeAddress(t)
	content := "# The operator configuration\n" +
		"DISPATCHER_SOLIDTRACE_URL=https://console.example.com\n" +
		"DISPATCHER_SOLIDTRACE_TOKEN=\"stk_from_file\"\n" +
		"DISPATCHER_DATABASE=" + filepath.Join(dir, "d.sqlite3") + "\n" +
		"DISPATCHER_ENCRYPTION_KEY=" + testKey + "\n" +
		"DISPATCHER_LISTEN=127.0.0.1:1\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	var stderr bytes.Buffer
	go func() {
		done <- run(ctx, []string{"-config", file}, env(map[string]string{"DISPATCHER_LISTEN": listen}), &stderr)
	}()

	// The listener from the environment answers: the API needs a token.
	var status int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		resp, err := http.Get("http://" + listen + "/api/v1/runs")
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
			break
		}
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("exit %d after a stop, want 0: %s", code, stderr.String())
	}
	if status != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/runs with no token: %d, want 401 (stderr %s)", status, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "d.sqlite3")); err != nil {
		t.Errorf("the database from the file was not made: %v", err)
	}
}

func TestTheDispatcherRefusesAConfigurationFileThatIsNotThere(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", filepath.Join(t.TempDir(), "none.env")}, env(nil), &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "configuration file") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}

const testKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func TestTheDispatcherRefusesAWorkInProgressPlaceThatIsNotOneOfTheTwo(t *testing.T) {
	var stderr bytes.Buffer
	code := run(context.Background(), nil, env(map[string]string{
		"DISPATCHER_SOLIDTRACE_URL":    "https://console.example.com",
		"DISPATCHER_SOLIDTRACE_TOKEN":  "stk_x",
		"DISPATCHER_DATABASE":          filepath.Join(t.TempDir(), "d.sqlite3"),
		"DISPATCHER_ENCRYPTION_KEY":    testKey,
		"DISPATCHER_WIP_REFS":          "refs/tags/wip",
		"DISPATCHER_BITBUCKET_API_URL": "api.bitbucket.example",
	}), &stderr)
	for _, want := range []string{"DISPATCHER_WIP_REFS", "refs/agent-wip or refs/heads/agent-wip", "DISPATCHER_BITBUCKET_API_URL"} {
		if code != 1 || !strings.Contains(stderr.String(), want) {
			t.Errorf("exit %d; the error does not name %q: %s", code, want, stderr.String())
		}
	}
}

func TestTheDispatcherRefusesToStartWithAnotherKeyThanTheDatabaseHas(t *testing.T) {
	values := map[string]string{
		"DISPATCHER_SOLIDTRACE_URL":   "https://console.example.com",
		"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
		"DISPATCHER_DATABASE":         filepath.Join(t.TempDir(), "d.sqlite3"),
		"DISPATCHER_ENCRYPTION_KEY":   testKey,
		"DISPATCHER_LISTEN":           freeAddress(t),
	}
	// The first start makes the database with testKey.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if code := run(ctx, nil, env(values), &stderr); code != 0 {
		t.Fatalf("the first start: exit %d: %s", code, stderr.String())
	}

	values["DISPATCHER_ENCRYPTION_KEY"] = "HxwbGhkYFxYVFBMSERAPDg0MCwoJCAcGBQQDAgEAAAE="
	stderr.Reset()
	code := run(ctx, nil, env(values), &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "DISPATCHER_ENCRYPTION_KEY is not the key") {
		t.Errorf("exit %d, stderr %q, want 1 and an error that names DISPATCHER_ENCRYPTION_KEY", code, stderr.String())
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}
