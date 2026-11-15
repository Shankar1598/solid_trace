// Package factory holds the module-wide checks of the factory module.
package factory

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// forbidden names each package in deps (lines of "<import path> <dir>")
// that is a Console or EventStore package: by its import path, or by a
// directory under repo's console/ or event_store/ (ADR 0019).
func forbidden(deps, repo string) []string {
	var found []string
	for _, line := range strings.Split(strings.TrimSpace(deps), "\n") {
		path, dir, _ := strings.Cut(line, " ")
		inside := func(part string) bool {
			rel, err := filepath.Rel(filepath.Join(repo, part), dir)
			return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
		}
		if strings.HasPrefix(path, "github.com/solidtrace/event_store") || strings.HasPrefix(path, "github.com/solidtrace/console") ||
			(dir != "" && (inside("console") || inside("event_store"))) {
			found = append(found, path)
		}
	}
	return found
}

func TestTheFactoryModuleImportsNothingFromConsoleOrEventStore(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-test", "-f", "{{.ImportPath}} {{.Dir}}", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "github.com/solidtrace/cli/") {
		t.Fatalf("go list gave no CLI package, so the check reads the wrong list:\n%s", out)
	}
	if found := forbidden(string(out), repo); len(found) > 0 {
		t.Errorf("the factory module imports Console or EventStore packages (ADR 0019): %v", found)
	}
}

func TestTheImportCheckFindsAConsoleOrEventStorePackage(t *testing.T) {
	repo := "/repo"
	deps := "net/http /usr/lib/go/src/net/http\n" +
		"github.com/solidtrace/cli/signin /repo/cli/signin\n" +
		"github.com/solidtrace/event_store/auth /home/x/go/pkg/mod/github.com/solidtrace/event_store/auth\n" +
		"example.com/vendored /repo/console/vendor/x\n" +
		"example.com/copy /repo/event_store/handler\n" +
		"example.com/near /repo/console_tools/x\n"
	got := forbidden(deps, repo)
	want := []string{"github.com/solidtrace/event_store/auth", "example.com/vendored", "example.com/copy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("forbidden = %v, want %v", got, want)
	}
}
