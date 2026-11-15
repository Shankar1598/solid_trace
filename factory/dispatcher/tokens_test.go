package dispatcher_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// eachPersonCall sends each Run verb that a person has with token, and
// returns the answers by verb.
func eachPersonCall(env *factorytest.Env, token, runID string) map[string]factorytest.Answer {
	return map[string]factorytest.Answer{
		"start": env.API(token, "POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "checkout", Prompt: "Fix it"}),
		"list":  env.API(token, "GET", "/api/v1/runs", nil),
		"get":   env.API(token, "GET", "/api/v1/runs/"+runID, nil),
	}
}

// checksOf is the number of token checks that SolidTrace got for token.
func checksOf(env *factorytest.Env, token string) int {
	n := 0
	for _, c := range env.SolidTrace.CallsTo("/oauth/introspect") {
		if c.Form.Get("token") == token {
			n++
		}
	}
	return n
}

func TestTheDispatcherChecksTheOrganizationOfItsOwnTokenOnce(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	for range 3 {
		if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
			t.Fatalf("list: %d %s", a.Status, a.Body)
		}
		env.Clock.Advance(time.Minute)
	}
	if n := checksOf(env, factorytest.DispatcherToken); n != 1 {
		t.Errorf("%d checks of the Dispatcher's own token, want 1", n)
	}
}

func TestATokenWithOnlyTheFactoryScopeCanStartGetAndListRuns(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	ana.Answer.Scope = "factory"
	env.SolidTrace.Token(ana.Token, ana.Answer)

	var run dispatcherapi.Run
	started := ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "checkout", Prompt: "Fix it"})
	if started.Status != http.StatusCreated {
		t.Fatalf("start: %d %s, want 201", started.Status, started.Body)
	}
	started.JSON(t, &run)
	if a := ana.API("GET", "/api/v1/runs/"+run.ID, nil); a.Status != http.StatusOK {
		t.Errorf("get: %d %s, want 200", a.Status, a.Body)
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 1 || runs.Runs[0].ID != run.ID {
		t.Errorf("list: %+v, want the one Run %s", runs.Runs, run.ID)
	}
}

func TestTheAPIRefusesATokenWithNoScopeOrWithoutTheFactoryScope(t *testing.T) {
	cases := map[string]struct{ scope, want string }{
		// SolidTrace gives the scope mcp for an API token made with no
		// scope (ApiToken#effective_scopes).
		"no scope":                 {"mcp", "acts only over the MCP server"},
		"an empty scope":           {"", "acts only over the MCP server"},
		"only spaces":              {"   ", "acts only over the MCP server"},
		"mcp and other scopes":     {"mcp sessions:write", "no factory scope"},
		"other scopes":             {"sessions:write mcp:read", "no factory scope"},
		"a scope that starts so":   {"factory:admin", "no factory scope"},
		"a scope that ends so":     {"notfactory", "no factory scope"},
		"the scope in other cases": {"Factory", "no factory scope"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := factorytest.New(t)
			ana := env.Member("Ana")
			ana.Answer.Scope = c.scope
			env.SolidTrace.Token(ana.Token, ana.Answer)
			for verb, a := range eachPersonCall(env, ana.Token, "aaaaaaaaaaaaaaaa") {
				var problem dispatcherapi.Problem
				a.JSON(t, &problem)
				if a.Status != http.StatusForbidden || !strings.Contains(problem.Error, c.want) ||
					!strings.Contains(problem.Error, "solidtrace login") {
					t.Errorf("%s: %d %s, want 403 that says %q and names solidtrace login", verb, a.Status, a.Body, c.want)
				}
				if got, want := a.Header.Get("WWW-Authenticate"), `Bearer error="insufficient_scope", scope="factory"`; got != want {
					t.Errorf("%s: WWW-Authenticate %q, want %q", verb, got, want)
				}
			}
		})
	}
}

func TestTheCLIPrintsWhyATokenWithoutTheFactoryScopeIsRefused(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	ana.Answer.Scope = "sessions:write"
	env.SolidTrace.Token(ana.Token, ana.Answer)

	r := ana.CLI("start", "--dispatcher", env.URL(), "--project", "checkout", "Fix it")
	if r.Code != 1 || !strings.Contains(r.Stderr, "403") || !strings.Contains(r.Stderr, "no factory scope") {
		t.Errorf("exit %d, stderr %q, want 1 and the 403 reason", r.Code, r.Stderr)
	}
}

