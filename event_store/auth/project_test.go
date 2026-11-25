package auth

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// projectKeysDatabase is a Console database with the columns that
// ValidateKey reads: Project 1 is an ordinary Project, 2 an Agent Project
// with two Captured repositories, and 3 an Agent Project with none. Key
// orphan has no Project row.
func projectKeysDatabase(t *testing.T) *ProjectAuth {
	t.Helper()
	path := filepath.Join(t.TempDir(), "console.sqlite3")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		"CREATE TABLE projects (id INTEGER PRIMARY KEY, agent BOOLEAN NOT NULL DEFAULT 0, captured_repositories JSON NOT NULL DEFAULT '[]')",
		"CREATE TABLE project_keys (project_id INTEGER, public_key TEXT)",
		`INSERT INTO projects (id, agent, captured_repositories) VALUES
			(1, 0, '[]'), (2, 1, '["github.com/acme/shop","bitbucket.org/acme/*"]'), (3, 1, '[]')`,
		`INSERT INTO project_keys (project_id, public_key) VALUES
			(1, 'ordinary'), (2, 'agent'), (3, 'agent-empty'), (9, 'orphan')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	projectAuth, err := NewProjectAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projectAuth.Close)
	return projectAuth
}

func TestValidateKeyGivesTheAgentProjectWithItsCapturedRepositories(t *testing.T) {
	projectAuth := projectKeysDatabase(t)
	for _, c := range []struct {
		key  string
		want ProjectKey
	}{
		{"ordinary", ProjectKey{ProjectID: 1}},
		{"agent", ProjectKey{ProjectID: 2, Agent: true, CapturedRepositories: []string{"github.com/acme/shop", "bitbucket.org/acme/*"}}},
		{"agent-empty", ProjectKey{ProjectID: 3, Agent: true, CapturedRepositories: []string{}}},
		{"orphan", ProjectKey{ProjectID: 9}},
		{"unknown", ProjectKey{}},
	} {
		got, err := projectAuth.ValidateKey(c.key)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ValidateKey(%q) = %+v, %v; want %+v", c.key, got, err, c.want)
		}
	}
}
