package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// FactoryFileName is the factory file's path in a Project's folder, and
// at the repository's root.
const FactoryFileName = ".solidtrace/factory.toml"

// maxFactoryFile is the largest factory file that the Dispatcher reads.
const maxFactoryFile = 64 << 10

// factoryFileWait is the longest time that the read of a factory file
// from the forge can take.
const factoryFileWait = time.Minute

// factoryFile is what the Dispatcher keeps of a Run's factory file for
// one Execution. Its zero value is no file: the defaults.
type factoryFile struct {
	// Path is the path of the file in the repository ("" is none).
	Path        string                `json:"path,omitempty"`
	MachineKind string                `json:"machine_kind,omitempty"`
	Setup       string                `json:"setup,omitempty"`
	Checks      []dispatcherapi.Check `json:"checks,omitempty"`
	// AllowedHosts are in Squid's form: a name that starts with a dot
	// also matches each subdomain.
	AllowedHosts []string             `json:"allowed_hosts,omitempty"`
	Limits       dispatcherapi.Limits `json:"limits"`
	SaveExclude  []string             `json:"save_exclude,omitempty"`
}

// limits are the file's limits, with the default for each that it does
// not set.
func (f factoryFile) limits() dispatcherapi.Limits {
	l := defaultLimits
	if f.Limits.IdleSeconds > 0 {
		l.IdleSeconds = f.Limits.IdleSeconds
	}
	if f.Limits.TotalSeconds > 0 {
		l.TotalSeconds = f.Limits.TotalSeconds
	}
	return l
}

// factoryFilePaths are the places of the factory file, in the order that
// the Dispatcher looks: the Project's folder first, then the root.
func factoryFilePaths(folder string) []string {
	folder = strings.Trim(folder, "/")
	if folder == "" {
		return []string{FactoryFileName}
	}
	return []string{path.Join(folder, FactoryFileName), FactoryFileName}
}

// readFactoryFile reads the factory file of a repository from its default
// branch only, so that a change on a branch does not apply until a person
// merges it. No file gives the defaults. Each error is fit for the reason
// on a Run.
func (d *Dispatcher) readFactoryFile(ctx context.Context, f forge.Forge, repository, folder string) (factoryFile, error) {
	ctx, cancel := context.WithTimeout(ctx, factoryFileWait)
	defer cancel()
	found, body, err := f.DefaultBranchFile(ctx, repository, factoryFilePaths(folder)...)
	if err != nil {
		return factoryFile{}, err
	}
	if found == "" {
		return factoryFile{}, nil
	}
	file, problem := d.parseFactoryFile(body)
	if problem != "" {
		return factoryFile{}, fmt.Errorf("the factory file %s on the default branch of %s is not valid: %s. "+
			"Correct it on the default branch", found, repository, problem)
	}
	file.Path = found
	return file, nil
}

// factoryFileTOML is the factory file's TOML:
//
//	machine_kind = "large"
//	setup = "bin/setup"
//	allowed_hosts = ["api.stripe.com", "*.example.com"]
//	save_exclude = ["tmp/", "*.log"]
//
//	[limits]
//	idle = "45m"
//	total = "12h"
//
//	[[checks]]
//	name = "test"
//	command = "go test ./..."
type factoryFileTOML struct {
	MachineKind  *string  `toml:"machine_kind"`
	Setup        *string  `toml:"setup"`
	AllowedHosts []string `toml:"allowed_hosts"`
	SaveExclude  []string `toml:"save_exclude"`
	Limits       struct {
		Idle  *string `toml:"idle"`
		Total *string `toml:"total"`
	} `toml:"limits"`
	Checks []struct {
		Name    string `toml:"name"`
		Command string `toml:"command"`
	} `toml:"checks"`
}

