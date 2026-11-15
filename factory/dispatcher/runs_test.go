package dispatcher_test

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

var queuedLine = regexp.MustCompile(`(?m)^Run (\S+) is queued\.$`)

// newEnv is a factory test environment with the Projects checkout and
// search, each with Factory settings.
func newEnv(t *testing.T) *factorytest.Env {
	t.Helper()
	env := factorytest.New(t)
	env.Project("checkout")
	env.Project("search")
	return env
}

// start runs 'solidtrace start' and returns the Run id that it printed.
func start(t *testing.T, p *factorytest.Person, args ...string) (string, factorytest.Result) {
	t.Helper()
	result := p.CLI(append([]string{"start"}, args...)...)
	if result.Code != 0 {
		t.Fatalf("solidtrace start: exit %d\nstdout: %s\nstderr: %s", result.Code, result.Stdout, result.Stderr)
	}
	m := queuedLine.FindStringSubmatch(result.Stdout)
	if m == nil {
		t.Fatalf("no 'Run <id> is queued.' line in:\n%s", result.Stdout)
	}
	return m[1], result
}

func TestStartMakesAQueuedRunAndPrintsItsIDAndConsoleLink(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")

	id, result := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")

	link := env.SolidTrace.URL() + "/example/runs/" + id
	if !strings.Contains(result.Stdout, link) {
		t.Errorf("stdout has no Console link %s:\n%s", link, result.Stdout)
	}
	got := ana.CLI("runs", "get", id)
	if got.Code != 0 {
		t.Fatalf("runs get: exit %d: %s", got.Code, got.Stderr)
	}
	for _, want := range []string{"queued", "checkout", "Fix the flaky cart test", "ana@example.com"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("runs get has no %q:\n%s", want, got.Stdout)
		}
	}
}

func TestRunsListShowsEachRunsStateProjectAndPromptNewestFirst(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	first, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")
	env.Clock.Advance(time.Minute)
	second, _ := start(t, ana, "--project", "search", "Add a synonym list")

	list := ana.CLI("runs", "list")
	if list.Code != 0 {
		t.Fatalf("runs list: exit %d: %s", list.Code, list.Stderr)
	}
	lines := strings.Split(strings.TrimSpace(list.Stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and 2 Runs:\n%s", list.Stdout)
	}
	for i, want := range [][]string{{second, "queued", "search", "Add a synonym list"},
		{first, "queued", "checkout", "Fix the flaky cart test"}} {
		for _, w := range want {
			if !strings.Contains(lines[i+1], w) {
				t.Errorf("line %d has no %q: %s", i+1, w, lines[i+1])
			}
		}
	}
}

func TestTheConsoleLinkFollowsTheOrganizationsCurrentSlug(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")

	// An Admin changes the Organization's slug in Console.
	renamed := ana.Answer
	renamed.Organizations = []recorder.Organization{{ID: 7, Slug: "example-co", Name: "Example", Role: "member"}}
	env.SolidTrace.Token(ana.Token, renamed)
	// The Dispatcher keeps the answer of a token check for 60 seconds.
	env.Clock.Advance(time.Minute)

	link := env.SolidTrace.URL() + "/example-co/runs/" + id
	if got := ana.CLI("runs", "get", id); got.Code != 0 || !strings.Contains(got.Stdout, link) {
		t.Errorf("runs get after a slug change: exit %d, want link %s:\n%s%s", got.Code, link, got.Stdout, got.Stderr)
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 1 || runs.Runs[0].URL != link || runs.Runs[0].Organization != "example-co" {
		t.Errorf("runs list after a slug change: %+v, want link %s", runs.Runs, link)
	}
}

func TestStartTakesEachWordAfterDoubleDashAsThePrompt(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")

	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "--", "Fix", "the", "-race", "failure", "--project")

	var run dispatcherapi.Run
	ana.API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)
	if run.Prompt != "Fix the -race failure --project" || run.Project != "checkout" {
		t.Errorf("Run has project %q and prompt %q, want checkout and %q", run.Project, run.Prompt, "Fix the -race failure --project")
	}
}

func TestTheDispatcherKeepsItsRunsAfterARestart(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")

	env.Restart()

	got := ana.CLI("runs", "get", id)
	if got.Code != 0 || !strings.Contains(got.Stdout, "Fix the flaky cart test") {
		t.Fatalf("after a restart, runs get: exit %d\n%s%s", got.Code, got.Stdout, got.Stderr)
	}
	list := ana.CLI("runs", "list")
	if !strings.Contains(list.Stdout, id) {
		t.Errorf("after a restart, runs list has no %s:\n%s", id, list.Stdout)
	}
}

func TestAPersonOfAnotherOrganizationGetsNoRun(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	id, _ := start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix the flaky cart test")
	other := env.PersonIn("Bo", recorder.Organization{ID: 8, Slug: "other", Name: "Other"}, "admin")

	for _, path := range []string{"/api/v1/runs/" + id, "/api/v1/runs"} {
		if a := other.API("GET", path, nil); a.Status != http.StatusForbidden || strings.Contains(string(a.Body), id) {
			t.Errorf("another Organization's GET %s: %d %s, want 403 with no Run", path, a.Status, a.Body)
		}
	}
}