func TestATokenThatIsNotActiveIsRefusedWith401(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	env.SolidTrace.Revoke(ana.Token)

	for verb, a := range eachPersonCall(env, ana.Token, "aaaaaaaaaaaaaaaa") {
		if a.Status != http.StatusUnauthorized || !strings.Contains(string(a.Body), "not active") {
			t.Errorf("%s: %d %s, want 401 that says not active", verb, a.Status, a.Body)
		}
		if got, want := a.Header.Get("WWW-Authenticate"), `Bearer error="invalid_token"`; got != want {
			t.Errorf("%s: WWW-Authenticate %q, want %q", verb, got, want)
		}
	}
}

func TestARevokedTokenIsRefusedWithin60Seconds(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
		t.Fatalf("list: %d %s", a.Status, a.Body)
	}
	env.SolidTrace.Revoke(ana.Token)

	env.Clock.Advance(59 * time.Second)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
		t.Errorf("59 s after the check: %d %s, want the kept answer (200)", a.Status, a.Body)
	}
	env.Clock.Advance(time.Second)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusUnauthorized {
		t.Errorf("60 s after the check: %d %s, want 401", a.Status, a.Body)
	}
	if n := checksOf(env, ana.Token); n != 2 {
		t.Errorf("%d token checks, want 2 (one at the start, one after 60 s)", n)
	}
}

func TestAKeptAnswerIsCheckedAgainEach60Seconds(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	for range 3 {
		for range 3 {
			if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
				t.Fatalf("list: %d %s", a.Status, a.Body)
			}
			env.Clock.Advance(10 * time.Second)
		}
		env.Clock.Advance(30 * time.Second)
	}
	if n := checksOf(env, ana.Token); n != 3 {
		t.Errorf("%d token checks in 3 minutes, want 3", n)
	}
}

func TestARefusedTokenIsCheckedAgainOnTheNextCall(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	scope := ana.Answer.Scope
	ana.Answer.Scope = "sessions:write"
	env.SolidTrace.Token(ana.Token, ana.Answer)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusForbidden {
		t.Fatalf("without factory: %d %s, want 403", a.Status, a.Body)
	}

	ana.Answer.Scope = scope
	env.SolidTrace.Token(ana.Token, ana.Answer)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
		t.Errorf("with factory at once after: %d %s, want 200", a.Status, a.Body)
	}
}

func TestATokenIsRefusedAtItsExpiryEvenWhenItsAnswerIsKept(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	ana.Answer.Exp = env.Clock.Now().Add(20 * time.Second).Unix()
	env.SolidTrace.Token(ana.Token, ana.Answer)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
		t.Fatalf("before the expiry: %d %s", a.Status, a.Body)
	}

	env.Clock.Advance(20 * time.Second)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusUnauthorized || !strings.Contains(string(a.Body), "expired") {
		t.Errorf("at the expiry: %d %s, want 401 that says expired", a.Status, a.Body)
	}
}

func TestATokenThatSolidTraceGivesAsExpiredIsRefused(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	ana.Answer.Exp = env.Clock.Now().Add(-time.Second).Unix()
	env.SolidTrace.Token(ana.Token, ana.Answer)
	if a := ana.API("GET", "/api/v1/runs", nil); a.Status != http.StatusUnauthorized || !strings.Contains(string(a.Body), "expired") {
		t.Errorf("%d %s, want 401 that says expired", a.Status, a.Body)
	}
}

func TestAKeptAnswerIsOnlyForItsOwnToken(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	var run dispatcherapi.Run
	ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "checkout", Prompt: "Fix it"}).JSON(t, &run)
	bo := env.PersonIn("Bo", recorder.Organization{ID: 8, Slug: "other", Name: "Other"}, "admin")

	// The Dispatcher serves only the Organization of its own token, so
	// it refuses each call of Bo; it must never act as Ana for him.
	for verb, a := range eachPersonCall(env, bo.Token, run.ID) {
		if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "serves the Organization example") {
			t.Errorf("another Organization's %s: %d %s, want 403 that names the Organization example", verb, a.Status, a.Body)
		}
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 1 || runs.Runs[0].ID != run.ID {
		t.Errorf("Ana lists %+v, want only her Run %s", runs.Runs, run.ID)
	}
}

