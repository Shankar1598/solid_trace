// Command releasefiles makes the files that each GitHub release gives
// beside the binaries of cli/build: each Claude Code plugin as a zip, the
// plugins' marketplace file, the install script, and one checksum file.
// The release workflow runs it after cli/build:
//
//	go run ./internal/releasefiles -tag v0.19.0 -repository owner/name
//
// Each plugin entry of the marketplace file is an archive source: the
// release's zip, with its SHA-256. So the marketplace file and the plugins
// of one release are pinned to that release.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The names of the release files.
const (
	PluginZip   = "solidtrace-plugin.zip"
	SkillsZip   = "solidtrace-skills.zip"
	Marketplace = "marketplace.json"
	Install     = "install.sh"
	Sums        = "SHA256SUMS"
)

// plugin is one Claude Code plugin of the release: its folder in cli, its
// zip, and its marketplace entry. Its name must be the name in its
// plugin.json.
type plugin struct {
	Name        string
	Dir         string
	Zip         string
	Description string
}

var plugins = []plugin{
	{Name: "solidtrace", Dir: "plugin", Zip: PluginZip,
		Description: "Uploads your Agent sessions in the captured repositories to SolidTrace."},
	{Name: "solidtrace-skills", Dir: "plugin-skills", Zip: SkillsZip,
		Description: "Skills that review a change and open its PR, with a record of what the review found and what happened to each finding."},
}

type options struct {
	// Dist holds the binaries; the other release files go there too.
	Dist string
	// Root is the cli folder, which holds the folder of each plugin.
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
	download := "https://github.com/" + o.Repository + "/releases/download/" + o.Tag

	var entries []any
	for _, p := range plugins {
		zipped, err := pluginZip(filepath.Join(o.Root, p.Dir), p.Name, m[1])
		if err != nil {
			return fmt.Errorf("%s: %w", p.Dir, err)
		}
		if err := os.WriteFile(filepath.Join(o.Dist, p.Zip), zipped, 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(zipped)
		entries = append(entries, map[string]any{
			"name":        p.Name,
			"description": p.Description,
			"source": map[string]any{
				"source": "archive",
				"url":    download + "/" + p.Zip,
				"sha256": hex.EncodeToString(sum[:]),
			},
		})
	}
	if err := writeMarketplace(filepath.Join(o.Dist, Marketplace), entries); err != nil {
		return err
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

// pluginZip zips the plugin directory, with the version in plugin.json.
// The entries have a fixed time, so one tree gives one zip.
func pluginZip(dir, name, version string) ([]byte, error) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		rel, err := filepath.Rel(dir, path)
		paths = append(paths, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		if rel == ".claude-plugin/plugin.json" {
			if data, err = withVersion(data, name, version); err != nil {
				return nil, err
			}
		}
		w, err := z.CreateHeader(&zip.FileHeader{Name: rel, Method: zip.Deflate,
			Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if !slices.Contains(paths, ".claude-plugin/plugin.json") {
		return nil, errors.New("the plugin has no .claude-plugin/plugin.json")
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func withVersion(manifest []byte, name, version string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	// Claude Code installs <name>@<marketplace>: the entry's name and the
	// manifest's name must agree.
	if m["name"] != name {
		return nil, fmt.Errorf("plugin.json: the name is %v, not %s", m["name"], name)
	}
	m["version"] = version
	out, err := json.MarshalIndent(m, "", "  ")
	return append(out, '\n'), err
}

func writeMarketplace(path string, entries []any) error {
	m := map[string]any{
		"name":        "solidtrace",
		"description": "The SolidTrace plugins for Claude Code, from one SolidTrace release.",
		"owner":       map[string]any{"name": "SolidTrace"},
		"plugins":     entries,
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
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
