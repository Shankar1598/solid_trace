package dispatcher

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/recorder"
)

// The API has no Admin action yet (ticket 28 adds the first ones), so
// these tests put the Admin check in front of a small action of their own.
// The internal package is necessary for that; the check is the same one
// that each Admin route uses.

type adminWorld struct {
	t          *testing.T
	solidTrace *recorder.Recorder
	handler    http.Handler
	mu         sync.Mutex
	now        time.Time
}

const adminCallerToken = "stk_dispatcher"

func newAdminWorld(t *testing.T) *adminWorld {
	t.Helper()
	w := &adminWorld{t: t, solidTrace: recorder.New(adminCallerToken), now: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	t.Cleanup(w.solidTrace.Close)
	w.solidTrace.Token(adminCallerToken, recorder.Answer{Sub: "1", Scope: "run_events:write"})
	d, err := Open(Config{
		SolidTraceURL:   w.solidTrace.URL(),
		SolidTraceToken: adminCallerToken,
		Database:        filepath.Join(t.TempDir(), "dispatcher.sqlite3"),
	}, Options{Now: w.clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	w.handler = d.person(d.admin(func(rw http.ResponseWriter, _ *http.Request, p person) {
		writeJSON(rw, http.StatusOK, map[string]string{"done_by": p.name})
	}))
	return w
}

func (w *adminWorld) clock() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

func (w *adminWorld) advance(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = w.now.Add(d)
}

// person makes an active factory token with role in Organization 7.
func (w *adminWorld) person(token, role string) {
	w.solidTrace.Token(token, recorder.Answer{
		Sub: "100", Username: token + "@example.com", Scope: "factory",
		Organizations: []recorder.Organization{{ID: 7, Slug: "example", Name: "Example", Role: role}},
	})
}

func (w *adminWorld) call(token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin-action", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec
}

func TestAMemberGets403OnAnAdminActionAndAnAdminCanDoIt(t *testing.T) {
	w := newAdminWorld(t)
	w.person("stk_admin", "admin")
	w.person("stk_member", "member")

	if rec := w.call("stk_admin"); rec.Code != http.StatusOK {
		t.Errorf("an Admin: %d %s, want 200", rec.Code, rec.Body)
	}
	rec := w.call("stk_member")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "only an Admin") {
		t.Errorf("a member: %d %s, want 403 that says only an Admin", rec.Code, rec.Body)
	}
}

func TestOnlyTheAdminRoleCanDoAnAdminAction(t *testing.T) {
	w := newAdminWorld(t)
	for i, role := range []string{"", "Admin", "owner", "admin ", " admin", "member"} {
		token := fmt.Sprintf("stk_role_%d", i)
		w.person(token, role)
		if rec := w.call(token); rec.Code != http.StatusForbidden {
			t.Errorf("role %q: %d %s, want 403", role, rec.Code, rec.Body)
		}
	}
}

func TestAnAdminWhoBecomesAMemberLosesTheAdminActionsWithin60Seconds(t *testing.T) {
	w := newAdminWorld(t)
	w.person("stk_ana", "admin")
	if rec := w.call("stk_ana"); rec.Code != http.StatusOK {
		t.Fatalf("an Admin: %d %s", rec.Code, rec.Body)
	}
	w.person("stk_ana", "member")

	w.advance(60 * time.Second)
	if rec := w.call("stk_ana"); rec.Code != http.StatusForbidden {
		t.Errorf("60 s after the change to member: %d %s, want 403", rec.Code, rec.Body)
	}
}

func TestAMemberWhoBecomesAnAdminGetsTheAdminActionsWithin60Seconds(t *testing.T) {
	w := newAdminWorld(t)
	w.person("stk_ana", "member")
	if rec := w.call("stk_ana"); rec.Code != http.StatusForbidden {
		t.Fatalf("a member: %d %s", rec.Code, rec.Body)
	}
	w.person("stk_ana", "admin")
	w.advance(60 * time.Second)
	if rec := w.call("stk_ana"); rec.Code != http.StatusOK {
		t.Errorf("60 s after the change to Admin: %d %s, want 200", rec.Code, rec.Body)
	}
}

func TestAnAdminOfAnotherOrganizationIsNotAnAdminHere(t *testing.T) {
	w := newAdminWorld(t)
	w.solidTrace.Token("stk_two", recorder.Answer{
		Sub: "101", Scope: "factory",
		Organizations: []recorder.Organization{
			{ID: 7, Slug: "example", Role: "member"},
			{ID: 8, Slug: "other", Role: "admin"},
		},
	})
	if rec := w.call("stk_two"); rec.Code != http.StatusForbidden {
		t.Errorf("a token of two Organizations: %d %s, want 403", rec.Code, rec.Body)
	}
}
