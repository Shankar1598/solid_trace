package dispatcher_test

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// admin is an Admin of factorytest.Organization whose CLI knows the
// Dispatcher.
func admin(t *testing.T, env *factorytest.Env) *factorytest.Person {
	t.Helper()
	dana := env.PersonIn("Dana", factorytest.Organization, "admin")
	if r := dana.CLI("runs", "list", "--dispatcher", env.URL()); r.Code != 0 {
		t.Fatalf("runs list: exit %d: %s", r.Code, r.Stderr)
	}
	return dana
}

// member is a member of factorytest.Organization whose CLI knows the
// Dispatcher.
func member(t *testing.T, env *factorytest.Env) *factorytest.Person {
	t.Helper()
	ana := env.Member("Ana")
	if r := ana.CLI("runs", "list", "--dispatcher", env.URL()); r.Code != 0 {
		t.Fatalf("runs list: exit %d: %s", r.Code, r.Stderr)
	}
	return ana
}

func mustCLI(t *testing.T, p *factorytest.Person, args ...string) factorytest.Result {
	t.Helper()
	r := p.CLI(args...)
	if r.Code != 0 {
		t.Fatalf("solidtrace %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), r.Code, r.Stdout, r.Stderr)
	}
	return r
}

func TestAnAdminSetsAProjectsFactorySettingsAndAMemberReadsThem(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	dana, ana := admin(t, env), member(t, env)

	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "https://Bitbucket.org/example/shop.git",
		"--folder", "/apps/checkout/", "--base-branch", "develop")

	got := mustCLI(t, ana, "projects", "get", "checkout")
	for _, want := range []string{"bitbucket.org/example/shop", "apps/checkout", "develop", "dana@example.com"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("projects get has no %q:\n%s", want, got.Stdout)
		}
	}
	var settings dispatcherapi.FactorySettings
	ana.API("GET", "/api/v1/projects/checkout/settings", nil).JSON(t, &settings)
	if settings.Repository != "bitbucket.org/example/shop" || settings.Folder != "apps/checkout" ||
		settings.BaseBranch != "develop" || settings.Project != "checkout" {
		t.Errorf("the settings are %+v", settings)
	}

	a := ana.API("PUT", "/api/v1/projects/checkout/settings", dispatcherapi.SaveFactorySettings{Repository: "bitbucket.org/example/other"})
	if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "only an Admin") {
		t.Errorf("a member's save: %d %s, want 403", a.Status, a.Body)
	}
	if r := ana.CLI("projects", "set", "checkout", "--repository", "bitbucket.org/example/other"); r.Code != 1 ||
		!strings.Contains(r.Stderr, "only an Admin") {
		t.Errorf("a member's projects set: exit %d, stderr %q", r.Code, r.Stderr)
	}
	ana.API("GET", "/api/v1/projects/checkout/settings", nil).JSON(t, &settings)
	if settings.Repository != "bitbucket.org/example/shop" {
		t.Errorf("after a member's save the repository is %q", settings.Repository)
	}
}

func TestAnEmptyBaseBranchMeansTheRepositorysDefaultBranch(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	dana := admin(t, env)
	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/shop")

	got := mustCLI(t, dana, "projects", "get", "checkout")
	if !strings.Contains(got.Stdout, "the repository's default branch") || !strings.Contains(got.Stdout, "the root") {
		t.Errorf("projects get:\n%s", got.Stdout)
	}
}

