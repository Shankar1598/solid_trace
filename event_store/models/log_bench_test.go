package models

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// BenchmarkEncodeLogPage times encoding a page of Logs as the Query route
// answers it: Fiber's c.JSON calls json.Marshal on a fiber.Map.
//
//	go test -run '^$' -bench . -benchmem -count 6 ./models/
func BenchmarkEncodeLogPage(b *testing.B) {
	logs := make([]Log, 200)
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for i := range logs {
		logs[i] = Log{
			ID:          uuid.Must(uuid.NewV7()),
			Time:        at.Add(-time.Duration(i) * time.Millisecond),
			Level:       "info",
			Message:     fmt.Sprintf("Completed 200 OK in %dms for GET /api/v1/products/search?q=shoes&page=%d \"café\"", 100+i%400, i%10),
			Service:     "web",
			Environment: "production",
			TraceID:     fmt.Sprintf("4bf92f3577b34da6a3ce929d0e0e%04x", i),
			SpanID:      fmt.Sprintf("00f067aa0ba9%04x", i),
			Attributes: Attributes{
				String: map[string]string{
					"http.method":     "GET",
					"http.path":       "/api/v1/products/search",
					"http.request_id": fmt.Sprintf("9f8e7d6c-5b4a-4321-8fed-%012d", i),
					"http.user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
					"client.name":     "Example Store",
					"client.plan":     "enterprise",
					"client.region":   "eu-west",
					"params.q":        "shoes",
					"worker.host":     fmt.Sprintf("web-7f9c-%d", i%4),
					"worker.thread":   "puma 003",
				},
				Number: map[string]float64{
					"http.status":      200,
					"http.duration_ms": float64(100+i%400) + 0.4,
					"client.id":        float64(1000 + i%50),
					"params.page":      float64(i % 10),
					"db.queries":       float64(i % 20),
					"db.time_ms":       97.8,
					"worker.pid":       float64(4000 + i%100),
				},
				Bool: map[string]bool{"db.cached": false, "retry": false},
				JSON: map[string]string{
					"tags":                 `["search","api","v1"]`,
					"params.filters.color": `["red","blue"]`,
				},
			},
		}
	}
	page := map[string]any{"logs": logs, "next_cursor": "MjAyNi0xMC0yMVQxMjowMDowMFo", "elapsed_ms": int64(52)}

	b.ReportAllocs()
	for b.Loop() {
		body, err := json.Marshal(page)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(body)))
	}
}
