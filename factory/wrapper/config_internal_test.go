package wrapper

import (
	"strings"
	"testing"
)

// The end deletes the workspace and the home: a configuration that makes
// one of them the root, a relative path, or a folder in another of the
// three is refused, with the name of the variable.
func TestAWorkspaceHomeOrCacheThatTheEndCouldDeleteTooMuchOfIsRefused(t *testing.T) {
	base := map[string]string{EnvDispatcherURL: "http://127.0.0.1:1", EnvExecution: "e-1", EnvIdentityFile: "/run/identity",
		EnvWorkspace: "/srv/workspace", EnvHome: "/srv/home", EnvCache: "/srv/cache"}
	load := func(changes map[string]string) error {
		env := map[string]string{}
		for k, v := range base {
			env[k] = v
		}
		for k, v := range changes {
			env[k] = v
		}
		_, err := loadConfig(func(k string) string { return env[k] })
		return err
	}
	if err := load(nil); err != nil {
		t.Fatalf("the base configuration: %v", err)
	}
	cases := []struct {
		name    string
		changes map[string]string
		names   string
	}{
		{"the workspace is the root", map[string]string{EnvWorkspace: "/"}, EnvWorkspace},
		{"the home is the root", map[string]string{EnvHome: "/"}, EnvHome},
		{"the cache is the root", map[string]string{EnvCache: "//"}, EnvCache},
		{"the home is a relative path", map[string]string{EnvHome: "relative/path"}, EnvHome},
		{"the cache is the home", map[string]string{EnvCache: "/srv/home"}, EnvCache},
		{"the workspace is in the home", map[string]string{EnvWorkspace: "/srv/home/workspace"}, EnvWorkspace},
		{"the home is in the workspace", map[string]string{EnvHome: "/srv/workspace/home/"}, EnvHome},
		{"the cache is in the workspace", map[string]string{EnvCache: "/srv/workspace/cache"}, EnvCache},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := load(c.changes)
			if err == nil || !strings.Contains(err.Error(), c.names) {
				t.Errorf("loadConfig: %v, want an error that names %s", err, c.names)
			}
		})
	}
}
