package dispatcher_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

var secretLine = regexp.MustCompile(`(?m)^Secret: +(\S+)$`)
var idLine = regexp.MustCompile(`(?m)^Sender: +(\S+)$`)

func TestAnAdminMakesASenderWhoseSecretShowsOneTime(t *testing.T) {
	env := factorytest.New(t)
	env.Project("shop")
	dana, ana := admin(t, env), member(t, env)

	made := mustCLI(t, dana, "senders", "create", "Checkout alerts", "--project", "shop")
	secret, id := secretLine.FindStringSubmatch(made.Stdout), idLine.FindStringSubmatch(made.Stdout)
	if secret == nil || id == nil || !strings.Contains(made.Stdout, factorytest.PublicWebhookURL+"/webhooks/generic/"+id[1]) ||
		!strings.Contains(made.Stdout, "one time") {
		t.Fatalf("senders create:\n%s", made.Stdout)
	}

	for _, p := range []*factorytest.Person{dana, ana} {
		list := mustCLI(t, p, "senders", "list")
		if strings.Contains(list.Stdout, secret[1]) || !strings.Contains(list.Stdout, "Checkout alerts") ||
			!strings.Contains(list.Stdout, id[1]) || !strings.Contains(list.Stdout, "shop") {
			t.Errorf("senders list:\n%s", list.Stdout)
		}
		a := p.API("GET", "/api/v1/senders", nil)
		if a.Status != http.StatusOK || strings.Contains(string(a.Body), secret[1]) || strings.Contains(string(a.Body), `"secret"`) {
			t.Errorf("GET /senders: %d %s", a.Status, a.Body)
		}
	}

	// The secret that the CLI showed signs the sender's deliveries.
	path := "/webhooks/generic/" + id[1]
	accepted(t, sendGeneric(t, env, path, secret[1], "d-1", shopDelivery()))
	if list := mustCLI(t, ana, "senders", "list"); strings.Contains(list.Stdout, "never") {
		t.Errorf("senders list after a delivery shows no time of the last delivery:\n%s", list.Stdout)
	}

	mustCLI(t, dana, "senders", "delete", id[1])
	if a := sendGeneric(t, env, path, secret[1], "d-2", shopDelivery()); a.Status != http.StatusNotFound {
		t.Errorf("a delivery of a deleted sender: %d %s, want 404", a.Status, a.Body)
	}
	if list := mustCLI(t, ana, "senders", "list"); strings.Contains(list.Stdout, id[1]) {
		t.Errorf("senders list after the delete:\n%s", list.Stdout)
	}
}

func TestAMemberWhoIsNotAnAdminCannotMakeOrDeleteASender(t *testing.T) {
	env := factorytest.New(t)
	ana := member(t, env)
	sender, _ := env.Sender("Alerts")

	if a := ana.API("POST", "/api/v1/senders", dispatcherapi.SaveSender{Name: "Mine"}); a.Status != http.StatusForbidden {
		t.Errorf("a member makes a sender: %d %s, want 403", a.Status, a.Body)
	}
	if a := ana.API("DELETE", "/api/v1/senders/"+sender.ID, nil); a.Status != http.StatusForbidden {
		t.Errorf("a member deletes a sender: %d %s, want 403", a.Status, a.Body)
	}
	if r := ana.CLI("senders", "create", "Mine"); r.Code != 1 || !strings.Contains(r.Stderr, "Admin") {
		t.Errorf("senders create as a member: exit %d %s", r.Code, r.Stderr)
	}
	if list := mustCLI(t, ana, "senders", "list"); !strings.Contains(list.Stdout, sender.ID) || strings.Contains(list.Stdout, "Mine") {
		t.Errorf("senders list:\n%s", list.Stdout)
	}
}

func TestASenderNeedsANameOnOneLineAndADeleteOfNoSuchSenderGets404(t *testing.T) {
	env := factorytest.New(t)
	dana := admin(t, env)
	for _, name := range []string{"", "  ", "two\nlines", strings.Repeat("n", 101)} {
		if a := dana.API("POST", "/api/v1/senders", dispatcherapi.SaveSender{Name: name}); a.Status != http.StatusBadRequest {
			t.Errorf("the name %q: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	if a := dana.API("DELETE", "/api/v1/senders/nosuchsender", nil); a.Status != http.StatusNotFound {
		t.Errorf("delete of no such sender: %d %s, want 404", a.Status, a.Body)
	}
}
