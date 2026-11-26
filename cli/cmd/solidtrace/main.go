// Command solidtrace is the SolidTrace agent CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/cli/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dir, err := configDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "solidtrace: %v\n", err)
		os.Exit(1)
	}
	env := command.Env{
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		ConfigDir:       dir,
		HTTP:            &http.Client{Timeout: 30 * time.Second},
		OpenBrowser:     browser(),
		Now:             time.Now,
		Sleep:           sleep,
		Version:         version.Version,
		Detach:          detach,
		AgentSocket:     os.Getenv(agentsocket.Env),
		ClaudeSessionID: os.Getenv("CLAUDE_CODE_SESSION_ID"),
	}
	if home, err := os.UserHomeDir(); err == nil || os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		env.ClaudeProjectsDir = command.ClaudeProjectsDir(os.Getenv, home)
	}
	os.Exit(command.Run(ctx, os.Args[1:], env))
}

// configDir is SOLIDTRACE_CONFIG_DIR, else solidtrace in the user's config
// directory (~/.config on Linux, ~/Library/Application Support on macOS).
func configDir() (string, error) {
	if dir := os.Getenv("SOLIDTRACE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New("no config directory: set SOLIDTRACE_CONFIG_DIR")
	}
	return filepath.Join(base, "solidtrace"), nil
}

// browser opens a URL in the person's browser, or is nil on a machine with
// no browser: Linux with no display (for example over ssh).
func browser() func(string) error {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
		name = "xdg-open"
	default:
		return nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil
	}
	return opener(path, OpenerWait)
}

// OpenerWait is how long the CLI waits for open or xdg-open to exit.
const OpenerWait = 5 * time.Second

// opener runs the program at path with the URL. When the program exits
// with an error in the first wait (xdg-open with no browser handler, open
// over ssh), the browser did not open, and the CLI uses the device flow.
// A program that is still running after wait (xdg-open can wait for the
// browser) has opened it, and the CLI does not stop it.
func opener(path string, wait time.Duration) func(string) error {
	return func(url string) error {
		cmd := exec.Command(path, url)
		if err := cmd.Start(); err != nil {
			return err
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("%s: %w", filepath.Base(path), err)
			}
			return nil
		case <-timer.C:
			return nil
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
