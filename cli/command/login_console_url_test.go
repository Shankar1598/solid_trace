package command_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Console answers 404 to the OAuth metadata request when no Console URL is
// set (or it is not HTTPS), so the message tells the operator what to set.
func TestLoginToAConsoleWithNoConsoleURLTellsTheOperatorToSetIt(t *testing.T) {
	w := newWorld(t, acme)
	console := httptest.NewServer(http.NotFoundHandler())
	defer console.Close()

	if code := w.run("login", "--ingest-url", w.agent.URL(), console.URL); code == 0 {
		t.Fatal("login passed with no OAuth metadata")
	}
	if !strings.Contains(w.stderr.String(), "set the Console URL in Settings → Instance") {
		t.Errorf("stderr: %s", w.stderr)
	}
	w.assertNotSignedIn()
}