func TestTheDispatcherSaysWhenSolidTraceRefusesItsOwnToken(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")

	status := env.API("", "GET", "/api/v1/status", nil)
	var ok dispatcherapi.Status
	status.JSON(t, &ok)
	if status.Status != http.StatusOK || !ok.SolidTrace.TokenActive || ok.SolidTrace.Error != "" {
		t.Fatalf("status with an active token: %d %s, want 200 and token_active", status.Status, status.Body)
	}

	// The Admin who made the Dispatcher's token leaves the Organization.
	env.SolidTrace.Revoke(factorytest.DispatcherToken)
	env.Clock.Advance(10 * time.Second) // the status keeps its answer for 10 s

	status = env.API("", "GET", "/api/v1/status", nil)
	var down dispatcherapi.Status
	status.JSON(t, &down)
	if status.Status != http.StatusServiceUnavailable || down.SolidTrace.TokenActive ||
		!strings.Contains(down.SolidTrace.Error, "not active") || !strings.Contains(down.SolidTrace.Error, "Admin") {
		t.Errorf("status with an inactive token: %d %s, want 503 that says the token is not active and names the Admin",
			status.Status, status.Body)
	}
	a := ana.API("GET", "/api/v1/runs", nil)
	if a.Status != http.StatusServiceUnavailable || !strings.Contains(string(a.Body), "Dispatcher's SolidTrace token is not active") {
		t.Errorf("a person's call: %d %s, want 503 that says the Dispatcher's token is not active", a.Status, a.Body)
	}
}

func TestTheStatusChecksTheDispatchersOwnTokenAtMostOnceIn10Seconds(t *testing.T) {
	env := factorytest.New(t)
	for range 50 {
		if a := env.API("", "GET", "/api/v1/status", nil); a.Status != http.StatusOK {
			t.Fatalf("status: %d %s", a.Status, a.Body)
		}
	}
	if n := len(env.SolidTrace.CallsTo("/oauth/introspect")); n != 1 {
		t.Errorf("%d token checks for 50 status calls, want 1", n)
	}

	env.SolidTrace.Revoke(factorytest.DispatcherToken)
	env.Clock.Advance(9 * time.Second)
	if a := env.API("", "GET", "/api/v1/status", nil); a.Status != http.StatusOK {
		t.Errorf("9 s after the check: %d %s, want the kept answer (200)", a.Status, a.Body)
	}
	env.Clock.Advance(time.Second)
	if a := env.API("", "GET", "/api/v1/status", nil); a.Status != http.StatusServiceUnavailable {
		t.Errorf("10 s after the check: %d %s, want 503", a.Status, a.Body)
	}
	if n := len(env.SolidTrace.CallsTo("/oauth/introspect")); n != 2 {
		t.Errorf("%d token checks, want 2 (one at the start, one after 10 s)", n)
	}
}

func TestTheStatusSaysWhenSolidTraceCannotBeReached(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTrace.Close()

	status := env.API("", "GET", "/api/v1/status", nil)
	var down dispatcherapi.Status
	status.JSON(t, &down)
	if status.Status != http.StatusServiceUnavailable || down.SolidTrace.TokenActive ||
		!strings.Contains(down.SolidTrace.Error, "could not reach SolidTrace") {
		t.Errorf("%d %s, want 503 that says SolidTrace could not be reached", status.Status, status.Body)
	}
}

func TestTheStatusGivesTheDispatchersVersion(t *testing.T) {
	env := factorytest.New(t)
	env.Version = "v0.19.2"
	env.Restart()
	var status dispatcherapi.Status
	env.API("", "GET", "/api/v1/status", nil).JSON(t, &status)
	if status.Version != "v0.19.2" {
		t.Errorf("version %q, want v0.19.2", status.Version)
	}
}

