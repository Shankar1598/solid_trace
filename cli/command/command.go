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
	// Stdin is the hook input of `capture`, and the token of
	// `connections set`.
	Stdin io.Reader
	// Dir is the current directory, in which `start` finds the Project
	// ("" is the process's own).
	Dir    string
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
	// ClaudeProjectsDir is where Claude Code keeps the transcripts, for
	// `capture status`.
	ClaudeProjectsDir string
	// Detach starts `solidtrace capture` in the background with input on
	// its stdin, and does not wait for it. The hook of the plugin
	// (`capture --detach`) uses it.
	Detach func(input []byte) error
	// AgentSocket is the path of the agent's socket inside a Run (""
	// outside one), for `report`.
	AgentSocket string
}

const usage = `solidtrace: the SolidTrace agent CLI.

Usage:
  solidtrace login [--device] [--ingest-url URL] [Console URL]
      Sign in through SolidTrace in your browser, or with a code (--device)
      on a machine with no browser. The first sign-in to an instance needs
      --ingest-url: Console's capture setup page gives the login command.
  solidtrace capture
      Upload the new bytes of the Agent session that the Claude Code hook
      input on stdin names, when its repository is captured. It always
      exits 0; a failure goes to capture.log in the config directory. At
      SessionStart it sweeps each Agent session on the machine and sends
      what was not sent.
  solidtrace capture --detach
      The same, in the background: it reads the hook input, starts
      solidtrace capture with it, and returns at once. The hooks of the
      SolidTrace plugin for Claude Code run this.
  solidtrace capture status
      Show the Organization, its captured repositories, and each Agent
      session on this machine as uploaded, not in the list, or failed.
  solidtrace start [--project PROJECT] [--dispatcher URL] PROMPT
      Start a Run: an agent works on PROMPT with the Factory settings of
      PROJECT. With no --project, the Project is the one whose Factory
      settings name the current git repository and a folder that holds
      the current folder (the deepest folder wins). It prints the Run's
      id and its link in Console. The first Dispatcher verb needs
      --dispatcher with the Dispatcher's URL, which your operator gives;
      later verbs use the same URL.
  solidtrace runs list [--dispatcher URL]
      List the Runs of your Organization, newest first.
  solidtrace runs get [--dispatcher URL] RUN_ID
      Show one Run: its state, Project and prompt.
  solidtrace projects list [--dispatcher URL]
      List the Factory settings of your Organization's Projects.
  solidtrace projects get [--dispatcher URL] PROJECT
      Show a Project's Factory settings.
  solidtrace projects set PROJECT --repository REPOSITORY [--folder FOLDER]
          [--base-branch BRANCH] [--connection NAME] [--dispatcher URL]
      Save a Project's Factory settings (an Admin only). REPOSITORY is
      host/workspace/name or its clone URL. Several Projects can name one
      repository, each with its folder. Each value that you do not give
      is cleared: no folder is the root, and no base branch is the
      repository's default branch.
  solidtrace connections list [--dispatcher URL]
      List your Organization's connections (never their tokens or
      secrets), with the webhook URL of each connection that sends
      webhooks and the time of its last webhook.
  solidtrace connections set NAME [--kind bitbucket] [--dispatcher URL] < TOKEN
      Save a connection with the token on stdin, or replace its token
      (an Admin only). The Dispatcher keeps the token encrypted.
  solidtrace connections webhook-secret NAME [--dispatcher URL] < SECRET
      Save the secret that signs the connection's webhooks, with the
      secret on stdin (an Admin only). Paste its webhook URL into the
      connection's service. The Dispatcher keeps the secret encrypted.
  solidtrace report --outcome done|needs-a-person|failed --summary TEXT
          [--question TEXT]
      Inside a Run only: report the outcome of your work to the wrapper.
      The last report sets the Run's outcome. needs-a-person needs
      --question, and ends the Execution at once; a person's answer
      continues the Run.
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
	case "capture":
		if len(args) > 1 && args[1] == "status" {
			return captureStatus(ctx, env)
		}
		if len(args) > 1 && args[1] == "--detach" {
			return detachHook(env)
		}
		return captureHook(ctx, env)
	case "start":
		return start(ctx, args[1:], env)
	case "runs":
		return runs(ctx, args[1:], env)
	case "projects":
		return projects(ctx, args[1:], env)
	case "connections":
		return connections(ctx, args[1:], env)
	case "report":
		return report(ctx, args[1:], env)
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
