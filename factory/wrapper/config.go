package wrapper

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// EnvDir is the Execution's directory: it holds checkout/, secrets/
	// and the agent's socket (default: the working directory).
	EnvDir = "WRAPPER_DIR"
	// EnvHarness is the harness program (default claude).
	EnvHarness = "WRAPPER_HARNESS"
	// EnvDocker is the docker program of the end's container step
	// (default docker). When it is not there, the step is skipped.
	EnvDocker = "WRAPPER_DOCKER"
	// EnvUploadEvery is how often the wrapper uploads the Run's Agent
	// sessions during the Execution, as a Go duration (default 5s).
	EnvUploadEvery = "WRAPPER_UPLOAD_EVERY"
	// EnvMachineNotice is the Machine notice: gce (the default), GCE's
	// instance/preempted, or none, for a machine that has no such signal.
	EnvMachineNotice = "WRAPPER_MACHINE_NOTICE"
	// EnvMetadataURL is the GCE metadata server's computeMetadata/v1/ root
	// (default http://metadata.google.internal/computeMetadata/v1/).
	EnvMetadataURL = "WRAPPER_METADATA_URL"
)

// projectName is the Claude Code project directory name of a Run when
// CLAUDE_CODE_PROJECT_DIR_NAME does not give one. Ticket 33 fixes one for
// each repository.
const projectName = "run"

// config is the wrapper's configuration.
type config struct {
	dispatcherURL, execution, identityFile, dir, harness, docker string
	// claudeConfig and projectName are the harness's CLAUDE_CONFIG_DIR
	// and CLAUDE_CODE_PROJECT_DIR_NAME: the wrapper uploads the Agent
	// sessions under <claudeConfig>/projects/<projectName>.
	claudeConfig, projectName string
	uploadEvery               time.Duration
	// machineNotice is gce or none; metadataURL is GCE's metadata root.
	machineNotice, metadataURL string
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		dispatcherURL: strings.TrimRight(getenv(EnvDispatcherURL), "/"),
		execution:     getenv(EnvExecution),
		identityFile:  getenv(EnvIdentityFile),
		dir:           getenv(EnvDir),
		harness:       getenv(EnvHarness),
		docker:        getenv(EnvDocker),
		claudeConfig:  getenv("CLAUDE_CONFIG_DIR"),
		projectName:   getenv("CLAUDE_CODE_PROJECT_DIR_NAME"),
		uploadEvery:   uploadEvery,
		machineNotice: getenv(EnvMachineNotice),
		metadataURL:   getenv(EnvMetadataURL),
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
	if every := getenv(EnvUploadEvery); every != "" {
		d, err := time.ParseDuration(every)
		if err != nil || d <= 0 {
			problems = append(problems, EnvUploadEvery+" must be a duration such as 5s, not "+every)
		}
		c.uploadEvery = d
	}
	switch c.machineNotice {
	case "":
		c.machineNotice = noticeGCE
	case noticeGCE, noticeNone:
	default:
		problems = append(problems, EnvMachineNotice+" must be gce or none, not "+c.machineNotice)
	}
	if c.metadataURL == "" {
		c.metadataURL = defaultMetadataURL
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
		c.claudeConfig = filepath.Join(c.dir, "claude")
	}
	if c.projectName == "" {
		c.projectName = projectName
	}
	return c, nil
}

func (c config) checkout() string { return filepath.Join(c.dir, "checkout") }
func (c config) secrets() string  { return filepath.Join(c.dir, "secrets") }
func (c config) socket() string   { return filepath.Join(c.dir, "agent.sock") }

// projectDir is the Claude Code project directory of the Run: its Agent
// sessions and its evidence/.
func (c config) projectDir() string { return filepath.Join(c.claudeConfig, "projects", c.projectName) }

// uploadState holds what the uploads saw of each file. The agent never
// writes it.
func (c config) uploadState() string { return filepath.Join(c.dir, "uploads") }
