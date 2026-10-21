package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/ingest/normalise"
)

// The Log ingest benchmarks time the JSON work of one request: splitting its
// body into Logs and normalising each, as IngestLogs and storeLogs do, but
// without Pebble. Run them on each Go toolchain to compare its JSON engine:
//
//	go test -run '^$' -bench . -benchmem -count 6 ./ingest/

// benchLogs is how many Logs a benchmark request carries.
const benchLogs = 100

var benchReceipt = normalise.Receipt{At: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), RetentionDays: 30}

// benchLog is one JSON Log of about 1 KB, shaped like an application's
// request Log: the built-in fields under their common aliases, nested
// objects, an array, numbers, booleans and escaped strings.
func benchLog(i int) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-21T11:%02d:%02d.%06dZ","level":"info",`+
		`"message":"Completed 200 OK in %dms (Views: 41.2ms | ActiveRecord: 97.8ms) for GET /api/v1/products/search?q=shoes&page=%d \"café\"",`+
		`"service":"web","environment":"production","trace_id":"4bf92f3577b34da6a3ce929d0e0e%04x","span_id":"00f067aa0ba9%04x",`+
		`"http":{"method":"GET","path":"/api/v1/products/search","status":200,"duration_ms":%d.4,"request_id":"9f8e7d6c-5b4a-4321-8fed-%012d",`+
		`"user_agent":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15"},`+
		`"client":{"id":%d,"name":"Example Store","plan":"enterprise","region":"eu-west"},`+
		`"params":{"q":"shoes","page":%d,"filters":{"color":["red","blue"],"size":{"min":7,"max":11}}},`+
		`"db":{"queries":%d,"cached":false,"time_ms":97.8},"tags":["search","api","v1"],"retry":false,`+
		`"worker":{"host":"web-7f9c-%d","pid":%d,"thread":"puma 003"}}`,
		i/60%60, i%60, i*1000%1000000, 100+i%400, i%10, i, i, 100+i%400, i,
		1000+i%50, i%10, i%20, i%4, 4000+i%100)
}

func benchArrayBody() []byte {
	logs := make([]string, benchLogs)
	for i := range logs {
		logs[i] = benchLog(i)
	}
	return []byte("[" + strings.Join(logs, ",") + "]")
}

func benchNDJSONBody() []byte {
	var body bytes.Buffer
	for i := range benchLogs {
		body.WriteString(benchLog(i))
		body.WriteByte('\n')
	}
	return body.Bytes()
}

// benchSentryPayload is one Sentry log item's payload, its records' fields
// typed as sentry-go sends them.
func benchSentryPayload() []byte {
	records := make([]string, benchLogs)
	for i := range records {
		records[i] = fmt.Sprintf(`{"timestamp":1792580%03d.%06d,"trace_id":"4bf92f3577b34da6a3ce929d0e0e%04x","level":"info","severity_number":9,`+
			`"body":"Completed 200 OK in %dms for GET /api/v1/products/search?q=shoes&page=%d",`+
			`"attributes":{"sentry.environment":{"value":"production","type":"string"},"service.name":{"value":"web","type":"string"},`+
			`"sentry.release":{"value":"web@1.42.0","type":"string"},"sentry.sdk.name":{"value":"sentry.go","type":"string"},`+
			`"http.status":{"value":200,"type":"integer"},"http.duration_ms":{"value":%d.4,"type":"double"},`+
			`"client.id":{"value":%d,"type":"integer"},"cached":{"value":false,"type":"boolean"},`+
			`"route":{"value":"/api/v1/products/search","type":"string"},"worker.host":{"value":"web-7f9c-%d","type":"string"}}}`,
			i%1000, i*1000%1000000, i, 100+i%400, i%10, 100+i%400, 1000+i%50, i%4)
	}
	return []byte(`{"items":[` + strings.Join(records, ",") + `]}`)
}

func BenchmarkLogBodyToLogs(b *testing.B) {
	for _, bench := range []struct {
		name   string
		body   []byte
		ndjson bool
	}{
		{"array", benchArrayBody(), false},
		{"ndjson", benchNDJSONBody(), true},
	} {
		b.Run(bench.name, func(b *testing.B) {
			b.SetBytes(int64(len(bench.body)))
			b.ReportAllocs()
			for b.Loop() {
				objects, dropped := logObjects(bench.body, bench.ndjson)
				if dropped != 0 || len(objects) != benchLogs {
					b.Fatalf("got %d Logs and %d dropped", len(objects), dropped)
				}
				for _, object := range objects {
					normalise.JSON(object, benchReceipt)
				}
			}
		})
	}

	b.Run("sentry", func(b *testing.B) {
		payloads := [][]byte{benchSentryPayload()}
		b.SetBytes(int64(len(payloads[0])))
		b.ReportAllocs()
		for b.Loop() {
			records, dropped := sentryLogRecords(payloads)
			if dropped != 0 || len(records) != benchLogs {
				b.Fatalf("got %d Logs and %d dropped", len(records), dropped)
			}
			for _, record := range records {
				normalise.Sentry(record, benchReceipt)
			}
		}
	})
}

// BenchmarkTransformedLogs times normalising the objects a Log transform
// returned, which goes through JSON once more.
func BenchmarkTransformedLogs(b *testing.B) {
	objects := make([]map[string]any, benchLogs)
	for i := range objects {
		if err := json.Unmarshal([]byte(benchLog(i)), &objects[i]); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, object := range objects {
			if _, _, err := normalise.Transformed(object, benchReceipt); err != nil {
				b.Fatal(err)
			}
		}
	}
}
