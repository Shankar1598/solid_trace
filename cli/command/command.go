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
	// LinearAuthorizeURL is Linear's approval page for `connections add
	// linear` ("" is LinearAuthorizeURL; the tests set a fake).
	LinearAuthorizeURL string
	Now                func() time.Time
	Sleep              func(ctx context.Context, d time.Duration) error
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
  solidtrace start [--project PROJECT] [--branch BRANCH] [--dispatcher URL] PROMPT
      Start a Run: an agent works on PROMPT with the Factory settings of
      PROJECT. With no --project, the Project is the one whose Factory
      settings name the current git repository and a folder that holds
      the current folder (the deepest folder wins). It prints the Run's
      id and its link in Console. The first Dispatcher verb needs
      --dispatcher with the Dispatcher's URL, which your operator gives;
      later verbs use the same URL.
  solidtrace stop [--dispatcher URL] RUN_ID
      Stop a Run that is queued, running or waiting on a person. The Run
      is stopped at once; its agent is interrupted and its Execution
      ends. A message continues a stopped Run.
  solidtrace message [--dispatcher URL] RUN_ID MESSAGE
      Send MESSAGE to a Run. The agent of a running Run gets it when its
      current tool call ends, in the same turn, or in a new turn; there
      is no interrupt. A queued Run's agent gets it after the prompt. A
      Run that waits on a person, is done, failed or stopped is queued:
      a new Execution continues its Agent session with the message. A
      Run that waits on an account limit or is taken over keeps it until
      the Run continues. A Closed Run refuses it. A message that the
      agent did not get goes to a new Execution.
  solidtrace interrupt [--dispatcher URL] RUN_ID [MESSAGE]
      Interrupt the agent of a running Run at once: its current tool call
      stops. With MESSAGE, the agent gets it in the same Execution when
      its turn stops, and works on. With no MESSAGE, the Execution ends,
      and the Run waits on a person; a message continues it.
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
  solidtrace connections add linear NAME --client-id ID [--team KEY] [--port PORT]
          [--dispatcher URL] < CLIENT_SECRET
      Install the Linear OAuth app as the connection NAME (an Admin only),
      with the app's client secret on stdin. It opens Linear's approval
      page with actor=app, so the factory's status changes and comments
      show as the app. The app's callback URL must be
      http://localhost:PORT/callback (PORT 47615 by default). The
      Dispatcher keeps the tokens encrypted and refreshes them. The
      connection is of the Linear team KEY, such as ENG; leave out
      --team when the app can read only one team. Run it again to
      install the app again.
  solidtrace connections webhook-secret NAME [--dispatcher URL] < SECRET
      Save the secret that signs the connection's webhooks, with the
      secret on stdin (an Admin only). Paste its webhook URL into the
      connection's service. The Dispatcher keeps the secret encrypted.
  solidtrace senders list [--dispatcher URL]
      List your Organization's senders of the generic webhook (never
      their secrets), with each webhook URL and the time of the last
      delivery.
  solidtrace senders create NAME [--project PROJECT]... [--dispatcher URL]
      Make a sender (an Admin only): a tool that starts Runs with signed
      requests to its webhook URL, in the Projects that you give (none is
      each Project). It prints the sender's secret one time only.
  solidtrace senders delete SENDER_ID [--dispatcher URL]
      Delete a sender (an Admin only). Its webhook URL then gets 404.
  solidtrace schedules list [--project PROJECT] [--dispatcher URL]
      List your Organization's schedules, with the next fire time and the
      last Run of each.
  solidtrace schedules get ID
      Show one schedule.
  solidtrace schedules create PROJECT --name NAME --cron CRON
          [--time-zone ZONE] [--base-branch BRANCH] PROMPT
      Make a schedule: at each fire time of CRON (five fields: minute,
      hour, day of the month, month, day of the week) in ZONE (an IANA
      time zone; none is UTC), it starts a Run of PROJECT with PROMPT.
      The shortest interval between two fire times is 1 hour, unless your
      operator sets another. A fire time while the schedule's last Run is
      active starts no Run.
  solidtrace schedules update ID [--name NAME] [--cron CRON]
          [--time-zone ZONE] [--base-branch BRANCH] [--prompt PROMPT]
      Change the values that you give.
  solidtrace schedules pause ID
  solidtrace schedules continue ID
      Stop the schedule's fire times at once, or start them again from
      now. A Run that the schedule started continues.
  solidtrace schedules delete ID
      Delete the schedule. The Runs that it started continue.
  solidtrace check [COMMAND...]
      Inside a Run only: the factory's tool runs the checks and records
      them as Evidence (the command, the exit status, the time and the
      last 100 lines of output). With no COMMAND, it runs each check of
      the factory file; with a COMMAND, it runs that command (sh -c).
      The next report sends the records. A check that you run with a
      tool of your own is not a check. Exit 1 when a check failed.
  solidtrace report --outcome done|needs-a-person|failed --summary TEXT
          [--question TEXT]
      Inside a Run only: report the outcome of your work to the wrapper.
      The last report sets the Run's outcome. needs-a-person needs
      --question, and ends the Execution at once; a person's answer
      continues the Run.
  solidtrace pr --title TEXT [--description TEXT]
      Inside a Run only: push the Run's branch, and open its PR to the
      base branch. Each later pr updates the title and description of
      the same PR (a flag that you do not give stays as it is). pr never
      merges.
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
	case "stop":
		return stop(ctx, args[1:], env)
	case "message":
		return message(ctx, args[1:], env)
	case "interrupt":
		return interrupt(ctx, args[1:], env)
	case "projects":
		return projects(ctx, args[1:], env)
	case "connections":
		return connections(ctx, args[1:], env)
	case "senders":
		return senders(ctx, args[1:], env)
	case "schedules":
		return schedules(ctx, args[1:], env)
	case "check":
		return check(ctx, args[1:], env)
	case "report":
		return report(ctx, args[1:], env)
	case "pr":
		return pr(ctx, args[1:], env)
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
