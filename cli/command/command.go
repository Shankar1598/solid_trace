// Package command is the solidtrace command line: it reads the arguments
// and runs one verb. Env holds everything that it reads from the machine,
// so tests run it against fakes.
package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/solidtrace/cli/agentapi"
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
	// ClaudeSessionID is ClaudeSessionEnv, which Claude Code sets in
	// the environment of its Bash tool: the Agent session of `evidence
	// add`.
	ClaudeSessionID string
}

var usage = `solidtrace: the SolidTrace agent CLI.

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
  solidtrace capture status [--all]
      Show whether capture works on this machine: each setup step (the
      sign-in, the Claude Code plugin, the captured repositories list and
      the uploads) as done or not done, with what to do, and a count of the
      Agent sessions on this machine as uploaded, not captured, failed or
      not checked yet. --all lists each Agent session.
  solidtrace start [--project PROJECT] [--branch BRANCH] [--dispatcher URL] PROMPT
      Start a Run: an agent works on PROMPT with the Factory settings of
      PROJECT. With no --project, the Project is the one whose Factory
      settings name the current git repository and a folder that holds
      the current folder (the deepest folder wins). It prints the Run's
      id and its link in Console. The first Dispatcher verb needs
      --dispatcher with the Dispatcher's URL, which your operator gives;
      later verbs use the same URL.
  solidtrace stop [--dispatcher URL] RUN_ID
      Stop a Run that is queued, running, or waiting on a person or an
      account limit. The Run is stopped at once; its agent is
      interrupted and its Execution ends. A message continues a stopped
      Run.
  solidtrace switch-account [--dispatcher URL] RUN_ID
      Move a Run that waits on an account limit to another subscription
      account that is free. The Run is queued, and a new Execution
      continues its Agent session on that account. With no other account
      free, the switch is refused with the first reset time.
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
  solidtrace take-over [--dispatcher URL] RUN_ID
      Take over a Run. A running Run: its agent is interrupted, and the same
      Agent session continues in a terminal on the same machine. It
      prints the machine and the command that attaches you (for example
      in the Herdr app). When you leave the Agent session, a report that
      you made in it sets the Run's state; with no report, the Run waits
      on a person. A Run that waits on a person, is done, failed or
      stopped is queued: a new Execution continues its Agent session in a
      terminal on a new machine, and the Run page shows the machine and
      the command that attaches you. A Run that waits on an account
      limit refuses it, with the reset time.
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
          [--question TEXT] [--cause-report TEXT]
      Inside a Run only: report the outcome of your work to the wrapper.
      The last report sets the Run's outcome. needs-a-person needs
      --question, and ends the Execution at once; a person's answer
      continues the Run. --cause-report goes with done, in a Run that
      an Issue or a Monitor started: the cause and what proves it.
  solidtrace pr --title TEXT [--description TEXT]
      Inside a Run only: push the Run's branch, and open its PR to the
      base branch. Each later pr updates the title and description of
      the same PR (a flag that you do not give stays as it is). pr never
      merges.
  solidtrace review sessions [--branch B] [--base REF] [--session ID] [--days N] [--all]
      Print as JSON the Agent sessions on this machine that link to the PR
      of branch B (default: the current branch) since REF (default: the
      merge base with origin/HEAD, else with main), in the git repository
      of the current folder. It reads the main transcripts that changed in
      the last N days (default 14) with a cwd in the repository or in one
      of its worktrees, and the transcripts of their subagents. The links
      of a session are: the PR's commits whose subject is the subject of
      the message of one of its git commit or git merge commands (a strong
      link), its lines on branch B, and the PR's files that it edited. A
      session with a strong link or lines on branch B is in the
      repository, also when its worktree was removed. A transcript that
      cannot be read is skipped with a warning. --session ID is the
      current session: it is always in the list, and an error in its
      transcript stops the command. --all also lists the repository's
      sessions with no link.
  solidtrace review digest --session ID... | --file PATH... [--skills REGEX]
      Print the review timeline of one or more Claude Code transcripts as
      JSON, for an agent that records what a review found and what
      happened to each finding. Give --session or --file once for each
      session. --session finds <projects>/*/ID.jsonl in CLAUDE_CONFIG_DIR
      (else ~/.claude); --file reads PATH. The events of all sessions are
      in one list in time order, and each event names its session. Each
      session has its task (the first person message, or a first slash
      command that is not one such as /clear or /model), the person's
      messages and answers, the commits and the subagents' prompts, and
      from its first review on also: each review's start, the agent's
      texts, each solidtrace-findings block, the subagents' reports and
      the edits that the tool did. A session that starts after a review of
      another session keeps all its events. A review is a skill whose name
      matches --skills (by default a name whose last part is review or
      interrogate, or ends in -review), that a person or the agent starts,
      or a solidtrace-findings block. A block comes from a solidtrace
      review block or solidtrace review fixed command that did not fail
      (from the heredoc or here-string that review block reads, else from
      the output; a command that gives no block is a block with an
      error), and from the agent's text or a subagent's report. With
      --session, the digest also reads the session's subagent transcripts
      (<projects>/*/ID/subagents/*.jsonl), and takes from them only the
      blocks of those two commands. Each block of a subagent has its
      agent id as its writer (no writer is the main transcript). A block
      in a text or a report that is the same JSON as the block of a
      command in the same session is not in the digest again.
  solidtrace review check --digest PATH --record PATH
      Check a review record (open-pr's record.md, version 2) against the
      digest that it was made from: each outcome, event type, decision
      and found_by kind is known; each event has the source block or
      record and evidence that is in the digest; the story is filled in;
      the record has the digest's sessions, and reviews when the digest
      has runs; each finding of each solidtrace-findings block (update
      blocks too) is in the record with its block events; the findings
      of an update block are on the run that it names, of the same
      session and writer; each outcome agrees with its events; each
      finding that a commit's Review-Findings trailer names (the latest
      block of that run of the main transcript before the commit) has a
      fix event with no run that found it again between them;
      each confirmation's given is true or false; and round, when it is
      there, is 1 or more. Print the counts, the problems and the warnings
      (a block of the digest that cannot be read) as JSON. Exit 1 when
      there is a problem, 2 when a file cannot be read.
  solidtrace review record --digest PATH [--notes PATH] [--pr URL] [--round N]
      Print as JSON the review record (version 2) of a digest. Without
      --notes, it is the mechanical part: one run for each findings block
      that is not an update block and for each review start with no
      block, numbered in time order; each block finding with the id
      <run>.<block id> and its events; a refound finding on the earlier
      finding of its writer; the events of each update block on the
      finding of the run that it names, of the same writer; a fix event
      for a finding that a commit's Review-Findings trailer names (the
      latest block of that run before the commit), when no other fix
      event has the commit with no run that found it again between
      them; the confirmations; and each finding's outcome from its
      events. --notes adds what needs reading: the story, more events,
      the findings from outside the reviews (P, A and X) and of a review
      start with no block (T<run>-<n>), each confirmation's given, and
      low confidence. Then each outcome is worked out again. --pr sets
      pr (default null), and --round sets round (default 1). A block
      that the record cannot read or place is a warning on stderr. Exit
      1 when the notes name a finding, a confirmation or an evidence uuid
      that is not there, or are not of their shape; 2 when a file cannot
      be read.
  solidtrace review fixed [COMMIT]
      Print one solidtrace-findings update block for each run that the
      Review-Findings trailers of COMMIT (default HEAD) name, with a fix
      event for each finding: the commit's full hash and the files that
      it changes (all of them: git cannot say which file fixed which
      finding), in the form of review block. Exit 1 when the commit has
      no Review-Findings trailer.
  solidtrace review block
      Read one solidtrace-findings block (code-review's findings.md,
      version 2) as JSON on stdin, check it, and print it as one fenced
      solidtrace-findings block of compact JSON. A run block has run,
      skill, trigger, stage, step, base, head, reviewers, verifier,
      findings, confirmations and dropped (skipped is optional); an
      update block has update and findings with id and events only. A
      field that the format does not have is a problem. Exit 1 with each
      problem on stderr when the block is not valid.
  solidtrace review final FILE [--session ID]
      Check the review record of a PR in FILE (version 2, as review
      record writes it), and write it to
      <project folder>/<ID>/solidtrace/review-records/round-<n>-<h>.json,
      the folder of the Agent session in CLAUDE_CONFIG_DIR (else
      ~/.claude), with a rename. <h> is the first 12 hex characters of the SHA-256 of
      the record's pr. The Agent session is --session, else
      ` + ClaudeSessionEnv + `, which Claude Code sets. open-pr and update-pr
      end with this command. The capture uploads the file with the Agent
      session, and SolidTrace keeps the record from it. A round written
      again replaces its file. It prints the round, the counts of
      findings and fixed findings, and the path on one line. Exit 1 when
      the file is not a record of version 2, has a field that the record
      does not have, or is more than ` + recordLimit + `, or when the Agent session
      has no transcript.
  solidtrace evidence add FILE... [--caption TEXT] [--session ID]
      Upload each FILE as an Evidence file of the Agent session: a PNG,
      JPEG, WebP or GIF image, or an MP4 or WebM video, of at most ` + evidenceLimit + `,
      that proves a PR. The Agent session is --session, else
      ` + ClaudeSessionEnv + `, which Claude Code sets. --caption (at most
      ` + strconv.Itoa(agentapi.MaxEvidenceCaption) + ` characters) goes with each FILE. For each FILE it prints one
      line: the link to the file's page in Console, the type and the size.
      Members of the Organization open the link after they sign in. The
      same FILE again gives the same link. A FILE that fails prints its
      error, and the next FILE uploads. Exit 1 when a FILE failed.
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
			return captureStatus(ctx, args[2:], env)
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
	case "switch-account":
		return switchAccount(ctx, args[1:], env)
	case "message":
		return message(ctx, args[1:], env)
	case "take-over":
		return takeOver(ctx, args[1:], env)
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
	case "review":
		return reviewCommand(ctx, args[1:], env)
	case "evidence":
		return evidence(ctx, args[1:], env)
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
