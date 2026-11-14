package main_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/storage"
)

// The agent ingest API's first call: the captured repositories of the
// token's Organization. EventStore checks the token itself, from Console's
// tables, as Console's own token check does.

const capturedRepositoriesPath = "/api/agent/v1/captured-repositories"

// consoleRows writes rows into Console's test database as Console's models
// would, for the tables the token check reads.
type consoleRows struct {
	t   *testing.T
	db  *sql.DB
	now time.Time
	seq int
}

func newConsoleRows(t *testing.T, env *TestEnv) *consoleRows {
	t.Helper()
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Open Console's database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &consoleRows{t: t, db: db, now: env.Clock.Now()}
}

// railsTime is a time as Rails writes a datetime to SQLite.
func railsTime(at time.Time) string {
	return at.UTC().Format("2006-01-02 15:04:05.000000")
}

func (r *consoleRows) insert(query string, args ...any) int64 {
	r.t.Helper()
	result, err := r.db.Exec(query, args...)
	if err != nil {
		r.t.Fatalf("%s: %v", query, err)
	}
	id, _ := result.LastInsertId()
	return id
}

func (r *consoleRows) exec(query string, args ...any) {
	r.t.Helper()
	if _, err := r.db.Exec(query, args...); err != nil {
		r.t.Fatalf("%s: %v", query, err)
	}
}

func (r *consoleRows) next() int {
	r.seq++
	return r.seq
}

func (r *consoleRows) organization(name string) int64 {
	n := r.next()
	return r.insert("INSERT INTO organizations (name, slug, created_at, updated_at) VALUES (?, ?, ?, ?)",
		name, fmt.Sprintf("org-%d", n), railsTime(r.now), railsTime(r.now))
}

func (r *consoleRows) user() int64 {
	n := r.next()
	return r.insert("INSERT INTO users (email, name, created_at, updated_at) VALUES (?, ?, ?, ?)",
		fmt.Sprintf("agent-user-%d@example.com", n), fmt.Sprintf("User %d", n), railsTime(r.now), railsTime(r.now))
}

// member makes user a member of organization and returns the membership.
func (r *consoleRows) member(organization, user int64) int64 {
	return r.insert("INSERT INTO organizations_users (organization_id, user_id, role, created_at, updated_at) VALUES (?, ?, 0, ?, ?)",
		organization, user, railsTime(r.now), railsTime(r.now))
}

// agentProject turns Agent sessions on for organization, with entries as
// its captured repositories.
func (r *consoleRows) agentProject(organization int64, entries ...string) int64 {
	n := r.next()
	list, _ := json.Marshal(entries)
	return r.insert(`INSERT INTO projects (name, slug, organization_id, agent, captured_repositories, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?, ?)`, "Agent sessions", fmt.Sprintf("agent-sessions-%d", n), organization, string(list),
		railsTime(r.now), railsTime(r.now))
}

