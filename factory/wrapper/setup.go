package wrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
)

// runSetupCommand runs the factory file's setup command with sh -c in
// the agent's working directory, after the checkout and before the
// harness. It runs in the Execution's kill boundary, so a process that it
// leaves goes at the end. A stop or a limit (ctx) kills its process
// group. A failure gives the exit status and the end of the output.
func (e *execution) runSetupCommand(ctx context.Context, environ []string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", e.task.Setup)
	cmd.Dir = e.workDir
	cmd.Env = e.gitEnv(environ, "")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// A process that the command leaves can hold the output open.
	cmd.WaitDelay = gitWait
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, e.log)
	cmd.Stderr = cmd.Stdout
	fmt.Fprintf(e.log, "wrapper: the setup command: %s\n", e.task.Setup)
	err := e.boundary.Start(cmd)
	if err == nil {
		err = cmd.Wait()
	}
	if err != nil {
		text := fmt.Sprintf("the setup command (%s) failed: %v", e.task.Setup, err)
		if tail := strings.TrimSpace(lastLines(out.String(), 20)); tail != "" {
			text += ": " + tail
		}
		return errors.New(text)
	}
	return nil
}
