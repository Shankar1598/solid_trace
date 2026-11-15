package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/solidtrace/cli/dispatcherapi"
)

// reportFile is the file under evidence/ that each report writes: the
// full summary, the question and the checks.
const reportFile = "report.md"

// nextReportFile, in the project directory and not under evidence/,
// holds the text of a report until the Dispatcher accepts the report. So
// no upload sends it.
const nextReportFile = ".report.md.next"

// check is the agent's check call: the wrapper runs the factory file's
// checks, or the agent's command, and records each. The next report
// sends the records. A check runs in the Execution's kill boundary, and
// stops when the agent's call goes (the end kills the agent).
func (e *execution) check(w http.ResponseWriter, r *http.Request, environ []string) {
	var run dispatcherapi.CheckRun
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&run); err != nil {
		writeProblem(w, http.StatusBadRequest, "the body must be a JSON object with command")
		return
	}
	e.mu.Lock()
	ending := e.ending
	e.mu.Unlock()
	if ending {
		writeProblem(w, http.StatusConflict, "the Execution is ending: a check now does not count")
		return
	}
	todo := e.task.Checks
	if command := strings.TrimSpace(run.Command); command != "" {
		if len(command) > dispatcherapi.MaxCheckText || !utf8.ValidString(command) {
			writeProblem(w, http.StatusBadRequest, fmt.Sprintf("a check's command is UTF-8 text of at most %d bytes",
				dispatcherapi.MaxCheckText))
			return
		}
		todo = []dispatcherapi.Check{{Command: command}}
	}
	if len(todo) == 0 {
		writeProblem(w, http.StatusBadRequest, "the factory file names no checks: give a command, such as "+
			"solidtrace check go test ./...")
		return
	}
	answer := dispatcherapi.CheckAnswer{Checks: []dispatcherapi.CheckResult{}}
	for _, c := range todo {
		key := checkKey{name: c.Name, command: c.Command}
		if !e.hold(key) {
			writeProblem(w, http.StatusConflict, fmt.Sprintf("an Execution records at most %d different checks",
				dispatcherapi.MaxChecks))
			return
		}
		result := e.runCheck(r.Context(), environ, c)
		if r.Context().Err() != nil {
			// The agent went: the check did not end, so it is no record.
			e.record(key, nil)
			return
		}
		e.record(key, &result)
		answer.Checks = append(answer.Checks, result)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(answer)
}

// checkKey is the identity of a check: a later run of the same name and
// command replaces its record.
type checkKey struct{ name, command string }

func (k checkKey) is(r dispatcherapi.CheckResult) bool { return r.Name == k.name && r.Command == k.command }

// hold holds a place for a run of the check key, and is false when the
// Execution has MaxChecks other checks, recorded or running.
func (e *execution) hold(key checkKey) bool {
	e.checksMu.Lock()
	defer e.checksMu.Unlock()
	if e.running[key] == 0 && !slices.ContainsFunc(e.checks, key.is) {
		others := len(e.checks)
		for k := range e.running {
			if !slices.ContainsFunc(e.checks, k.is) {
				others++
			}
		}
		if others >= dispatcherapi.MaxChecks {
			return false
		}
	}
	if e.running == nil {
		e.running = map[checkKey]int{}
	}
	e.running[key]++
	return true
}

// record lets go of a place that hold gave, and keeps result (when it
// is not nil) as the last result of its check: a later run of the same
// name and command replaces it in its place.
func (e *execution) record(key checkKey, result *dispatcherapi.CheckResult) {
	e.checksMu.Lock()
	defer e.checksMu.Unlock()
	if e.running[key]--; e.running[key] <= 0 {
		delete(e.running, key)
	}
	if result == nil {
		return
	}
	if i := slices.IndexFunc(e.checks, key.is); i >= 0 {
		e.checks[i] = *result
		return
	}
	e.checks = append(e.checks, *result)
}

// runCheck runs one check with sh -c in the agent's working directory,
// as the setup command runs, and gives its record.
func (e *execution) runCheck(ctx context.Context, environ []string, c dispatcherapi.Check) dispatcherapi.CheckResult {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
	cmd.Dir = e.workDir
	cmd.Env = e.gitEnv(environ, "")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = gitWait
	out := &tail{max: 4 * dispatcherapi.MaxCheckOutput}
	cmd.Stdout, cmd.Stderr = out, out
	fmt.Fprintf(e.log, "wrapper: a check: %s\n", c.Command)
	started := time.Now()
	err := e.boundary.Start(cmd)
	if err == nil {
		err = cmd.Wait()
	}
	status := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		status = exit.ExitCode()
	case err != nil:
		status = -1
		fmt.Fprintf(out, "\n%v\n", err)
	}
	return dispatcherapi.CheckResult{Name: c.Name, Command: c.Command, ExitStatus: status, Passed: err == nil,
		StartedAt: started.UTC(), DurationMS: time.Since(started).Milliseconds(), Output: checkOutput(out.String())}
}