func TestSeveralProjectsNameOneRepositoryWithAFolderEach(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	env.SolidTraceProject("search")
	dana := admin(t, env)
	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/shop", "--folder", "apps/checkout")
	mustCLI(t, dana, "projects", "set", "search", "--repository", "bitbucket.org/example/shop", "--folder", "apps/search")

	var list dispatcherapi.ProjectList
	member(t, env).API("GET", "/api/v1/projects", nil).JSON(t, &list)
	folders := map[string]string{}
	for _, p := range list.Projects {
		if p.Repository != "bitbucket.org/example/shop" {
			t.Errorf("%s has repository %q", p.Project, p.Repository)
		}
		folders[p.Project] = p.Folder
	}
	if len(folders) != 2 || folders["checkout"] != "apps/checkout" || folders["search"] != "apps/search" {
		t.Errorf("the list is %+v", list.Projects)
	}
	listed := mustCLI(t, dana, "projects", "list")
	if !strings.Contains(listed.Stdout, "apps/checkout") || !strings.Contains(listed.Stdout, "apps/search") {
		t.Errorf("projects list:\n%s", listed.Stdout)
	}
}

func TestASaveRefusesAProjectThatSolidTraceDoesNotHaveAndWrongValues(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	dana := admin(t, env)

	if a := dana.API("PUT", "/api/v1/projects/nothing/settings", dispatcherapi.SaveFactorySettings{Repository: "bitbucket.org/example/shop"}); a.Status != http.StatusNotFound || !strings.Contains(string(a.Body), "nothing") {
		t.Errorf("a Project that SolidTrace does not have: %d %s, want 404", a.Status, a.Body)
	}
	for name, save := range map[string]dispatcherapi.SaveFactorySettings{
		"no repository":                {},
		"a repository with no name":    {Repository: "bitbucket.org/example"},
		"a local path":                 {Repository: "/srv/git/shop"},
		"a folder that goes up":        {Repository: "bitbucket.org/example/shop", Folder: "apps/../.."},
		"a base branch with a space":   {Repository: "bitbucket.org/example/shop", BaseBranch: "my branch"},
		"a base branch like an option": {Repository: "bitbucket.org/example/shop", BaseBranch: "-x"},
	} {
		if a := dana.API("PUT", "/api/v1/projects/checkout/settings", save); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	if a := dana.API("GET", "/api/v1/projects/checkout/settings", nil); a.Status != http.StatusNotFound ||
		!strings.Contains(string(a.Body), "no Factory settings") {
		t.Errorf("after refused saves, get: %d %s, want 404 that says there are no Factory settings", a.Status, a.Body)
	}
}

func TestAnAdminAddsABitbucketConnectionAndAMemberListsItWithNoToken(t *testing.T) {
	env := factorytest.New(t)
	dana, ana := admin(t, env), member(t, env)

	r := dana.RunCLI("", "ATCTT3xFfGN0-secret-for-tests\n", "connections", "set", "bitbucket", "--kind", "bitbucket")
	if r.Code != 0 {
		t.Fatalf("connections set: exit %d: %s", r.Code, r.Stderr)
	}

	list := mustCLI(t, ana, "connections", "list")
	if !strings.Contains(list.Stdout, "bitbucket") || !strings.Contains(list.Stdout, "dana@example.com") {
		t.Errorf("connections list:\n%s", list.Stdout)
	}
	a := ana.API("GET", "/api/v1/connections", nil)
	if strings.Contains(string(a.Body), "secret-for-tests") {
		t.Errorf("the list gives the token: %s", a.Body)
	}
	var connections dispatcherapi.Connections
	a.JSON(t, &connections)
	if len(connections.Connections) != 1 || connections.Connections[0].Kind != dispatcherapi.ForgeBitbucket {
		t.Errorf("the connections are %+v", connections.Connections)
	}
	if r := ana.RunCLI("", "token\n", "connections", "set", "mine", "--kind", "bitbucket"); r.Code != 1 ||
		!strings.Contains(r.Stderr, "only an Admin") {
		t.Errorf("a member's connections set: exit %d, stderr %q", r.Code, r.Stderr)
	}
	for name, save := range map[string]dispatcherapi.SaveConnection{
		"no token":      {Kind: "bitbucket"},
		"another kind":  {Kind: "github", Token: "x"},
		"no kind given": {Token: "x"},
	} {
		if a := dana.API("PUT", "/api/v1/connections/other", save); a.Status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	if r := dana.RunCLI("", "", "connections", "set", "empty", "--kind", "bitbucket"); r.Code == 0 {
		t.Errorf("connections set with no token on stdin: exit 0")
	}
}

func TestAConnectionKeepsATokenOfAtMost8KiB(t *testing.T) {
	env := factorytest.New(t)
	dana := admin(t, env)
	const maxToken = 8 << 10
	if a := dana.API("PUT", "/api/v1/connections/big", dispatcherapi.SaveConnection{Kind: "bitbucket",
		Token: strings.Repeat("x", maxToken+1)}); a.Status != http.StatusBadRequest || !strings.Contains(string(a.Body), "8 KiB") {
		t.Errorf("a token of 8 KiB and 1 byte: %d %s, want 400", a.Status, a.Body)
	}
	if a := dana.API("PUT", "/api/v1/connections/big", dispatcherapi.SaveConnection{Kind: "bitbucket",
		Token: strings.Repeat("x", maxToken)}); a.Status != http.StatusOK {
		t.Errorf("a token of 8 KiB: %d %s, want 200", a.Status, a.Body)
	}
}

func TestAProjectCanNameOnlyAConnectionThatTheOrganizationHas(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	dana := admin(t, env)
	if a := dana.API("PUT", "/api/v1/connections/ours", dispatcherapi.SaveConnection{Kind: "bitbucket", Token: "t"}); a.Status != http.StatusOK {
		t.Fatalf("our connection: %d %s", a.Status, a.Body)
	}

	theirs := dana.API("PUT", "/api/v1/projects/checkout/settings",
		dispatcherapi.SaveFactorySettings{Repository: "bitbucket.org/example/shop", Connection: "theirs"})
	if theirs.Status != http.StatusUnprocessableEntity || !strings.Contains(string(theirs.Body), "theirs") {
		t.Errorf("a save that names a connection that the Organization does not have: %d %s, want 422", theirs.Status, theirs.Body)
	}
	ours := mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/shop", "--connection", "ours")
	if !strings.Contains(ours.Stdout, "ours") {
		t.Errorf("projects set:\n%s", ours.Stdout)
	}
}

func TestTheDispatcherRefusesTheSettingsAndConnectionsOfAnotherOrganization(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	bo := env.PersonIn("Bo", recorder.Organization{ID: 8, Slug: "other", Name: "Other"}, "admin")

	calls := map[string]factorytest.Answer{
		"save a connection": bo.API("PUT", "/api/v1/connections/theirs", dispatcherapi.SaveConnection{Kind: "bitbucket", Token: "t"}),
		"list connections":  bo.API("GET", "/api/v1/connections", nil),
		"save settings": bo.API("PUT", "/api/v1/projects/web/settings",
			dispatcherapi.SaveFactorySettings{Repository: "bitbucket.org/other/web"}),
		"list settings": bo.API("GET", "/api/v1/projects", nil),
	}
	for name, a := range calls {
		if a.Status != http.StatusForbidden || !strings.Contains(string(a.Body), "serves the Organization example, not other") {
			t.Errorf("%s: %d %s, want 403 that names the Organization that the Dispatcher serves", name, a.Status, a.Body)
		}
	}
	if logs := env.Logs(); strings.Contains(logs, "could not list the Projects") {
		t.Errorf("the Dispatcher blames SolidTrace:\n%s", logs)
	}
	dana := admin(t, env)
	var connections dispatcherapi.Connections
	dana.API("GET", "/api/v1/connections", nil).JSON(t, &connections)
	if len(connections.Connections) != 0 {
		t.Errorf("the Organization lists %+v, want no connection", connections.Connections)
	}
}

func TestACopyOfTheDatabaseFileShowsNoSecret(t *testing.T) {
	env := factorytest.New(t)
	dana := admin(t, env)
	const token = "ATCTT3xFfGN0-copy-of-the-database-secret"
	if r := dana.RunCLI("", token+"\n", "connections", "set", "bitbucket", "--kind", "bitbucket"); r.Code != 0 {
		t.Fatalf("connections set: exit %d: %s", r.Code, r.Stderr)
	}

	files, _ := filepath.Glob(env.Config.Database + "*")
	if len(files) == 0 {
		t.Fatal("no database file")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(token)) || bytes.Contains(data, []byte("copy-of-the-database")) {
			t.Errorf("%s holds the token", filepath.Base(file))
		}
	}
}

func TestTheDispatcherRefusesToStartWithAnotherEncryptionKey(t *testing.T) {
	env := factorytest.New(t)
	config := env.Config
	config.EncryptionKey = "HxwbGhkYFxYVFBMSERAPDg0MCwoJCAcGBQQDAgEAAAE="

	d, err := dispatcher.Open(config, dispatcher.Options{})
	if err == nil {
		d.Close()
		t.Fatal("the Dispatcher started with another encryption key")
	}
	if !strings.Contains(err.Error(), dispatcher.EnvEncryptionKey) || !strings.Contains(err.Error(), "not the key") {
		t.Errorf("the error is %q, want one that names %s", err, dispatcher.EnvEncryptionKey)
	}
	config.EncryptionKey = "short"
	if _, err := dispatcher.Open(config, dispatcher.Options{}); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("a key that is not 32 bytes: %v", err)
	}
}

