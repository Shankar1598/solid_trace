// Command releasefiles makes the files that each GitHub release gives
// beside the binaries of cli/build: the install script and one checksum
// file. The release workflow runs it after cli/build:
//
//	go run ./internal/releasefiles -tag v0.19.0 -repository owner/name
//
// The Claude Code plugin is not a release file. Claude Code gets it from the
// marketplace file at the repository root (.claude-plugin/marketplace.json)
// at the release's tag. So the release refuses a tag that is not the version
// in the plugin's plugin.json: Claude Code updates a plugin only when that
// version changes.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The names of the release files.
const (
	Install = "install.sh"
	Sums    = "SHA256SUMS"
)

// pluginManifest is the plugin's plugin.json, in the cli folder.
var pluginManifest = filepath.Join("plugin", ".claude-plugin", "plugin.json")

type options struct {
	// Dist holds the binaries; the other release files go there too.
	Dist string
	// Root is the cli folder, which holds the plugin folder.
	Root          string
	InstallScript string
	Tag           string
	// Repository is the public GitHub repository, owner/name.
	Repository string
}

var (
	tagPattern        = regexp.MustCompile(`^v(\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?)$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

func main() {
	o := options{Dist: "dist", Root: ".", InstallScript: "install.sh"}
	flag.StringVar(&o.Dist, "dist", o.Dist, "the directory of the binaries and the release files")
	flag.StringVar(&o.Tag, "tag", "", "the release tag, such as v0.19.0")
	flag.StringVar(&o.Repository, "repository", "", "the GitHub repository, owner/name")
	flag.Parse()
	if err := write(o); err != nil {
		fmt.Fprintf(os.Stderr, "releasefiles: %v\n", err)
		os.Exit(1)
	}
}

func write(o options) error {
	m := tagPattern.FindStringSubmatch(o.Tag)
	if m == nil {
		return fmt.Errorf("the tag %q is not a version such as v0.19.0", o.Tag)
	}
	if !repositoryPattern.MatchString(o.Repository) {
		return fmt.Errorf("the repository %q is not owner/name", o.Repository)
	}
	version, err := pluginVersion(filepath.Join(o.Root, pluginManifest))
	if err != nil {
		return err
	}
	if version != m[1] {
		return fmt.Errorf("%s has the version %q, not %s: the release commit must set it", pluginManifest, version, m[1])
	}
	script, err := os.ReadFile(o.InstallScript)
	if err != nil {
		return err
	}
	script = bytes.ReplaceAll(script, []byte("@VERSION@"), []byte(o.Tag))
	script = bytes.ReplaceAll(script, []byte("@REPOSITORY@"), []byte(o.Repository))
	if err := os.WriteFile(filepath.Join(o.Dist, Install), script, 0o755); err != nil {
		return err
	}
	return writeSums(o.Dist)
}

// pluginVersion is the version in the plugin.json at path.
func pluginVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return manifest.Version, nil
}

// writeSums writes the checksum file of each other file in dir, in the
// format of sha256sum and shasum -a 256.
func writeSums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || e.Name() == Sums {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), e.Name())
	}
	return os.WriteFile(filepath.Join(dir, Sums), []byte(b.String()), 0o644)
}
