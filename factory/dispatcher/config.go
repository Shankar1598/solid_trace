package dispatcher

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/solidtrace/factory/forge"
)

// Config is the operator configuration. The Dispatcher reads it at start
// from a file and from the environment. The file holds NAME=value lines
// with the same names as the environment (blank lines and lines that start
// with # are not read), and a name in the environment wins over the file.
// A later ticket adds the checkpoint interval.
type Config struct {
	// SolidTraceURL is the Console URL: the OAuth issuer that checks
	// tokens, and the base of each link to a Run's page.
	SolidTraceURL string
	// SolidTraceIngestURL is SolidTrace's ingest URL: the agent ingest
	// API, where each Execution's wrapper uploads its Run's Agent
	// sessions ("" is SolidTraceURL).
	SolidTraceIngestURL string
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
	// WebhookListen is the address of the webhook listener, which serves
	// only POST /webhooks/<kind>/<id>. Caddy (or the operator's own load
	// balancer) sends /webhooks/* from the public name to it. "" is no
	// webhook listener: the backstops alone find each change.
	WebhookListen string
	// WebhookURL is the public webhook URL, such as
	// https://factory.example.com: the base of each connection's webhook
	// URL that the Dispatcher shows ("" shows none).
	WebhookURL string
	// SenderStartsPerMinute is the most Runs that one sender of the
	// generic webhook can start in a minute (0 is
	// DefaultSenderStartsPerMinute). SenderActiveRuns is the most Runs of
	// one sender with an Execution queued or live (0 is
	// DefaultSenderActiveRuns). Above each, a delivery gets 429.
	SenderStartsPerMinute int
	SenderActiveRuns      int
	// BitbucketAPIURL and BitbucketGitURL are the bases of Bitbucket
	// Cloud's REST API and clone URLs ("" is Bitbucket Cloud's own). The
	// tests set them to the fake Forge.
	BitbucketAPIURL string
	BitbucketGitURL string
	// LinearAPIURL is the base of Linear's API and token endpoint ("" is
	// Linear's own). The tests set it to the fake Linear.
	LinearAPIURL string
	// NotionAPIURL is the base of Notion's API ("" is Notion's own). The
	// tests set it to the fake Notion.
	NotionAPIURL string
	// WIPRefs is the place of the work-in-progress refs: refs/agent-wip
	// (the default), or refs/heads/agent-wip for a forge that refuses refs
	// outside refs/heads (the fallback of cluster check 12).
	WIPRefs string
	// AtAccountLimit is what a Run does when its account reaches its
	// usage limit: AtAccountLimitSwitch ("" is it) or AtAccountLimitWait
	// (the fallback of cluster check 5).
	AtAccountLimit string

	// NomadAddress is the Nomad server's HTTP API, such as
	// http://127.0.0.1:4646. With none, the Dispatcher has no Runner, and
	// each Run stays queued.
	NomadAddress string
	// NomadToken is the Nomad ACL token of the Dispatcher: it registers,
	// dispatches, reads and stops the jobs of the namespace, and reads the
	// nodes.
	NomadToken string
	// NomadNamespace is the Nomad namespace of the jobs ("" is "default").
	NomadNamespace string
	// MachineKinds are the kinds of agent machine, each a Nomad node pool
	// of the same name. The first is the default, unless
	// DefaultMachineKind names another.
	MachineKinds       []MachineKind
	DefaultMachineKind string
	// WrapperURL is the private listener's URL as an agent machine
	// reaches it, for each wrapper's calls.
	WrapperURL string
	// WrapperCommand is the path of the wrapper on an agent machine.
	WrapperCommand string

	// SquidAllowlist is the allowlist file that Squid reads: the
	// Dispatcher writes it, and runs SquidReload when it changes ("" is
	// none: the operator keeps the allowlist). DefaultAllowlist is the
	// operator's default list (one Squid dstdomain on each line, # starts
	// a comment), such as the reference configuration's
	// reference/squid/allowlist.txt. SquidReload is a shell command
	// (DefaultSquidReload when "").
	SquidAllowlist   string
	DefaultAllowlist string
	SquidReload      string

	// ScheduleMinInterval is the shortest time between two fire times of a
	// schedule, checked at save (DefaultScheduleMinInterval when 0).
	ScheduleMinInterval time.Duration

	// TriggerWord is the word at the start of a comment that makes the
	// comment a message to the Run of its ticket (DefaultTriggerWord when
	// ""). It has no spaces.
	TriggerWord string

	// scheduleMinIntervalProblem is why EnvScheduleMinInterval could not
	// be read.
	scheduleMinIntervalProblem string
	// machineKindsProblem is why EnvMachineKinds could not be read.
	machineKindsProblem string
	// limitProblems are the sender limits that could not be read.
	limitProblems []string
}

