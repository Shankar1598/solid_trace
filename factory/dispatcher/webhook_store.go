package dispatcher

import (
	"database/sql"
	"errors"
	"time"
)

func webhookSecretPurpose(id string) string { return "webhook secret " + id }

// saveWebhookSecret seals and keeps the webhook secret of the
// Organization's connection with name.
func (s *store) saveWebhookSecret(organizationID, name, secret string) error {
	id, err := s.connectionID(organizationID, name)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE connections SET webhook_secret = ? WHERE id = ?`,
		s.sealer.seal(webhookSecretPurpose(id), []byte(secret)), id)
	return err
}

const webhookSourceColumns = `id, name, kind, organization_id, last_webhook_at`

func scanWebhookSource(row interface{ Scan(...any) error }) (WebhookSource, error) {
	var source WebhookSource
	var lastWebhook sql.NullInt64
	err := row.Scan(&source.ID, &source.Name, &source.Kind, &source.organizationID, &lastWebhook)
	if lastWebhook.Valid {
		source.LastWebhook = time.UnixMicro(lastWebhook.Int64).UTC()
	}
	return source, err
}

// scanWith scans a row's columns and then more.
type scanWith struct {
	row  interface{ Scan(...any) error }
	more any
}

func (s scanWith) Scan(dest ...any) error { return s.row.Scan(append(dest, s.more)...) }

// webhookSource reads the connection with id, and opens its webhook
// secret (nil when it has none).
func (s *store) webhookSource(id string) (WebhookSource, []byte, error) {
	var sealed []byte
	source, err := scanWebhookSource(scanWith{s.db.QueryRow(`SELECT `+webhookSourceColumns+`, webhook_secret
		FROM connections WHERE id = ?`, id), &sealed})
	if errors.Is(err, sql.ErrNoRows) {
		return source, nil, errNotFound
	}
	if err != nil || sealed == nil {
		return source, nil, err
	}
	secret, err := s.sealer.open(webhookSecretPurpose(id), sealed)
	return source, secret, err
}

// webhookSources reads each connection, with no token.
func (s *store) webhookSources() ([]WebhookSource, error) {
	rows, err := s.db.Query(`SELECT ` + webhookSourceColumns + ` FROM connections ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []WebhookSource
	for rows.Next() {
		source, err := scanWebhookSource(rows)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

// withToken reads the connection with id, and opens its token.
func (s *store) withToken(id string) (WebhookSource, error) {
	source, _, err := s.webhookSource(id)
	if err != nil {
		return source, err
	}
	_, token, err := s.connection(source.organizationID, source.Name)
	source.Token = token
	return source, err
}

// dedupeFor is how long the Dispatcher keeps a delivery id.
const dedupeFor = 7 * 24 * time.Hour

// keepDelivery keeps the time of a verified webhook of the connection,
// and its delivery id for dedupeFor. It is false when the connection had
// the delivery id in the last dedupeFor: a repeat that does nothing.
func (s *store) keepDelivery(connectionID, delivery string, at time.Time) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM webhook_deliveries WHERE received_at < ?`, at.Add(-dedupeFor).UnixMicro()); err != nil {
		return false, err
	}
	result, err := tx.Exec(`INSERT INTO webhook_deliveries (connection_id, delivery, received_at) VALUES (?, ?, ?)
		ON CONFLICT DO NOTHING`, connectionID, delivery, at.UnixMicro())
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE connections SET last_webhook_at = ? WHERE id = ?`, at.UnixMicro(), connectionID); err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, tx.Commit()
}
