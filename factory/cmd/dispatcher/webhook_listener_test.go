package main

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheWebhookListenerServesOnlyWebhooksAndThePrivateListenerServesTheAPI(t *testing.T) {
	private, webhooks := freeAddress(t), freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	var stderr bytes.Buffer
	go func() {
		done <- run(ctx, nil, env(map[string]string{
			"DISPATCHER_SOLIDTRACE_URL":   "https://console.example.com",
			"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
			"DISPATCHER_DATABASE":         filepath.Join(t.TempDir(), "d.sqlite3"),
			"DISPATCHER_ENCRYPTION_KEY":   testKey,
			"DISPATCHER_LISTEN":           private,
			"DISPATCHER_WEBHOOK_LISTEN":   webhooks,
			"DISPATCHER_WEBHOOK_URL":      "https://factory.example.com/",
		}), &stderr)
	}()
	status := func(method, url string) int {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			req, _ := http.NewRequest(method, url, strings.NewReader("{}"))
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				resp.Body.Close()
				return resp.StatusCode
			}
		}
		return 0
	}
	got := map[string]int{
		"GET /api/v1/runs on the private listener":      status("GET", "http://"+private+"/api/v1/runs"),
		"GET /api/v1/runs on the webhook listener":      status("GET", "http://"+webhooks+"/api/v1/runs"),
		"GET /api/v1/status on the webhook listener":    status("GET", "http://"+webhooks+"/api/v1/status"),
		"POST /webhooks/fake/x on the webhook listener": status("POST", "http://"+webhooks+"/webhooks/fake/x"),
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("exit %d after a stop, want 0: %s", code, stderr.String())
	}
	want := map[string]int{
		"GET /api/v1/runs on the private listener":      http.StatusUnauthorized,
		"GET /api/v1/runs on the webhook listener":      http.StatusNotFound,
		"GET /api/v1/status on the webhook listener":    http.StatusNotFound,
		"POST /webhooks/fake/x on the webhook listener": http.StatusNotFound,
	}
	for name, status := range want {
		if got[name] != status {
			t.Errorf("%s: %d, want %d (stderr %s)", name, got[name], status, stderr.String())
		}
	}
	if !strings.Contains(stderr.String(), webhooks) {
		t.Errorf("the log does not name the webhook listener's address %s: %s", webhooks, stderr.String())
	}
}

func TestTheDispatcherRefusesAPublicWebhookURLThatIsNotHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	code := run(ctx, nil, env(map[string]string{
		"DISPATCHER_LISTEN":           freeAddress(t),
		"DISPATCHER_SOLIDTRACE_URL":   "https://console.example.com",
		"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
		"DISPATCHER_DATABASE":         filepath.Join(t.TempDir(), "d.sqlite3"),
		"DISPATCHER_ENCRYPTION_KEY":   testKey,
		"DISPATCHER_WEBHOOK_URL":      "factory.example.com",
	}), &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "DISPATCHER_WEBHOOK_URL") {
		t.Errorf("exit %d, stderr %q, want 1 and an error that names DISPATCHER_WEBHOOK_URL", code, stderr.String())
	}
}
