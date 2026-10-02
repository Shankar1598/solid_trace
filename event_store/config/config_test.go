package config_test

import (
	"testing"

	"github.com/solidtrace/event_store/config"
)

// loadFrom loads the config the way the deploy does: from the file named by
// EVENT_STORE_CONFIG.
func loadFrom(t *testing.T, path string) *config.Config {
	t.Helper()
	t.Setenv("EVENT_STORE_CONFIG", path)
	return config.Load()
}

func TestDeployConfigIngestsOnTCPPort4000(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if cfg.Port != "4000" {
		t.Errorf("ingest port = %q, want 4000", cfg.Port)
	}
	if cfg.SocketPath != "" {
		t.Errorf("ingest socket = %q, want none", cfg.SocketPath)
	}
}
