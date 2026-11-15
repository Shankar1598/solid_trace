package main_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Child tokens at the agent ingest API: the Dispatcher's token makes a
// child token for one Execution, with sessions:write and sessions:read for
// the Run's Agent session ids (Console, ApiToken.issue_child!). EventStore
// gives the child only those Agent sessions, and refuses it once it or its
// parent no longer works.

// dispatcherScopes are the Scopes of the Dispatcher's token
// (Oauth::Scopes::DISPATCHER).
var dispatcherScopes = []string{"run_events:write", "issues:comment", "tokens:create"}

func TestAgentChildTokens(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	now := env.Clock.Now()
	parentExpiry := now.Add(30 * 24 * time.Hour)
	childExpiry := now.Add(3 * time.Hour)
	run := unitUpload{path: "session.jsonl", runID: "run-0193", project: "shop", body: lines("Fix issue 42")}

	dispatcher := func() int64 {
		_, id := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: dispatcherScopes,
			expiresAt: &parentExpiry})
		return id
	}
	child := func(parent int64) (string, int64) {
		return o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{runSession, laptopSession},
			expiresAt: &childExpiry, parent: parent})
	}

	t.Run("a child token uploads for its two Agent session ids, and is refused for a third", func(t *testing.T) {
		secret, _ := child(dispatcher())

		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 200)
		expectStatus(t, uploadUnit(t, env, secret, laptopSession, run), 200)
		expectStatus(t, uploadUnit(t, env, secret, otherSession, run), 403)
		answer, _ := offsetsOf(t, env, secret, otherSession)
		expectStatus(t, answer, 403)
	})

	t.Run("a revoked child token stops at once", func(t *testing.T) {
		secret, id := child(dispatcher())
		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 200)

		o.rows.exec("DELETE FROM api_tokens WHERE id = ?", id)

		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 401)
	})

	t.Run("revoking the parent stops each child at once", func(t *testing.T) {
		parent := dispatcher()
		first, _ := child(parent)
		second, _ := child(parent)
		expectStatus(t, uploadUnit(t, env, first, runSession, run), 200)

		// Console's delete of the parent also deletes its children (the
		// foreign key). Here only the parent's row goes, so EventStore's
		// own check of the parent is what refuses the children.
		deleteWithoutForeignKeys(t, env, parent)

		expectStatus(t, uploadUnit(t, env, first, runSession, run), 401)
		expectStatus(t, uploadUnit(t, env, second, laptopSession, run), 401)
	})

	t.Run("a child whose parent has expired is refused", func(t *testing.T) {
		parent := dispatcher()
		secret, _ := child(parent)
		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 200)

		o.rows.exec("UPDATE api_tokens SET expires_at = ? WHERE id = ?", railsTime(now), parent)

		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 401)
	})

	// The Dispatcher adds to a Run each Agent session id that the wrapper
	// finds, and the agent can write any id there.
	t.Run("a child token does not add to a person's own Agent session that its list names", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, o.secret, otherSession, unitUpload{path: "session.jsonl", body: lines("Mine")}), 200)
		secret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{otherSession},
			expiresAt: &childExpiry, parent: dispatcher()})

		answer := uploadUnit(t, env, secret, otherSession, unitUpload{path: "session.jsonl", offset: len(lines("Mine")),
			runID: "run-0193", project: "shop", body: lines("Not yours")})
		expectStatus(t, answer, 403)
		if msg, _ := answer.body["error"].(string); !strings.Contains(msg, "only to its Run's Agent sessions") {
			t.Errorf("Expected a clear body, got %s", answer.raw)
		}
	})

	t.Run("the Dispatcher's token's children stop when its Admin leaves the Organization", func(t *testing.T) {
		secret, _ := child(dispatcher())
		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 200)

		o.rows.exec("UPDATE organizations_users SET discarded_at = ? WHERE organization_id = ? AND user_id = ?",
			railsTime(now), o.organization, o.user)

		expectStatus(t, uploadUnit(t, env, secret, runSession, run), 401)
	})
}

// deleteWithoutForeignKeys deletes the API token id on a connection with
// no foreign keys, so the rows that point to it stay.
func deleteWithoutForeignKeys(t *testing.T, env *TestEnv, id int64) {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath+"?_foreign_keys=0")
	if err != nil {
		t.Fatalf("Open Console's database: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("DELETE FROM api_tokens WHERE id = ?", id); err != nil {
		t.Fatalf("Delete API token %d: %v", id, err)
	}
	var children int
	if err := db.QueryRow("SELECT COUNT(*) FROM api_tokens WHERE parent_id = ?", id).Scan(&children); err != nil || children == 0 {
		t.Fatalf("Expected the children to stay, got %d (%v)", children, err)
	}
}
