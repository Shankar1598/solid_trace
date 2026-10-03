package main_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// seedProjectSlug adds a Project's row, with its slug, to the Console
// database.
func seedProjectSlug(t *testing.T, sqlitePath string, projectID uint32, slug string) {
	t.Helper()
	db, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite for seeding: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		"INSERT INTO projects (id, name, slug, organization_id, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
		projectID, slug, slug, time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to seed project %d: %v", projectID, err)
	}
}

// sourceValidation is what validate says of a Query's source.
type sourceValidation struct {
	Errors []queryProblem `json:"errors"`
	Source *string        `json:"source"`
}

func validateSource(t *testing.T, env *TestEnv, projectID uint32, query string) sourceValidation {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/logs/validate", projectID), map[string]string{"query": query})
	if status != 200 {
		t.Fatalf("Validate %q expected 200, got %d: %s", query, status, raw)
	}
	var v sourceValidation
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("Decode validation %q: %v", raw, err)
	}
	return v
}

func TestAQueryNamesItsLogsByLogsOrTheProjectsSlug(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectSlug(t, env.SQLitePath, 123, "my-api")

	sources := map[string]string{
		`my-api`:                             "my-api",
		"my-api\n| where level == \"error\"": "my-api",
		"  my-api // the API\n| search \"timeout\"": "my-api",
		`my-api|where status - 1 == 199`:            "my-api",
		`['my-api'] | where level == "error"`:       "my-api",
		`logs | where level == "error"`:             "logs",
		`where level == "error"`:                    "",
		`count`:                                     "",
	}
	for query, want := range sources {
		v := validateSource(t, env, 123, query)
		got := ""
		if v.Source != nil {
			got = *v.Source
		}
		if len(v.Errors) != 0 || got != want {
			t.Errorf("%q: expected source %q and no errors, got %q and %+v", query, want, got, v.Errors)
		}
	}

	// Another name is a mistake at the name, though validate still gives it,
	// so the explorer can open the Project it names.
	others := map[string]queryProblem{
		`other-api | where level == "error"`: {Line: 1, Column: 1, EndLine: 1, EndColumn: 10},
		"2fa\n| take 5":                      {Line: 1, Column: 1, EndLine: 1, EndColumn: 4},
		`['other api']`:                      {Line: 1, Column: 1, EndLine: 1, EndColumn: 14},
	}
	for query, want := range others {
		v := validateSource(t, env, 123, query)
		if len(v.Errors) != 1 || v.Source == nil {
			t.Errorf("%q: expected one mistake and the source, got %+v and %v", query, v.Errors, v.Source)
			continue
		}
		got := v.Errors[0]
		want.Message = fmt.Sprintf("expected my-api, this Project's slug, or logs, got %q", *v.Source)
		if got != want {
			t.Errorf("%q: expected %+v, got %+v", query, want, got)
		}
	}

	status, raw := postQueryApp(t, env, "/api/123/logs/query",
		queryRequest{Query: `other-api | where level == "error"`, From: allTime.from, To: allTime.to})
	if status != 400 {
		t.Errorf("A run of another Project's name: expected 400, got %d: %s", status, raw)
	}
	runQuery(t, env, 123, queryRequest{Query: `my-api | where level == "error"`, From: allTime.from, To: allTime.to})

	// A Project Console doesn't know has no slug to check against.
	if v := validateSource(t, env, 456, `other-api | where level == "error"`); len(v.Errors) != 0 {
		t.Errorf("Expected no mistakes for a Project Console doesn't know, got %+v", v.Errors)
	}
}
