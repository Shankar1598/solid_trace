package wrapper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The names of the wrapper's configuration in its environment. The Runner
// sets them (the Nomad job template, ticket 43). The harness never gets a
// name that starts with WRAPPER_.
const (
	// EnvDispatcherURL is the Dispatcher's private listener.
	EnvDispatcherURL = "WRAPPER_DISPATCHER_URL"
	// EnvExecution is the Execution's id, from the dispatch.
	EnvExecution = "WRAPPER_EXECUTION_ID"
	// EnvIdentityFile is the file that holds the workload identity JWT.
	// The JWT has an exp, so the Runner must renew it in this file (Nomad:
	// an identity with file = true and a ttl). The wrapper reads the file
	// on each call.
	EnvIdentityFile = "WRAPPER_IDENTITY_FILE"
	// EnvDir is the Execution's directory: it holds secrets/, the upload
	// state and the agent's socket (default: the working directory). When
	// it is not there, the wrapper makes it (0711) in its parent. The run
	// user must be able to search it and each parent, and must not be able
	// to change them (root owns them), or the wrapper stops. A Nomad task's
	// directory is not such a directory: Nomad's data_dir is 0700. So the
	// Nomad Runner names one under nomadrunner.DefaultExecutionsDir.
	EnvDir = "WRAPPER_DIR"
	// EnvHarness is the harness program (default claude).
	EnvHarness = "WRAPPER_HARNESS"
	// EnvDocker is the docker program of the end's container step
	// (default docker). When it is not there, the step is skipped.
	EnvDocker = "WRAPPER_DOCKER"
	// EnvWorkspace is the folder of the checkouts (default /workspace). A
	// repository's checkout is <workspace>/<host>/<workspace>/<name> on
	// each machine, so that a resume finds the same path. The end deletes
	// the folder.
	EnvWorkspace = "WRAPPER_WORKSPACE"
	// EnvHome is the run user's home (default /home/agent): HOME, the git
	// configuration and CLAUDE_CONFIG_DIR (<home>/.claude). The end
	// deletes it.
	EnvHome = "WRAPPER_HOME"
	// EnvCache is the caches folder (default /var/cache/factory), the
	// harness's XDG_CACHE_HOME. It stays after the end. The wrapper makes
	// the workspace, the home and the caches folder for the run user.
	EnvCache = "WRAPPER_CACHE"
	// EnvUploadEvery is how often the wrapper uploads the Run's Agent
	// sessions during the Execution, as a Go duration (default 5s).
	EnvUploadEvery = "WRAPPER_UPLOAD_EVERY"
	// EnvClockFile is for tests only: a file that holds the time (RFC
	// 3339). The wrapper's limits then read their clock from it, and the
	// wrapper checks them every 20 ms, not every second.
	EnvClockFile = "WRAPPER_CLOCK_FILE"
)

// limitEvery is how often the wrapper checks the Execution's limits.
const limitEvery = time.Second

// testLimitEvery is limitEvery with a clock file.
const testLimitEvery = 20 * time.Millisecond

// config is the wrapper's configuration.
type config struct {
	dispatcherURL, execution, identityFile, dir, harness, docker string
	workspace, home, cache                                       string
	// claudeConfig and projectName are the harness's CLAUDE_CONFIG_DIR
	// (default <home>/.claude) and CLAUDE_CODE_PROJECT_DIR_NAME (default
	// projectDirName of the task's repository, which Main sets): the
	// wrapper uploads the Agent sessions under
	// <claudeConfig>/projects/<projectName>.
	claudeConfig, projectName string
	uploadEvery               time.Duration
	// clock is the clock of the limits, and limitEvery how often the
	// wrapper checks them.
	clock      func() time.Time
	limitEvery time.Duration
}