// The default limits of each sender of the generic webhook.
const (
	DefaultSenderStartsPerMinute = 10
	DefaultSenderActiveRuns      = 5
)

// parseLimit reads a whole number of 1 or more ("" is 0, the default).
func parseLimit(name, value string) (int, string) {
	if strings.TrimSpace(value) == "" {
		return 0, ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0, name + " must be a whole number of 1 or more, not " + fmt.Sprintf("%q", value)
	}
	return n, ""
}

// parseMachineKinds reads a list of name=memory (MB).
func parseMachineKinds(list string) ([]MachineKind, string) {
	var kinds []MachineKind
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, memory, _ := strings.Cut(item, "=")
		mb, err := strconv.Atoi(strings.TrimSpace(memory))
		name = strings.TrimSpace(name)
		if name == "" || !nodePoolName.MatchString(name) || err != nil || mb <= 0 {
			return nil, EnvMachineKinds + " must be a list of name=memory in MB, such as standard=12288, " +
				"with each name a Nomad node pool; " + fmt.Sprintf("%q is not", item)
		}
		for _, k := range kinds {
			if k.Name == name {
				return nil, EnvMachineKinds + " names the kind " + fmt.Sprintf("%q", name) + " twice"
			}
		}
		kinds = append(kinds, MachineKind{Name: name, MemoryMB: mb})
	}
	return kinds, ""
}

// nodePoolName is the form of a Nomad node pool's name.
var nodePoolName = regexp.MustCompile(`^[a-zA-Z0-9-_]{1,128}$`)

// MachineKind gives the kind with name; "" is the default kind.
func (c Config) MachineKind(name string) (MachineKind, bool) {
	if name == "" {
		name = c.DefaultMachineKind
	}
	for _, k := range c.MachineKinds {
		if name == "" || k.Name == name {
			return k, true
		}
	}
	return MachineKind{}, false
}

// MachineKind is a kind of agent machine: a Nomad node pool whose
// clients each have MemoryMB of memory for the task. An Execution's job
// reserves all of it, so that one Run works on each machine.
type MachineKind struct {
	Name     string
	MemoryMB int
}

// DefaultWrapperCommand is the wrapper's path on an agent machine when
// none is set.
const DefaultWrapperCommand = "/usr/local/bin/factory-wrapper"

// The names of the operator configuration.
const (
	EnvConfigFile      = "DISPATCHER_CONFIG"
	EnvSolidTraceURL   = "DISPATCHER_SOLIDTRACE_URL"
	EnvSolidTraceToken = "DISPATCHER_SOLIDTRACE_TOKEN"
	EnvIngestURL       = "DISPATCHER_SOLIDTRACE_INGEST_URL"
	EnvDatabase        = "DISPATCHER_DATABASE"
	EnvEncryptionKey   = "DISPATCHER_ENCRYPTION_KEY"
	EnvListen          = "DISPATCHER_LISTEN"
	EnvWebhookListen   = "DISPATCHER_WEBHOOK_LISTEN"
	EnvWebhookURL      = "DISPATCHER_WEBHOOK_URL"
	EnvBitbucketAPIURL = "DISPATCHER_BITBUCKET_API_URL"
	EnvBitbucketGitURL = "DISPATCHER_BITBUCKET_GIT_URL"
	EnvLinearAPIURL    = "DISPATCHER_LINEAR_API_URL"
	EnvNotionAPIURL    = "DISPATCHER_NOTION_API_URL"
	EnvWIPRefs         = "DISPATCHER_WIP_REFS"
	EnvNomadAddress    = "DISPATCHER_NOMAD_ADDR"
	EnvNomadToken      = "DISPATCHER_NOMAD_TOKEN"
	EnvNomadNamespace  = "DISPATCHER_NOMAD_NAMESPACE"
	// EnvMachineKinds is a list of name=memory in MB, such as
	// "standard=12288,large=28672".
	EnvMachineKinds       = "DISPATCHER_MACHINE_KINDS"
	EnvDefaultMachineKind = "DISPATCHER_DEFAULT_MACHINE_KIND"
	EnvWrapperURL         = "DISPATCHER_WRAPPER_URL"
	EnvWrapperCommand     = "DISPATCHER_WRAPPER_COMMAND"
	EnvSquidAllowlist     = "DISPATCHER_SQUID_ALLOWLIST"
	EnvDefaultAllowlist   = "DISPATCHER_DEFAULT_ALLOWLIST"
	EnvSquidReload        = "DISPATCHER_SQUID_RELOAD"
	// EnvScheduleMinInterval is a Go duration, such as 1h or 30m.
	EnvScheduleMinInterval = "DISPATCHER_SCHEDULE_MIN_INTERVAL"
	EnvTriggerWord         = "DISPATCHER_TRIGGER_WORD"
	// EnvAtAccountLimit is AtAccountLimitSwitch (the default) or
	// AtAccountLimitWait.
	EnvAtAccountLimit = "DISPATCHER_AT_ACCOUNT_LIMIT"
)

