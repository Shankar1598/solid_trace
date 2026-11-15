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
	// EnvDir is the Execution's directory: it holds checkout/, secrets/
	// and the agent's socket (default: the working directory).
	EnvDir = "WRAPPER_DIR"
	// EnvHarness is the harness program (default claude).
	EnvHarness = "WRAPPER_HARNESS"
	// EnvDocker is the docker program of the end's container step
	// (default docker). When it is not there, the step is skipped.
	EnvDocker = "WRAPPER_DOCKER"
)

// config is the wrapper's configuration.
type config struct {
	dispatcherURL, execution, identityFile, dir, harness, docker string
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		dispatcherURL: strings.TrimRight(getenv(EnvDispatcherURL), "/"),
		execution:     getenv(EnvExecution),
		identityFile:  getenv(EnvIdentityFile),
		dir:           getenv(EnvDir),
		harness:       getenv(EnvHarness),
		docker:        getenv(EnvDocker),
	}
	var problems []string
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

func (c config) checkout() string { return filepath.Join(c.dir, "checkout") }
func (c config) secrets() string  { return filepath.Join(c.dir, "secrets") }
func (c config) socket() string   { return filepath.Join(c.dir, "agent.sock") }