func TestTheAPIRefusesNoTokenAndATokenThatIsNotActive(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	body := dispatcherapi.StartRun{Project: "checkout", Prompt: "x"}

	for name, token := range map[string]string{"no token": "", "an unknown token": "stk_unknown"} {
		a := env.API(token, "POST", "/api/v1/runs", body)
		if a.Status != http.StatusUnauthorized || !strings.Contains(string(a.Body), "solidtrace login") {
			t.Errorf("%s: %d %s, want 401 that names solidtrace login", name, a.Status, a.Body)
		}
	}
	env.SolidTrace.Revoke(ana.Token)
	result := ana.CLI("start", "--dispatcher", env.URL(), "--project", "checkout", "x")
	if result.Code != 1 || !strings.Contains(result.Stderr, "solidtrace login") {
		t.Errorf("a revoked token: exit %d, stderr %q", result.Code, result.Stderr)
	}
}

func TestStartRefusesARunWithNoProjectOrPrompt(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	for _, body := range []dispatcherapi.StartRun{{Project: "checkout"}, {Prompt: "Fix it"}, {Project: " ", Prompt: "Fix it"}, {Project: "checkout", Prompt: "  \n\t"}} {
		if a := ana.API("POST", "/api/v1/runs", body); a.Status != http.StatusBadRequest {
			t.Errorf("%+v: %d %s, want 400", body, a.Status, a.Body)
		}
	}
	if r := ana.CLI("start", "--dispatcher", env.URL(), "--project", "checkout"); r.Code != 2 {
		t.Errorf("start with no prompt: exit %d, want 2", r.Code)
	}
}

func TestARunVerbWithNoDispatcherURLSaysHowToGiveIt(t *testing.T) {
	env := newEnv(t)
	r := env.Member("Ana").CLI("runs", "list")
	if r.Code != 1 || !strings.Contains(r.Stderr, "--dispatcher") {
		t.Errorf("exit %d, stderr %q", r.Code, r.Stderr)
	}
}

func TestTheRecorderKeepsEachTokenCheckThatTheDispatcherSends(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	start(t, ana, "--dispatcher", env.URL(), "--project", "checkout", "Fix it")

	var calls []recorder.Call
	for _, c := range env.SolidTrace.CallsTo("/oauth/introspect") {
		if c.Form.Get("token") == ana.Token {
			calls = append(calls, c)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("%d token checks, want 1: %+v", len(calls), env.SolidTrace.Calls())
	}
	if calls[0].Authorization != "Bearer "+factorytest.DispatcherToken || calls[0].Form.Get("token") != ana.Token {
		t.Errorf("the check is %+v, want the Dispatcher's token as caller and Ana's token as the checked token", calls[0])
	}
}

func TestTheAPIRefusesATokenThatActsInNoOrganizationOrInMoreThanOne(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	orgs := map[string][]recorder.Organization{
		"no Organization": nil,
		"two Organizations": {factorytest.Organization,
			{ID: 8, Slug: "other", Name: "Other", Role: "member"}},
	}
	for name, organizations := range orgs {
		answer := ana.Answer
		answer.Organizations = organizations
		env.SolidTrace.Token(ana.Token, answer)
		for _, method := range []string{"POST", "GET"} {
			var body any
			if method == "POST" {
				body = dispatcherapi.StartRun{Project: "checkout", Prompt: "Fix it"}
			}
			a := ana.API(method, "/api/v1/runs", body)
			if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "exactly one Organization") {
				t.Errorf("%s, %s: %d %s, want 403 that names exactly one Organization", name, method, a.Status, a.Body)
			}
		}
	}
}

func TestStartRefusesABodyThatIsNotAStartRunObject(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	big := `{"project":"checkout","prompt":"` + strings.Repeat("x", 1<<20) + `"}`
	for name, body := range map[string]string{
		"an unknown field":        `{"project":"checkout","prompt":"Fix it","extra":1}`,
		"a misspelled field":      `{"projct":"checkout","prompt":"Fix it"}`,
		"a body that is not JSON": `project=checkout&prompt=Fix+it`,
		"a body over 1 MiB":       big,
	} {
		if a := ana.API("POST", "/api/v1/runs", []byte(body)); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 0 {
		t.Errorf("%d Runs are queued after bad bodies, want 0", len(runs.Runs))
	}
}

func TestTheRunsListGivesTheNewest100Runs(t *testing.T) {
	env := newEnv(t)
	ana := env.Member("Ana")
	var newest dispatcherapi.Run
	for i := range 101 {
		env.Clock.Advance(time.Second)
		ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "checkout", Prompt: "Run " + strconv.Itoa(i)}).JSON(t, &newest)
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 100 {
		t.Fatalf("the list has %d Runs, want 100", len(runs.Runs))
	}
	if runs.Runs[0].ID != newest.ID || runs.Runs[99].Prompt != "Run 1" {
		t.Errorf("the list goes from %q to %q, want from the newest Run %q to \"Run 1\"", runs.Runs[0].Prompt, runs.Runs[99].Prompt, newest.Prompt)
	}
}