func TestATriggerForAProjectThatSolidTraceDeletedIsRefusedAndTheSettingsAreOrphaned(t *testing.T) {
	env := factorytest.New(t)
	project := env.Project("checkout")
	ana := member(t, env)

	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, project.ID)
	a := ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "checkout", Prompt: "Fix it"})

	if a.Status != http.StatusUnprocessableEntity || !strings.Contains(string(a.Body), "checkout") {
		t.Errorf("start: %d %s, want 422 that names the Project", a.Status, a.Body)
	}
	if logs := env.Logs(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "checkout") {
		t.Errorf("no warning that names the Project:\n%s", logs)
	}
	var list dispatcherapi.ProjectList
	ana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if len(list.Projects) != 1 || !list.Projects[0].Orphaned {
		t.Errorf("the settings are %+v, want orphaned", list.Projects)
	}
	if r := ana.CLI("projects", "list"); !strings.Contains(r.Stdout, "orphaned") {
		t.Errorf("projects list does not say orphaned:\n%s", r.Stdout)
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 0 {
		t.Errorf("%d Runs, want 0", len(runs.Runs))
	}

	// A new Project with the same slug is another Project: it has no
	// Factory settings until an Admin saves them.
	env.SolidTraceProject("checkout")
	if a := ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "checkout", Prompt: "Fix it"}); a.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(a.Body), "no Factory settings") {
		t.Errorf("start for the new Project: %d %s, want 422 for no Factory settings", a.Status, a.Body)
	}
}

