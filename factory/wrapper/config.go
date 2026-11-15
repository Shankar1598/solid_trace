package wrapper

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// EnvDir is the Execution's directory: it holds secrets/
	// and the agent's socket (default: the working directory).
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
	// harness's XDG_CACHE_HOME. It stays after the end.
	EnvCache = "WRAPPER_CACHE"
)

// config is the wrapper's configuration.
type config struct {
	dispatcherURL, execution, identityFile, dir, harness, docker string
	workspace, home, cache                                       string
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
	if c.dispatcherURL == "" {
		problems = append(problems, EnvDispatcherURL+" is not set")
	}
	if c.execution == "" {
		problems = append(problems, EnvExecution+" is not set")
	}
	if c.identityFile == "" {
		problems = append(problems, EnvIdentityFile+" is not set")
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
	return c, nil
}

// checkout is the fixed checkout path of a repository (host/workspace/name).
func (c config) checkout(repository string) string { return filepath.Join(c.workspace, repository) }

// claudeConfig is the harness's CLAUDE_CONFIG_DIR.
func (c config) claudeConfig() string { return filepath.Join(c.home, ".claude") }

// gitConfig is the run user's git configuration: the credential helper
// and the ssh rewrite. It is outside the checkout.
func (c config) gitConfig() string { return filepath.Join(c.home, ".gitconfig") }

func (c config) secrets() string { return filepath.Join(c.dir, "secrets") }
func (c config) socket() string  { return filepath.Join(c.dir, "agent.sock") }
