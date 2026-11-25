package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type ProjectAuth struct {
	db *sql.DB
}

func NewProjectAuth(sqlitePath string) (*ProjectAuth, error) {
	db, err := sql.Open("sqlite3", sqlitePath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	return &ProjectAuth{db: db}, nil
}

// ProjectKey is what a Project key gives, as Console's tables say at the
// time of the request (ADR 0001).
type ProjectKey struct {
	ProjectID uint32
	// Agent is true for a key of an Organization's Agent Project. That key
	// works on the OTLP routes only, and stores only the records of
	// CapturedRepositories, the Agent Project's list as Console saved it.
	Agent                bool
	CapturedRepositories []string
}

// ValidateKey returns the Project of a key, with ProjectID 0 for an unknown
// key. The Agent Project and its Captured repositories are read with the
// key, so a change to the list applies to the next request.
func (a *ProjectAuth) ValidateKey(publicKey string) (ProjectKey, error) {
	var (
		key   ProjectKey
		agent bool
		list  sql.NullString
	)
	err := a.db.QueryRow(`
		SELECT k.project_id, COALESCE(p.agent, 0), p.captured_repositories
		FROM project_keys k LEFT JOIN projects p ON p.id = k.project_id
		WHERE k.public_key = ?`,
		publicKey,
	).Scan(&key.ProjectID, &agent, &list)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectKey{}, nil
	}
	if err != nil {
		return ProjectKey{}, err
	}
	if !agent {
		return key, nil
	}
	key.Agent = true
	if err := json.Unmarshal([]byte(list.String), &key.CapturedRepositories); err != nil {
		return ProjectKey{}, fmt.Errorf("captured repositories of Project %d: %w", key.ProjectID, err)
	}
	return key, nil
}

func (a *ProjectAuth) Close() {
	a.db.Close()
}