// checkName is the form of a check's name.
var checkName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// parseFactoryFile reads and checks the file. It gives the first problem,
// with its line where TOML gives one.
func (d *Dispatcher) parseFactoryFile(body []byte) (factoryFile, string) {
	if len(body) > maxFactoryFile {
		return factoryFile{}, fmt.Sprintf("it is larger than %d KiB", maxFactoryFile>>10)
	}
	var in factoryFileTOML
	meta, err := toml.Decode(string(body), &in)
	if err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			return factoryFile{}, fmt.Sprintf("line %d: %s", pe.Position.Line, pe.Message)
		}
		return factoryFile{}, strings.TrimPrefix(err.Error(), "toml: ")
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = k.String()
		}
		return factoryFile{}, "it has the unknown key " + strings.Join(names, ", ") +
			"; the keys are machine_kind, setup, allowed_hosts, save_exclude, [limits] idle and total, " +
			"and [[checks]] name and command"
	}
	var f factoryFile
	if in.MachineKind != nil {
		f.MachineKind = strings.TrimSpace(*in.MachineKind)
		if problem := d.checkMachineKind(f.MachineKind); problem != "" {
			return factoryFile{}, problem
		}
	}
	if in.Setup != nil {
		f.Setup = strings.TrimSpace(*in.Setup)
		if f.Setup == "" {
			return factoryFile{}, "setup is empty: remove it, or give the setup command"
		}
	}
	for i, c := range in.Checks {
		name, command := strings.TrimSpace(c.Name), strings.TrimSpace(c.Command)
		switch {
		case !checkName.MatchString(name):
			return factoryFile{}, fmt.Sprintf("check %d: the name must be 1 to 63 lower-case letters, digits, '.', '_' "+
				"or '-', not %q", i+1, c.Name)
		case command == "":
			return factoryFile{}, fmt.Sprintf("the check %s has no command", name)
		case slices.ContainsFunc(f.Checks, func(x dispatcherapi.Check) bool { return x.Name == name }):
			return factoryFile{}, fmt.Sprintf("two checks have the name %s", name)
		}
		f.Checks = append(f.Checks, dispatcherapi.Check{Name: name, Command: command})
	}
	for _, h := range in.AllowedHosts {
		host, ok := squidHost(h)
		if !ok {
			return factoryFile{}, fmt.Sprintf("allowed_hosts: %q is not a host name, such as api.example.com, "+
				"or a wildcard, such as *.example.com", h)
		}
		f.AllowedHosts = append(f.AllowedHosts, host)
	}
	for _, p := range in.SaveExclude {
		if strings.TrimSpace(p) == "" || strings.ContainsAny(p, "\x00\n\r") {
			return factoryFile{}, fmt.Sprintf("save_exclude: %q is not a pattern", p)
		}
		f.SaveExclude = append(f.SaveExclude, p)
	}
	for _, l := range []struct {
		name  string
		value *string
		to    *int64
	}{{"idle", in.Limits.Idle, &f.Limits.IdleSeconds}, {"total", in.Limits.Total, &f.Limits.TotalSeconds}} {
		if l.value == nil {
			continue
		}
		v, err := time.ParseDuration(strings.TrimSpace(*l.value))
		if err != nil || v < time.Minute || v%time.Second != 0 {
			return factoryFile{}, fmt.Sprintf("limits.%s must be a duration of 1 minute or more in whole seconds, "+
				"such as 45m or 12h, not %q", l.name, *l.value)
		}
		*l.to = int64(v / time.Second)
	}
	return f, ""
}

// checkMachineKind is the problem with a machine kind that the operator
// did not define. A Dispatcher with no kinds (no Runner on Nomad) takes
// each name.
func (d *Dispatcher) checkMachineKind(name string) string {
	if len(d.config.MachineKinds) == 0 {
		return ""
	}
	if _, ok := d.config.MachineKind(name); ok && name != "" {
		return ""
	}
	var names []string
	for _, k := range d.config.MachineKinds {
		names = append(names, k.Name)
	}
	return fmt.Sprintf("machine_kind %q is not a machine kind of the operator; the kinds are %s", name,
		strings.Join(names, ", "))
}

// hostLabel is one part of a host name.
var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// squidHost gives a host of the factory file in Squid's dstdomain form:
// *.example.com and .example.com become .example.com, which matches
// example.com and each of its subdomains. It refuses an IP address, a
// port, a scheme, a path and a name of one part.
func squidHost(h string) (string, bool) {
	h = strings.ToLower(strings.TrimSpace(h))
	wildcard := false
	if rest, ok := strings.CutPrefix(h, "*."); ok {
		h, wildcard = rest, true
	} else if rest, ok := strings.CutPrefix(h, "."); ok {
		h, wildcard = rest, true
	}
	labels := strings.Split(h, ".")
	if len(h) > 253 || len(labels) < 2 {
		return "", false
	}
	for _, l := range labels {
		if !hostLabel.MatchString(l) {
			return "", false
		}
	}
	// The last part has a letter: no IP address.
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return "", false
	}
	if wildcard {
		return "." + h, true
	}
	return h, true
}

// encodeFactoryFile is the file as the executions row keeps it.
func encodeFactoryFile(f factoryFile) (string, error) {
	b, err := json.Marshal(f)
	return string(b), err
}

func decodeFactoryFile(s string) (factoryFile, error) {
	var f factoryFile
	if s == "" {
		return f, nil
	}
	err := json.Unmarshal([]byte(s), &f)
	return f, err
}
