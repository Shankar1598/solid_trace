package storage

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// ReadStatement is one SQL statement a Log read ran, with how long DuckDB
// took to run it (duckdb-go runs a statement whole before its rows are
// read) and its error, if it failed.
type ReadStatement struct {
	SQL      string
	Duration time.Duration
	Err      error
}

// ReadStatements collects the statements a Log read runs, in the order they
// ran, so a run can log the SQL a Query became: which hours, which sources
// and which columns depend on the Logs stored then, not on the Query alone
// (.scratch/call-timings/spec.md).
type ReadStatements struct {
	mu   sync.Mutex
	list []ReadStatement
}

type readStatementsKey struct{}

// WithReadStatements returns a context whose Log reads record their
// statements in the ReadStatements it also returns.
func WithReadStatements(ctx context.Context) (context.Context, *ReadStatements) {
	statements := &ReadStatements{}
	return context.WithValue(ctx, readStatementsKey{}, statements), statements
}

// List returns the statements recorded so far.
func (s *ReadStatements) List() []ReadStatement {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ReadStatement(nil), s.list...)
}

func recordStatement(ctx context.Context, query string, began time.Time, err error) {
	statements, _ := ctx.Value(readStatementsKey{}).(*ReadStatements)
	if statements == nil {
		return
	}
	statements.mu.Lock()
	defer statements.mu.Unlock()
	statements.list = append(statements.list, ReadStatement{SQL: query, Duration: time.Since(began), Err: err})
}

// queryRead runs a Log read's query, recording it in ctx's ReadStatements.
func queryRead(ctx context.Context, conn *sql.Conn, query string) (*sql.Rows, error) {
	began := time.Now()
	rows, err := conn.QueryContext(ctx, query)
	recordStatement(ctx, query, began, err)
	return rows, err
}

// queryReadRow runs a Log read's query of one row into dest, recording it
// in ctx's ReadStatements.
func queryReadRow(ctx context.Context, conn *sql.Conn, query string, dest ...any) error {
	began := time.Now()
	err := conn.QueryRowContext(ctx, query).Scan(dest...)
	recordStatement(ctx, query, began, err)
	return err
}
