package command

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/credentials"
	"github.com/solidtrace/cli/signin"
)

// resumeWait is the longest time that resume waits for SolidTrace to have
// the last uploads ready (its 503 with Retry-After), as the wrapper's
// restore does.
const resumeWait = 2 * time.Minute

// resumeTimeout is the longest time of one download: the server's own
// limit of one download.
const resumeTimeout = 10 * time.Minute

// resumeStaging is the start of the name of the temporary folder beside the
// project folder, into which resume downloads. A folder of this name that
// a crash left is deleted at the next resume of the same Agent session.
const resumeStaging = ".solidtrace-resume-"

// resumeIDShape is an Agent session id: a UUID in lower case, as Claude
// Code names its transcripts.
var resumeIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// resumeFacts is what resume finds before it downloads: the sign-in, the
// server's answer and this machine's checkout and transcript. decide makes
// all the choices from it.
type resumeFacts struct {
	id           string
	organization string
	// canDownload is true when the sign-in has sessions:download.
	canDownload bool
	// session is nil when SolidTrace does not have the Agent session, and
	// when the sign-in cannot ask (canDownload is false).
	session *agentapi.AgentSession
	// serverEnd is the end offset of the transcript in SolidTrace, and
	// serverHas is false when SolidTrace has no transcript.
	serverEnd uint64
	serverHas bool
	// dir is the current folder.
	dir string
	// root is the root of the checkout of dir, "" when dir is not in a git
	// repository.
	root string
	// remotes are the repositories of the checkout, as capture finds them.
	remotes []string
	// target is the folder of the Agent session in the checkout, and
	// project is its Claude Code project folder.
	target  string
	project string
	// elsewhere is the folder of a copy of the transcript on this machine
	// in another project folder, "" when there is none.
	elsewhere string
	// localSize is the size of the transcript on this machine, and
	// localHas is false when this machine has none.
	localSize uint64
	localHas  bool
	// fork is --fork.
	fork bool
}

// resumePlan is what decide chose.
type resumePlan struct {
	download bool
	fork     bool
}

// A resumeRefusal is one reason that resume does not continue, with the
// message that tells the person what to do. decide uses the first whose
// when is true.
type resumeRefusal struct {
	when    func(f resumeFacts) bool
	message func(f resumeFacts) string
}

var resumeRefusals = []resumeRefusal{
	{
		when: func(f resumeFacts) bool { return !f.canDownload },
		message: func(resumeFacts) string {
			return "Run solidtrace login again: this sign-in cannot download Agent sessions."
		},
	},
	{
		when: func(f resumeFacts) bool { return f.session == nil },
		message: func(f resumeFacts) string {
			return fmt.Sprintf("SolidTrace has no Agent session %s in %s.", f.id, f.organization)
		},
	},
	{
		when: func(f resumeFacts) bool { return f.root == "" },
		message: func(f resumeFacts) string {
			return fmt.Sprintf("%s is not in a git checkout. Run solidtrace resume in a checkout of %s.",
				f.dir, f.session.Repository)
		},
	},
	{
		when: func(f resumeFacts) bool {
			return !slices.ContainsFunc(f.remotes, func(r string) bool { return capture.Matches(f.session.Repository, r) })
		},
		message: func(f resumeFacts) string {
			return fmt.Sprintf("The Agent session is in %s, and this checkout is not. Clone %s, and run solidtrace "+
				"resume in that checkout.", f.session.Repository, f.session.Repository)
		},
	},
	{
		when: func(f resumeFacts) bool { return f.session.Folder != "" && !filepath.IsLocal(f.session.Folder) },
		message: func(f resumeFacts) string {
			return fmt.Sprintf("The folder %q of the Agent session is not a folder in its repository.", f.session.Folder)
		},
	},
	{
		when: func(f resumeFacts) bool { return !f.serverHas },
		message: func(f resumeFacts) string {
			return fmt.Sprintf("SolidTrace has no transcript of the Agent session %s.", f.id)
		},
	},
	{
		// A second copy makes capture send the shorter copy again as a new
		// version, and the transcript in SolidTrace goes back.
		when: func(f resumeFacts) bool { return f.elsewhere != "" },
		message: func(f resumeFacts) string {
			command := "claude --resume " + f.id
			if f.forks() {
				command += " --fork-session"
			}
			return fmt.Sprintf("This machine has the Agent session in %s. Run this command in that folder:\n  %s",
				f.elsewhere, command)
		},
	},
}

