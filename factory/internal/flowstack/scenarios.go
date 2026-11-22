package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/fakeforge"
	"github.com/solidtrace/factory/internal/fakeharness"
	"github.com/solidtrace/factory/internal/factorytest"
)

// scenario is one Run of the demo: its prompt, the script of its scripted
// Harness, and the state that the Run reaches.
type scenario struct {
	// Name says in a few words what the Run shows, for the person.
	Name string
	// Prompt is the Run's prompt and title.
	Prompt string
	// Script is the lines of the Harness' stream.
	Script []string
	// Want is the state that the Run reaches by itself. A Run that the
	// demo stops reaches StateStopped, and a Run that works until a
	// person stops it stays in StateRunning.
	Want string
	// Stop says that the demo stops the Run when it works.
	Stop bool
}

// Lines of the scripts of the scripted Harness (the same steps as the
// Seam 3 tests of the wrapper).
const (
	takeTask   = `{"fake":"await_message"}`
	waitForEnd = `{"fake":"await_message"}`
	endResult  = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"Done.","stop_reason":"end_turn","session_id":"","uuid":"","total_cost_usd":0}`
	// untilStopped works until the interrupt that a stop sends.
	untilStopped = `{"fake":"busy","ms":1000,"eof_code":0}`
)

// say is an assistant message with text.
func say(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "session_id": "", "uuid": "", "parent_tool_use_id": nil,
		"message": map[string]any{
			"id": "msg_01Demo", "type": "message", "role": "assistant", "model": "claude-haiku-5-5", "stop_reason": "end_turn",
			"content": []any{map[string]any{"type": "text", "text": text}},
		},
	})
	return string(b)
}

// program is a step that runs a program in the working directory, as the
// agent's Bash tool does.
func program(args ...string) string {
	b, _ := json.Marshal(map[string]any{"fake": "run", "args": args})
	return string(b)
}

func shellStep(command string) string { return program("sh", "-c", command) }

// initLine is the recorded init event of the Harness.
func initLine() (string, error) {
	stream, err := fakeharness.Stream("turn-with-tool-call")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(stream), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil && ev["type"] == "system" && ev["subtype"] == "init" {
			return line, nil
		}
	}
	return "", errors.New("the recorded stream has no init event")
}

// demoRepository is the files of the repository that the demo's Project
// has on the fake Forge.
var demoRepository = map[string]string{
	"README.md":   "# Shop\n\nThe demo's repository on the fake Forge.\n",
	"cart.txt":    "total: 40\ndiscount: 5\n",
	"checkout.md": "# Checkout\n\nThe labels of the checkout page.\n",
}

// commitStep is a step in which the agent commits a change on the Run's
// branch.
func commitStep(message string) string {
	return shellStep(`echo "fixed: ` + message + `" > fix.txt && git add fix.txt && git commit -q -m "` + message + `"`)
}

// scenarios is the Runs of the demo, in the order that the demo starts
// them.
func scenarios() ([]scenario, error) {
	init, err := initLine()
	if err != nil {
		return nil, err
	}
	failed, err := fakeharness.Stream("harness-exits-mid-turn")
	if err != nil {
		return nil, err
	}
	var failedScript []string
	for _, line := range strings.Split(strings.TrimSpace(string(failed)), "\n") {
		failedScript = append(failedScript, line)
	}
	end := []string{endResult, waitForEnd}
	script := func(lines ...string) []string {
		return append(append([]string{takeTask, init}, lines...), end...)
	}
	return []scenario{
		{
			Name:   "done, with a PR, two checks that passed and one Evidence file",
			Prompt: "Demo: fix the cart total (done, with a PR and Evidence)",
			Want:   dispatcherapi.StateDone,
			Script: script(
				say("I read the cart code. The discount was taken off two times."),
				commitStep("Take the discount off one time"),
				program("solidtrace", "check", "echo '12 tests, 0 failures'"),
				program("solidtrace", "check", "echo 'lint: no problems'"),
				shellStep(`printf 'cart total before: 30\ncart total after: 35\n' > "$SOLIDTRACE_EVIDENCE_DIR/cart-total.txt"`),
				program("solidtrace", "pr", "--title", "Take the discount off one time",
					"--description", "The cart took the discount off two times. It does it one time now."),
				program("solidtrace", "report", "--outcome", "done", "--summary",
					"The cart total is right now.\nThe discount was taken off two times; now it is one time."),
				say("The cart total is right now."),
			),
		},
		{
			Name:   "done, with no Evidence (no check passed)",
			Prompt: "Demo: rename the checkout labels (done, no Evidence)",
			Want:   dispatcherapi.StateDone,
			Script: script(
				say("I changed the labels. The check that I ran does not pass."),
				commitStep("Rename the checkout labels"),
				program("solidtrace", "check", "echo 'label check: 2 labels are missing'; exit 2"),
				program("solidtrace", "pr", "--title", "Rename the checkout labels",
					"--description", "New labels for the checkout page."),
				program("solidtrace", "report", "--outcome", "done", "--summary",
					"The labels have new names.\nOne check does not pass, so this PR has no Evidence."),
				say("The labels have new names."),
			),
		},
		{
			Name:   "needs a person, with a question",
			Prompt: "Demo: show the price in the buyer's currency (needs a person)",
			Want:   dispatcherapi.StateWaitingOnPerson,
			Script: script(
				say("The cart can show a price in two ways. I need your choice."),
				program("solidtrace", "report", "--outcome", "needs-a-person", "--summary",
					"I found two places that format a price.", "--question",
					"Must the price use the buyer's currency or the shop's currency?"),
				say("I wait for your answer."),
			),
		},
		{
			Name:   "failed (the harness exited in the middle of a turn)",
			Prompt: "Demo: fix the flaky inventory test (failed)",
			Want:   dispatcherapi.StateFailed,
			Script: failedScript,
		},
		{
			Name:   "stopped by the person while it runs",
			Prompt: "Demo: update the shipping rates (stopped while it runs)",
			Want:   dispatcherapi.StateStopped,
			Stop:   true,
			Script: []string{takeTask, init, say("I read the shipping rates. This is a long task."), untilStopped},
		},
		{
			Name:   "keeps running until a person stops it in Console",
			Prompt: "Demo: move the order history to the new table (runs until you stop it)",
			Want:   dispatcherapi.StateRunning,
			Script: []string{takeTask, init, say("I start the move of the order history."), untilStopped},
		},
	}, nil
}

// staticToken is a person's token that the Dispatcher accepts.
type staticToken string

func (t staticToken) Token(context.Context) (string, error)             { return string(t), nil }
func (t staticToken) Refreshed(context.Context, string) (string, error) { return string(t), nil }

// stage is what the scenarios need of the demo: the Dispatcher's API as a
// person, the Project, and the fake Runner.
type stage struct {
	Client  *dispatcherapi.Client
	Project string
	// Agent queues the script of the Harness of the next Execution.
	Agent func(lines ...string)
	// Dispatched lists the Executions that the Runner started.
	Dispatched func() []string
	// Say tells the person what happens.
	Say func(format string, args ...any)
}

// started is a Run of a scenario.
type started struct {
	scenario
	ID string
}

// settleTime is the longest wait for one Run to reach its state.
const settleTime = 90 * time.Second

// Start starts the Runs one at a time, so that each script goes to its
// own Run, and waits until each Run has its state. The demo sends the
// stop of the Runs that it stops.
func (s stage) Start(ctx context.Context, list []scenario) ([]started, error) {
	var runs []started
	for _, sc := range list {
		before := len(s.Dispatched())
		s.Agent(sc.Script...)
		run, err := s.Client.Start(ctx, dispatcherapi.StartRun{Project: s.Project, Prompt: sc.Prompt})
		if err != nil {
			return runs, fmt.Errorf("start %q: %w", sc.Prompt, err)
		}
		runs = append(runs, started{scenario: sc, ID: run.ID})
		if err := waitUntil(ctx, 60*time.Second, func() bool { return len(s.Dispatched()) > before }); err != nil {
			return runs, fmt.Errorf("the Runner did not start an Execution for %q: %w", sc.Prompt, err)
		}
	}
	for _, r := range runs {
		if r.Stop {
			if _, err := s.waitState(ctx, r, dispatcherapi.StateRunning); err != nil {
				return runs, err
			}
			// Let the agent work for a short time, and upload some of its
			// Agent session, before the person stops it.
			if err := sleep(ctx, 3*time.Second); err != nil {
				return runs, err
			}
			if _, err := s.Client.Stop(ctx, r.ID); err != nil {
				return runs, fmt.Errorf("stop %q: %w", r.Prompt, err)
			}
		}
	}
	for _, r := range runs {
		if _, err := s.waitState(ctx, r, r.Want); err != nil {
			return runs, err
		}
	}
	return runs, nil
}

// webhookSecret is the secret of the demo's forge webhook.
const webhookSecret = "demo-webhook-secret"

// MergeFirstPR merges the PR of the Run on the fake Forge and sends the
// Dispatcher the webhook that Bitbucket sends, so that the Run closes with
// the reason pr_merged and its closed event has the Run's totals. It
// returns when the Dispatcher closed the Run.
func (s stage) MergeFirstPR(ctx context.Context, env *factorytest.Env, r started) error {
	repo := "example/" + s.Project
	prs := env.Forge.PRs(repo)
	if len(prs) == 0 {
		return fmt.Errorf("the fake Forge has no PR in %s to merge", repo)
	}
	connection, err := s.Client.SaveWebhookSecret(ctx, factorytest.ForgeConnection, webhookSecret)
	if err != nil {
		return fmt.Errorf("save the webhook secret: %w", err)
	}
	path, ok := strings.CutPrefix(connection.WebhookURL, factorytest.PublicWebhookURL)
	if !ok {
		return fmt.Errorf("the forge connection has the webhook URL %q", connection.WebhookURL)
	}
	// The Run's PR is the first that the demo made.
	pr := prs[0]
	env.Forge.SetState(repo, pr.ID, "MERGED")
	if a := env.ForgeWebhook(path, webhookSecret, "{demo-merged}", fakeforge.EventPRFulfilled, env.Forge.PRWebhook(repo, pr.ID)); a.Status != 200 {
		return fmt.Errorf("the merge webhook: %d %s", a.Status, a.Body)
	}
	return waitUntil(ctx, 30*time.Second, func() bool {
		for _, ev := range env.RunEvents(r.ID) {
			if ev["event.kind"] == "closed" {
				return true
			}
		}
		return false
	})
}

// waitState waits until the Run has the state, and for a state that is not
// running, until its Executions ended.
func (s stage) waitState(ctx context.Context, r started, state string) (dispatcherapi.Run, error) {
	var run dispatcherapi.Run
	var last error
	err := waitUntil(ctx, settleTime, func() bool {
		run, last = s.Client.Get(ctx, r.ID)
		if last != nil || run.State != state {
			return false
		}
		if state != dispatcherapi.StateRunning {
			for _, e := range run.Executions {
				if e.State != dispatcherapi.ExecutionEnded {
					return false
				}
			}
		}
		return true
	})
	if err != nil {
		return run, fmt.Errorf("the Run %q (%s) is %s, not %s, after %s: %v %v", r.Prompt, r.ID, run.State, state, settleTime, last, err)
	}
	return run, nil
}

// StopRunning stops each Run that has not ended, and waits until it
// stopped, so that no Run stays in SolidTrace as running with no agent.
func (s stage) StopRunning(ctx context.Context, runs []started) {
	for _, r := range runs {
		run, err := s.Client.Get(ctx, r.ID)
		if err != nil || run.State != dispatcherapi.StateRunning && run.State != dispatcherapi.StateQueued {
			continue
		}
		if _, err := s.Client.Stop(ctx, r.ID); err != nil {
			s.Say("could not stop Run %s: %v", r.ID, err)
			continue
		}
		if _, err := s.waitState(ctx, r, dispatcherapi.StateStopped); err != nil {
			s.Say("%v", err)
		}
	}
}

func waitUntil(ctx context.Context, limit time.Duration, ok func() bool) error {
	deadline := time.Now().Add(limit)
	for !ok() {
		if time.Now().After(deadline) {
			return errors.New("timed out")
		}
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
