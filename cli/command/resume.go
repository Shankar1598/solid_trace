package command

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
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
	"unicode"
	"unicode/utf8"

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

// resumeIDShape is an Agent session id: a UUID in lower case, as Claude
// Code names its transcripts.
var resumeIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// resumeFacts is what resume finds before it downloads: the sign-in, the
// server's answer and this machine's checkout. refusal makes the choices
// from it.
type resumeFacts struct {
	id           string
	organization string
	// canDownload is true when the sign-in has sessions:download.
	canDownload bool
	// session is nil when SolidTrace does not have the Agent session, and
	// when the sign-in cannot ask (canDownload is false).
	session *agentapi.AgentSession
	// serverHas is false when SolidTrace has no transcript.
	serverHas bool
	// dir is the current folder.
	dir string
	// root is the root of the checkout of dir, "" when dir is not in a git
	// repository.
	root string
	// remotes are the repositories of the checkout, as capture finds them.
	remotes []string
	// target is the folder of the Agent session in the checkout.
	target string
}

// A resumeRefusal is one reason that resume does not download, with the
// message that tells the person what to do. refusal uses the first whose
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
}

// refusal is the message of the first refusal, "" when resume downloads.
func refusal(f resumeFacts) string {
	for _, r := range resumeRefusals {
		if r.when(f) {
			return r.message(f)
		}
	}
	return ""
}

// resume continues an Agent session of this machine in the folder where it
// started, with the same id. Else it downloads the Agent session into a
// temporary folder and starts a fork of it: a new Agent session with the
// history. So a download never goes into a Claude Code project folder,
// and capture never sends another person's or a Run's transcript.
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
	printOnly := flags.Bool("print", false, "print the command of Claude Code and its folder, and do not start it")
	positional, ok := parseInterspersed(flags, args)
	if !ok {
		return 2
	}
	if len(positional) != 1 || !resumeIDShape.MatchString(positional[0]) {
		return fail(2, "give one SESSION_ID, the Agent session's UUID in lower case")
	}
	id := positional[0]
	if env.ClaudeProjectsDir == "" {
		return fail(1, "no Claude Code projects folder: set CLAUDE_CONFIG_DIR")
	}
	start := func(folder string, claude []string) int {
		claude = append(claude, claudeArgs...)
		if *printOnly {
			words := []string{"cd", shellQuote(folder), "&&", "claude"}
			for _, arg := range claude {
				words = append(words, shellQuote(arg))
			}
			fmt.Fprintln(env.Stdout, strings.Join(words, " "))
			return 0
		}
		if env.StartClaude == nil {
			return fail(1, "this machine cannot start Claude Code")
		}
		if err := env.StartClaude(folder, claude); err != nil {
			return fail(1, "start Claude Code: %v", err)
		}
		return 0
	}

	folder, err := localStartFolder(env, id)
	if err != nil {
		return fail(1, "%v", err)
	}
	if folder != "" {
		return start(folder, []string{"--resume", id})
	}

	source := env.Source()
	creds, err := source.Credentials()
	if err != nil {
		return fail(1, "%v", err)
	}
	client := &agentapi.Client{IngestURL: creds.IngestURL, HTTP: env.HTTP, Tokens: source, Version: env.Version,
		Warnings: env.Stderr}
	f, err := resumeFactsOf(ctx, env, client, creds, id)
	if err != nil {
		return fail(1, "%v", err)
	}
	if message := refusal(f); message != "" {
		return fail(1, "%s", message)
	}
	transcript, err := downloadTranscript(ctx, env, client, id)
	if err != nil {
		return fail(1, "%v", err)
	}
	tellBranch(ctx, env, f.root, lastBranch(transcript))
	if info, err := os.Stat(f.target); err != nil || !info.IsDir() {
		os.RemoveAll(filepath.Dir(transcript))
		return fail(1, "the folder %s of the Agent session is not in this checkout. Switch to its branch, and run "+
			"solidtrace resume again.", f.target)
	}
	fork := newSessionID()
	// The fork keeps the cwd of the history, which can be a folder of
	// another machine. The record tells capture's sweep, and the next
	// resume, that the fork starts in the target folder.
	if err := capture.SetStartDir(env.ConfigDir, fork, f.target); err != nil {
		fmt.Fprintf(env.Stderr, "solidtrace resume: keep the folder of the Agent session for capture: %v\n", err)
	}
	fmt.Fprintf(env.Stderr, "solidtrace resume: the new Agent session %s starts from the history of the Agent "+
		"session %s.\n", fork, id)
	return start(f.target, []string{"--resume", transcript, "--fork-session", "--session-id", fork})
}

