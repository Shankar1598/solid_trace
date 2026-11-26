package clickhousetest

import (
	"context"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/storage"
)

// OpenClickHouse is storage.NewClickHouse and Prepare, for a test that needs
// the database ready before it goes on.
func OpenClickHouse(cfg config.ClickHouse, bucket *config.ObjectStore) (*storage.ClickHouse, error) {
	ch, err := storage.NewClickHouse(cfg, bucket)
	if err != nil {
		return nil, err
	}
	if err := ch.Prepare(context.Background()); err != nil {
		ch.Close()
		return nil, err
	}
	return ch, nil
}
