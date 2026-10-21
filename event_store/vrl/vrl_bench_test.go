package vrl_test

import (
	"testing"
	"time"

	"github.com/solidtrace/event_store/vrl"
)

// jobLines are real sidekiq.log job lines from the tagalys Project.
var jobLines = []string{
	"2026-10-21T06:58:49.598Z 9515 TID-gmxjgddu8 AnalyticsEventProcessorWorker JID-fb60f1964d1872965850d482 INFO: done: 0.575 sec",
	"2026-10-21T06:58:49.576Z 14350 TID-gooxmac6s AnalyticsEventsIndexWorker JID-ab25342d5aa64d8eeca8becd INFO: done: 7.414 sec",
	"2026-10-21T06:58:49.572Z 11607 TID-goka00jjc RecentlyViewedCacheRefreshWorker JID-2efcf8a27f997c8c92b2ffcd INFO: done: 0.544 sec",
	"2026-10-21T06:58:49.527Z 11075 TID-go1ft004g StoreSpecificShopifyAddToCartWorker JID-c81f02dd4649e07cb775304c INFO: start",
	"2026-10-21T06:58:49.413Z 9006 TID-gsmeaghos RecentlyViewedCacheRefreshWorker JID-99136ad6004a1abf5252c935 INFO: done: 0.64 sec",
}

// otherLines are longer lines that aren't job lines, made up in the shape
// of a Rails app's sidekiq.log. Most share the job lines' prefix, so the
// regex gets far before it fails.
var otherLines = []string{
	`2026-10-21T06:58:49.601Z 14350 TID-gooxmac6s AnalyticsEventsIndexWorker JID-ab25342d5aa64d8eeca8becd WARN: Elasticsearch::Transport::Transport::Errors::ServiceUnavailable: [503] {"error":{"root_cause":[{"type":"es_rejected_execution_exception","reason":"rejected execution of coordinating operation [coordinating_and_primary_bytes=0, replica_bytes=0, all_bytes=0, coordinating_operation_bytes=1048576, max_coordinating_and_primary_bytes=107374182]"}]},"status":503}`,
	"2026-10-21T06:58:49.602Z 14350 TID-gooxmac6s WARN: /app/vendor/bundle/ruby/3.3.0/gems/elasticsearch-transport-7.17.10/lib/elasticsearch/transport/transport/base.rb:218:in `__raise_transport_error'",
	`2026-10-21T06:58:49.610Z 11607 TID-goka00jjc RecentlyViewedCacheRefreshWorker JID-2efcf8a27f997c8c92b2ffcd INFO: refreshed recently viewed cache store_id=4821 client_code=acme-store products=48 user_ids=["5f3a9c1e2b","8d7e6f5a4c","1b2c3d4e5f"] cache_key=rv:4821:v3:5f3a9c1e2b duration_ms=37.4 source=storefront`,
	`2026-10-21T06:58:49.633Z 11075 TID-go1ft004g StoreSpecificShopifyAddToCartWorker JID-c81f02dd4649e07cb775304c INFO: Shopify API call POST https://acme-store.myshopify.com/admin/api/2024-01/graphql.json status=200 cost={"requestedQueryCost":12,"actualQueryCost":10,"throttleStatus":{"maximumAvailable":2000.0,"currentlyAvailable":1990,"restoreRate":100.0}} took=0.41s`,
}

// BenchmarkSidekiqProgram is the Log transform's probe (spec, Order of
// work): the sidekiq program on Logs shaped as Log processing will pass
// them, through the cgo bridge and msgpack both ways. It passes at about
// 75 µs per Log or less. It runs in about ten seconds:
//
//	cd event_store && vrl/build && go test -run '^$' -bench SidekiqProgram ./vrl/
func BenchmarkSidekiqProgram(b *testing.B) {
	program, diagnostics := vrl.Compile(sidekiqProgram)
	if program == nil {
		b.Fatalf("Compile failed: %+v", diagnostics)
	}
	defer program.Free()

	// One job line in five gives the mix production's 292 B a message.
	mix := append([]string{}, jobLines...)
	for range 5 {
		mix = append(mix, otherLines...)
	}
	for _, c := range []struct {
		name     string
		messages []string
		perCall  int
	}{
		{"job lines", jobLines, 500},
		{"mix", mix, 500},
		{"mix, one Log per call", mix, 1},
	} {
		b.Run(c.name, func(b *testing.B) {
			logs, bytes := sidekiqLogs(c.messages, 500)
			outcomes, err := program.Run(logs)
			if err != nil {
				b.Fatal(err)
			}
			for i, outcome := range outcomes {
				if _, parsed := outcome.Log["sidekiq"]; parsed != (i%len(c.messages) < len(jobLines)) {
					b.Fatalf("Log %d: %#v", i, outcome)
				}
			}
			b.ResetTimer()
			for i := range b.N {
				start := i * c.perCall % len(logs)
				if _, err := program.Run(logs[start : start+c.perCall]); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/1e3/float64(b.N*c.perCall), "us/Log")
			b.ReportMetric(float64(bytes)/float64(len(logs)), "B/message")
		})
	}
}

// sidekiqLogs makes n Logs from the messages, in turn, as the indexer
// Service's Logs look to a program.
func sidekiqLogs(messages []string, n int) ([]map[string]any, int) {
	logs := make([]map[string]any, n)
	bytes := 0
	at := time.Date(2026, 10, 21, 6, 58, 49, 598_000_000, time.UTC)
	for i := range logs {
		message := messages[i%len(messages)]
		bytes += len(message)
		logs[i] = map[string]any{
			"time":        at.Add(time.Duration(i) * time.Millisecond),
			"level":       "info",
			"message":     message,
			"service":     "indexer",
			"file":        "sidekiq.log",
			"hostname":    "indexer-7f9c6d5b8-x2kqp",
			"region_code": "us-east-1",
		}
	}
	return logs, bytes
}
