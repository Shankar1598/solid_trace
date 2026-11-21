package dispatcher_test

import (
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/dispatcher"
)

// The operator sets the interval of the checkpoint: 5 minutes when it
// sets none, and 0 turns the checkpoint off.
func TestTheOperatorSetsTheIntervalOfTheCheckpoint(t *testing.T) {
	load := func(value string) (dispatcher.Config, error) {
		env := map[string]string{
			dispatcher.EnvSolidTraceURL:   "https://console.example.com",
			dispatcher.EnvSolidTraceToken: "stk_x",
			dispatcher.EnvDatabase:        "/var/lib/dispatcher/d.sqlite3",
			dispatcher.EnvEncryptionKey:   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
		}
		if value != "" {
			env[dispatcher.EnvCheckpointInterval] = value
		}
		return dispatcher.LoadConfig("", func(k string) string { return env[k] })
	}
	for value, want := range map[string]time.Duration{"": 5 * time.Minute, "10m": 10 * time.Minute, "0": 0, "0s": 0} {
		if c, err := load(value); err != nil || c.CheckpointInterval != want {
			t.Errorf("%q: %v, %v, want %v", value, c.CheckpointInterval, err, want)
		}
	}
	for _, wrong := range []string{"five minutes", "-5m", "10"} {
		if _, err := load(wrong); err == nil || !strings.Contains(err.Error(), dispatcher.EnvCheckpointInterval) {
			t.Errorf("%q: the error %v does not name %s", wrong, err, dispatcher.EnvCheckpointInterval)
		}
	}
}