// forks is true when Claude Code continues the Agent session as a fork:
// with --fork, and for an Agent session that is not yours. When an older
// server does not say whose it is, a fork is the safe choice.
func (f resumeFacts) forks() bool {
	return f.fork || f.session.Yours == nil || !*f.session.Yours
}

// decide gives the plan of resume, or the message of the first refusal. A
// transcript on this machine with at least the server's end offset is not
// downloaded again. Your own Agent session continues with the same id;
// another person's, a Run's, or one with --fork continues as a fork.
func decide(f resumeFacts) (resumePlan, string) {
	for _, r := range resumeRefusals {
		if r.when(f) {
			return resumePlan{}, r.message(f)
		}
	}
	return resumePlan{
		download: !f.localHas || f.localSize < f.serverEnd,
		fork:     f.forks(),
	}, ""
}

func resume(ctx context.Context, args []string, env Env) int {
	fail := func(code int, format string, a ...any) int {
		fmt.Fprintf(env.Stderr, "solidtrace resume: "+format+"\n", a...)
		return code
	}
	var claudeArgs []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, claudeArgs = args[:i], args[i+1:]
	}
	flags := flag.NewFlagSet("resume", flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	fork := flags.Bool("fork", false, "continue as a new Agent session, also when it is yours")
	printOnly := flags.Bool("print", false, "download, print the command of Claude Code and its folder, and do not start it")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) != 1 || !resumeIDShape.MatchString(positional[0]) {
		return fail(2, "give one SESSION_ID, the Agent session's UUID in lower case")
	}
	if env.ClaudeProjectsDir == "" {
		return fail(1, "no Claude Code projects folder: set CLAUDE_CONFIG_DIR")
	}

	source := env.Source()
	creds, err := source.Credentials()
	if err != nil {
		return fail(1, "%v", err)
	}
	client := &agentapi.Client{IngestURL: creds.IngestURL, HTTP: env.HTTP, Tokens: source, Version: env.Version,
		Warnings: env.Stderr}
	f, err := resumeFactsOf(ctx, env, client, creds, positional[0], *fork)
	if err != nil {
		return fail(1, "%v", err)
	}
	plan, refusal := decide(f)
	if refusal != "" {
		return fail(1, "%s", refusal)
	}

	if plan.download {
		if err := downloadSession(ctx, env, client, f.id, f.project); err != nil {
			return fail(1, "%v", err)
		}
	}
	tellBranch(ctx, env, f.root, lastBranch(filepath.Join(f.project, f.id+".jsonl")))
	if info, err := os.Stat(f.target); err != nil || !info.IsDir() {
		return fail(1, "the folder %s of the Agent session is not in this checkout. Switch to its branch, and run "+
			"solidtrace resume again.", f.target)
	}

	claude := []string{"--resume", f.id}
	if plan.fork {
		claude = append(claude, "--fork-session")
	}
	claude = append(claude, claudeArgs...)
	if *printOnly {
		words := []string{"cd", shellQuote(f.target), "&&", "claude"}
		for _, arg := range claude {
			words = append(words, shellQuote(arg))
		}
		fmt.Fprintln(env.Stdout, strings.Join(words, " "))
		return 0
	}
	if env.StartClaude == nil {
		return fail(1, "this machine cannot start Claude Code")
	}
	if err := env.StartClaude(f.target, claude); err != nil {
		return fail(1, "start Claude Code: %v", err)
	}
	return 0
}

