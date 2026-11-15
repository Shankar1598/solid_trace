package dispatcher

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// The Squid allowlist. The Dispatcher writes the file that Squid reads as
// the operator's default list, SolidTrace's hosts (the agents upload
// through the proxy), and the extra hosts of the factory file of each
// repository that a Project names. One list is for all agent machines.
// When the file changes, the Dispatcher runs the reload command.

// DefaultSquidReload is the command that makes Squid read its
// configuration again when none is set.
const DefaultSquidReload = "squid -k reconfigure"

// squidReloadWait is the longest time of the reload command.
const squidReloadWait = 30 * time.Second

// allowlist writes Squid's allowlist.
type allowlist struct {
	// file is the allowlist that Squid reads ("" is none: the Dispatcher
	// does not write one).
	file string
	// base are the default list and SolidTrace's hosts.
	base []string
	// reload is a shell command.
	reload string

	mu sync.Mutex
	// loaded is the content that Squid has: the file's content at the
	// start, then the last one that a reload took.
	loaded string
}

// newAllowlist reads the operator's default list.
func newAllowlist(c Config) (*allowlist, error) {
	a := &allowlist{file: c.SquidAllowlist, reload: c.SquidReload}
	if a.file == "" {
		return a, nil
	}
	if a.reload == "" {
		a.reload = DefaultSquidReload
	}
	data, err := os.ReadFile(c.DefaultAllowlist)
	if err != nil {
		return nil, fmt.Errorf("read the default allowlist (%s): %w", EnvDefaultAllowlist, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			a.base = append(a.base, strings.ToLower(line))
		}
	}
	for _, u := range []string{c.SolidTraceURL, c.SolidTraceIngestURL} {
		if parsed, err := url.Parse(u); err == nil && parsed.Hostname() != "" {
			a.base = append(a.base, strings.ToLower(parsed.Hostname()))
		}
	}
	if current, err := os.ReadFile(a.file); err == nil {
		a.loaded = string(current)
	}
	return a, nil
}

// content is the allowlist file with extra hosts. A host that a name
// with a leading dot already matches is left out, as Squid warns of it.
func (a *allowlist) content(extra []string) string {
	slices.Sort(extra)
	all := append(slices.Clone(a.base), extra...)
	var b strings.Builder
	b.WriteString("# Squid's allowlist. The SolidTrace Dispatcher writes this file: the default list, SolidTrace's\n" +
		"# hosts and the allowed_hosts of each repository's factory file. Do not edit it.\n")
	seen := map[string]bool{}
	for _, h := range all {
		if seen[h] || covered(h, all) {
			continue
		}
		seen[h] = true
		b.WriteString(h + "\n")
	}
	return b.String()
}

// covered tells if another name of list, with a leading dot, matches h.
func covered(h string, list []string) bool {
	for _, d := range list {
		if strings.HasPrefix(d, ".") && d != h && (h == d[1:] || strings.HasSuffix(h, d)) {
			return true
		}
	}
	return false
}

// write writes the allowlist with extra hosts when it changed, and then
// reloads Squid. A failed reload is tried again at the next write. The
// caller holds a.mu.
func (a *allowlist) write(extra []string) error {
	want := a.content(extra)
	if want == a.loaded {
		return nil
	}
	// In place, not by a rename: Squid in a container can see the file
	// through a bind mount of the file, which a rename leaves on the old
	// file.
	if err := os.WriteFile(a.file, []byte(want), 0o644); err != nil {
		return fmt.Errorf("write the Squid allowlist %s: %w", a.file, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), squidReloadWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", a.reload)
	cmd.WaitDelay = time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reload Squid (%s): %v: %s", a.reload, err, strings.TrimSpace(string(out)))
	}
	a.loaded = want
	return nil
}

// has tells if the list that Squid loaded allows each of hosts.
func (a *allowlist) has(hosts []string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	var loaded []string
	for _, line := range strings.Split(a.loaded, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			loaded = append(loaded, line)
		}
	}
	for _, h := range hosts {
		if !slices.Contains(loaded, h) && !covered(h, loaded) {
			return false
		}
	}
	return true
}

// refreshAllowlist writes the allowlist from the hosts that the database
// keeps, and gives the error of the write or of the reload. It holds the
// lock from the read to the reload, so that a refresh that read the
// database earlier cannot write its older list after a newer one.
func (d *Dispatcher) refreshAllowlist() error {
	a := d.allowlist
	if a.file == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	hosts, err := d.store.allowedHosts()
	if err == nil {
		err = a.write(hosts)
	}
	if err != nil {
		d.log.Error("could not change the Squid allowlist", "error", err)
	}
	return err
}

// setRepositoryHosts keeps the extra hosts of the factory file that the
// Dispatcher read for a repository and a Project folder.
func (s *store) setRepositoryHosts(repository, folder string, hosts []string, at time.Time) error {
	if hosts == nil {
		hosts = []string{}
	}
	data, err := json.Marshal(hosts)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO repository_hosts (repository, folder, hosts, read_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (repository, folder) DO UPDATE SET hosts = excluded.hosts, read_at = excluded.read_at`,
		repository, folder, string(data), at.UnixMicro())
	return err
}

// allowedHosts are the extra hosts of each repository and folder that the
// Factory settings of a Project name (orphaned settings name none), and
// the hosts of the factory file of each Execution that has not ended. An
// Execution of a Run that started before a change of the settings keeps
// its hosts until it ends.
func (s *store) allowedHosts() ([]string, error) {
	rows, err := s.db.Query(`SELECT h.hosts FROM repository_hosts h WHERE EXISTS (SELECT 1 FROM factory_settings s
		WHERE s.repository = h.repository AND s.folder = h.folder AND s.orphaned_at IS NULL)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var hosts []string
		if err := json.Unmarshal([]byte(data), &hosts); err != nil {
			return nil, errors.New("a repository's allowed hosts are not a JSON list: " + err.Error())
		}
		all = append(all, hosts...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	live, err := s.db.Query(`SELECT factory_file FROM executions WHERE ended_at IS NULL AND factory_file != ''`)
	if err != nil {
		return nil, err
	}
	defer live.Close()
	for live.Next() {
		var data string
		if err := live.Scan(&data); err != nil {
			return nil, err
		}
		file, err := decodeFactoryFile(data)
		if err != nil {
			return nil, errors.New("an Execution's factory file is not valid JSON: " + err.Error())
		}
		all = append(all, file.AllowedHosts...)
	}
	return all, live.Err()
}
