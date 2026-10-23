package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/logger"
)

// ClickHouse is EventStore's database on the ClickHouse server beside it.
type ClickHouse struct {
	conn     driver.Conn
	database string
}

// clickHouseMigration is one step of making EventStore's tables. Its
// statements must be safe to run again (CREATE … IF NOT EXISTS): a step whose
// record wasn't written runs again at the next start.
type clickHouseMigration struct {
	version    uint32
	statements []string
}

// clickHouseMigrations are the steps, in version order. Append a step; never
// change one that has shipped.
var clickHouseMigrations = []clickHouseMigration{
	logsTableMigration,
	eventsTableMigration,
}

// clickHousePingTimeout bounds a health check of ClickHouse.
const clickHousePingTimeout = 3 * time.Second

// OpenClickHouse connects to the ClickHouse server in cfg and makes
// EventStore's database if it is missing. With cfg.ConfigDir set, it first
// writes the tables' storage policy for bucket (nil for none) and waits for
// ClickHouse to load it. Then it runs the migrations the database hasn't had.
func OpenClickHouse(cfg config.ClickHouse, bucket *config.ObjectStore) (*ClickHouse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	server, err := openClickHouseConn(cfg, "")
	if err != nil {
		return nil, err
	}
	err = server.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+cfg.Database)
	server.Close()
	if err != nil {
		return nil, fmt.Errorf("making database %s on ClickHouse at %s: %w", cfg.Database, cfg.Address, err)
	}

	conn, err := openClickHouseConn(cfg, cfg.Database)
	if err != nil {
		return nil, err
	}
	ch := &ClickHouse{conn: conn, database: cfg.Database}
	if cfg.ConfigDir != "" {
		if err := ch.useStoragePolicy(ctx, cfg.ConfigDir, bucket); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if err := ch.migrate(ctx, clickHouseMigrations); err != nil {
		conn.Close()
		return nil, err
	}
	return ch, nil
}

func openClickHouseConn(cfg config.ClickHouse, database string) (driver.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Address},
		Auth: clickhouse.Auth{
			Database: database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to ClickHouse at %s: %w", cfg.Address, err)
	}
	return conn, nil
}

// migrate runs each step of steps the database has no record of, in order,
// and records it.
func (ch *ClickHouse) migrate(ctx context.Context, steps []clickHouseMigration) error {
	if err := ch.conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version UInt32,
		applied_at DateTime64(3, 'UTC') DEFAULT now64(3)
	) ENGINE = MergeTree ORDER BY version`); err != nil {
		return fmt.Errorf("making ClickHouse's schema_migrations: %w", err)
	}
	done, err := ch.appliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("reading ClickHouse's schema_migrations: %w", err)
	}
	for _, step := range steps {
		if done[step.version] {
			continue
		}
		for _, statement := range step.statements {
			if err := ch.conn.Exec(ctx, statement); err != nil {
				return fmt.Errorf("ClickHouse migration %d: %w", step.version, err)
			}
		}
		if err := ch.conn.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", step.version); err != nil {
			return fmt.Errorf("recording ClickHouse migration %d: %w", step.version, err)
		}
		logger.L.Info("ClickHouse migration done", "version", step.version)
	}
	return nil
}

func (ch *ClickHouse) appliedMigrations(ctx context.Context) (map[uint32]bool, error) {
	rows, err := ch.conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	done := map[uint32]bool{}
	for rows.Next() {
		var version uint32
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		done[version] = true
	}
	return done, rows.Err()
}

// Ping checks that ClickHouse answers.
func (ch *ClickHouse) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), clickHousePingTimeout)
	defer cancel()
	return ch.conn.Ping(ctx)
}

// Conn is the connection to EventStore's database.
func (ch *ClickHouse) Conn() driver.Conn { return ch.conn }

// Database is EventStore's database's name.
func (ch *ClickHouse) Database() string { return ch.database }

func (ch *ClickHouse) Close() error { return ch.conn.Close() }
