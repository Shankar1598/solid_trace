// Command capture-go captures the envelope sentry-go sends for a Transaction
// with fixed ids and times, so that each Span's self time is known. It is a
// module of its own, outside EventStore's:
//
//	go run . <output file> <trace id> <parent span id>
//
// The Transaction is go-api answering the HTTP call that a Span of the Rails
// request in sentry-ruby's capture made, sampled at 5% by a TracesSampler: a
// cache read and a query.
package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	out, trace, parent := os.Args[1], os.Args[2], os.Args[3]
	var envelope []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			body, _ = gzip.NewReader(r.Body)
		}
		envelope, _ = io.ReadAll(body)
	}))
	defer server.Close()

	dsn := strings.Replace(server.URL, "http://", "http://public_key@", 1) + "/1"
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:           dsn,
		EnableTracing: true,
		TracesSampler: func(sentry.SamplingContext) float64 { return 0.05 },
		Environment:   "production",
		Release:       "go-api@2.0.0",
		ServerName:    "go-1",
	}); err != nil {
		panic(err)
	}

	t0 := time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	ids := func(spanID string, start time.Time) sentry.SpanOption {
		return func(s *sentry.Span) {
			copy(s.SpanID[:], mustHex(spanID))
			s.StartTime = start
		}
	}

	// Sampled at 5%: try until the sampler keeps one.
	var tx *sentry.Span
	for tx == nil || tx.Sampled != sentry.SampledTrue {
		tx = sentry.StartTransaction(context.Background(), "GET /rates",
			sentry.ContinueFromHeaders(trace+"-"+parent+"-1", "sentry-trace_id="+trace+",sentry-sample_rate=0.25,sentry-sampled=true,sentry-public_key=public_key"),
			sentry.WithTransactionSource(sentry.SourceRoute),
			sentry.WithOpName("http.server"),
			ids("c000000000000001", at(52)))
	}
	tx.SetTag("currency", "EUR")

	cache := sentry.StartSpan(tx.Context(), "cache.get", sentry.WithDescription("rates:EUR"), ids("c000000000000002", at(54)))
	cache.SetData("cache.hit", false)
	cache.Status = sentry.SpanStatusOK
	cache.EndTime = at(58)
	cache.Finish()

	query := sentry.StartSpan(tx.Context(), "db.sql.query",
		sentry.WithDescription("SELECT rate FROM rates WHERE currency = 'EUR' AND day = '2026-10-02'"), ids("c000000000000003", at(60)))
	query.Status = sentry.SpanStatusOK
	query.EndTime = at(75)
	query.Finish()

	tx.Status = sentry.HTTPtoSpanStatus(200)
	tx.EndTime = at(78)
	tx.Finish()
	if !sentry.Flush(5 * time.Second) {
		panic("not sent")
	}
	if err := os.WriteFile(out, envelope, 0o644); err != nil {
		panic(err)
	}
}

func mustHex(s string) []byte {
	var b []byte
	if _, err := fmt.Sscanf(s, "%x", &b); err != nil {
		panic(err)
	}
	return b
}