// resumeFactsOf finds the facts of resume. It asks the server only when
// the sign-in has sessions:download.
func resumeFactsOf(ctx context.Context, env Env, client *agentapi.Client, creds credentials.Credentials, id string,
	fork bool) (resumeFacts, error) {
	f := resumeFacts{id: id, fork: fork, dir: env.Dir,
		canDownload: slices.Contains(strings.Fields(creds.Scope), signin.ScopeSessionsDownload)}
	if creds.Organization != nil {
		f.organization = creds.Organization.Name
	}
	if f.dir == "" {
		var err error
		if f.dir, err = os.Getwd(); err != nil {
			return f, err
		}
	}
	if !f.canDownload {
		return f, nil
	}
	answer, err := client.AgentSession(ctx, id)
	var refused *agentapi.Error
	if errors.As(err, &refused) && refused.Status == http.StatusForbidden {
		return f, refusedDownload(refused)
	}
	if err != nil {
		return f, fmt.Errorf("ask SolidTrace for the Agent session: %w", err)
	}
	f.session = answer.Session
	for _, file := range answer.Files {
		if file.Path == id+".jsonl" {
			f.serverEnd, f.serverHas = file.EndOffset, true
		}
	}
	if f.session == nil {
		return f, nil
	}
	remotes, _, inRepo, err := capture.Remotes(ctx, f.dir)
	if err != nil {
		return f, fmt.Errorf("read the remotes of the checkout: %w", err)
	}
	if !inRepo {
		return f, nil
	}
	if f.root, err = gitOutput(ctx, f.dir, "rev-parse", "--show-toplevel"); err != nil {
		return f, err
	}
	f.remotes = remotes
	f.target = filepath.Join(f.root, filepath.FromSlash(f.session.Folder))
	f.project = filepath.Join(env.ClaudeProjectsDir, capture.ClaudeProjectFolder(f.target))
	if info, err := os.Stat(filepath.Join(f.project, id+".jsonl")); err == nil && info.Mode().IsRegular() {
		f.localSize, f.localHas = uint64(info.Size()), true
	}
	sessions, err := capture.Sessions(env.ClaudeProjectsDir)
	if err != nil {
		return f, fmt.Errorf("read the Claude Code projects folder: %w", err)
	}
	for _, s := range sessions {
		project := filepath.Dir(s.TranscriptPath)
		if s.ID == id && project != f.project && !strings.HasPrefix(filepath.Base(project), resumeStaging) {
			f.elsewhere = cmp.Or(capture.StartDir(s.TranscriptPath, ""), project)
		}
	}
	return f, nil
}

// downloadSession downloads the Agent session into a temporary folder beside
// the project folder, then renames each file into the project folder, the
// transcript last. So a crash leaves the project folder with a whole
// transcript or with the earlier one, and the next resume downloads again.
func downloadSession(ctx context.Context, env Env, client *agentapi.Client, id, project string) error {
	staging := filepath.Join(filepath.Dir(project), resumeStaging+id)
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("delete the folder that an earlier resume left: %w", err)
	}
	// The files go one folder down, so that capture's sweep, which reads
	// <projects>/*/<id>.jsonl, does not find them.
	files := filepath.Join(staging, "files")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	root, err := os.OpenRoot(files)
	if err != nil {
		return err
	}
	defer root.Close()

	download, err := downloadWhenReady(ctx, env, client, id)
	if err != nil {
		return err
	}
	extracted, err := agentapi.Extract(download.Archive, root, nil)
	download.Archive.Close()
	if err != nil {
		return err
	}
	transcript := id + ".jsonl"
	var names []string
	for _, file := range extracted {
		switch {
		case file.Name == transcript && file.Flags != "":
			return fmt.Errorf("the transcript of the Agent session is %s in SolidTrace, so a resume is not "+
				"promised. Start a new Agent session.", file.Flags)
		case file.Flags != "":
			fmt.Fprintf(env.Stderr, "solidtrace resume: %s is %s in SolidTrace. Resume does not download it.\n",
				file.Name, file.Flags)
		case file.Name != transcript:
			names = append(names, file.Name)
		}
	}
	if !slices.ContainsFunc(extracted, func(file agentapi.ExtractedFile) bool { return file.Name == transcript }) {
		return fmt.Errorf("the download of the Agent session %s has no transcript", id)
	}
	for _, name := range append(names, transcript) {
		to := filepath.Join(project, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(files, filepath.FromSlash(name)), to); err != nil {
			return err
		}
	}
	return nil
}

