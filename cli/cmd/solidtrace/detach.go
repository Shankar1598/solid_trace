package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// detach starts this binary as `solidtrace capture` with input on its
// stdin, in a session of its own, and does not wait for it. The child has
// no stdout and no stderr (capture writes only to its log), so the hook's
// stdout closes when the hook exits, and Claude Code does not wait. Its own
// session keeps it alive when Claude Code exits after SessionEnd or the
// terminal closes.
func detach(input []byte) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	cmd := exec.Command(self, "capture")
	cmd.Stdin = r
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		w.Close()
		return err
	}
	// The input is a few hundred bytes, far below a pipe's buffer, so the
	// write does not wait for the child.
	_, err = w.Write(input)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if rerr := cmd.Process.Release(); err == nil {
		err = rerr
	}
	if err != nil {
		return fmt.Errorf("give the hook input: %w", err)
	}
	return nil
}
