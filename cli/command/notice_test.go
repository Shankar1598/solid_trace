package command_test

import (
	"context"
	"strings"
	"testing"

	"github.com/solidtrace/cli/internal/fake"
)

func TestAUserInSeveralOrganizationsGetsATokenForTheOneTheyPick(t *testing.T) {
	w := newWorld(t, acme, beta)
	w.oauth.Pick = "beta"
	w.signIn()

	out := w.stdout.String()
	if !strings.Contains(out, "Beta Labs") || !strings.Contains(out, "github.com/beta/app") {
		t.Errorf("notice is not for Beta Labs:\n%s", out)
	}
	if strings.Contains(out, "Acme") || strings.Contains(out, "bitbucket.org/acme/shop") {
		t.Errorf("notice names the other Organization:\n%s", out)
	}
	org, _ := w.stored()["organization"].(map[string]any)
	if org["slug"] != "beta" {
		t.Errorf("stored Organization %v, want beta", org)
	}
	list, err := w.api().CapturedRepositories(context.Background())
	if err != nil || list.Organization.Slug != "beta" {
		t.Errorf("the token acts in %+v (%v), want beta", list.Organization, err)
	}
}

func TestTheNoticeSaysThatNothingUploadsWithNoCapturedRepositories(t *testing.T) {
	w := newWorld(t, fake.Organization{Name: "Acme", Slug: "acme"})
	w.signIn()

	if out := w.stdout.String(); !strings.Contains(out, "no captured repositories") {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestLoginPrintsOneWarningLineWhenTheServerVersionDiffers(t *testing.T) {
	w := newWorld(t, acme)
	w.agent.Version = "v0.21.0"
	w.signIn()

	if n := strings.Count(w.stderr.String(), "Warning:"); n != 1 {
		t.Fatalf("warnings: %d\n%s", n, w.stderr)
	}
	line := strings.TrimSpace(w.stderr.String())
	if strings.Contains(line, "\n") || !strings.Contains(line, "v0.19.0") || !strings.Contains(line, "v0.21.0") {
		t.Errorf("warning: %q", line)
	}

	// One client warns once, whatever the number of calls.
	api := w.api()
	before := w.stderr.String()
	for range 3 {
		if _, err := api.CapturedRepositories(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(strings.TrimPrefix(w.stderr.String(), before), "Warning:"); n != 1 {
		t.Errorf("warnings over three calls of one client: %d", n)
	}
}

func TestNoVersionWarningForTheSameVersionOrNoHeader(t *testing.T) {
	for name, server := range map[string]string{"same": "v0.19.0", "no v": "0.19.0", "no header": ""} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, acme)
			w.agent.Version = server
			w.signIn()
			if strings.Contains(w.stderr.String(), "Warning") {
				t.Errorf("stderr: %s", w.stderr)
			}
		})
	}
}

func TestEachRequestSendsTheCLIVersion(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()

	for _, agent := range w.agent.UserAgents {
		if agent != "solidtrace/v0.19.0" {
			t.Errorf("User-Agent %q", agent)
		}
	}
}

func TestLoginNamesTheMinimumVersionWhenTheServerRefusesAnOldCLI(t *testing.T) {
	w := newWorld(t, acme)
	w.version = "v0.15.2"
	w.agent.MinimumVersion = "0.16.0"

	if code := w.login(); code == 0 {
		t.Fatal("login passed")
	}
	if !strings.Contains(w.stderr.String(), "0.16.0 or later") {
		t.Errorf("stderr: %s", w.stderr)
	}
}

func TestLoginWithAWrongIngestURLSaysSo(t *testing.T) {
	w := newWorld(t, acme)

	code := w.run("login", "--ingest-url", "http://127.0.0.1:1", w.oauth.URL())
	if code == 0 {
		t.Fatal("login passed")
	}
	if !strings.Contains(w.stderr.String(), "--ingest-url") {
		t.Errorf("stderr: %s", w.stderr)
	}
}