func TestATriggerForAProjectWithNoFactorySettingsIsRefusedWithAWarning(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("search")
	ana := member(t, env)

	a := ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "search", Prompt: "Fix it"})
	if a.Status != http.StatusUnprocessableEntity || !strings.Contains(string(a.Body), "no Factory settings") {
		t.Errorf("start: %d %s, want 422", a.Status, a.Body)
	}
	if logs := env.Logs(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "search") {
		t.Errorf("no warning that names the Project:\n%s", logs)
	}
}

func TestTheSettingsFollowTheProjectWhenItsSlugChanges(t *testing.T) {
	env := factorytest.New(t)
	project := env.Project("checkout")
	ana := member(t, env)

	project.Slug = "cart"
	env.SolidTrace.SetProject(factorytest.Organization.Slug, project)

	id, _ := start(t, ana, "--project", "cart", "Fix it")
	var run dispatcherapi.Run
	ana.API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)
	if run.Project != "cart" {
		t.Errorf("the Run's Project is %q, want cart", run.Project)
	}
	var list dispatcherapi.ProjectList
	ana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if len(list.Projects) != 1 || list.Projects[0].Project != "cart" || list.Projects[0].Orphaned {
		t.Errorf("the list is %+v, want cart, not orphaned", list.Projects)
	}

	// The Dispatcher keeps the new slug: when SolidTrace deletes the
	// Project, the warning names cart.
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, project.ID)
	ana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if warning := orphanWarning(env.Logs()); !strings.Contains(warning, "project=cart") {
		t.Errorf("the orphan warning is %q, want project=cart", warning)
	}
}