func TestTheAPIAcceptsACLIOfTheLast3MinorVersionsAndRefusesAnOlderOne(t *testing.T) {
	env := factorytest.New(t)
	env.Version = "v0.19.2"
	env.Restart()
	ana := env.Member("Ana")

	for _, version := range []string{"0.16.0", "v0.16.0", "0.17.3", "0.19.2", "0.20.0", "1.0.0", "dev", "0.16.0-rc.1"} {
		ana.Version = version
		if r := ana.CLI("runs", "list", "--dispatcher", env.URL()); r.Code != 0 {
			t.Errorf("solidtrace %s: exit %d, stderr %q, want it accepted", version, r.Code, r.Stderr)
		}
	}
	checks := len(env.SolidTrace.CallsTo("/oauth/introspect"))
	for _, version := range []string{"0.15.9", "0.9.0", "v0.0.1"} {
		ana.Version = version
		r := ana.CLI("runs", "list")
		if r.Code != 1 || !strings.Contains(r.Stderr, "0.16.0 or later") || strings.Count(r.Stderr, "\n") != 1 {
			t.Errorf("solidtrace %s: exit %d, stderr %q, want one error line that names 0.16.0", version, r.Code, r.Stderr)
		}
	}
	if n := len(env.SolidTrace.CallsTo("/oauth/introspect")) - checks; n != 0 {
		t.Errorf("%d token checks for a CLI that is too old, want 0", n)
	}
	a := env.API(ana.Token, "GET", "/api/v1/runs", nil)
	if a.Status != http.StatusOK {
		t.Errorf("a client that is not the solidtrace CLI: %d %s, want 200", a.Status, a.Body)
	}
}

func TestTheRefusalOfAnOldCLIGivesTheMinimumVersionInItsBody(t *testing.T) {
	env := factorytest.New(t)
	env.Version = "v1.1.0"
	env.Restart()
	ana := env.Member("Ana")
	ana.Version = "0.30.0"
	if r := ana.CLI("runs", "list", "--dispatcher", env.URL()); r.Code != 0 {
		t.Errorf("solidtrace 0.30.0 at a Dispatcher v1.1.0: exit %d, stderr %q, want it accepted", r.Code, r.Stderr)
	}

	env.Version = "v0.5.0"
	env.Restart()
	a := env.APIAs("solidtrace/0.1.4", ana.Token, "GET", "/api/v1/runs", nil)
	var body struct {
		Error          string `json:"error"`
		MinimumVersion string `json:"minimum_version"`
	}
	if err := json.Unmarshal(a.Body, &body); err != nil {
		t.Fatal(err)
	}
	if a.Status != http.StatusUpgradeRequired || body.MinimumVersion != "0.2.0" || !strings.Contains(body.Error, "0.2.0") {
		t.Errorf("%d %s, want 426 with minimum_version 0.2.0", a.Status, a.Body)
	}
}

func TestTheMinimumCLIAtTheEdgeOfTheMinorVersions(t *testing.T) {
	env := factorytest.New(t)
	ana := env.Member("Ana")
	cases := []struct {
		dispatcher, refused, accepted string
	}{
		{"v1.3.0", "0.9.0", "1.0.0"},
		{"v1.3.7", "0.99.0", "1.0.0"},
		{"v1.2.0", "", "0.0.1"},
		{"v1.4.0", "1.0.9", "1.1.0"},
		{"v0.3.0", "", "0.0.0"},
		{"v0.4.0", "0.0.9", "0.1.0"},
	}
	for _, c := range cases {
		env.Version = c.dispatcher
		env.Restart()
		if a := env.APIAs("solidtrace/"+c.accepted, ana.Token, "GET", "/api/v1/runs", nil); a.Status != http.StatusOK {
			t.Errorf("solidtrace %s at a Dispatcher %s: %d %s, want 200", c.accepted, c.dispatcher, a.Status, a.Body)
		}
		if c.refused == "" {
			continue
		}
		a := env.APIAs("solidtrace/"+c.refused, ana.Token, "GET", "/api/v1/runs", nil)
		var body struct {
			MinimumVersion string `json:"minimum_version"`
		}
		_ = json.Unmarshal(a.Body, &body)
		if a.Status != http.StatusUpgradeRequired || body.MinimumVersion != c.accepted {
			t.Errorf("solidtrace %s at a Dispatcher %s: %d %s, want 426 with minimum_version %s",
				c.refused, c.dispatcher, a.Status, a.Body, c.accepted)
		}
	}
}
