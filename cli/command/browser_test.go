package command_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func (w *world) assertNotSignedIn() {
	w.t.Helper()
	if _, err := os.Stat(w.credentialsFile()); !os.IsNotExist(err) {
		data, _ := os.ReadFile(w.credentialsFile())
		w.t.Errorf("credentials stored after a failed sign-in: %s", data)
	}
}

func TestLoginRefusesARedirectWithAnotherStateAndWaitsForTheRightOne(t *testing.T) {
	w := newWorld(t, acme)
	real := w.browser
	var strayStatus int
	w.browser = func(approval string) error {
		u, _ := url.Parse(approval)
		redirect := u.Query().Get("redirect_uri")
		// Another program on the machine sends its own code first.
		resp, err := http.Get(redirect + "?code=stolen&state=other&iss=" + url.QueryEscape(w.oauth.URL()))
		if err != nil {
			return err
		}
		resp.Body.Close()
		strayStatus = resp.StatusCode
		return real(approval)
	}

	if code := w.login(); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if strayStatus != http.StatusBadRequest {
		t.Errorf("stray redirect got %d, want 400", strayStatus)
	}
	for _, form := range w.oauth.TokenRequests {
		if form.Get("code") == "stolen" {
			t.Errorf("the CLI exchanged the stray code")
		}
	}
}

func TestLoginStopsWhenTheApprovalComesFromAnotherIssuer(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.Iss = "https://evil.example.com"

	if code := w.login(); code == 0 {
		t.Fatal("login passed with a wrong iss")
	}
	if !strings.Contains(w.stderr.String(), "another server") {
		t.Errorf("stderr: %s", w.stderr)
	}
	if len(w.oauth.TokenRequests) != 0 {
		t.Errorf("the CLI exchanged a code from another issuer")
	}
	w.assertNotSignedIn()
}

func TestLoginStopsWhenThePersonDenies(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.Deny = true

	if code := w.login(); code == 0 {
		t.Fatal("login passed after Deny")
	}
	if !strings.Contains(w.stderr.String(), "denied") {
		t.Errorf("stderr: %s", w.stderr)
	}
	w.assertNotSignedIn()
}

func TestLoginRefusesAConsoleURLThatIsNotHTTPS(t *testing.T) {
	w := newWorld(t, acme)

	for _, bad := range []string{"http://solidtrace.example.com", "ftp://solidtrace.example.com",
		"https://solidtrace.example.com/app", "https://user:pw@solidtrace.example.com"} {
		if code := w.run("login", bad); code != 2 {
			t.Errorf("login %s: exit %d, want 2", bad, code)
		}
	}
	if len(w.opened) != 0 {
		t.Errorf("browser opened for a bad URL")
	}
	w.assertNotSignedIn()
}

func TestLoginRefusesMetadataThatNamesAnotherIssuer(t *testing.T) {
	w := newWorld(t, acme)
	// localhost reaches the fake at 127.0.0.1, but the metadata names
	// 127.0.0.1 as the issuer.
	other := strings.Replace(w.oauth.URL(), "127.0.0.1", "localhost", 1)

	if code := w.run("login", "--ingest-url", w.agent.URL(), other); code == 0 {
		t.Fatal("login passed with another issuer")
	}
	if !strings.Contains(w.stderr.String(), "another issuer") {
		t.Errorf("stderr: %s", w.stderr)
	}
	if len(w.opened) != 0 {
		t.Errorf("browser opened")
	}
	w.assertNotSignedIn()
}