// orphanWarning is the Log line that says Factory settings are marked
// orphaned.
func orphanWarning(logs string) string {
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "marked orphaned") {
			return line
		}
	}
	return ""
}

func TestASaveMakesOrphanedSettingsNotOrphanedAgain(t *testing.T) {
	env := factorytest.New(t)
	project := env.Project("checkout")
	dana := admin(t, env)
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, project.ID)
	var list dispatcherapi.ProjectList
	dana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if len(list.Projects) != 1 || !list.Projects[0].Orphaned {
		t.Fatalf("the list is %+v, want checkout orphaned", list.Projects)
	}

	env.SolidTrace.SetProject(factorytest.Organization.Slug, project)
	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/checkout")

	var settings dispatcherapi.FactorySettings
	dana.API("GET", "/api/v1/projects/checkout/settings", nil).JSON(t, &settings)
	if settings.Orphaned {
		t.Errorf("after the save the settings are %+v, want not orphaned", settings)
	}
	dana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	if len(list.Projects) != 1 || list.Projects[0].Orphaned {
		t.Errorf("after the save the list is %+v, want checkout not orphaned", list.Projects)
	}
}

// checkout makes a git checkout with remote origin and the folders, and
// returns its root.
func checkout(t *testing.T, origin string, folders ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	for _, f := range folders {
		if err := os.MkdirAll(filepath.Join(root, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestStartFindsTheProjectFromTheGitRemoteAndTheFolder(t *testing.T) {
	env := factorytest.New(t)
	for _, slug := range []string{"shop", "checkout", "search"} {
		env.SolidTraceProject(slug)
	}
	dana := admin(t, env)
	mustCLI(t, dana, "projects", "set", "shop", "--repository", "bitbucket.org/example/shop")
	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/shop", "--folder", "apps/checkout")
	mustCLI(t, dana, "projects", "set", "search", "--repository", "bitbucket.org/example/shop", "--folder", "apps/search")
	ana := member(t, env)
	root := checkout(t, "git@bitbucket.org:example/shop.git", "apps/checkout/src", "apps/searching", "docs")

	for dir, want := range map[string]string{
		"apps/checkout/src": "checkout",
		"apps/checkout":     "checkout",
		"apps/searching":    "shop",
		"docs":              "shop",
		"":                  "shop",
	} {
		r := ana.RunCLI(filepath.Join(root, dir), "", "start", "Fix it")
		if r.Code != 0 {
			t.Errorf("start in %q: exit %d: %s", dir, r.Code, r.Stderr)
			continue
		}
		id := queuedLine.FindStringSubmatch(r.Stdout)[1]
		var run dispatcherapi.Run
		ana.API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)
		if run.Project != want {
			t.Errorf("start in %q made a Run of %q, want %q", dir, run.Project, want)
		}
	}
}

func TestStartListsTheMatchesAndStopsWhenNotExactlyOneProjectMatches(t *testing.T) {
	env := factorytest.New(t)
	env.SolidTraceProject("checkout")
	env.SolidTraceProject("checkout-v2")
	dana := admin(t, env)
	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/shop", "--folder", "apps/checkout")
	mustCLI(t, dana, "projects", "set", "checkout-v2", "--repository", "bitbucket.org/example/shop", "--folder", "apps/checkout")
	ana := member(t, env)
	shop := checkout(t, "https://ana@bitbucket.org/example/shop.git", "apps/checkout", "apps/search")

	two := ana.RunCLI(filepath.Join(shop, "apps/checkout"), "", "start", "Fix it")
	if two.Code != 1 || !strings.Contains(two.Stderr, "checkout-v2") || !strings.Contains(two.Stderr, "--project") ||
		!strings.Contains(two.Stderr, "checkout ") {
		t.Errorf("two matches: exit %d, stderr:\n%s", two.Code, two.Stderr)
	}
	none := ana.RunCLI(filepath.Join(shop, "apps/search"), "", "start", "Fix it")
	if none.Code != 1 || !strings.Contains(none.Stderr, "no Project") || !strings.Contains(none.Stderr, "bitbucket.org/example/shop") {
		t.Errorf("no match: exit %d, stderr:\n%s", none.Code, none.Stderr)
	}
	outside := ana.RunCLI(t.TempDir(), "", "start", "Fix it")
	if outside.Code != 1 || !strings.Contains(outside.Stderr, "--project") {
		t.Errorf("outside a git repository: exit %d, stderr:\n%s", outside.Code, outside.Stderr)
	}
	var runs dispatcherapi.Runs
	ana.API("GET", "/api/v1/runs", nil).JSON(t, &runs)
	if len(runs.Runs) != 0 {
		t.Errorf("%d Runs, want 0", len(runs.Runs))
	}

	// --project wins over the lookup.
	id, _ := startIn(t, ana, filepath.Join(shop, "apps/checkout"), "--project", "checkout-v2", "Fix it")
	var run dispatcherapi.Run
	ana.API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)
	if run.Project != "checkout-v2" {
		t.Errorf("the Run's Project is %q, want checkout-v2", run.Project)
	}
}

func startIn(t *testing.T, p *factorytest.Person, dir string, args ...string) (string, factorytest.Result) {
	t.Helper()
	r := p.RunCLI(dir, "", append([]string{"start"}, args...)...)
	m := queuedLine.FindStringSubmatch(r.Stdout)
	if r.Code != 0 || m == nil {
		t.Fatalf("solidtrace start: exit %d\nstdout: %s\nstderr: %s", r.Code, r.Stdout, r.Stderr)
	}
	return m[1], r
}

func TestTheListMarksTheSettingsOfADeletedProjectOrphanedAndStartSkipsThem(t *testing.T) {
	env := factorytest.New(t)
	checkoutProject := env.SolidTraceProject("checkout")
	env.SolidTraceProject("payments")
	dana := admin(t, env)
	mustCLI(t, dana, "projects", "set", "checkout", "--repository", "bitbucket.org/example/shop")
	env.SolidTrace.DeleteProject(factorytest.Organization.Slug, checkoutProject.ID)
	mustCLI(t, dana, "projects", "set", "payments", "--repository", "bitbucket.org/example/shop")

	var list dispatcherapi.ProjectList
	dana.API("GET", "/api/v1/projects", nil).JSON(t, &list)
	orphaned := map[string]bool{}
	for _, p := range list.Projects {
		orphaned[p.Project] = p.Orphaned
	}
	if len(orphaned) != 2 || !orphaned["checkout"] || orphaned["payments"] {
		t.Errorf("the list is %+v, want checkout orphaned and payments not", list.Projects)
	}
	if logs := env.Logs(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "orphaned") {
		t.Errorf("no warning that the settings are orphaned:\n%s", logs)
	}

	ana := member(t, env)
	id, _ := startIn(t, ana, checkout(t, "git@bitbucket.org:example/shop.git"), "Fix it")
	var run dispatcherapi.Run
	ana.API("GET", "/api/v1/runs/"+id, nil).JSON(t, &run)
	if run.Project != "payments" {
		t.Errorf("the Run's Project is %q, want payments", run.Project)
	}
}
