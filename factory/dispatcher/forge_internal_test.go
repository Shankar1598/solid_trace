package dispatcher

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/forge"
	"github.com/solidtrace/factory/internal/fakeforge"
)

func forgeWorld(t *testing.T, wipRefs string) (*Dispatcher, *fakeforge.Forge) {
	t.Helper()
	f := fakeforge.New(t)
	f.AddRepo("example/shop", nil)
	d := &Dispatcher{
		config: Config{BitbucketAPIURL: f.APIURL(), BitbucketGitURL: f.GitURL(), WIPRefs: wipRefs},
		store:  testStore(t, filepath.Join(t.TempDir(), "d.sqlite3")),
	}
	return d, f
}

// The Forge of a Project uses the token that an Admin saved in the named
// connection of the same Organization.
func TestAProjectsForgeUsesItsConnectionsToken(t *testing.T) {
	d, f := forgeWorld(t, "")
	dana := person{id: "1", name: "dana@example.com"}
	if _, err := d.store.saveConnection("7", "bitbucket", "bitbucket", f.Token, dana, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.Push("example/shop", "agent/r1", map[string]string{"fix.txt": "x\n"}, "Fix")

	fg, err := d.projectForge("7", "bitbucket")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := fg.OpenPR(context.Background(), "bitbucket.org/example/shop", forge.NewPR{Source: "agent/r1", Title: "Fix"})
	if err != nil || pr.State != forge.PROpen {
		t.Fatalf("open with the connection's token: %+v %v", pr, err)
	}
	if ref, _ := fg.WIPRef("r1"); ref != "refs/agent-wip/r1" {
		t.Errorf("the default WIP ref is %s", ref)
	}

	// The API accepts only the kinds that the Dispatcher knows; a row of
	// another kind (from a later version) is refused, not used as Bitbucket.
	if _, err := d.store.saveConnection("7", "gitlab", "gitlab", f.Token, dana, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ org, name, want string }{
		{"7", "gitlab", `of the kind "gitlab", which this Dispatcher does not know`},
		{"7", "", "name no forge connection"},
		{"7", "other", "no forge connection other"},
		{"8", "bitbucket", "no forge connection bitbucket"},
	} {
		if _, err := d.projectForge(c.org, c.name); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("projectForge(%q, %q): %v; want %q", c.org, c.name, err, c.want)
		}
	}
}

func TestTheOperatorSettingMovesTheWorkInProgressRefs(t *testing.T) {
	d, f := forgeWorld(t, forge.WIPInBranches)
	if _, err := d.store.saveConnection("7", "bitbucket", "bitbucket", f.Token, person{id: "1"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	fg, err := d.projectForge("7", "bitbucket")
	if err != nil {
		t.Fatal(err)
	}
	if ref, _ := fg.WIPRef("r1"); ref != "refs/heads/agent-wip/r1" {
		t.Errorf("the WIP ref with the setting is %s", ref)
	}
}
