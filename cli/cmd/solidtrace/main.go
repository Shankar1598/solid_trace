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
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		ConfigDir:   dir,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		OpenBrowser: browser(),
		Now:         time.Now,
		Sleep:       sleep,
		Version:     version.Version,
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
	return func(url string) error {
		cmd := exec.Command(path, url)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
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