// localStartFolder is the folder where the Agent session started, when
// this machine has its transcript in a Claude Code project folder and that
// folder is on this machine. Else it is "".
func localStartFolder(env Env, id string) (string, error) {
	sessions, err := capture.Sessions(env.ClaudeProjectsDir)
	if err != nil {
		return "", fmt.Errorf("read the Claude Code projects folder: %w", err)
	}
	// The cwd of a fork's history can be a folder of another machine, and
	// the record has the folder that resume started the fork in.
	record, _ := capture.RecordOf(env.ConfigDir, id)
	for _, s := range sessions {
		if s.ID != id {
			continue
		}
		folder := capture.StartDir(s.TranscriptPath, record.StartDir)
		if info, err := os.Stat(folder); err == nil && info.IsDir() {
			return folder, nil
		}
	}
	return "", nil
}

// newSessionID is a random UUID (version 4) in lower case, the id of a
// fork.
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// resumeFactsOf finds the facts of resume. It asks the server only when
// the sign-in has sessions:download.
func resumeFactsOf(ctx context.Context, env Env, client *agentapi.Client, creds credentials.Credentials,
	id string) (resumeFacts, error) {
	f := resumeFacts{id: id, dir: env.Dir,
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
	f.serverHas = slices.ContainsFunc(answer.Files, func(file agentapi.AgentSessionFile) bool {
		return file.Path == id+".jsonl"
	})
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
	return f, nil
}

// downloadTranscript downloads the Agent session into a new temporary
// folder, out of the Claude Code projects folder, and gives the path of
// its transcript there. The folder stays after a download: Claude Code
// reads the transcript when this process is gone, and the OS deletes the
// folder later.
func downloadTranscript(ctx context.Context, env Env, client *agentapi.Client, id string) (path string, err error) {
	folder, err := os.MkdirTemp("", "solidtrace-resume-*")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(folder)
		}
	}()
	root, err := os.OpenRoot(folder)
	if err != nil {
		return "", err
	}
	defer root.Close()

	download, err := downloadWhenReady(ctx, env, client, id)
	if err != nil {
		return "", err
	}
	extracted, err := agentapi.Extract(download.Archive, root, nil)
	download.Archive.Close()
	if err != nil {
		return "", err
	}
	transcript := id + ".jsonl"
	// An archive can have a name more than once: the last copy is the one
	// in the folder, as Extract wrote it.
	found, flags := false, ""
	for _, file := range extracted {
		if file.Name == transcript {
			found, flags = true, file.Flags
		}
	}
	switch {
	case !found:
		return "", fmt.Errorf("the download of the Agent session %s has no transcript", id)
	case flags != "":
		return "", fmt.Errorf("the transcript of the Agent session is %s in SolidTrace, so a resume is not "+
			"promised. Start a new Agent session.", flags)
	}
	return filepath.Join(folder, transcript), nil
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
	if !validBranch(ctx, root, branch) {
		return
	}
	current, _ := gitOutput(ctx, root, "branch", "--show-current")
	if branch == current {
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
			"its branch, run this command:\n  git switch %s\n", branch, cmp.Or(current, "no branch"), shellQuote(branch))
		return
	}
	fmt.Fprintf(env.Stderr, "The Agent session worked on the branch %s. This checkout does not have that branch. "+
		"To get it, run these commands:\n  git fetch\n  git switch %s\nSolidTrace does not have work that was not "+
		"pushed.\n", branch, shellQuote(branch))
}

// validBranch tells if branch is a branch name by git's rules that is safe
// to print. Another member wrote the transcript, so the name can hold
// terminal escapes or be an option of git switch.
func validBranch(ctx context.Context, root, branch string) bool {
	if branch == "" || branch == "HEAD" || strings.HasPrefix(branch, "-") || !utf8.ValidString(branch) {
		return false
	}
	for _, r := range branch {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	// git expands a name such as @{-1}, so the name must come back unchanged.
	out, err := gitOutput(ctx, root, "check-ref-format", "--branch", branch)
	return err == nil && out == branch
}

// shellQuote is s as one word of a POSIX shell.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
