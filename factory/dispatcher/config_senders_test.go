package dispatcher_test

import (
	"strings"
	"testing"

	"github.com/solidtrace/factory/dispatcher"
)

func TestTheOperatorSetsTheSenderLimitsAndTheConfigurationRefusesAWrongOne(t *testing.T) {
	valid := map[string]string{
		dispatcher.EnvSolidTraceURL:   "https://console.example.com",
		dispatcher.EnvSolidTraceToken: "stk_x",
		dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
		dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	}
	load := func(more map[string]string) (dispatcher.Config, error) {
		env := map[string]string{}
		for k, v := range valid {
			env[k] = v
		}
		for k, v := range more {
			env[k] = v
		}
		return dispatcher.LoadConfig("", func(k string) string { return env[k] })
	}

	c, err := load(map[string]string{dispatcher.EnvSenderStartsPerMinute: "30", dispatcher.EnvSenderActiveRuns: "2"})
	if err != nil || c.SenderStartsPerMinute != 30 || c.SenderActiveRuns != 2 {
		t.Errorf("limits 30 and 2: %+v, %v", c, err)
	}
	if c, err := load(nil); err != nil || c.SenderStartsPerMinute != 0 || c.SenderActiveRuns != 0 {
		t.Errorf("no limits (the defaults): %v, %v", c.SenderStartsPerMinute, err)
	}
	for _, name := range []string{dispatcher.EnvSenderStartsPerMinute, dispatcher.EnvSenderActiveRuns} {
		for _, value := range []string{"0", "-1", "ten"} {
			if _, err := load(map[string]string{name: value}); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%s: %v, want an error that names it", name, value, err)
			}
		}
	}
}
