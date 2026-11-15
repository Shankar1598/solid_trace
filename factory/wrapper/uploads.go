package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/cli/version"
)

// uploadEvery is how often the wrapper uploads the Run's Agent sessions
// during the Execution, when EnvUploadEvery does not say.
const uploadEvery = 5 * time.Second

// finalWait is the longest time of the last upload, after the harness
// exits and before the end call.
const finalWait = 2 * time.Minute

// evidenceDir is the folder of the project directory whose files upload
// with the Run's current Agent session (the Run page shows them).
const evidenceDir = "evidence"

// sessionFile is the name of an Agent session's transcript in the project
// directory.
var sessionFile = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)

// uploads sends the Run's Agent sessions to SolidTrace's agent ingest API
// with the Execution's child token, through the proxy of the wrapper's
// environment (HTTPS_PROXY, HTTP_PROXY, NO_PROXY). Only the wrapper holds
// the token: it is in memory, never in a file or in the harness's
// environment.
type uploads struct {
	client     *client
	task       dispatcherapi.Task
	projectDir string
	// dir is projectDir, open from before the run user's first process.
	dir      *os.Root
	every    time.Duration
	log      io.Writer
	capturer *capture.Capturer

	mu    sync.Mutex
	token dispatcherapi.SessionToken
	// refused are the Agent session ids that the Dispatcher did not add
	// to the Run: the wrapper does not ask for them again.
	refused map[string]bool

	stop context.CancelFunc
	done chan struct{}
}

func newUploads(cfg config, c *client, task dispatcherapi.Task, log io.Writer) *uploads {
	u := &uploads{client: c, task: task, projectDir: cfg.projectDir(), every: cfg.uploadEvery, log: log,
		token: task.Uploads, refused: map[string]bool{}}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	u.capturer = &capture.Capturer{
		API: &agentapi.Client{IngestURL: task.IngestURL, HTTP: &http.Client{Timeout: 30 * time.Second, Transport: transport},
			Tokens: u, Version: version.Version, Warnings: log},
		Dir: cfg.uploadState(),
		Now: time.Now,
		Log: func(format string, args ...any) { fmt.Fprintf(log, "wrapper: upload: "+format+"\n", args...) },
	}
	return u
}

// prepare makes the project directory and its evidence/ folder for the
// run user, and opens the project directory. It runs before the run
// user's first process: each upload reads through the open directory, so
// a link that the agent puts in place of the directory (or of a parent)
// does not give the wrapper (root) a file of another directory.
func (u *uploads) prepare(r runUser) error {
	if err := r.mkdirAll(filepath.Join(u.projectDir, evidenceDir)); err != nil {
		return err
	}
	dir, err := os.OpenRoot(u.projectDir)
	if err != nil {
		return err
	}
	u.dir = dir
	return nil
}

// Token is the Execution's child token (agentapi.Tokens).
func (u *uploads) Token(context.Context) (string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.token.Token == "" {
		return "", errors.New("the Dispatcher gave the Execution no token")
	}
	return u.token.Token, nil
}

// Refreshed asks the Dispatcher for a new token after SolidTrace refused
// rejected (agentapi.Tokens).
func (u *uploads) Refreshed(ctx context.Context, rejected string) (string, error) {
	u.mu.Lock()
	if u.token.Token != rejected {
		defer u.mu.Unlock()
		return u.token.Token, nil
	}
	u.mu.Unlock()
	if err := u.ask(ctx, ""); err != nil {
		return "", err
	}
	return u.Token(ctx)
}

// errRefused is the Dispatcher's refusal of a new Agent session id.
var errRefused = errors.New("the Dispatcher refused the Agent session")

// ask is the token call: it adds sessionID ("" adds none) to the Run, and
// keeps the new token.
func (u *uploads) ask(ctx context.Context, sessionID string) error {
	a, err := u.client.call(ctx, "token", dispatcherapi.TokenRequest{AgentSessionID: sessionID})
	if err != nil {
		return err
	}
	if a.status == http.StatusConflict || a.status == http.StatusBadRequest {
		return fmt.Errorf("%w: %s", errRefused, a.message)
	}
	if err := a.err("token"); err != nil {
		return err
	}
	var tok dispatcherapi.SessionToken
	if err := json.Unmarshal(a.body, &tok); err != nil || tok.Token == "" {
		return fmt.Errorf("the Dispatcher's token answer has no token: %v", err)
	}
	u.mu.Lock()
	u.token = tok
	u.mu.Unlock()
	return nil
}

// start uploads every u.every until finish.
func (u *uploads) start() {
	ctx, stop := context.WithCancel(context.Background())
	u.stop, u.done = stop, make(chan struct{})
	go func() {
		defer close(u.done)
		tick := time.NewTicker(u.every)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				u.pass(ctx, false)
			}
		}
	}()
}

// finish stops the uploads during the Execution and sends the last bytes
// of each file.
func (u *uploads) finish() {
	if u.stop != nil {
		u.stop()
		<-u.done
	}
	ctx, cancel := context.WithTimeout(context.Background(), finalWait)
	defer cancel()
	u.pass(ctx, true)
	u.dir.Close()
}

// pass is one upload of each of the Run's Agent sessions on disk. An
// Agent session id that the Run does not have (a /clear) is added first.
// final sends each file that is not .jsonl at once.
func (u *uploads) pass(ctx context.Context, final bool) {
	entries, err := fs.ReadDir(u.dir.FS(), ".")
	if err != nil {
		fmt.Fprintln(u.log, "wrapper: upload: read the project directory:", err)
		return
	}
	var onDisk []string
	for _, entry := range entries {
		m := sessionFile.FindStringSubmatch(entry.Name())
		if m == nil || !entry.Type().IsRegular() {
			continue
		}
		onDisk = append(onDisk, m[1])
		u.mu.Lock()
		known, refused := slices.Contains(u.token.AgentSessionIDs, m[1]), u.refused[m[1]]
		u.mu.Unlock()
		if known || refused {
			continue
		}
		if err := u.ask(ctx, m[1]); err != nil {
			fmt.Fprintf(u.log, "wrapper: upload: add the Agent session %s to the Run: %v\n", m[1], err)
			if errors.Is(err, errRefused) {
				u.mu.Lock()
				u.refused[m[1]] = true
				u.mu.Unlock()
			}
		}
	}
	u.mu.Lock()
	tok := u.token
	u.mu.Unlock()
	for _, id := range onDisk {
		if !slices.Contains(tok.AgentSessionIDs, id) || ctx.Err() != nil {
			continue
		}
		r := capture.RunUpload{SessionID: id, ProjectDir: u.projectDir, Dir: u.dir, RunID: u.task.RunID, Project: u.task.Project,
			Repository: capture.Repository{Name: u.task.Repository, Folder: u.task.Folder}, Final: final}
		if id == tok.SessionID {
			r.Extra = []string{evidenceDir}
		}
		if err := u.capturer.UploadRun(ctx, r); err != nil && ctx.Err() == nil {
			fmt.Fprintf(u.log, "wrapper: upload: %s: %v\n", id, err)
		}
	}
}

// claudeEnv is what the harness's environment gets for the uploads: the
// fixed Claude Code config and project directory, and the evidence/
// folder.
func (u *uploads) claudeEnv(cfg config) []string {
	return []string{"CLAUDE_CONFIG_DIR=" + cfg.claudeConfig, "CLAUDE_CODE_PROJECT_DIR_NAME=" + cfg.projectName,
		agentsocket.EvidenceEnv + "=" + filepath.Join(u.projectDir, evidenceDir)}
}
