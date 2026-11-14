package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// imageTags runs the release job's image-tags script, which names the tags
// that the join job puts on an image, and returns its output.
func imageTags(t *testing.T, event, prerelease, tag string) string {
	t.Helper()
	script := filepath.Join("..", "..", "..", ".github", "scripts", "image-tags")
	out, err := exec.Command("sh", script, "ghcr.io/o/solid-trace-console", tag, event, prerelease).CombinedOutput()
	if err != nil {
		t.Fatalf("image-tags: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAPublishedReleaseMovesLatest(t *testing.T) {
	got := imageTags(t, "release", "false", "v0.20.0")
	want := "--tag ghcr.io/o/solid-trace-console:v0.20.0 --tag ghcr.io/o/solid-trace-console:latest"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestATestReleaseDoesNotMoveLatest(t *testing.T) {
	for _, c := range []struct{ name, event, prerelease, tag string }{
		{"pre-release", "release", "true", "v0.20.0-rc.1"},
		{"pre-release tag not marked", "release", "false", "v0.20.0-rc.1"},
		{"run by hand", "workflow_dispatch", "", "v0.20.0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := imageTags(t, c.event, c.prerelease, c.tag)
			want := "--tag ghcr.io/o/solid-trace-console:" + c.tag
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}
