package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

// The factory demo (bin/factory-demo): the real Dispatcher and the real
// wrapper with the Seam 3 fakes, connected to the person's running dev
// SolidTrace, with six Runs of different outcomes to look at in Console.
//
//	flowstack demo up     set up, start the Runs, wait for Ctrl-C, put back
//	flowstack demo down   stop a running demo, and put back what it changed
//
// Console's side of the set-up is the rake tasks in
// console/lib/tasks/factory_demo.rake. The demo keeps what it changed in
// console/tmp/factory-demo.json until it has put it back, so a demo that
// died (SIGKILL, a power cut) is cleaned up by the next up or down.

// token is an API token that the rake task made.
type token struct {
	ID     int    `json:"id"`
	Secret string `json:"secret,omitempty"`
}

// demoState is what the rake task made and the demo changed. The Console
// task's "down" reads it too; the secrets are not kept in the file.
type demoState struct {
	PID           int    `json:"pid"`
	Org           string `json:"org"`
	Project       string `json:"project"`
	Admin         string `json:"admin"`
	ConsoleURL    string `json:"console_url"`
	DispatcherURL string `json:"dispatcher_url,omitempty"`
	Old           struct {
		ConsoleURL    string `json:"console_url"`
		DispatcherURL string `json:"dispatcher_url"`
	} `json:"old"`
	TurnedOn        bool  `json:"turned_on"`
	DispatcherToken token `json:"dispatcher_token"`
	PersonToken     token `json:"person_token"`
}

type demoFlags struct {
	consoleDir, consoleURL, ingestURL, org, project, admin string
}

func runDemo(args []string) int {
	if len(args) == 0 || args[0] != "up" && args[0] != "down" {
		fmt.Fprintln(os.Stderr, "usage: flowstack demo up|down [--console-dir DIR] [--console-url URL] [--ingest-url URL] [--org SLUG] [--project SLUG] [--admin EMAIL]")
		return 2
	}
	var f demoFlags
	set := flag.NewFlagSet("demo "+args[0], flag.ContinueOnError)
	set.StringVar(&f.consoleDir, "console-dir", "", "Console's directory (default: console/ here or above)")
	set.StringVar(&f.consoleURL, "console-url", "http://localhost:4001", "the dev Console's URL")
	set.StringVar(&f.ingestURL, "ingest-url", "http://localhost:4000", "the dev EventStore's ingest URL")
	set.StringVar(&f.org, "org", "solid-trace-org", "the Organization's slug")
	set.StringVar(&f.project, "project", "solid-trace-project", "the Project's slug")
	set.StringVar(&f.admin, "admin", "admin@solidtrace.local", "the email of an Admin of the Organization")
	if err := set.Parse(args[1:]); err != nil {
		return 2
	}
	if f.consoleDir == "" {
		var err error
		if f.consoleDir, err = findConsole(); err != nil {
			fmt.Fprintln(os.Stderr, "demo:", err)
			return 2
		}
	}
	f.consoleURL = strings.TrimRight(f.consoleURL, "/")
	if args[0] == "down" {
		return demoDown(f)
	}
	return demoUp(f)
}

// findConsole looks for console/ in the working directory and above.
func findConsole() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		for _, candidate := range []string{filepath.Join(dir, "console"), dir} {
			if _, err := os.Stat(filepath.Join(candidate, "bin", "rails")); err == nil {
				return candidate, nil
			}
		}
		if parent := filepath.Dir(dir); parent != dir {
			dir = parent
			continue
		}
		return "", errors.New("found no console/ directory: give --console-dir")
	}
}

func statePath(f demoFlags) string { return filepath.Join(f.consoleDir, "tmp", "factory-demo.json") }

func readState(f demoFlags) (*demoState, error) {
	data, err := os.ReadFile(statePath(f))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s demoState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("read %s: %w", statePath(f), err)
	}
	return &s, nil
}