func (r *consoleRows) project(organization int64) int64 {
	n := r.next()
	return r.insert("INSERT INTO projects (name, slug, organization_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		"App", fmt.Sprintf("app-%d", n), organization, railsTime(r.now), railsTime(r.now))
}

func digestOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// apiTokenRow is an API token as ApiToken.issue! saves it.
type apiTokenRow struct {
	user         int64
	organization int64 // 0: none
	scopes       []string
	expiresAt    *time.Time
	sessionIDs   []string // nil: no list
}

// apiToken saves token and returns its secret and id.
func (r *consoleRows) apiToken(token apiTokenRow) (string, int64) {
	r.t.Helper()
	secret := fmt.Sprintf("st_agent_ingest_test_%d", r.next())
	scopes, _ := json.Marshal(token.scopes)
	if token.scopes == nil {
		scopes = []byte("[]")
	}
	var organization, expires, sessionIDs any
	if token.organization != 0 {
		organization = token.organization
	}
	if token.expiresAt != nil {
		expires = railsTime(*token.expiresAt)
	}
	if token.sessionIDs != nil {
		ids, _ := json.Marshal(token.sessionIDs)
		sessionIDs = string(ids)
	}
	id := r.insert(`INSERT INTO api_tokens (name, token_digest, user_id, scopes, organization_id, expires_at, agent_session_ids, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "capture", digestOf(secret), token.user, string(scopes), organization, expires,
		sessionIDs, railsTime(r.now), railsTime(r.now))
	return secret, id
}

// accessTokenRow is an OAuth access token under a Grant, as a sign-in of
// `solidtrace login` makes it.
type accessTokenRow struct {
	user         int64
	organization int64
	scopes       []string
	resource     string
	expiresAt    time.Time
}

// accessToken saves token under a new Grant and returns its secret, the
// token's id and the Grant's id.
func (r *consoleRows) accessToken(token accessTokenRow) (string, int64, int64) {
	r.t.Helper()
	n := r.next()
	client := r.insert(`INSERT INTO oauth_clients (kind, client_id, name, redirect_uris, grant_types, metadata, created_at, updated_at)
		VALUES ('first_party', ?, 'solidtrace', '[]', '[]', '{}', ?, ?)`, fmt.Sprintf("solidtrace-cli-%d", n),
		railsTime(r.now), railsTime(r.now))
	scopes, _ := json.Marshal(token.scopes)
	resources, _ := json.Marshal([]string{token.resource})
	grant := r.insert(`INSERT INTO oauth_grants (user_id, oauth_client_id, organization_id, scopes, resources, approved_via, approved_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'browser', ?, ?, ?)`, token.user, client, token.organization, string(scopes), string(resources),
		railsTime(r.now), railsTime(r.now), railsTime(r.now))
	secret := fmt.Sprintf("sto_agent_ingest_test_%d", n)
	id := r.insert(`INSERT INTO oauth_tokens (kind, token_digest, oauth_grant_id, resource, expires_at, created_at, updated_at)
		VALUES ('access', ?, ?, ?, ?, ?, ?)`, digestOf(secret), grant, token.resource, railsTime(token.expiresAt),
		railsTime(r.now), railsTime(r.now))
	return secret, id, grant
}

// agentAnswer is one answer of the agent ingest API.
type agentAnswer struct {
	status  int
	version string
	body    map[string]any
	raw     string
}

func callAgentAPI(t *testing.T, app *fiber.App, path string, headers map[string]string) agentAnswer {
	t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := app.Test(req, 2000)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	answer := agentAnswer{status: resp.StatusCode, version: resp.Header.Get("SolidTrace-Version"), raw: string(raw)}
	if err := json.Unmarshal(raw, &answer.body); err != nil {
		t.Fatalf("GET %s answered %d with a body that is not JSON: %q", path, resp.StatusCode, raw)
	}
	return answer
}

func capturedRepositories(t *testing.T, env *TestEnv, secret string) agentAnswer {
	t.Helper()
	headers := map[string]string{}
	if secret != "" {
		headers["Authorization"] = "Bearer " + secret
	}
	return callAgentAPI(t, env.App, capturedRepositoriesPath, headers)
}

func repositoriesOf(t *testing.T, answer agentAnswer) []string {
	t.Helper()
	if answer.status != 200 {
		t.Fatalf("Expected 200, got %d: %s", answer.status, answer.raw)
	}
	list, ok := answer.body["captured_repositories"].([]any)
	if !ok {
		t.Fatalf("No list of captured repositories in %s", answer.raw)
	}
	entries := make([]string, 0, len(list))
	for _, entry := range list {
		entries = append(entries, entry.(string))
	}
	return entries
}

func expectStatus(t *testing.T, answer agentAnswer, status int) {
	t.Helper()
	if answer.status != status {
		t.Fatalf("Expected %d, got %d: %s", status, answer.status, answer.raw)
	}
}

func TestAgentIngestCapturedRepositories(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	rows := newConsoleRows(t, env)
	now := env.Clock.Now()
	later := now.Add(24 * time.Hour)
	factoryResource := "https://console.example.com/factory"

	acme := rows.organization("Acme")
	rows.agentProject(acme, "bitbucket.org/acme/app", "bitbucket.org/acme-tools/*")
	rows.project(acme)
	other := rows.organization("Other")
	rows.agentProject(other, "github.com/other/secret")
	off := rows.organization("Off")
	rows.project(off)

	engineer := rows.user()
	rows.member(acme, engineer)
	rows.member(off, engineer)

	t.Run("an API token with sessions:write gets its Organization's list", func(t *testing.T) {
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}, expiresAt: &later})
		answer := capturedRepositories(t, env, secret)
		got := repositoriesOf(t, answer)
		want := []string{"bitbucket.org/acme/app", "bitbucket.org/acme-tools/*"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("Expected %v, got %v", want, got)
		}
		organization, _ := answer.body["organization"].(map[string]any)
		if organization["name"] != "Acme" {
			t.Fatalf("Expected the answer to name the Organization Acme: %s", answer.raw)
		}
		if answer.version != "v0.19.0" {
			t.Fatalf("Expected the server's version in SolidTrace-Version, got %q", answer.version)
		}
	})

	t.Run("an API token with no expiry and a list of Agent sessions works", func(t *testing.T) {
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write", "sessions:read"},
			sessionIDs: []string{"run-1"}})
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); len(got) != 2 {
			t.Fatalf("Expected Acme's two entries, got %v", got)
		}
	})

	t.Run("an OAuth access token of a sign-in with sessions:write gets its Organization's list", func(t *testing.T) {
		secret, _, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write", "factory"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); len(got) != 2 {
			t.Fatalf("Expected Acme's two entries, got %v", got)
		}
	})

	t.Run("a token reaches only its own Organization's list", func(t *testing.T) {
		// The engineer is in Off too, which has no Agent Project; Other's
		// list must never show.
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: off, scopes: []string{"sessions:write"}})
		answer := capturedRepositories(t, env, secret)
		if got := repositoriesOf(t, answer); len(got) != 0 {
			t.Fatalf("Expected an empty list with Agent sessions off, got %v", got)
		}
		if strings.Contains(answer.raw, "secret") || strings.Contains(answer.raw, "acme") {
			t.Fatalf("The answer shows another Organization's list: %s", answer.raw)
		}
	})

	t.Run("a token for an Organization that the user is not in gets 401", func(t *testing.T) {
		// Console refuses this at save; a row that has it anyway must not
		// reach Other's Agent Project.
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: other, scopes: []string{"sessions:write"}})
		expectStatus(t, capturedRepositories(t, env, secret), 401)
		oauth, _, _ := rows.accessToken(accessTokenRow{user: engineer, organization: other,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		expectStatus(t, capturedRepositories(t, env, oauth), 401)
	})

	t.Run("a token with no Organization reaches no Organization's list", func(t *testing.T) {
		// Console gives each token with Scopes one Organization; a row
		// without one is refused, not read as every Organization.
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, scopes: []string{"sessions:write"}})
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("an access token with no resource gets 401", func(t *testing.T) {
		secret, id, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		rows.exec("UPDATE oauth_tokens SET resource = NULL WHERE id = ?", id)
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("no token gets 401", func(t *testing.T) {
		answer := capturedRepositories(t, env, "")
		expectStatus(t, answer, 401)
		if answer.version != "v0.19.0" {
			t.Fatalf("Expected the server's version on a 401 too, got %q", answer.version)
		}
		expectStatus(t, callAgentAPI(t, env.App, capturedRepositoriesPath, map[string]string{"Authorization": "Basic abc"}), 401)
	})

	t.Run("a token in the query string is not read", func(t *testing.T) {
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}})
		expectStatus(t, callAgentAPI(t, env.App, capturedRepositoriesPath+"?token="+secret+"&access_token="+secret, nil), 401)
	})

	t.Run("an unknown token gets 401", func(t *testing.T) {
		expectStatus(t, capturedRepositories(t, env, "st_not_a_token"), 401)
		expectStatus(t, capturedRepositories(t, env, "sto_not_a_token"), 401)
		expectStatus(t, capturedRepositories(t, env, "testkey123"), 401)
	})

	t.Run("a token's digest finds it, not its secret", func(t *testing.T) {
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}})
		expectStatus(t, capturedRepositories(t, env, digestOf(secret)), 401)
	})

	t.Run("an expired API token gets 401, up to and at its expiry", func(t *testing.T) {
		past := now.Add(-time.Second)
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}, expiresAt: &past})
		expectStatus(t, capturedRepositories(t, env, secret), 401)

		at := now
		secret, _ = rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}, expiresAt: &at})
		expectStatus(t, capturedRepositories(t, env, secret), 401)

		soon := now.Add(time.Second)
		secret, _ = rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}, expiresAt: &soon})
		repositoriesOf(t, capturedRepositories(t, env, secret))
		env.Clock.Advance(time.Second)
		defer env.Clock.Set(now)
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("an expired OAuth access token gets 401", func(t *testing.T) {
		secret, _, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now})
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("a revoked API token gets 401", func(t *testing.T) {
		secret, id := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}})
		repositoriesOf(t, capturedRepositories(t, env, secret))
		rows.exec("DELETE FROM api_tokens WHERE id = ?", id)
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("a revoked OAuth access token gets 401", func(t *testing.T) {
		secret, id, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		repositoriesOf(t, capturedRepositories(t, env, secret))
		rows.exec("UPDATE oauth_tokens SET revoked_at = ? WHERE id = ?", railsTime(now), id)
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("an access token whose Grant is revoked gets 401", func(t *testing.T) {
		secret, _, grant := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		repositoriesOf(t, capturedRepositories(t, env, secret))
		rows.exec("UPDATE oauth_grants SET revoked_at = ? WHERE id = ?", railsTime(now), grant)
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("a refresh token is not an access token", func(t *testing.T) {
		secret, id, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		rows.exec("UPDATE oauth_tokens SET kind = 'refresh' WHERE id = ?", id)
		expectStatus(t, capturedRepositories(t, env, secret), 401)
	})

	t.Run("a token whose user left the Organization gets 401", func(t *testing.T) {
		leaver := rows.user()
		membership := rows.member(acme, leaver)
		rows.member(off, leaver)
		apiSecret, _ := rows.apiToken(apiTokenRow{user: leaver, organization: acme, scopes: []string{"sessions:write"}})
		oauthSecret, _, _ := rows.accessToken(accessTokenRow{user: leaver, organization: acme,
			scopes: []string{"sessions:write"}, resource: factoryResource, expiresAt: now.Add(time.Hour)})
		repositoriesOf(t, capturedRepositories(t, env, apiSecret))
		repositoriesOf(t, capturedRepositories(t, env, oauthSecret))

		// Leaving discards the membership; the user stays in Off.
		rows.exec("UPDATE organizations_users SET discarded_at = ? WHERE id = ?", railsTime(now), membership)
		expectStatus(t, capturedRepositories(t, env, apiSecret), 401)
		expectStatus(t, capturedRepositories(t, env, oauthSecret), 401)
	})

	t.Run("a token without sessions:write gets 403 that names the scope", func(t *testing.T) {
		for name, scopes := range map[string][]string{
			"no scope":       nil,
			"mcp:read":       {"mcp:read"},
			"sessions:read":  {"sessions:read"},
			"factory":        {"factory"},
			"a similar name": {"sessions:writer", "sessions"},
		} {
			organization := acme
			var ids []string
			if scopes == nil {
				organization = 0
			}
			if len(scopes) == 1 && scopes[0] == "sessions:read" {
				ids = []string{"run-1"}
			}
			secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: organization, scopes: scopes, sessionIDs: ids})
			answer := capturedRepositories(t, env, secret)
			if answer.status != 403 || !strings.Contains(answer.raw, "sessions:write") {
				t.Fatalf("%s: expected 403 that names sessions:write, got %d: %s", name, answer.status, answer.raw)
			}
			if strings.Contains(answer.raw, "bitbucket.org") {
				t.Fatalf("%s: the 403 shows the list: %s", name, answer.raw)
			}
		}
	})

	t.Run("a trailing slash on an access token's resource is the same resource", func(t *testing.T) {
		// Oauth::Issuer.normalize_resource removes trailing slashes.
		secret, _, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write", "mcp"}, resource: factoryResource + "/", expiresAt: now.Add(time.Hour)})
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); len(got) != 2 {
			t.Fatalf("Expected Acme's two entries, got %v", got)
		}
		secret, _, _ = rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"sessions:write", "mcp"}, resource: "https://console.example.com/mcp/", expiresAt: now.Add(time.Hour)})
		expectStatus(t, capturedRepositories(t, env, secret), 403)
	})

	t.Run("an access token whose resource has a query or a fragment gets 401", func(t *testing.T) {
		// Console keeps the query and the fragment when it compares
		// resources, so such a resource is not the factory's.
		for _, resource := range []string{factoryResource + "?x=1", factoryResource + "#f"} {
			secret, _, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
				scopes: []string{"sessions:write"}, resource: resource, expiresAt: now.Add(time.Hour)})
			answer := capturedRepositories(t, env, secret)
			if answer.status != 401 {
				t.Errorf("%s: expected 401, got %d: %s", resource, answer.status, answer.raw)
			}
		}
	})

	t.Run("an access token for another resource has no sessions:write", func(t *testing.T) {
		// The Grant has sessions:write, but this token is for the MCP
		// server: a token gets only its Grant's Scopes for its own
		// resource.
		secret, _, _ := rows.accessToken(accessTokenRow{user: engineer, organization: acme,
			scopes: []string{"mcp", "sessions:write"}, resource: "https://console.example.com/mcp", expiresAt: now.Add(time.Hour)})
		answer := capturedRepositories(t, env, secret)
		if answer.status != 403 || !strings.Contains(answer.raw, "sessions:write") {
			t.Fatalf("Expected 403 that names sessions:write, got %d: %s", answer.status, answer.raw)
		}
	})

	t.Run("an Organization that requires Two-factor refuses a user without it", func(t *testing.T) {
		strict := rows.organization("Strict")
		rows.agentProject(strict, "bitbucket.org/strict/app")
		rows.exec("UPDATE organizations SET require_two_factor = 1 WHERE id = ?", strict)
		person := rows.user()
		rows.member(strict, person)
		secret, _ := rows.apiToken(apiTokenRow{user: person, organization: strict, scopes: []string{"sessions:write"}})
		answer := capturedRepositories(t, env, secret)
		if answer.status != 403 || !strings.Contains(answer.raw, "Two-factor") {
			t.Fatalf("Expected 403 that names Two-factor, got %d: %s", answer.status, answer.raw)
		}

		rows.exec("UPDATE users SET authenticator_confirmed_at = ? WHERE id = ?", railsTime(now), person)
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); len(got) != 1 {
			t.Fatalf("Expected Strict's entry once the user has Two-factor, got %v", got)
		}

		// A passkey is Two-factor too, with no authenticator.
		holder := rows.user()
		rows.member(strict, holder)
		rows.insert(`INSERT INTO passkeys (user_id, credential_id, public_key, name, host, created_at, updated_at)
			VALUES (?, ?, 'key', 'Laptop', 'console.example.com', ?, ?)`, holder, fmt.Sprintf("credential-%d", holder),
			railsTime(now), railsTime(now))
		secret, _ = rows.apiToken(apiTokenRow{user: holder, organization: strict, scopes: []string{"sessions:write"}})
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); len(got) != 1 {
			t.Fatalf("Expected Strict's entry for a user with a passkey, got %v", got)
		}
	})

	t.Run("an Organization with Agent sessions turned off gives an empty list", func(t *testing.T) {
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: off, scopes: []string{"sessions:write"}})
		answer := capturedRepositories(t, env, secret)
		if got := repositoriesOf(t, answer); got == nil || len(got) != 0 {
			t.Fatalf("Expected an empty list, got %v", got)
		}
	})

	t.Run("only the Agent Project's list is given, not another Project's", func(t *testing.T) {
		// The ordinary Project comes first, with a value in its column.
		mixed := rows.organization("Mixed")
		ordinary := rows.project(mixed)
		rows.exec(`UPDATE projects SET captured_repositories = '["github.com/mixed/ordinary"]' WHERE id = ?`, ordinary)
		rows.agentProject(mixed, "github.com/mixed/agent")
		rows.member(mixed, engineer)
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: mixed, scopes: []string{"sessions:write"}})
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); strings.Join(got, ",") != "github.com/mixed/agent" {
			t.Fatalf("Expected only the Agent Project's entry, got %v", got)
		}
	})

	t.Run("an Agent Project with an empty list gives an empty list", func(t *testing.T) {
		empty := rows.organization("Empty")
		rows.agentProject(empty)
		rows.member(empty, engineer)
		secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: empty, scopes: []string{"sessions:write"}})
		if got := repositoriesOf(t, capturedRepositories(t, env, secret)); len(got) != 0 {
			t.Fatalf("Expected an empty list, got %v", got)
		}
	})
}

func TestAgentIngestClientVersions(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	rows := newConsoleRows(t, env)
	acme := rows.organization("Acme")
	rows.agentProject(acme, "bitbucket.org/acme/app")
	engineer := rows.user()
	rows.member(acme, engineer)
	secret, _ := rows.apiToken(apiTokenRow{user: engineer, organization: acme, scopes: []string{"sessions:write"}})

	call := func(app *fiber.App, userAgent string) agentAnswer {
		headers := map[string]string{"Authorization": "Bearer " + secret}
		if userAgent != "" {
			headers["User-Agent"] = userAgent
		}
		return callAgentAPI(t, app, capturedRepositoriesPath, headers)
	}

	// The server is v0.19.0: it accepts the last 3 minor versions before
	// its own (0.16, 0.17, 0.18), its own, and newer ones.
	for _, accepted := range []string{
		"solidtrace/0.16.0", "solidtrace/v0.16.0", "solidtrace/0.16.9 (linux; arm64)", "solidtrace/0.18.4",
		"solidtrace/0.19.0", "solidtrace/0.19.0-rc1", "solidtrace/0.20.0", "solidtrace/1.0.0", "solidtrace/dev",
		"curl/8.5.0", "",
	} {
		answer := call(env.App, accepted)
		if answer.status != 200 {
			t.Errorf("%q: expected 200, got %d: %s", accepted, answer.status, answer.raw)
		}
	}

	for _, old := range []string{"solidtrace/0.15.9", "solidtrace/v0.12.0", "solidtrace/0.1.0 (darwin; arm64)"} {
		answer := call(env.App, old)
		if answer.status != 426 {
			t.Errorf("%q: expected 426, got %d: %s", old, answer.status, answer.raw)
			continue
		}
		if answer.body["minimum_version"] != "0.16.0" || !strings.Contains(fmt.Sprint(answer.body["error"]), "0.16.0") {
			t.Errorf("%q: expected one error that names the minimum version 0.16.0, got %s", old, answer.raw)
		}
		if answer.version != "v0.19.0" {
			t.Errorf("%q: expected the server's version on the 426, got %q", old, answer.version)
		}
	}

	// A too-old client hears that first, also with no token: signing in
	// again does not help it.
	old := callAgentAPI(t, env.App, capturedRepositoriesPath, map[string]string{"User-Agent": "solidtrace/0.15.0"})
	expectStatus(t, old, 426)

	// A new major version on the same API path is not a breaking change:
	// until the server has 3 minors of its own major, it also accepts the
	// earlier major, whose last minor it does not know. After that, the
	// earlier major is too old.
	for _, tc := range []struct {
		server, client string
		status         int
		minimum        string
	}{
		{"v1.0.0", "solidtrace/0.19.0", 200, ""},
		{"v1.0.0", "solidtrace/0.0.1", 200, ""},
		{"v1.2.0", "solidtrace/0.99.0", 200, ""},
		{"v1.2.0", "solidtrace/1.0.0", 200, ""},
		{"v1.3.0", "solidtrace/1.0.0", 200, ""},
		{"v1.3.0", "solidtrace/0.99.0", 426, "1.0.0"},
		{"v2.1.0", "solidtrace/0.99.0", 426, "1.0.0"},
	} {
		answer := call(newAgentApp(t, env, tc.server), tc.client)
		if answer.status != tc.status {
			t.Errorf("%s against %s: expected %d, got %d: %s", tc.client, tc.server, tc.status, answer.status, answer.raw)
			continue
		}
		if tc.minimum != "" && answer.body["minimum_version"] != tc.minimum {
			t.Errorf("%s against %s: expected the minimum version %s, got %s", tc.client, tc.server, tc.minimum, answer.raw)
		}
	}

	// A development server, which has no release version, accepts each.
	dev := newAgentApp(t, env, "dev")
	answer := call(dev, "solidtrace/0.1.0")
	expectStatus(t, answer, 200)
	if answer.version != "dev" {
		t.Errorf("Expected the version dev in the header, got %q", answer.version)
	}
}

// newAgentApp is an ingest app with only the agent ingest API, as a server
// of version would serve it, on env's Console tables.
func newAgentApp(t *testing.T, env *TestEnv, version string) *fiber.App {
	t.Helper()
	tokens, err := auth.NewTokens(env.SQLitePath)
	if err != nil {
		t.Fatalf("Open Console's tokens: %v", err)
	}
	t.Cleanup(func() { tokens.Close() })
	settings, err := storage.NewConsoleSettings(env.SQLitePath)
	if err != nil {
		t.Fatalf("Open Console's settings: %v", err)
	}
	t.Cleanup(func() { settings.Close() })
	app := fiber.New()
	handler.RegisterAgentRoutes(app, handler.NewAgentHandler(handler.AgentConfig{
		Tokens: tokens, Settings: settings, Files: env.PebbleWriter, NotifyFiles: env.AgentFileProcessor.Notify,
		Clock: env.Clock, Version: version,
	}))
	return app
}
