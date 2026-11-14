package dispatcher_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/solidtrace/factory/internal/factorytest"
)

var queuedLine = regexp.MustCompile(`(?m)^Run (\S+) is queued\.$`)

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
	env := factorytest.New(t)
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
