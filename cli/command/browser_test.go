package command_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
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

	// A good --ingest-url, so that only the Console URL can stop the login.
	// http:// on a loopback host (the fake at 127.0.0.1, and localhost in
	// TestLoginRefusesMetadataThatNamesAnotherIssuer) passes this check.
	for _, bad := range []string{"http://solidtrace.example.com", "http://10.100.0.3:3000", "ftp://solidtrace.example.com",
		"https://solidtrace.example.com/app", "https://user:pw@solidtrace.example.com"} {
		before := len(w.stderr.String())
		if code := w.run("login", "--ingest-url", w.agent.URL(), bad); code != 2 {
			t.Errorf("login %s: exit %d, want 2", bad, code)
		}
		if strings.HasPrefix(bad, "http://") && !strings.Contains(w.stderr.String()[before:], "not https://") {
			t.Errorf("login %s: stderr: %s", bad, w.stderr.String()[before:])
		}
	}
	if len(w.opened) != 0 {
		t.Errorf("browser opened for a bad URL")
	}
	w.assertNotSignedIn()
}

func TestLoginRefusesMetadataThatSendsTheTokensElsewhere(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.TokenEndpoint = "http://198.51.100.92/oauth/token"

	if code := w.login(); code == 0 {
		t.Fatal("login passed with a token endpoint at another address")
	}
	if !strings.Contains(w.stderr.String(), "another address") {
		t.Errorf("stderr: %s", w.stderr)
	}
	if len(w.opened) != 0 {
		t.Errorf("browser opened")
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

func TestLoginWorksWhenTheConsoleURLHasCapitalLetters(t *testing.T) {
	w := newWorld(t, acme)
	// Console keeps the Console URL setting as the operator typed it, and
	// names it so as the issuer.
	issuer := strings.Replace(w.oauth.URL(), "127.0.0.1", "LocalHost", 1)
	w.oauth.Issuer = issuer

	if code := w.run("login", "--ingest-url", w.agent.URL(), issuer); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if got := w.oauth.Authorizations[0].Query.Get("resource"); got != issuer+"/factory" {
		t.Errorf("resource = %q, want the issuer's %q", got, issuer+"/factory")
	}
	if _, err := w.signedInToken(); err != nil {
		t.Errorf("no sign-in: %v", err)
	}
}

func TestLoginRefusesMetadataWithNoPKCES256OrNoTokenEndpoint(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"plain only":        func(m map[string]any) { m["code_challenge_methods_supported"] = []string{"plain"} },
		"no methods":        func(m map[string]any) { m["code_challenge_methods_supported"] = []string{} },
		"methods not given": func(m map[string]any) { delete(m, "code_challenge_methods_supported") },
		"no token endpoint": func(m map[string]any) { delete(m, "token_endpoint") },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, acme)
			w.oauth.EditMetadata = edit

			if code := w.login(); code == 0 {
				t.Fatal("login passed")
			}
			if !strings.Contains(w.stderr.String(), "does not give the sign-in that the CLI needs") {
				t.Errorf("stderr: %s", w.stderr)
			}
			if len(w.opened) != 0 || len(w.oauth.Authorizations) != 0 || len(w.oauth.DeviceRequests) != 0 {
				t.Errorf("the sign-in started")
			}
			w.assertNotSignedIn()
		})
	}
}

func TestLoginAcceptsOnlyTheFirstRedirectWithTheRightState(t *testing.T) {
	w := newWorld(t, acme)
	noFollow := &http.Client{Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var firstStatus, replayStatus int
	var replayErr error
	w.browser = func(approval string) error {
		w.opened = append(w.opened, approval)
		resp, err := noFollow.Get(approval)
		if err != nil {
			return err
		}
		resp.Body.Close()
		callback, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			return err
		}
		resp, err = noFollow.Get(callback.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		firstStatus = resp.StatusCode
		// Another program on the machine replays the redirect, with the
		// right state and its own code.
		q := callback.Query()
		q.Set("code", "replayed")
		callback.RawQuery = q.Encode()
		resp, replayErr = noFollow.Get(callback.String())
		if replayErr == nil {
			resp.Body.Close()
			replayStatus = resp.StatusCode
		}
		return nil
	}

	if code := w.login(); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if firstStatus != http.StatusOK {
		t.Errorf("first redirect got %d, want 200", firstStatus)
	}
	if replayErr != nil || replayStatus != http.StatusBadRequest {
		t.Errorf("replayed redirect got %d (%v), want 400", replayStatus, replayErr)
	}
	for _, form := range w.oauth.TokenRequests {
		if form.Get("code") == "replayed" {
			t.Errorf("the CLI exchanged the replayed code")
		}
	}
}