// downloadWhenReady asks for the download, and waits while the server
// answers 503 (the last uploads are not ready yet), for at most resumeWait.
func downloadWhenReady(ctx context.Context, env Env, client *agentapi.Client, id string) (agentapi.Download, error) {
	ctx, cancel := context.WithTimeout(ctx, resumeTimeout)
	hc := &http.Client{Transport: env.HTTP.Transport}
	deadline := env.Now().Add(resumeWait)
	for {
		download, err := client.DownloadAgentSession(ctx, hc, id)
		var answer *agentapi.Error
		switch {
		case errors.As(err, &answer) && answer.Status == http.StatusServiceUnavailable && !env.Now().Before(deadline):
			cancel()
			return download, fmt.Errorf("SolidTrace did not make the last uploads ready in %d minutes. Run "+
				"solidtrace resume again later.", int(resumeWait.Minutes()))
		case errors.As(err, &answer) && answer.Status == http.StatusServiceUnavailable:
			if err := env.Sleep(ctx, time.Duration(max(answer.RetryAfter, 1))*time.Second); err != nil {
				cancel()
				return download, err
			}
			continue
		case errors.As(err, &answer) && answer.Status == http.StatusForbidden:
			cancel()
			return download, refusedDownload(answer)
		case err != nil:
			cancel()
			return download, fmt.Errorf("download the Agent session: %w", err)
		}
		download.Archive = cancelOnClose{download.Archive, cancel}
		return download, nil
	}
}

// refusedDownload is the error of a 403 from /files or /download. An
// older server refuses another person's Agent session on both.
func refusedDownload(answer *agentapi.Error) error {
	return fmt.Errorf("SolidTrace refused the download (%s). An older server refuses downloads. "+
		"Ask its operator to upgrade it.", answer.Message)
}

// cancelOnClose ends the download's context when its archive is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}

// lastBranch is the last git branch that the transcript names, "" when it
// names none.
func lastBranch(transcript string) string {
	file, err := os.Open(transcript)
	if err != nil {
		return ""
	}
	defer file.Close()
	var branch string
	r := bufio.NewReaderSize(file, 1<<20)
	key := []byte(`"gitBranch"`)
	for {
		line, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A long line (a tool result) is read to its end, and not used.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = r.ReadSlice('\n')
			}
			line = nil
		}
		if bytes.Contains(line, key) {
			var entry struct {
				GitBranch string `json:"gitBranch"`
			}
			if json.Unmarshal(line, &entry) == nil && entry.GitBranch != "" {
				branch = entry.GitBranch
			}
		}
		if err != nil {
			return branch
		}
	}
}

// tellBranch tells the person how to get the Agent session's branch when
// the checkout is on another branch. It does not change the checkout.
func tellBranch(ctx context.Context, env Env, root, branch string) {
	current, _ := gitOutput(ctx, root, "branch", "--show-current")
	if branch == "" || branch == "HEAD" || branch == current {
		return
	}
	refs, _ := gitOutput(ctx, root, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	remotes, _ := gitOutput(ctx, root, "remote")
	has := false
	for _, ref := range strings.Split(refs, "\n") {
		if ref == "refs/heads/"+branch {
			has = true
		}
		for _, remote := range strings.Fields(remotes) {
			if ref == "refs/remotes/"+remote+"/"+branch {
				has = true
			}
		}
	}
	if has {
		fmt.Fprintf(env.Stderr, "The Agent session worked on the branch %s. This checkout is on %s. To work on "+
			"its branch, run this command:\n  git switch %s\n", branch, cmp.Or(current, "no branch"), branch)
		return
	}
	fmt.Fprintf(env.Stderr, "The Agent session worked on the branch %s. This checkout does not have that branch. "+
		"To get it, run these commands:\n  git fetch\n  git switch %s\nSolidTrace does not have work that was not "+
		"pushed.\n", branch, branch)
}

// shellQuote is s as one word of a POSIX shell.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
