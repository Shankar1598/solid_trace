// Command send sends one Transaction, and an Event in it, through
// DualTransport, as EventStore's test of docs/traces.md does
// (traces_docs_test.go). It is a module of its own, outside EventStore's:
//
//	go run . <sentry.io's DSN> <SolidTrace's DSN> <trace id>
package main

import (
	"context"
	"encoding/hex"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	sentryDSN, solidTraceDSN, traceID := os.Args[1], os.Args[2], os.Args[3]
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              sentryDSN,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        &DualTransport{SolidTraceDSN: solidTraceDSN},
	}); err != nil {
		panic(err)
	}

	tx := sentry.StartTransaction(context.Background(), "GET /rates",
		sentry.WithTransactionSource(sentry.SourceRoute),
		sentry.WithOpName("http.server"),
		func(s *sentry.Span) {
			if _, err := hex.Decode(s.TraceID[:], []byte(traceID)); err != nil {
				panic(err)
			}
		})
	query := sentry.StartSpan(tx.Context(), "db.sql.query", sentry.WithDescription("SELECT 1"))
	time.Sleep(10 * time.Millisecond)
	query.Finish()
	sentry.CaptureMessage("Rates for EUR are missing")
	tx.Status = sentry.SpanStatusOK
	tx.Finish()

	if !sentry.Flush(5 * time.Second) {
		panic("not sent")
	}
}