// What a Run does when its account reaches its usage limit
// (Config.AtAccountLimit).
const (
	// AtAccountLimitSwitch: the Run continues at once on another free
	// account, and waits on an account limit only when none is free.
	AtAccountLimitSwitch = "switch"
	// AtAccountLimitWait: the Run stays on its account, and waits for its
	// reset time. It is the fallback of cluster check 5, for when a
	// resume under another account does not work. A person can still
	// switch a waiting Run's account.
	AtAccountLimitWait = "wait"
)

// DefaultTriggerWord is the trigger word when the operator sets none.
const DefaultTriggerWord = "/agent"

// triggerWord is the word that starts a comment to a Run.
func (c Config) triggerWord() string { return cmp.Or(c.TriggerWord, DefaultTriggerWord) }

// EnvSenderStartsPerMinute and EnvSenderActiveRuns are the limits of each
// sender of the generic webhook.
const (
	EnvSenderStartsPerMinute = "DISPATCHER_SENDER_STARTS_PER_MINUTE"
	EnvSenderActiveRuns      = "DISPATCHER_SENDER_ACTIVE_RUNS"
)

// DefaultScheduleMinInterval is the shortest time between two fire times
// of a schedule when the operator sets none.
const DefaultScheduleMinInterval = time.Hour

// scheduleMinInterval is the minimum that a save checks.
func (c Config) scheduleMinInterval() time.Duration {
	if c.ScheduleMinInterval > 0 {
		return c.ScheduleMinInterval
	}
	return DefaultScheduleMinInterval
}

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
		SolidTraceURL:       strings.TrimRight(value(EnvSolidTraceURL), "/"),
		SolidTraceToken:     value(EnvSolidTraceToken),
		SolidTraceIngestURL: strings.TrimRight(value(EnvIngestURL), "/"),
		Database:            value(EnvDatabase),
		EncryptionKey:       value(EnvEncryptionKey),
		Listen:              value(EnvListen),
		WebhookListen:       value(EnvWebhookListen),
		WebhookURL:          strings.TrimRight(value(EnvWebhookURL), "/"),
		BitbucketAPIURL:     strings.TrimRight(value(EnvBitbucketAPIURL), "/"),
		BitbucketGitURL:     strings.TrimRight(value(EnvBitbucketGitURL), "/"),
		LinearAPIURL:        strings.TrimRight(value(EnvLinearAPIURL), "/"),
		NotionAPIURL:        strings.TrimRight(value(EnvNotionAPIURL), "/"),
		WIPRefs:             value(EnvWIPRefs),
		AtAccountLimit:      strings.TrimSpace(value(EnvAtAccountLimit)),

		NomadAddress:       strings.TrimRight(value(EnvNomadAddress), "/"),
		NomadToken:         value(EnvNomadToken),
		NomadNamespace:     value(EnvNomadNamespace),
		DefaultMachineKind: value(EnvDefaultMachineKind),
		WrapperURL:         strings.TrimRight(value(EnvWrapperURL), "/"),
		WrapperCommand:     value(EnvWrapperCommand),
		SquidAllowlist:     value(EnvSquidAllowlist),
		DefaultAllowlist:   value(EnvDefaultAllowlist),
		SquidReload:        value(EnvSquidReload),
		TriggerWord:        strings.TrimSpace(value(EnvTriggerWord)),
	}
	c.MachineKinds, c.machineKindsProblem = parseMachineKinds(value(EnvMachineKinds))
	for _, limit := range []struct {
		name string
		into *int
	}{{EnvSenderStartsPerMinute, &c.SenderStartsPerMinute}, {EnvSenderActiveRuns, &c.SenderActiveRuns}} {
		var problem string
		if *limit.into, problem = parseLimit(limit.name, value(limit.name)); problem != "" {
			c.limitProblems = append(c.limitProblems, problem)
		}
	}
	if v := value(EnvScheduleMinInterval); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			c.scheduleMinIntervalProblem = EnvScheduleMinInterval + " (the shortest time between two fire times " +
				"of a schedule) must be a duration such as 1h or 30m, not " + fmt.Sprintf("%q", v)
		}
		c.ScheduleMinInterval = d
	}
	if c.WrapperCommand == "" {
		c.WrapperCommand = DefaultWrapperCommand
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
	if c.NotionAPIURL == "" {
		c.NotionAPIURL = NotionAPIURL
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
	for name, v := range map[string]string{EnvBitbucketAPIURL: c.BitbucketAPIURL, EnvBitbucketGitURL: c.BitbucketGitURL, EnvLinearAPIURL: c.LinearAPIURL,
		EnvNotionAPIURL: c.NotionAPIURL, EnvIngestURL: c.SolidTraceIngestURL, EnvWebhookURL: c.WebhookURL} {
		if u, err := url.Parse(v); v != "" && (err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "") {
			problems = append(problems, name+" must be an http:// or https:// URL, not "+fmt.Sprintf("%q", v))
		}
	}
	if c.WIPRefs != "" && c.WIPRefs != forge.WIPOutsideBranches && c.WIPRefs != forge.WIPInBranches {
		problems = append(problems, EnvWIPRefs+" (the place of the work-in-progress refs) must be "+
			forge.WIPOutsideBranches+" or "+forge.WIPInBranches+", not "+fmt.Sprintf("%q", c.WIPRefs))
	}
	if c.AtAccountLimit != "" && c.AtAccountLimit != AtAccountLimitSwitch && c.AtAccountLimit != AtAccountLimitWait {
		problems = append(problems, EnvAtAccountLimit+" (what a Run does at its account's usage limit) must be "+
			AtAccountLimitSwitch+" or "+AtAccountLimitWait+", not "+fmt.Sprintf("%q", c.AtAccountLimit))
	}
	problems = append(problems, c.limitProblems...)
	if c.SenderStartsPerMinute < 0 || c.SenderActiveRuns < 0 {
		problems = append(problems, EnvSenderStartsPerMinute+" and "+EnvSenderActiveRuns+" must be 1 or more")
	}
	if c.scheduleMinIntervalProblem != "" {
		problems = append(problems, c.scheduleMinIntervalProblem)
	}
	if strings.ContainsFunc(c.TriggerWord, unicode.IsSpace) {
		problems = append(problems, EnvTriggerWord+" (the word that starts a comment to a Run) must be one word "+
			"with no spaces, not "+fmt.Sprintf("%q", c.TriggerWord))
	}
	if c.SquidAllowlist != "" && c.DefaultAllowlist == "" {
		problems = append(problems, EnvDefaultAllowlist+" (the default allowlist, such as the reference "+
			"configuration's squid/allowlist.txt) is not set, and "+EnvSquidAllowlist+" is")
	}
	problems = append(problems, c.nomadProblems()...)
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

// senderLimits are the limits of each sender: starts in a minute, and Runs
// with an Execution queued or live.
func (c Config) senderLimits() (starts, active int) {
	return cmp.Or(c.SenderStartsPerMinute, DefaultSenderStartsPerMinute),
		cmp.Or(c.SenderActiveRuns, DefaultSenderActiveRuns)
}

// ingestURL is where the wrappers upload.
func (c Config) ingestURL() string {
	if c.SolidTraceIngestURL != "" {
		return c.SolidTraceIngestURL
	}
	return c.SolidTraceURL
}

// nomadProblems names each value of the Runner that is absent or wrong.
// With no Nomad address there is no Runner, and none is necessary.
func (c Config) nomadProblems() []string {
	if c.machineKindsProblem != "" {
		return []string{c.machineKindsProblem}
	}
	if c.NomadAddress == "" {
		return nil
	}
	var problems []string
	if u, err := url.Parse(c.NomadAddress); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		problems = append(problems, EnvNomadAddress+" must be an http:// or https:// URL, not "+fmt.Sprintf("%q", c.NomadAddress))
	}
	if len(c.MachineKinds) == 0 {
		problems = append(problems, EnvMachineKinds+" (the kinds of agent machine, as name=memory in MB) is not set")
	} else if _, ok := c.MachineKind(""); !ok {
		problems = append(problems, EnvDefaultMachineKind+" names "+fmt.Sprintf("%q", c.DefaultMachineKind)+
			", which is not in "+EnvMachineKinds)
	}
	if c.WrapperURL == "" {
		problems = append(problems, EnvWrapperURL+" (the private listener's URL as an agent machine reaches it) is not set")
	} else if u, err := url.Parse(c.WrapperURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		problems = append(problems, EnvWrapperURL+" must be an http:// or https:// URL, not "+fmt.Sprintf("%q", c.WrapperURL))
	}
	if !strings.HasPrefix(c.WrapperCommand, "/") {
		problems = append(problems, EnvWrapperCommand+" must be an absolute path, not "+fmt.Sprintf("%q", c.WrapperCommand))
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
