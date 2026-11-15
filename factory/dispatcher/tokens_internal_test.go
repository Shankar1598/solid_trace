package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// These tests use the internal package to see the number of kept answers,
// and to give the status call a request context that is cancelled.

// checksOf is the number of token checks that SolidTrace got for token.
func (w *adminWorld) checksOf(token string) int {
	n := 0
	for _, c := range w.solidTrace.CallsTo("/oauth/introspect") {
		if c.Form.Get("token") == token {
			n++
		}
	}
	return n
}

func (w *adminWorld) mustCall(token string) {
	w.t.Helper()
	if rec := w.call(token); rec.Code != http.StatusOK {
		w.t.Fatalf("%s: %d %s, want 200", token, rec.Code, rec.Body)
	}
}

func TestTheDispatcherKeepsAtMostKeepLimitAnswersAndStillAcceptsATokenOverTheLimit(t *testing.T) {
	w := newAdminWorld(t)
	tokens := make([]string, keepLimit+1)
	for i := range tokens {
		tokens[i] = fmt.Sprintf("stk_person_%d", i)
		w.person(tokens[i], "admin")
	}
	for _, token := range tokens {
		w.mustCall(token)
	}
	if n := len(w.d.tokens.kept); n != keepLimit {
		t.Fatalf("%d kept answers, want %d", n, keepLimit)
	}

	// The token over the limit is accepted, but its answer is not kept.
	over := tokens[keepLimit]
	w.mustCall(over)
	if n := w.checksOf(over); n != 2 {
		t.Errorf("the token over the limit: %d token checks for 2 calls, want 2 (its answer is not kept)", n)
	}
	w.mustCall(tokens[0])
	if n := w.checksOf(tokens[0]); n != 1 {
		t.Errorf("a token in the limit: %d token checks for 2 calls, want 1 (its answer is kept)", n)
	}

	// After 60 s the kept answers are out of date: the next check drops
	// them and keeps its own answer.
	w.advance(keepFor)
	w.mustCall(over)
	w.mustCall(over)
	if n := w.checksOf(over); n != 3 {
		t.Errorf("the token over the limit, 60 s later: %d token checks, want 3 (its new answer is kept)", n)
	}
	if n := len(w.d.tokens.kept); n != 1 {
		t.Errorf("%d kept answers after 60 s, want 1", n)
	}
}

func TestAStatusCallWhoseClientWentAwayDoesNotStopTheCheckOfTheDispatchersOwnToken(t *testing.T) {
	w := newAdminWorld(t)
	status := func(ctx context.Context) dispatcherapi.Status {
		req := httptest.NewRequest(http.MethodGet, dispatcherapi.Path+"/status", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		w.d.Handler().ServeHTTP(rec, req)
		var s dispatcherapi.Status
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatalf("decode %s: %v", rec.Body, err)
		}
		return s
	}

	gone, cancel := context.WithCancel(context.Background())
	cancel()
	status(gone)
	w.advance(5 * time.Second)
	if s := status(context.Background()); !s.SolidTrace.TokenActive || s.SolidTrace.Error != "" {
		t.Errorf("the next status call: %+v, want token_active (the check of the first call is not cancelled)", s.SolidTrace)
	}
}
