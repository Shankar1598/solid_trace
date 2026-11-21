package factory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The factory is off unless the operator turns it on: a SolidTrace install
// (deploy/) starts no Dispatcher, and never builds or pulls its image. The
// Dispatcher has its own image, on the server VM beside the Nomad server.
func TestASolidTraceInstallStartsNoDispatcher(t *testing.T) {
	for _, name := range []string{"docker-compose.yml", "event_store.yml", filepath.Join("install"), filepath.Join("setup-gcp")} {
		body, err := os.ReadFile(filepath.Join("..", "deploy", name))
		if err != nil {
			t.Fatal(err)
		}
		if lower := strings.ToLower(string(body)); strings.Contains(lower, "dispatcher") {
			t.Errorf("deploy/%s names the Dispatcher: the factory is off in a SolidTrace install", name)
		}
	}
}
