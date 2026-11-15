package wrapper_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// twoRunningRuns starts two Runs whose agents wait at a gate, and gives
// the Runs' ids, their Executions' ids and the gates.
func twoRunningRuns(t *testing.T, env *factorytest.Env, ana *factorytest.Person, after ...string) (
	runs, executions [2]string, opens [2]func()) {
	t.Helper()
	for i := range runs {
		wait, open := gate(t)
		env.Runner.Agent(append([]string{takeTask, initLine(t), wait}, after...)...)
		runs[i] = start(t, env, ana, "Fix the cart")
		executions[i] = waitFor(t, env, ana, runs[i], dispatcherapi.StateRunning).Executions[0].ID
		opens[i] = open
		// The wrapper serves the agent's socket after the clone.
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			if _, err := os.Stat(env.Runner.Socket(executions[i])); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return runs, executions, opens
}

func TestTheTaskCallNeedsTheWorkloadIdentityOfItsExecution(t *testing.T) {
	env, ana := newEnv(t)
	_, executions, _ := twoRunningRuns(t, env, ana)
	a, b := executions[0], executions[1]
	valid := env.Runner.Claims(factorytest.Workload(a))
	with := func(name string, value any) map[string]any {
		claims := map[string]any{}
		for k, v := range valid {
			claims[k] = v
		}
		if value == nil {
			delete(claims, name)
		} else {
			claims[name] = value
		}
		return claims
	}
	now := env.Clock.Now()
	refused := map[string]string{
		"no JWT":                     "",
		"not a JWT":                  "stk_person_ana",
		"a JWT of another Execution": env.Runner.Identity(factorytest.Workload(b)),
		"a forged JWT":               env.Runner.Forged(valid),
		"an expired JWT":             env.Runner.Sign(with("exp", now.Add(-time.Second).Unix())),
		"a JWT with no exp":          env.Runner.Sign(with("exp", nil)),
		"a JWT not valid yet":        env.Runner.Sign(with("nbf", now.Add(time.Hour).Unix())),
		"a JWT for another audience": env.Runner.Sign(with("aud", "nomad.example")),
		"a JWT with no subject":      env.Runner.Sign(with("sub", nil)),
		"a person's token":           ana.Token,
	}
	for name, token := range refused {
		for _, verb := range []string{"task", "report", "end"} {
			got := env.API(token, "POST", "/api/v1/executions/"+a+"/"+verb, map[string]string{})
			if got.Status != http.StatusUnauthorized {
				t.Errorf("%s call with %s: %d %s", verb, name, got.Status, got.Body)
			}
		}
	}
	// An Execution that does not exist, with a JWT that names it.
	ghost := env.API(env.Runner.Identity(factorytest.Workload("nosuchexecution")), "POST",
		"/api/v1/executions/nosuchexecution/task", map[string]string{})
	if ghost.Status != http.StatusUnauthorized {
		t.Errorf("task call of an Execution that does not exist: %d", ghost.Status)
	}

	got := env.API(env.Runner.Identity(factorytest.Workload(a)), "POST", "/api/v1/executions/"+a+"/task",
		map[string]string{})
	if got.Status != http.StatusOK {
		t.Fatalf("task call with the Execution's own JWT: %d %s", got.Status, got.Body)
	}
	var task dispatcherapi.Task
	got.JSON(t, &task)
	if task.ExecutionID != a || task.Prompt != "Fix the cart" || task.ForgeToken != env.Forge.Token {
		t.Errorf("task: %+v", task)
	}
}

func TestTheAgentCannotReportForAnotherRun(t *testing.T) {
	env, ana := newEnv(t)
	runs, executions, opens := twoRunningRuns(t, env, ana, reply, result, waitForEnd)

	// The agent of the first Run names the second Run and its Execution
	// in a report on its own socket.
	body := `{"outcome":"done","summary":"Done for the other Run","run_id":"` + runs[1] +
		`","execution_id":"` + executions[1] + `"}`
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://agent"+agentsocket.ReportPath,
		bytes.NewBufferString(body))
	resp, err := agentsocket.Client(env.Runner.Socket(executions[0])).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// The agent holds no credential for the Dispatcher: a call with none,
	// or with its own report's text, is refused.
	direct := env.API("", "POST", "/api/v1/executions/"+executions[1]+"/report",
		dispatcherapi.Report{Outcome: dispatcherapi.OutcomeDone, Summary: "Direct"})
	if direct.Status != http.StatusUnauthorized {
		t.Errorf("a report straight to the Dispatcher: %d", direct.Status)
	}
	opens[0]()
	opens[1]()

	first := waitFor(t, env, ana, runs[0], dispatcherapi.StateDone)
	second := waitFor(t, env, ana, runs[1], dispatcherapi.StateFailed)
	if first.Summary != "Done for the other Run" {
		t.Errorf("the report did not go to the Run of the socket: %q", first.Summary)
	}
	if second.Reason != dispatcherapi.EndNoReport || second.Summary != "" {
		t.Errorf("the other Run took the report: %s %q %q", second.State, second.Reason, second.Summary)
	}
}

func TestCallsOnAnEndedExecutionAreRefused(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	execution := run.Executions[0].ID
	identity := env.Runner.Identity(factorytest.Workload(execution))

	for verb, body := range map[string]any{
		"task":   map[string]string{},
		"report": dispatcherapi.Report{Outcome: dispatcherapi.OutcomeFailed, Summary: "Late"},
		"end":    dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndHarnessExited},
	} {
		got := env.API(identity, "POST", "/api/v1/executions/"+execution+"/"+verb, body)
		if got.Status != http.StatusConflict {
			t.Errorf("%s on an ended Execution: %d %s", verb, got.Status, got.Body)
		}
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateDone || run.Summary != "Fixed" {
		t.Errorf("the Run changed: %s %q", run.State, run.Summary)
	}
}
