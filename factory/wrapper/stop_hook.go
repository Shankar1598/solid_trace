package wrapper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// The Stop hook. The wrapper installs it in the harness (a --settings
// file outside the checkout), and Claude Code runs it each time that the
// agent ends its turn. The hook is the wrapper's own program with the
// argument StopHookCommand. It reads the run state file, which the
// wrapper and the hook share, and refuses the stop or allows it. Its
// checks, in order:
//
//  1. A message arrived after the last report (ticket 57 adds it).
//  2. Background tasks still run: refuse one time in a turn.
//  3. No report: refuse. After maxNoReportRefusals such refusals the hook
//     allows the stop, and the wrapper ends the Execution with the reason
//     "no report" at the turn's result. A refusal of check 1 or 2 does not
//     count as a "no report" refusal.
//
// The run state file is not a secret and not a security boundary: the
// agent can write it. A hook that cannot read or write the file allows the
// stop. The outcome then still comes only from the report.

// EnvRunState names the run state file in the harness's environment: the
// Stop hook and the agent's solidtrace commands get its path from it.
const EnvRunState = "SOLIDTRACE_RUN_STATE"

// StopHookCommand is the argument of the wrapper's program that runs it
// as the Stop hook.
const StopHookCommand = "stop-hook"

// maxNoReportRefusals is how many times the hook refuses a stop with no
// report in an Execution.
const maxNoReportRefusals = 2

// stopHookTimeout is the Stop hook's limit in seconds, in the settings.
const stopHookTimeout = 30

// runState is the content of the run state file.
type runState struct {
	// The wrapper writes these. Turn is the count of results so far: the
	// current turn's number. BackgroundTasks is the count of background
	// tasks that run, from the last background_tasks_changed event.
	// Reported tells that the Dispatcher took a report in this Execution.
	Turn            int  `json:"turn"`
	BackgroundTasks int  `json:"background_tasks"`
	Reported        bool `json:"reported"`
	// The Stop hook writes these. BackgroundRefused is Turn + 1 of the
	// last refusal for background tasks (0: none yet).
	BackgroundRefused int `json:"background_refused"`
	NoReportRefusals  int `json:"no_report_refusals"`
}

// changeRunState applies change to the run state file under an exclusive
// lock, and writes the file when the state changed. With create, an
// absent file is a zero state; without it, an absent file is an error.
func changeRunState(path string, create bool, change func(*runState)) error {
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	// The lock goes with the close. The file is never replaced (a rename
	// would make a second inode, with a lock of its own).
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock %s: %w", path, err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	var s runState
	if len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, &s); err != nil {
			return fmt.Errorf("%s is not a run state: %w", path, err)
		}
	}
	before := s
	change(&s)
	if s == before && len(b) > 0 {
		return nil
	}
	out, _ := json.Marshal(s)
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err = f.WriteAt(append(out, '\n'), 0)
	return err
}

// stopDecision gives the reason to refuse the stop ("" allows it), and
// records the refusal in s.
func stopDecision(s *runState) string {
	if s.BackgroundTasks > 0 && s.BackgroundRefused != s.Turn+1 {
		s.BackgroundRefused = s.Turn + 1
		if s.BackgroundTasks == 1 {
			return "1 background task still runs. If your report depends on its result, wait for it and report again. " +
				"If not, stop it or stop now."
		}
		return fmt.Sprintf("%d background tasks still run. If your report depends on their result, wait for it and "+
			"report again. If not, stop them or stop now.", s.BackgroundTasks)
	}
	if !s.Reported && s.NoReportRefusals < maxNoReportRefusals {
		s.NoReportRefusals++
		return fmt.Sprintf("You did not report. Before you stop, run: solidtrace report --outcome "+
			"done|needs-a-person|failed --summary \"<what you did, or why it failed>\" (with needs-a-person, also "+
			"--question \"<your question>\"). This is refusal %d of %d: after it, a stop with no report fails the Run "+
			"with the reason \"no report\".", s.NoReportRefusals, maxNoReportRefusals)
	}
	return ""
}

// StopHook is the Stop hook program. It reads the hook input on stdin,
// and writes the decision to refuse the stop on stdout. Exit code 1 (an
// error, which does not refuse the stop) when the run state file cannot
// be read or written.
func StopHook(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	_, _ = io.Copy(io.Discard, io.LimitReader(stdin, 1<<20))
	path := getenv(EnvRunState)
	if path == "" {
		fmt.Fprintln(stderr, "wrapper stop-hook: "+EnvRunState+" is not set, so the stop is allowed")
		return 0
	}
	var reason string
	err := changeRunState(path, false, func(s *runState) { reason = stopDecision(s) })
	if err != nil {
		fmt.Fprintln(stderr, "wrapper stop-hook: the stop is allowed, because the run state is not available:", err)
		return 1
	}
	if reason == "" {
		return 0
	}
	_ = json.NewEncoder(stdout).Encode(map[string]string{"decision": "block", "reason": reason})
	return 0
}

// installStopHook writes the harness's settings with the Stop hook, and a
// new run state file. Both are outside the checkout.
func (e *execution) installStopHook() error {
	program, err := ownProgram()
	if err != nil {
		return fmt.Errorf("find the wrapper's program for the Stop hook: %w", err)
	}
	settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": shellQuote(program) + " " + StopHookCommand,
			"timeout": stopHookTimeout}}}}}})
	if err := os.WriteFile(e.cfg.harnessSettings(), settings, 0o600); err != nil {
		return fmt.Errorf("write the harness's settings: %w", err)
	}
	if err := os.Remove(e.cfg.runState()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the old run state: %w", err)
	}
	if err := changeRunState(e.cfg.runState(), true, func(*runState) {}); err != nil {
		return fmt.Errorf("write the run state: %w", err)
	}
	return nil
}

// setRunState changes the run state file. A failure is logged: the hook
// then allows a stop that it could refuse, and the outcome still comes
// from the report.
func (e *execution) setRunState(change func(*runState)) {
	if err := changeRunState(e.cfg.runState(), false, change); err != nil {
		fmt.Fprintln(e.log, "wrapper: the run state of the Stop hook:", err)
	}
}

// ownProgram is the absolute path of the program that runs: the name that
// started it (os.Args[0]), not the target of a link, so that a program
// with several names keeps this one.
func ownProgram() (string, error) {
	program := os.Args[0]
	if !strings.Contains(program, "/") {
		found, err := exec.LookPath(program)
		if err != nil {
			return "", err
		}
		program = found
	}
	return filepath.Abs(program)
}
