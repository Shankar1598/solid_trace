package dispatcher

import (
	"database/sql"
	"errors"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// storedRoute is a route with the id of its connection.
type storedRoute struct {
	dispatcherapi.Route
	connectionID string
}

// routeTaken is a save of a route whose pair another Project has.
type routeTaken struct {
	route   dispatcherapi.Route
	project string
}

func (e *routeTaken) Error() string {
	return "the Project " + e.project + " has the route (" + e.route.Connection + ", " + quote(e.route.Match) +
		"): two Projects cannot have the same pair"
}

// saveRoutes replaces the routes of the Project in tx. A pair that the
// orphaned Factory settings of another Project have moves to this
// Project; a pair of another Project that is not orphaned is a
// *routeTaken.
func saveRoutes(tx *sql.Tx, projectID string, routes []storedRoute) error {
	for _, r := range routes {
		var slug string
		var orphaned bool
		err := tx.QueryRow(`SELECT s.project_slug, s.orphaned_at IS NOT NULL FROM routes r
			JOIN factory_settings s ON s.project_id = r.project_id
			WHERE r.connection_id = ? AND r.match_value = ? AND r.project_id != ?`,
			r.connectionID, r.Match, projectID).Scan(&slug, &orphaned)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			continue
		case err != nil:
			return err
		case !orphaned:
			return &routeTaken{r.Route, slug}
		}
		if _, err := tx.Exec(`DELETE FROM routes WHERE connection_id = ? AND match_value = ?`,
			r.connectionID, r.Match); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM routes WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	for _, r := range routes {
		if _, err := tx.Exec(`INSERT INTO routes (connection_id, match_value, project_id) VALUES (?, ?, ?)`,
			r.connectionID, r.Match, projectID); err != nil {
			return err
		}
	}
	return nil
}

// routesOf reads the routes of the Organization's Projects, by Project
// id, each list by connection name and match value.
func (s *store) routesOf(organizationID string) (map[string][]dispatcherapi.Route, error) {
	rows, err := s.db.Query(`SELECT r.project_id, c.name, r.match_value FROM routes r
		JOIN connections c ON c.id = r.connection_id WHERE c.organization_id = ?
		ORDER BY c.name, r.match_value`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := map[string][]dispatcherapi.Route{}
	for rows.Next() {
		var projectID string
		var r dispatcherapi.Route
		if err := rows.Scan(&projectID, &r.Connection, &r.Match); err != nil {
			return nil, err
		}
		routes[projectID] = append(routes[projectID], r)
	}
	return routes, rows.Err()
}

// route finds the Project of a ticket of the connection whose routing
// field has value: the route with that match value, else the
// connection's empty route. It is errNotFound when neither exists.
func (s *store) route(connectionID, value string) (string, error) {
	var projectID string
	err := s.db.QueryRow(`SELECT project_id FROM routes WHERE connection_id = ? AND match_value IN (?, '')
		ORDER BY match_value DESC LIMIT 1`, connectionID, value).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return projectID, err
}

// matchValues are the match values of the connection's routes that are
// not empty.
func (s *store) matchValues(connectionID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT match_value FROM routes WHERE connection_id = ? AND match_value != ''
		ORDER BY match_value`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// trackerSettings reads the saved settings of the connection with id.
func (s *store) trackerSettings(connectionID string) (dispatcherapi.TrackerSettings, error) {
	var t dispatcherapi.TrackerSettings
	err := s.db.QueryRow(`SELECT `+trackerColumns+` FROM connections WHERE id = ?`, connectionID).
		Scan(trackerFields(&t)...)
	if errors.Is(err, sql.ErrNoRows) {
		return t, errNotFound
	}
	return t, err
}

// saveTrackerSettings replaces the settings of the Organization's
// connection with name.
func (s *store) saveTrackerSettings(organizationID, name string, t dispatcherapi.TrackerSettings) error {
	_, err := s.db.Exec(`UPDATE connections SET ready_status = ?, routing_field = ?, notion_database = ?, working_status = ?,
		review_status = ?, waiting_status = ? WHERE organization_id = ? AND name = ?`,
		t.ReadyStatus, t.RoutingField, t.Database, t.WorkingStatus, t.ReviewStatus, t.WaitingStatus, organizationID, name)
	return err
}

// trackerColumns are the columns of a connection's tracker settings, in
// the order of trackerFields.
const trackerColumns = `ready_status, routing_field, notion_database, working_status, review_status, waiting_status`

func trackerFields(t *dispatcherapi.TrackerSettings) []any {
	return []any{&t.ReadyStatus, &t.RoutingField, &t.Database, &t.WorkingStatus, &t.ReviewStatus, &t.WaitingStatus}
}

// ticketHasRun is the SQL test that a ticket has an Active Run or a Run of
// its edit (ticketHasRunArgs gives its arguments).
func ticketHasRun() string {
	return `EXISTS (SELECT 1 FROM runs WHERE ticket_connection_id = ? AND ticket_id = ?
		AND (ticket_edit = ? OR ` + activeRunSQL() + `))`
}

func ticketHasRunArgs(connectionID, ticketID, edit string) []any {
	return append([]any{connectionID, ticketID, edit}, activeRunArgs()...)
}

// ticketRun is true when the ticket of the connection has an Active Run
// or a Run of its edit.
func (s *store) ticketRun(connectionID string, t Ticket) (bool, error) {
	var seen bool
	err := s.db.QueryRow(`SELECT `+ticketHasRun(), ticketHasRunArgs(connectionID, t.ID, t.Edit)...).Scan(&seen)
	return seen, err
}

// ticketNoticed is true when the ticket's edit got the "no route" (or the
// orphaned) notice. The notice stops only a second comment or warning for
// the edit: a route that an Admin saves later still starts its Run.
func (s *store) ticketNoticed(connectionID string, t Ticket) (bool, error) {
	var noticed bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM ticket_notices WHERE connection_id = ? AND ticket_id = ? AND edit = ?)`,
		connectionID, t.ID, t.Edit).Scan(&noticed)
	return noticed, err
}

// noticeTicket keeps the ticket's edit as the one that got the "no
// route" notice.
func (s *store) noticeTicket(connectionID string, t Ticket, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO ticket_notices (connection_id, ticket_id, edit, noticed_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (connection_id, ticket_id) DO UPDATE SET edit = excluded.edit, noticed_at = excluded.noticed_at`,
		connectionID, t.ID, t.Edit, at.UnixMicro())
	return err
}

// insertTicketRun adds the queued Run of a ticket, unless the ticket has
// an Active Run or a Run of its edit. It is false when it added none.
func (s *store) insertTicketRun(r storedRun) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var seen bool
	err = tx.QueryRow(`SELECT `+ticketHasRun(), ticketHasRunArgs(r.ticketConnectionID, r.ticketID, r.ticketEdit)...).
		Scan(&seen)
	if err != nil || seen {
		return false, err
	}
	if _, err := insertRun(tx, r); err != nil {
		return false, err
	}
	return true, s.commit(tx)
}
