package dispatcher

import (
	"database/sql"
	"errors"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// storedSettings are one Project's Factory settings with the SolidTrace
// Project id that keys them.
type storedSettings struct {
	dispatcherapi.FactorySettings
	projectID string
}

const settingsColumns = `s.project_id, s.project_slug, s.repository, s.folder, s.base_branch, COALESCE(c.name, ''),
	s.orphaned_at IS NOT NULL, s.updated_at, s.updated_by_id, s.updated_by_name`

const settingsFrom = ` FROM factory_settings s LEFT JOIN connections c ON c.id = s.connection_id `

func scanSettings(row interface{ Scan(...any) error }) (storedSettings, error) {
	var s storedSettings
	var updated int64
	err := row.Scan(&s.projectID, &s.Project, &s.Repository, &s.Folder, &s.BaseBranch, &s.Connection, &s.Orphaned,
		&updated, &s.UpdatedBy.ID, &s.UpdatedBy.Name)
	s.UpdatedAt = time.UnixMicro(updated).UTC()
	return s, err
}

// settings reads the Factory settings of the Organization's Project.
func (s *store) settings(organizationID, projectID string) (storedSettings, error) {
	settings, err := scanSettings(s.db.QueryRow(`SELECT `+settingsColumns+settingsFrom+
		`WHERE s.organization_id = ? AND s.project_id = ?`, organizationID, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return settings, errNotFound
	}
	return settings, err
}

// allSettings reads the Factory settings of each Project of the
// Organization, by repository and folder.
func (s *store) allSettings(organizationID string) ([]storedSettings, error) {
	rows, err := s.db.Query(`SELECT `+settingsColumns+settingsFrom+`WHERE s.organization_id = ?
		ORDER BY s.repository, s.folder, s.project_slug`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []storedSettings
	for rows.Next() {
		settings, err := scanSettings(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, settings)
	}
	return all, rows.Err()
}

// saveSettings replaces the Factory settings of a Project. A save is of a
// Project that SolidTrace has, so it is no longer orphaned.
func (s *store) saveSettings(organizationID, projectID, slug string, save dispatcherapi.SaveFactorySettings,
	connectionID string, by person, at time.Time) error {
	connection := sql.NullString{String: connectionID, Valid: connectionID != ""}
	_, err := s.db.Exec(`INSERT INTO factory_settings (project_id, organization_id, project_slug, repository, folder,
		base_branch, connection_id, orphaned_at, updated_at, updated_by_id, updated_by_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)
		ON CONFLICT (project_id) DO UPDATE SET organization_id = excluded.organization_id,
			project_slug = excluded.project_slug, repository = excluded.repository, folder = excluded.folder,
			base_branch = excluded.base_branch, connection_id = excluded.connection_id, orphaned_at = NULL,
			updated_at = excluded.updated_at, updated_by_id = excluded.updated_by_id,
			updated_by_name = excluded.updated_by_name`,
		projectID, organizationID, slug, save.Repository, save.Folder, save.BaseBranch, connection,
		at.UnixMicro(), by.id, by.name)
	return err
}

// followSlug keeps the slug that SolidTrace gives now for a Project.
func (s *store) followSlug(projectID, slug string) error {
	_, err := s.db.Exec(`UPDATE factory_settings SET project_slug = ? WHERE project_id = ? AND project_slug != ?`,
		slug, projectID, slug)
	return err
}

// orphan marks the Factory settings of the Projects as orphaned, if they
// are not orphaned yet. It returns the slugs that it marked.
func (s *store) orphan(organizationID string, projectIDs []string, at time.Time) ([]string, error) {
	var marked []string
	for _, id := range projectIDs {
		var slug string
		err := s.db.QueryRow(`UPDATE factory_settings SET orphaned_at = ?
			WHERE organization_id = ? AND project_id = ? AND orphaned_at IS NULL RETURNING project_slug`,
			at.UnixMicro(), organizationID, id).Scan(&slug)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return marked, err
		}
		marked = append(marked, slug)
	}
	return marked, nil
}

// connectionID is the id of the Organization's connection with name.
func (s *store) connectionID(organizationID, name string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM connections WHERE organization_id = ? AND name = ?`,
		organizationID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return id, err
}

func connectionPurpose(id string) string { return "connection " + id }

// saveConnection makes the Organization's connection with name, or
// replaces its kind and token. The token is sealed.
func (s *store) saveConnection(organizationID, name, kind, token string, by person, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRow(`SELECT id FROM connections WHERE organization_id = ? AND name = ?`, organizationID, name).Scan(&id)
	made := errors.Is(err, sql.ErrNoRows)
	switch {
	case made:
		id = newRunID()
		_, err = tx.Exec(`INSERT INTO connections (id, organization_id, name, kind, token, updated_at, updated_by_id,
			updated_by_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, organizationID, name, kind,
			s.sealer.seal(connectionPurpose(id), []byte(token)), at.UnixMicro(), by.id, by.name)
	case err == nil:
		_, err = tx.Exec(`UPDATE connections SET kind = ?, token = ?, updated_at = ?, updated_by_id = ?,
			updated_by_name = ? WHERE id = ?`, kind, s.sealer.seal(connectionPurpose(id), []byte(token)),
			at.UnixMicro(), by.id, by.name, id)
	}
	if err != nil {
		return false, err
	}
	return made, tx.Commit()
}

// connections reads the Organization's connections, by name, with no
// token.
func (s *store) connections(organizationID string) ([]dispatcherapi.Connection, error) {
	rows, err := s.db.Query(`SELECT name, kind, updated_at, updated_by_id, updated_by_name FROM connections
		WHERE organization_id = ? ORDER BY name`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connections := []dispatcherapi.Connection{}
	for rows.Next() {
		var c dispatcherapi.Connection
		var updated int64
		if err := rows.Scan(&c.Name, &c.Kind, &updated, &c.UpdatedBy.ID, &c.UpdatedBy.Name); err != nil {
			return nil, err
		}
		c.UpdatedAt = time.UnixMicro(updated).UTC()
		connections = append(connections, c)
	}
	return connections, rows.Err()
}

// connectionToken opens the token of the Organization's connection with
// name. The Forge reads it for a Run.
func (s *store) connectionToken(organizationID, name string) (string, error) {
	var id string
	var sealed []byte
	err := s.db.QueryRow(`SELECT id, token FROM connections WHERE organization_id = ? AND name = ?`,
		organizationID, name).Scan(&id, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	token, err := s.sealer.open(connectionPurpose(id), sealed)
	return string(token), err
}
