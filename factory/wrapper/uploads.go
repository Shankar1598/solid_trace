package wrapper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"strings"
	"sync"
	"syscall"
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
	every      time.Duration
	log        io.Writer
	capturer   *capture.Capturer

	mu sync.Mutex
	// dir is projectDir, open from before the run user's first process.
	// It is nil until prepare: the preemption handler can start before.
	dir   *os.Root
	token dispatcherapi.SessionToken
	// refused are the Agent session ids that the Dispatcher did not add
	// to the Run: the wrapper does not ask for them again.
	refused map[string]bool

	stop     context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
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
	u.mu.Lock()
	u.dir = dir
	u.mu.Unlock()
	return nil
}

// adopt takes the open project directory of the Execution that a
// take-over follows on this machine, in place of prepare.
func (u *uploads) adopt(dir *os.Root) {
	u.mu.Lock()
	u.dir = dir
	u.mu.Unlock()
}

// root is the open project directory, or nil before prepare.
func (u *uploads) root() *os.Root {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.dir
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
	return u.askFor(ctx, dispatcherapi.TokenRequest{AgentSessionID: sessionID})
}

// askFor is the token call with req, and keeps the new token.
func (u *uploads) askFor(ctx context.Context, req dispatcherapi.TokenRequest) error {
	a, err := u.client.call(ctx, "token", req)
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

// stopLoop stops the uploads during the Execution, and waits for the
// pass that runs.
func (u *uploads) stopLoop() {
	u.stopOnce.Do(func() {
		if u.stop != nil {
			u.stop()
			<-u.done
		}
	})
}

// finish stops the uploads during the Execution and sends the last bytes
// of each file, within finalWait or until ctx ends.
func (u *uploads) finish(ctx context.Context) {
	u.stopLoop()
	ctx, cancel := context.WithTimeout(ctx, finalWait)
	defer cancel()
	u.pass(ctx, true)
}

// close closes the project directory. It comes after the last upload of
// the usual end and of the preemption handler: the handler's last upload
// comes after finish.
func (u *uploads) close() {
	if dir := u.root(); dir != nil {
		dir.Close()
	}
}

// last is the preemption handler's last upload: it stops the uploads
// during the Execution, sends the last bytes of each file, and gives how
// many bytes of the current Agent session's transcript SolidTrace has.
// It fails when SolidTrace has less of the transcript than the disk, or
// does not have an evidence/ file as it is on disk.
func (u *uploads) last(ctx context.Context) (*uint64, error) {
	u.stopLoop()
	u.pass(ctx, true)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u.mu.Lock()
	session := u.token.SessionID
	u.mu.Unlock()
	if session == "" {
		session = u.task.SessionID
	}
	answer, err := u.capturer.API.AgentSession(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("ask SolidTrace what it has of the transcript: %w", err)
	}
	var offset uint64
	hashes := map[string]string{}
	for _, f := range answer.Files {
		if f.Path == session+".jsonl" {
			offset = f.EndOffset
		}
		hashes[f.Path] = f.ContentHash
	}
	info, err := os.Stat(filepath.Join(u.projectDir, session+".jsonl"))
	if err == nil && uint64(info.Size()) > offset {
		return nil, fmt.Errorf("SolidTrace has %d of the transcript's %d bytes", offset, info.Size())
	}
	missing, err := u.missingEvidence(ctx, hashes)
	if err != nil {
		return nil, fmt.Errorf("read the evidence/ files: %w", err)
	}
	if len(missing) > 0 {
		return &offset, fmt.Errorf("SolidTrace does not have these evidence/ files as they are on disk: %s",
			strings.Join(missing, ", "))
	}
	return &offset, nil
}

// missingEvidence are the evidence/ files on disk whose content SolidTrace
// does not have (hashes: each path's content hash at SolidTrace). As the
// upload does, it skips each file that is empty or is not regular.
func (u *uploads) missingEvidence(ctx context.Context, hashes map[string]string) ([]string, error) {
	var missing []string
	err := filepath.WalkDir(filepath.Join(u.projectDir, evidenceDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(u.projectDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hash, err := fileHash(ctx, p)
		if err != nil {
			return err
		}
		if hash != "" && hashes[rel] != hash {
			missing = append(missing, rel)
		}
		return nil
	})
	return missing, err
}

// fileHash is the content hash of the file at p, as capture gives it
// ("" for an empty file).
func fileHash(ctx context.Context, p string) (string, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, ctxReader{ctx, f})
	if err != nil || n == 0 {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// ctxReader stops a read when its context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// pass is one upload of each of the Run's Agent sessions on disk. An
// Agent session id that the Run does not have (a /clear) is added first.
// final sends each file that is not .jsonl at once. Before prepare there
// is no project directory, so there is nothing to send.
func (u *uploads) pass(ctx context.Context, final bool) {
	dir := u.root()
	if dir == nil {
		return
	}
	entries, err := fs.ReadDir(dir.FS(), ".")
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
		r := capture.RunUpload{SessionID: id, ProjectDir: u.projectDir, Dir: dir, RunID: u.task.RunID, Project: u.task.Project,
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