// checkOutput is the last MaxCheckLines lines of out, at most
// MaxCheckOutput bytes of valid UTF-8.
func checkOutput(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > dispatcherapi.MaxCheckLines {
		lines = lines[len(lines)-dispatcherapi.MaxCheckLines:]
	}
	s := strings.Join(lines, "\n")
	if len(s) > dispatcherapi.MaxCheckOutput {
		s = s[len(s)-dispatcherapi.MaxCheckOutput:]
	}
	return strings.ToValidUTF8(s, "")
}

// tail is a writer that keeps the last max bytes written to it.
type tail struct {
	max int
	b   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2*t.max {
		t.b = append(t.b[:0], t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	if len(t.b) > t.max {
		return string(t.b[len(t.b)-t.max:])
	}
	return string(t.b)
}

// addEvidence adds the wrapper's part to the agent's report: the checks,
// the delivered-message counter, and the Evidence paths, with
// evidence/report.md. It writes the report's text to a new file, and
// gives commit: commit(true) puts the file in the place of
// evidence/report.md (the Dispatcher accepted the report), and
// commit(false) removes it. So evidence/report.md is always the text of
// the last report that counts.
func (e *execution) addEvidence(report *dispatcherapi.Report) (commit func(accepted bool)) {
	e.checksMu.Lock()
	report.Checks = slices.Clone(e.checks)
	e.checksMu.Unlock()
	report.DeliveredMessages = int(e.delivered.Load())
	dir := filepath.Join(e.uploads.projectDir, evidenceDir)
	final := filepath.Join(dir, reportFile)
	next := filepath.Join(e.uploads.projectDir, nextReportFile)
	if err := os.WriteFile(next, []byte(reportText(*report)), 0o600); err != nil {
		fmt.Fprintln(e.log, "wrapper: write evidence/report.md:", err)
	}
	report.Evidence = evidencePaths(dir, e.log)
	return func(accepted bool) {
		if !accepted {
			os.Remove(next)
			return
		}
		if err := os.Rename(next, final); err != nil {
			fmt.Fprintln(e.log, "wrapper: write evidence/report.md:", err)
		}
	}
}

// evidencePaths gives the paths of the regular files under dir, as
// evidence/<path>, sorted, at most MaxEvidence, with evidence/report.md.
// A link is not Evidence: the upload leaves it out. A path that the
// Dispatcher refuses (more than MaxEvidencePath bytes, or not UTF-8) is
// not Evidence either, and the log tells it.
func evidencePaths(dir string, log io.Writer) []string {
	paths := []string{evidenceDir + "/" + reportFile}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == reportFile {
			return nil
		}
		p := evidenceDir + "/" + filepath.ToSlash(rel)
		if len(p) > dispatcherapi.MaxEvidencePath || !utf8.ValidString(p) {
			fmt.Fprintf(log, "wrapper: %q is not Evidence: a path is UTF-8 text of at most %d bytes\n",
				p, dispatcherapi.MaxEvidencePath)
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	slices.Sort(paths)
	if len(paths) > dispatcherapi.MaxEvidence {
		paths = paths[:dispatcherapi.MaxEvidence]
	}
	return paths
}

// reportText is the text of evidence/report.md.
func reportText(r dispatcherapi.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Report: %s\n\n", strings.ReplaceAll(r.Outcome, "_", " "))
	if r.Summary != "" {
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", r.Summary)
	}
	if r.Question != "" {
		fmt.Fprintf(&b, "## Question\n\n%s\n\n", r.Question)
	}
	b.WriteString("## Checks\n\n")
	if len(r.Checks) == 0 {
		b.WriteString("No check ran.\n")
	}
	for _, c := range r.Checks {
		verdict := "pass"
		if !c.Passed {
			verdict = "fail"
		}
		label := "`" + c.Command + "`"
		if c.Name != "" {
			label = c.Name + " (" + label + ")"
		}
		fmt.Fprintf(&b, "### %s: %s\n\nExit status %d, at %s, in %d ms.\n\n```\n%s\n```\n\n", verdict, label,
			c.ExitStatus, c.StartedAt.Format(time.RFC3339), c.DurationMS, c.Output)
	}
	return b.String()
}
