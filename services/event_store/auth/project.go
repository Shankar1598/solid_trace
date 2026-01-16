package auth

import (
	"database/sql"

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

// ValidateKey returns project_id if key is valid, 0 otherwise
func (a *ProjectAuth) ValidateKey(publicKey string) (uint32, error) {
	var projectID uint32
	err := a.db.QueryRow(
		"SELECT project_id FROM project_keys WHERE public_key = ?",
		publicKey,
	).Scan(&projectID)

	if err == sql.ErrNoRows {
		return 0, nil
	}
	return projectID, err
}

func (a *ProjectAuth) Close() {
	a.db.Close()
}
