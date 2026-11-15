package dispatcher_test

import (
	"strings"
	"testing"

	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/forge"
)

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
