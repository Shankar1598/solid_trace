package dispatcher_test

import (
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/forge"
)

func TestTheOperatorSetsTheShortestIntervalOfASchedule(t *testing.T) {
	valid := map[string]string{
		dispatcher.EnvSolidTraceURL:   "https://console.example.com",
		dispatcher.EnvSolidTraceToken: "stk_x",
		dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
		dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	}
	load := func(value string) (dispatcher.Config, error) {
		env := map[string]string{dispatcher.EnvScheduleMinInterval: value}
		for k, v := range valid {
			env[k] = v
		}
		return dispatcher.LoadConfig("", func(k string) string { return env[k] })
	}
	if c, err := load("15m"); err != nil || c.ScheduleMinInterval != 15*time.Minute {
		t.Errorf("15m: %v, %v", c.ScheduleMinInterval, err)
	}
	for _, wrong := range []string{"an hour", "-1h", "0"} {
		if _, err := load(wrong); err == nil || !strings.Contains(err.Error(), dispatcher.EnvScheduleMinInterval) {
			t.Errorf("%q: the error %v does not name %s", wrong, err, dispatcher.EnvScheduleMinInterval)
		}
	}
}

// Each case has one wrong value; the error names it, and only it.
func TestTheConfigurationNamesTheOneWrongForgeSetting(t *testing.T) {
	valid := map[string]string{
		dispatcher.EnvSolidTraceURL:   "https://console.example.com",
		dispatcher.EnvSolidTraceToken: "stk_x",
		dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
		dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	}
	cases := []struct{ name, value string }{
		{dispatcher.EnvWIPRefs, "refs/tags/wip"},
		{dispatcher.EnvBitbucketAPIURL, "api.bitbucket.example"},
		{dispatcher.EnvBitbucketGitURL, "ftp://bitbucket.example"},
		{dispatcher.EnvIngestURL, "ingest.example.com:4443"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := map[string]string{c.name: c.value}
			for k, v := range valid {
				env[k] = v
			}
			_, err := dispatcher.LoadConfig("", func(k string) string { return env[k] })
			if err == nil || !strings.Contains(err.Error(), c.name) {
				t.Fatalf("the error %v does not name %s", err, c.name)
			}
			for _, other := range cases {
				if other.name != c.name && strings.Contains(err.Error(), other.name) {
					t.Errorf("the error %q names %s, which is valid", err, other.name)
				}
			}
		})
	}
	for _, place := range []string{forge.WIPOutsideBranches, forge.WIPInBranches, ""} {
		env := map[string]string{dispatcher.EnvWIPRefs: place}
		for k, v := range valid {
			env[k] = v
		}
		if _, err := dispatcher.LoadConfig("", func(k string) string { return env[k] }); err != nil {
			t.Errorf("the place %q: %v", place, err)
		}
	}
}

func TestTheSquidAllowlistNeedsTheDefaultList(t *testing.T) {
	env := map[string]string{
		dispatcher.EnvSolidTraceURL:   "https://console.example.com",
		dispatcher.EnvSolidTraceToken: "stk_x",
		dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
		dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		dispatcher.EnvSquidAllowlist:  "/etc/squid/allowlist.txt",
	}
	getenv := func(k string) string { return env[k] }
	if _, err := dispatcher.LoadConfig("", getenv); err == nil || !strings.Contains(err.Error(), dispatcher.EnvDefaultAllowlist) {
		t.Errorf("with no default list: %v", err)
	}
	env[dispatcher.EnvDefaultAllowlist] = "/etc/factory/allowlist.txt"
	env[dispatcher.EnvSquidReload] = "systemctl reload squid"
	c, err := dispatcher.LoadConfig("", getenv)
	if err != nil || c.SquidAllowlist != "/etc/squid/allowlist.txt" || c.DefaultAllowlist != "/etc/factory/allowlist.txt" ||
		c.SquidReload != "systemctl reload squid" {
		t.Errorf("%+v %v", c, err)
	}
}