// fileClock is a clock that reads the time from path. When the file
// cannot be read, it gives the last time that it read.
func fileClock(path string) (func() time.Time, error) {
	read := func() (time.Time, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	}
	last, err := read()
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		if t, err := read(); err == nil {
			last = t
		}
		return last
	}, nil
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		dispatcherURL: strings.TrimRight(getenv(EnvDispatcherURL), "/"),
		execution:     getenv(EnvExecution),
		identityFile:  getenv(EnvIdentityFile),
		dir:           getenv(EnvDir),
		harness:       getenv(EnvHarness),
		docker:        getenv(EnvDocker),
		workspace:     getenv(EnvWorkspace),
		home:          getenv(EnvHome),
		cache:         getenv(EnvCache),
		claudeConfig:  getenv("CLAUDE_CONFIG_DIR"),
		projectName:   getenv("CLAUDE_CODE_PROJECT_DIR_NAME"),
		uploadEvery:   uploadEvery,
		clock:         time.Now,
		limitEvery:    limitEvery,
	}
	for _, d := range []struct {
		value *string
		def   string
	}{{&c.workspace, "/workspace"}, {&c.home, "/home/agent"}, {&c.cache, "/var/cache/factory"}} {
		if *d.value == "" {
			*d.value = d.def
		}
		*d.value = filepath.Clean(*d.value)
	}
	var problems []string
	// The end deletes the workspace and the home: each must be a folder of
	// its own, never the root.
	for _, d := range []struct{ value, name string }{{c.workspace, EnvWorkspace}, {c.home, EnvHome}, {c.cache, EnvCache}} {
		if !filepath.IsAbs(d.value) || d.value == "/" {
			problems = append(problems, d.name+" must be an absolute path that is not /, not "+d.value)
		}
	}
	within := func(a, b string) bool { return a == b || strings.HasPrefix(a, b+"/") }
	if within(c.workspace, c.home) || within(c.home, c.workspace) || within(c.cache, c.home) ||
		within(c.cache, c.workspace) || within(c.home, c.cache) || within(c.workspace, c.cache) {
		problems = append(problems, EnvWorkspace+", "+EnvHome+" and "+EnvCache+" must be separate folders, none in another")
	}
	if path := getenv(EnvClockFile); path != "" {
		clock, err := fileClock(path)
		if err != nil {
			problems = append(problems, EnvClockFile+" must name a file with an RFC 3339 time: "+err.Error())
		}
		c.clock, c.limitEvery = clock, testLimitEvery
	}
	if c.dispatcherURL == "" {
		problems = append(problems, EnvDispatcherURL+" is not set")
	}
	if c.execution == "" {
		problems = append(problems, EnvExecution+" is not set")
	}
	if c.identityFile == "" {
		problems = append(problems, EnvIdentityFile+" is not set")
	}
	if every := getenv(EnvUploadEvery); every != "" {
		d, err := time.ParseDuration(every)
		if err != nil || d <= 0 {
			problems = append(problems, EnvUploadEvery+" must be a duration such as 5s, not "+every)
		}
		c.uploadEvery = d
	}
	if strings.ContainsAny(c.projectName, "/\x00") || c.projectName == "." || c.projectName == ".." {
		problems = append(problems, "CLAUDE_CODE_PROJECT_DIR_NAME must be one directory name, not "+c.projectName)
	}
	if len(problems) > 0 {
		return c, errors.New("the wrapper's configuration is not complete: " + strings.Join(problems, "; "))
	}
	if c.dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return c, err
		}
		c.dir = wd
	}
	if c.harness == "" {
		c.harness = "claude"
	}
	if c.docker == "" {
		c.docker = "docker"
	}
	if c.claudeConfig == "" {
		c.claudeConfig = filepath.Join(c.home, ".claude")
	}
	return c, nil
}

// makeDir makes the Execution's directory when it is not there: root's,
// and the run user can only search it.
func (c config) makeDir() error {
	err := os.Mkdir(c.dir, 0o711)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("make the Execution's directory (%s): %w", EnvDir, err)
	}
	return os.Chmod(c.dir, 0o711)
}

// checkout is the fixed checkout path of a repository (host/workspace/name).
func (c config) checkout(repository string) string { return filepath.Join(c.workspace, repository) }

// gitConfig is the run user's git configuration: the credential helper
// and the ssh rewrite. It is outside the checkout.
func (c config) gitConfig() string { return filepath.Join(c.home, ".gitconfig") }

func (c config) secrets() string { return filepath.Join(c.dir, "secrets") }
func (c config) socket() string  { return filepath.Join(c.dir, "agent.sock") }

// runState is the file that the wrapper and the Stop hook share, and
// harnessSettings the harness's --settings file, which installs the hook.
// Both are outside the checkout.
func (c config) runState() string        { return filepath.Join(c.dir, "run-state.json") }
func (c config) harnessSettings() string { return filepath.Join(c.dir, "harness-settings.json") }

// projectDir is the Claude Code project directory of the Run: its Agent
// sessions and its evidence/.
func (c config) projectDir() string { return filepath.Join(c.claudeConfig, "projects", c.projectName) }

// uploadState holds what the uploads saw of each file. The agent never
// writes it.
func (c config) uploadState() string { return filepath.Join(c.dir, "uploads") }
