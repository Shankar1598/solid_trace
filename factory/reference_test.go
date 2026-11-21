package factory

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/solidtrace/factory/dispatcher"
)

// The reference configuration's example is the operator's start: the Dispatcher must
// accept it, once the operator sets the two secrets, and read each value as the
// operator guide says.
func TestTheReferenceConfigurationStartsTheDispatcher(t *testing.T) {
	secrets := map[string]string{
		"DISPATCHER_SOLIDTRACE_TOKEN": "stk_example",
		"DISPATCHER_NOMAD_TOKEN":      "nomad-example",
		// The image's own setting (factory/Dockerfile): the example does not name it.
		"DISPATCHER_DATABASE": "/var/lib/dispatcher/dispatcher.sqlite3",
		// 32 bytes, as 'openssl rand -base64 32' makes.
		"DISPATCHER_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}
	config, err := dispatcher.LoadConfig(filepath.Join("reference", "dispatcher.env.example"),
		func(name string) string { return secrets[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.Listen != "10.100.0.3:7600" {
		t.Errorf("the private listener is %q, want the VM's private address", config.Listen)
	}
	if config.WebhookListen != "127.0.0.1:7601" || config.WebhookURL != "https://factory.example.com" {
		t.Errorf("the webhook listener is %q with the public URL %q", config.WebhookListen, config.WebhookURL)
	}
	if config.NomadAddress != "http://10.100.0.3:4646" || len(config.MachineKinds) != 1 || config.MachineKinds[0].Name != "standard" {
		t.Errorf("Nomad %q, machine kinds %v", config.NomadAddress, config.MachineKinds)
	}
	// Console does not serve the agent ingest: with no ingest URL, no Agent session arrives.
	if config.SolidTraceIngestURL != "https://solidtrace.example.com:4443" {
		t.Errorf("the ingest URL is %q, want SolidTrace's ingest on port 4443", config.SolidTraceIngestURL)
	}
	if config.SquidAllowlist != "/etc/squid/factory/allowlist.txt" || config.SquidReload != "true" {
		t.Errorf("Squid's allowlist %q, reload %q", config.SquidAllowlist, config.SquidReload)
	}
}

// Each name in the example, set or commented out, is a setting that the Dispatcher knows.
func TestEachNameInTheReferenceConfigurationIsASettingOfTheDispatcher(t *testing.T) {
	example, err := os.ReadFile(filepath.Join("reference", "dispatcher.env.example"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join("dispatcher", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile(`(?m)^#? ?(DISPATCHER_[A-Z_]+)=`).FindAllStringSubmatch(string(example), -1)
	if len(names) < 20 {
		t.Fatalf("the example has %d settings; the check reads the wrong lines", len(names))
	}
	for _, name := range names {
		if !strings.Contains(string(source), `"`+name[1]+`"`) {
			t.Errorf("%s is not a setting of the Dispatcher (dispatcher/config.go)", name[1])
		}
	}
}

// HashiCorp's Nomad package puts /etc/nomad.d/nomad.hcl in place, which turns on a server and
// a client. Nomad merges the files of the directory, and "enabled = false" in another file
// does not override a true. So the guide and the Nomad files must tell the operator to empty
// the package's file.
func TestTheGuideAndTheNomadFilesTellTheOperatorToEmptyThePackagesConfiguration(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "docs", "factory.md"),
		filepath.Join("reference", "nomad", "server.hcl"),
		filepath.Join("reference", "nomad", "client.hcl"),
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), ": > /etc/nomad.d/nomad.hcl") {
			t.Errorf("%s does not tell the operator to empty /etc/nomad.d/nomad.hcl", path)
		}
	}
	guide, err := os.ReadFile(filepath.Join("..", "docs", "factory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(guide), ": > /etc/nomad.d/nomad.hcl"); n < 2 {
		t.Errorf("the guide tells the operator %d time to empty nomad.hcl; the server VM and the agent VM both need it", n)
	}
}
