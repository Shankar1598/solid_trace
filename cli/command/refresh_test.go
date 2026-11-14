package command_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/internal/fake"
	"github.com/solidtrace/cli/signin"
)

func (w *world) signedInToken() (string, error) {
	return w.env().Source().Token(context.Background())
}

// api is the agent ingest API client that a later command (capture) uses.
func (w *world) api() *agentapi.Client {
	env := w.env()
	return &agentapi.Client{IngestURL: w.agent.URL(), HTTP: env.HTTP, Tokens: env.Source(), Version: w.version,
		Warnings: w.stderr}
}

func (w *world) stored() map[string]any {
	w.t.Helper()
	data, err := os.ReadFile(w.credentialsFile())
	if err != nil {
		w.t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(data, &c); err != nil {
		w.t.Fatal(err)
	}
	return c
}

func (w *world) signIn() {
	w.t.Helper()
	if code := w.login(); code != 0 {
		w.t.Fatalf("login: exit %d\nstderr: %s", code, w.stderr)
	}
}

func TestAnExpiredAccessTokenIsRefreshedWithTheRotatingRefreshToken(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	first := w.stored()

	w.clock.Add(61 * time.Minute)
	if _, err := w.api().CapturedRepositories(context.Background()); err != nil {
		t.Fatalf("call after the expiry: %v", err)
	}
	if w.oauth.RefreshRequests != 1 {
		t.Fatalf("refreshes: %d, want 1", w.oauth.RefreshRequests)
	}
	if w.oauth.SentRefreshToken[0] != first["refresh_token"] {
		t.Errorf("refresh sent another refresh token than the stored one")
	}
	second := w.stored()
	if second["refresh_token"] == first["refresh_token"] || second["access_token"] == first["access_token"] {
		t.Errorf("the new pair is not stored")
	}
	if strings.Contains(mustRead(t, w.credentialsFile()), first["refresh_token"].(string)) {
		t.Errorf("the used refresh token is still in the file")
	}

	// The next expiry uses the new refresh token: the used one is never
	// sent again, so Console never sees a reuse.
	w.clock.Add(61 * time.Minute)
	if _, err := w.api().CapturedRepositories(context.Background()); err != nil {
		t.Fatalf("second call after the expiry: %v", err)
	}
	if w.oauth.RefreshRequests != 2 || w.oauth.SentRefreshToken[1] != second["refresh_token"] {
		t.Errorf("second refresh sent %v", w.oauth.SentRefreshToken)
	}
	if w.oauth.GrantRevoked() {
		t.Errorf("the Grant was revoked: a refresh token was used twice")
	}

	// A live token is not refreshed.
	if _, err := w.api().CapturedRepositories(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.oauth.RefreshRequests != 2 {
		t.Errorf("a live token was refreshed")
	}
}

func TestAnAccessTokenIsRefreshedInTheLastMinuteBeforeItsExpiry(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	expires, err := time.Parse(time.RFC3339Nano, w.stored()["access_token_expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}

	// 61 seconds before the expiry, the token is live.
	w.clock.Add(expires.Sub(w.clock.Now()) - 61*time.Second)
	if _, err := w.signedInToken(); err != nil {
		t.Fatal(err)
	}
	if w.oauth.RefreshRequests != 0 {
		t.Fatalf("refreshed %d times 61 seconds before the expiry, want 0", w.oauth.RefreshRequests)
	}

	// 59 seconds before it, the token could expire on the way to the
	// server, so the CLI refreshes it first.
	w.clock.Add(2 * time.Second)
	if _, err := w.signedInToken(); err != nil {
		t.Fatal(err)
	}
	if w.oauth.RefreshRequests != 1 {
		t.Errorf("refreshed %d times 59 seconds before the expiry, want 1", w.oauth.RefreshRequests)
	}
}

func TestARefusedRefreshTokenTellsThePersonToSignInAgain(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.oauth.RevokeGrants()
	w.clock.Add(61 * time.Minute)

	_, err := w.api().CapturedRepositories(context.Background())
	if !errors.Is(err, signin.ErrSignInAgain) || !strings.Contains(err.Error(), "solidtrace login") {
		t.Fatalf("err = %v, want ErrSignInAgain", err)
	}
	stored := w.stored()
	if stored["refresh_token"] != nil || stored["access_token"] != nil {
		t.Errorf("the refused tokens are still stored: %v", stored)
	}
	if stored["instance_url"] != w.oauth.URL() {
		t.Errorf("the instance URL is gone: %v", stored)
	}

	// Later calls do not send the refused token again.
	_, err = w.signedInToken()
	if !errors.Is(err, signin.ErrSignInAgain) {
		t.Errorf("err = %v", err)
	}
	if w.oauth.RefreshRequests != 1 {
		t.Errorf("refreshes: %d, want 1", w.oauth.RefreshRequests)
	}

	// 'solidtrace login' with no URL signs in again at the same instance.
	w.oauth.Lock()
	w.oauth.Organizations["acme"].Repositories = []string{"bitbucket.org/acme/shop"}
	w.oauth.Unlock()
	if code := w.run("login"); code != 0 {
		t.Fatalf("login again: exit %d\nstderr: %s", code, w.stderr)
	}
	if _, err := w.signedInToken(); err != nil {
		t.Errorf("no sign-in after login again: %v", err)
	}
}

func TestARefreshWithNoAnswerKeepsTheSignIn(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	before := w.stored()
	w.oauth.RefreshDown = true
	w.clock.Add(61 * time.Minute)

	_, err := w.signedInToken()
	if err == nil || errors.Is(err, signin.ErrSignInAgain) {
		t.Fatalf("err = %v, want an error that is not ErrSignInAgain", err)
	}
	if w.stored()["refresh_token"] != before["refresh_token"] {
		t.Errorf("the refresh token changed after a failed refresh")
	}

	w.oauth.RefreshDown = false
	if _, err := w.signedInToken(); err != nil {
		t.Errorf("no refresh when the server is back: %v", err)
	}
}

func TestARefreshWhoseAnswerIsLostIsSentAgainAtOnceAndKeepsTheGrant(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	before := w.stored()
	// Console rotates the pair, but its answer does not come back.
	w.oauth.DropRefreshAnswers = 1
	w.clock.Add(61 * time.Minute)

	token, err := w.signedInToken()
	if err != nil {
		t.Fatalf("refresh with a lost answer: %v", err)
	}
	if w.oauth.RefreshRequests != 2 || w.oauth.SentRefreshToken[1] != before["refresh_token"] {
		t.Fatalf("refreshes %d, sent %v: want the same refresh token again at once", w.oauth.RefreshRequests,
			w.oauth.SentRefreshToken)
	}
	if w.oauth.GrantRevoked() {
		t.Fatal("the Grant was revoked")
	}
	after := w.stored()
	if after["access_token"] != token || after["refresh_token"] == before["refresh_token"] {
		t.Errorf("the pair of the repeat is not stored")
	}

	// The stored pair is the live chain: a later capture, after the 60
	// seconds of the repeat, refreshes with no reuse.
	w.clock.Add(61 * time.Minute)
	if _, err := w.api().CapturedRepositories(context.Background()); err != nil {
		t.Fatalf("call after the next expiry: %v", err)
	}
	if w.oauth.GrantRevoked() {
		t.Error("the Grant was revoked at the next refresh")
	}
}

func TestTwoProcessesThatRefreshAtTheSameTimeUseTheRefreshTokenOnce(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.clock.Add(61 * time.Minute)

	const processes = 8
	tokens := make([]string, processes)
	errs := make([]error, processes)
	var wg sync.WaitGroup
	for i := range processes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each has its own Source and its own open of the lock file,
			// as two capture processes have.
			tokens[i], errs[i] = w.env().Source().Token(context.Background())
		}()
	}
	wg.Wait()

	for i := range processes {
		if errs[i] != nil {
			t.Fatalf("process %d: %v", i, errs[i])
		}
		if tokens[i] != tokens[0] {
			t.Errorf("process %d got another access token", i)
		}
	}
	if w.oauth.RefreshRequests != 1 {
		t.Errorf("refreshes: %d, want 1", w.oauth.RefreshRequests)
	}
	if w.oauth.GrantRevoked() {
		t.Errorf("the Grant was revoked")
	}
}

func TestA401RefreshesTheTokenOnceAndTriesAgain(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	// The server ends the token before its stored expiry.
	w.oauth.ExpireAccessTokens()

	list, err := w.api().CapturedRepositories(context.Background())
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if list.Organization.Slug != "acme" {
		t.Errorf("list: %+v", list)
	}
	if w.oauth.RefreshRequests != 1 {
		t.Errorf("refreshes: %d, want 1", w.oauth.RefreshRequests)
	}
}

func TestA401AfterTheRefreshTellsThePersonToSignInAgain(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	// The agent ingest API refuses each token, also the new one.
	w.agent.Set(func(a *fake.AgentAPI) { a.RefuseTokens = true })

	_, err := w.api().CapturedRepositories(context.Background())
	if err == nil || !strings.Contains(err.Error(), "solidtrace login") {
		t.Fatalf("err = %v", err)
	}
	if w.oauth.RefreshRequests != 1 {
		t.Errorf("refreshes: %d, want 1 (no loop)", w.oauth.RefreshRequests)
	}
}

func TestACredentialsFileThatOtherUsersCanReadIsRefused(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	if err := os.Chmod(w.credentialsFile(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := w.signedInToken()
	if err == nil || !strings.Contains(err.Error(), "other users") {
		t.Fatalf("err = %v", err)
	}

	// A new sign-in replaces the file with one that only the user reads.
	if code := w.login(); code != 0 {
		t.Fatalf("login: exit %d\nstderr: %s", code, w.stderr)
	}
	info, err := os.Stat(w.credentialsFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %o after login", info.Mode().Perm())
	}
}

func TestNotSignedIn(t *testing.T) {
	w := newWorld(t, acme)

	_, err := w.signedInToken()
	if err == nil || !strings.Contains(err.Error(), "solidtrace login") {
		t.Fatalf("err = %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
