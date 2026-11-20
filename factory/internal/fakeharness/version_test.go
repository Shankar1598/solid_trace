package fakeharness_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/solidtrace/factory/internal/fakeharness"
)

// The recorded streams are from the Claude Code version that the agent VM image
// recipe installs. A change to one pin without the other fails here.
func TestTheStreamsAreFromTheImageRecipesClaudeCodeVersion(t *testing.T) {
	recipe, err := os.ReadFile("../../image/agent-vm.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^CLAUDE_CODE_VERSION=(\S+)$`).FindSubmatch(recipe)
	if m == nil {
		t.Fatal("no CLAUDE_CODE_VERSION in factory/image/agent-vm.sh")
	}
	if got := string(m[1]); got != fakeharness.Version {
		t.Fatalf("the image recipe installs Claude Code %s, but the streams are from %s (fakeharness.Version)", got, fakeharness.Version)
	}
}
