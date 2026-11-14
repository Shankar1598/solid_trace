// Package command is the solidtrace command line: it reads the arguments
// and runs one verb. Env holds everything that it reads from the machine,
// so tests run it against fakes.
package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/solidtrace/cli/credentials"
	"github.com/solidtrace/cli/signin"
)

// Env is the machine that the command runs on.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// ConfigDir holds the credentials file.
	ConfigDir string
	HTTP      *http.Client
	// OpenBrowser opens a URL in the person's browser. Nil means that the
	// machine has no browser, so `login` uses the device flow.
	OpenBrowser func(url string) error
	Now         func() time.Time
	Sleep       func(ctx context.Context, d time.Duration) error
	// Version is the CLI's version.
	Version string
}

const usage = `solidtrace: the SolidTrace agent CLI.

Usage:
  solidtrace login [--device] [--ingest-url URL] [Console URL]
      Sign in through SolidTrace in your browser, or with a code (--device)
      on a machine with no browser. The first sign-in to an instance needs
      --ingest-url: Console's capture setup page gives the login command.
  solidtrace version
      Print the version of this solidtrace.
`

// Run runs the command of args and returns its exit code.
func Run(ctx context.Context, args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "login":
		return login(ctx, args[1:], env)
	case "version", "--version":
		fmt.Fprintf(env.Stdout, "solidtrace %s\n", env.Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(env.Stderr, "solidtrace: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func (env Env) store() credentials.Store {
	return credentials.Store{Dir: env.ConfigDir}
}

func (env Env) signinClient() *signin.Client {
	return &signin.Client{HTTP: env.HTTP, UserAgent: "solidtrace/" + env.Version, Now: env.Now, Sleep: env.Sleep}
}

// Source is the sign-in's token source on env.
func (env Env) Source() *signin.Source {
	return &signin.Source{Store: env.store(), Client: env.signinClient()}
}