func writeState(f demoFlags, s *demoState) error {
	data, _ := json.Marshal(s)
	if err := os.MkdirAll(filepath.Dir(statePath(f)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(statePath(f), data, 0o600)
}

// rails runs a rake task of Console and gives its last line of output.
func rails(f demoFlags, task string, env ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mise", "exec", "--", "bin/rails", task)
	cmd.Dir = f.consoleDir
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rails %s: %w\n%s%s", task, err, out.String(), errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

// putBack puts back the settings, deletes the tokens, and forgets the
// state. It is safe to call it again.
func putBack(f demoFlags, s *demoState) error {
	data, _ := json.Marshal(s)
	if _, err := rails(f, "factory_demo:down", "DEMO_STATE="+string(data)); err != nil {
		return err
	}
	if err := os.Remove(statePath(f)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// isDemo is true when the process is a demo of this program (so that a
// process that has an old demo's pid is not stopped).
func isDemo(pid int) bool {
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	return err == nil && bytes.Contains(cmdline, []byte("demo\x00up"))
}

func tell(format string, args ...any) { fmt.Printf(format+"\n", args...) }

func demoDown(f demoFlags) int {
	s, err := readState(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	if s == nil {
		tell("No demo is up: nothing to put back.")
		return 0
	}
	if alive(s.PID) && isDemo(s.PID) {
		tell("Stopping the demo (process %d)...", s.PID)
		syscall.Kill(s.PID, syscall.SIGTERM)
		for i := 0; i < 300 && alive(s.PID); i++ {
			time.Sleep(time.Second)
		}
		if alive(s.PID) {
			fmt.Fprintf(os.Stderr, "demo: process %d did not stop in 5 minutes\n", s.PID)
			return 1
		}
	}
	// A demo that ended by itself has put back and removed its state.
	if s, err = readState(f); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	if s != nil {
		tell("Putting back what the demo changed...")
		if err := putBack(f, s); err != nil {
			fmt.Fprintln(os.Stderr, "demo:", err)
			return 1
		}
	}
	tell("The demo is down. Its Runs and Agent sessions stay in SolidTrace.")
	return 0
}

func demoUp(f demoFlags) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	t := newProcessTB()
	defer t.cleanup()

	if old, err := readState(f); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	} else if old != nil {
		if alive(old.PID) && isDemo(old.PID) {
			fmt.Fprintf(os.Stderr, "demo: a demo runs already (process %d). Run 'demo down' first.\n", old.PID)
			return 1
		}
		tell("A demo before this one did not finish. Putting back its settings...")
		if err := putBack(f, old); err != nil {
			fmt.Fprintln(os.Stderr, "demo:", err)
			return 1
		}
	}

	tell("Setting up Console (Agent sessions, the Dispatcher's token)...")
	line, err := rails(f, "factory_demo:up", "DEMO_ORG="+f.org, "DEMO_PROJECT="+f.project, "DEMO_ADMIN="+f.admin, "DEMO_CONSOLE_URL="+f.consoleURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	var state demoState
	if err := json.Unmarshal([]byte(line), &state); err != nil {
		fmt.Fprintf(os.Stderr, "demo: read the set-up of Console: %v\n%s\n", err, line)
		return 1
	}
	secrets := state
	state.PID = os.Getpid()
	state.DispatcherToken.Secret, state.PersonToken.Secret = "", ""
	if err := writeState(f, &state); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	// The first cleanup is the last one: after the Dispatcher stops and
	// sends its last events with its token.
	t.Cleanup(func() {
		tell("Putting back the settings...")
		if err := putBack(f, &state); err != nil {
			fmt.Fprintln(os.Stderr, "demo: could not put back the settings; run 'demo down':", err)
		}
	})

	env := factorytest.New(t, factorytest.RealClock,
		factorytest.AtSolidTrace(f.consoleURL, f.ingestURL, secrets.DispatcherToken.Secret),
		factorytest.WithHTTP(&http.Client{Timeout: 30 * time.Second}), factorytest.Executions)
	env.Forge.AddRepo("example/"+f.project, demoRepository)

	state.DispatcherURL = env.URL()
	if err := writeState(f, &state); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	if _, err := rails(f, "factory_demo:dispatcher_url", "DEMO_DISPATCHER_URL="+env.URL()); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}

	client := &dispatcherapi.Client{URL: env.URL(), HTTP: &http.Client{Timeout: 30 * time.Second},
		Tokens: staticToken(secrets.PersonToken.Secret), Version: "0.0.0-demo"}
	if _, err := client.SaveConnection(ctx, factorytest.ForgeConnection, dispatcherapi.SaveConnection{Kind: "bitbucket", Token: env.Forge.Token}); err != nil {
		fmt.Fprintln(os.Stderr, "demo: save the forge connection:", err)
		return 1
	}
	if _, err := client.SaveSettings(ctx, f.project, dispatcherapi.SaveFactorySettings{
		Repository: "bitbucket.org/example/" + f.project, Connection: factorytest.ForgeConnection}); err != nil {
		fmt.Fprintln(os.Stderr, "demo: save the Factory settings:", err)
		return 1
	}

	runsList := fmt.Sprintf("%s/%s/runs", f.consoleURL, f.org)
	tell("")
	tell("Open these pages in Console, signed in as %s:", f.admin)
	tell("  Runs list:       %s", runsList)
	tell("  Factory section: %s/%s/projects/%s", f.consoleURL, f.org, f.project)
	tell("")

	list, err := scenarios()
	if err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	s := stage{Client: client, Project: f.project, Agent: env.Runner.Agent, Dispatched: env.Runner.Dispatched, Say: tell}
	tell("Starting %d Runs (about a minute)...", len(list))
	runs, err := s.Start(ctx, list)
	code := 0
	switch {
	case ctx.Err() != nil:
		// Ctrl-C while the Runs start.
	case err != nil:
		fmt.Fprintf(os.Stderr, "demo: %v\nDispatcher log:\n%s", err, env.Logs())
		code = 1
	default:
		// One Run closes, so that the starter Factory Dashboard has a closed event to draw.
		tell("Merging the PR of the first Run on the fake Forge...")
		if err := s.MergeFirstPR(ctx, env, runs[0]); err != nil {
			fmt.Fprintf(os.Stderr, "demo: the merge did not close the first Run (the Dashboard stays empty): %v\n", err)
		}
		tell("The Runs:")
		for _, r := range runs {
			tell("  %s  %s", r.ID, r.Name)
			tell("      %s/%s/runs/%s", f.consoleURL, f.org, r.ID)
		}
		tell("")
		tell("The demo runs. Press Ctrl-C to stop it (or run 'demo down' in another terminal).")
		tell("The last Run works until you press its Stop button in Console.")
		<-ctx.Done()
	}

	tell("")
	tell("Stopping the demo. The Runs and their Agent sessions stay in SolidTrace.")
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s.StopRunning(stopCtx, runs)
	return code
}
