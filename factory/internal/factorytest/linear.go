package factorytest

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/fakelinear"
)

// LinearCLI runs the solidtrace CLI with stdin as its input, on a machine
// whose browser approves on the fake Linear (Env.Linear) and whose CLI
// opens the fake's approval page.
func (p *Person) LinearCLI(stdin string, args ...string) Result {
	return p.runCLI("", stdin, func(env *command.Env) {
		env.OpenBrowser = p.env.Linear.Browser
		env.LinearAuthorizeURL = p.env.Linear.AuthorizeURL()
	}, args...)
}

// LinearCallbackPort registers the callback URL of a free loopback port in
// the fake Linear's app, and gives the port, for the CLI's --port.
func (e *Env) LinearCallbackPort() string {
	e.t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		e.t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	e.Linear.Register("http://localhost:" + port + "/callback")
	return port
}

// LinearConnection installs the fake Linear's app as the connection with
// name, as an Admin with the CLI, with secret as its webhook secret ("" is
// none). It returns the path of its webhook URL.
func (e *Env) LinearConnection(name, secret string) string {
	e.t.Helper()
	port := e.LinearCallbackPort()
	r := e.Admin().LinearCLI(fakelinear.ClientSecret+"\n", "connections", "add", "linear", name,
		"--client-id", fakelinear.ClientID, "--port", port, "--dispatcher", e.URL())
	if r.Code != 0 {
		e.t.Fatalf("install the Linear connection %s: exit %d\n%s%s", name, r.Code, r.Stdout, r.Stderr)
	}
	var c dispatcherapi.Connection
	if secret != "" {
		a := e.admin.API("PUT", "/api/v1/connections/"+name+"/webhook-secret", dispatcherapi.SaveWebhookSecret{Secret: secret})
		if a.Status != http.StatusOK {
			e.t.Fatalf("save the webhook secret of %s: %d %s", name, a.Status, a.Body)
		}
		a.JSON(e.t, &c)
	} else {
		a := e.admin.API("GET", "/api/v1/connections", nil)
		var list dispatcherapi.Connections
		a.JSON(e.t, &list)
		for _, each := range list.Connections {
			if each.Name == name {
				c = each
			}
		}
	}
	path, ok := strings.CutPrefix(c.WebhookURL, PublicWebhookURL)
	if !ok || !strings.HasPrefix(path, "/webhooks/linear/") {
		e.t.Fatalf("the connection %s has the webhook URL %q, not one under %s/webhooks/linear/", name, c.WebhookURL,
			PublicWebhookURL)
	}
	return path
}

// Dispatcher is the running Dispatcher.
func (e *Env) Dispatcher() *dispatcher.Dispatcher { return e.current.Load() }

// LinearWebhook sends a Linear webhook of the kind (Issue, Comment) about
// the issue to path, at the clock's time, signed with secret.
func (e *Env) LinearWebhook(path, secret, delivery, kind, issueID string) Answer {
	e.t.Helper()
	header, body := fakelinear.Webhook(secret, delivery, kind, issueID, e.Clock.Now())
	return e.Webhook(path, header, body)
}
