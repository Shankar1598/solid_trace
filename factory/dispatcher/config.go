package dispatcher

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/solidtrace/factory/forge"
)

// Config is the operator configuration. The Dispatcher reads it at start
// from a file and from the environment. The file holds NAME=value lines
// with the same names as the environment (blank lines and lines that start
// with # are not read), and a name in the environment wins over the file.
// Later tickets add the Nomad address and token, the machine kinds, the
// default allowlist, the checkpoint interval, the public webhook URL and
// the trigger word.
type Config struct {
	// SolidTraceURL is the Console URL: the OAuth issuer that checks
	// tokens, and the base of each link to a Run's page.
	SolidTraceURL string
	// SolidTraceToken is the Dispatcher's own API token. A token belongs to
	// one Organization, and the Dispatcher serves only that Organization.
	SolidTraceToken string
	// Database is the path of the SQLite database.
	Database string
	// EncryptionKey is the key that encrypts each secret in the database:
	// 32 bytes in standard base64. The Dispatcher refuses to start with a
	// key that is not the one that encrypted the database's secrets.
	EncryptionKey string
	// Listen is the address of the private listener.
	Listen string
	// BitbucketAPIURL and BitbucketGitURL are the bases of Bitbucket
	// Cloud's REST API and clone URLs ("" is Bitbucket Cloud's own). The
	// tests set them to the fake Forge.
	BitbucketAPIURL string
	BitbucketGitURL string
	// WIPRefs is the place of the work-in-progress refs: refs/agent-wip
	// (the default), or refs/heads/agent-wip for a forge that refuses refs
	// outside refs/heads (the fallback of cluster check 12).
	WIPRefs string
}

// The names of the operator configuration.
const (
	EnvConfigFile      = "DISPATCHER_CONFIG"
	EnvSolidTraceURL   = "DISPATCHER_SOLIDTRACE_URL"
	EnvSolidTraceToken = "DISPATCHER_SOLIDTRACE_TOKEN"
	EnvDatabase        = "DISPATCHER_DATABASE"
	EnvEncryptionKey   = "DISPATCHER_ENCRYPTION_KEY"
	EnvListen          = "DISPATCHER_LISTEN"
	EnvBitbucketAPIURL = "DISPATCHER_BITBUCKET_API_URL"
	EnvBitbucketGitURL = "DISPATCHER_BITBUCKET_GIT_URL"
	EnvWIPRefs         = "DISPATCHER_WIP_REFS"
)

// DefaultListen is the private listener's address when none is set.
const DefaultListen = "127.0.0.1:7600"

// LoadConfig reads the operator configuration from the file at path ("" is
// no file) and from getenv. It refuses a configuration with a necessary
// value absent or wrong, and names each such value.
func LoadConfig(path string, getenv func(string) string) (Config, error) {
	c, err := readConfig(path, getenv)
	if err != nil {
		return Config{}, err
	}
	return c, c.Check()
}

// readConfig reads the configuration and checks no value.
func readConfig(path string, getenv func(string) string) (Config, error) {
	values := map[string]string{}
	if path != "" {
		file, err := readConfigFile(path)
		if err != nil {
			return Config{}, err
		}
		values = file
	}
	value := func(name string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return values[name]
	}
	c := Config{
		SolidTraceURL:   strings.TrimRight(value(EnvSolidTraceURL), "/"),
		SolidTraceToken: value(EnvSolidTraceToken),
		Database:        value(EnvDatabase),
		EncryptionKey:   value(EnvEncryptionKey),
		Listen:          value(EnvListen),
		BitbucketAPIURL: strings.TrimRight(value(EnvBitbucketAPIURL), "/"),
		BitbucketGitURL: strings.TrimRight(value(EnvBitbucketGitURL), "/"),
		WIPRefs:         value(EnvWIPRefs),
	}
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	if c.BitbucketAPIURL == "" {
		c.BitbucketAPIURL = forge.BitbucketAPIURL
	}
	if c.BitbucketGitURL == "" {
		c.BitbucketGitURL = forge.BitbucketGitURL
	}
	if c.WIPRefs == "" {
		c.WIPRefs = forge.WIPOutsideBranches
	}
	return c, nil
}

// Check names each necessary value that is absent or wrong.
func (c Config) Check() error {
	var problems []string
	problems = append(problems, c.storeProblems()...)
	if c.SolidTraceURL == "" {
		problems = append(problems, EnvSolidTraceURL+" (SolidTrace's URL) is not set")
	} else if u, err := url.Parse(c.SolidTraceURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		problems = append(problems, EnvSolidTraceURL+" must be an http:// or https:// URL, not "+fmt.Sprintf("%q", c.SolidTraceURL))
	}
	if c.SolidTraceToken == "" {
		problems = append(problems, EnvSolidTraceToken+" (the Dispatcher's API token) is not set")
	}
	for name, v := range map[string]string{EnvBitbucketAPIURL: c.BitbucketAPIURL, EnvBitbucketGitURL: c.BitbucketGitURL} {
		if u, err := url.Parse(v); v != "" && (err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "") {
			problems = append(problems, name+" must be an http:// or https:// URL, not "+fmt.Sprintf("%q", v))
		}
	}
	if c.WIPRefs != "" && c.WIPRefs != forge.WIPOutsideBranches && c.WIPRefs != forge.WIPInBranches {
		problems = append(problems, EnvWIPRefs+" (the place of the work-in-progress refs) must be "+
			forge.WIPOutsideBranches+" or "+forge.WIPInBranches+", not "+fmt.Sprintf("%q", c.WIPRefs))
	}
	if len(problems) > 0 {
		return errors.New("the operator configuration is not complete: " + strings.Join(problems, "; ") +
			". Set each in the configuration file or the environment")
	}
	return nil
}

// storeProblems names each absent or wrong value of the two that open
// the database: its path and the encryption key.
func (c Config) storeProblems() []string {
	var problems []string
	if c.Database == "" {
		problems = append(problems, EnvDatabase+" (the path of the SQLite database) is not set")
	}
	if c.EncryptionKey == "" {
		problems = append(problems, EnvEncryptionKey+" (the key that encrypts the secrets in the database; "+
			"make one with 'openssl rand -base64 32' and keep it) is not set")
	} else if _, err := c.encryptionKey(); err != nil {
		problems = append(problems, err.Error())
	}
	return problems
}

func readConfigFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read the configuration file: %w", err)
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: a line must be NAME=value", path, n)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(name)] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the configuration file: %w", err)
	}
	return values, nil
}
