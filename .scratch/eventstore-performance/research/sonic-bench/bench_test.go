package sonicbench

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// Same fixtures as event_store's ingest, processing and models benchmarks.
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

func arrayBody() []byte {
	logs := make([]string, 100)
	for i := range logs {
		logs[i] = benchLog(i)
	}
	return []byte("[" + strings.Join(logs, ",") + "]")
}

func benchEvent() []byte {
	var frames []string
	for i := range 8 {
		frames = append(frames, fmt.Sprintf(`{"filename":"app/services/order_%d.rb","abs_path":"/srv/app/app/services/order_%d.rb",`+
			`"function":"process_step_%d","lineno":%d,"colno":%d,"in_app":%t,`+
			`"context_line":"      result = process_step_%d(params[:id], user: current_user)","pre_context":["  def call","    validate!"],"post_context":["  end"]}`,
			i, i, i, 40+i*17, 5+i, i%3 != 0, i))
	}
	var crumbs []string
	for i := range 4 {
		crumbs = append(crumbs, fmt.Sprintf(`{"timestamp":"2026-10-21T11:59:5%d.123Z","category":"http","level":"info","message":"GET /api/v1/orders/%d 200",`+
			`"data":{"url":"https://shop.example.com/api/v1/orders/%d?ref=ab12cd34","status_code":200,"duration_ms":%d.5}}`, i, i, i, 20+i))
	}
	return []byte(`{"event_id":"fc6d8c0c43fc4630ad850ee518f1b9d0","timestamp":"2026-10-21T12:00:00.250Z","platform":"ruby","level":"error",` +
		`"logger":"rails","server_name":"web-7f9c-2","release":"web@1.42.0","environment":"production",` +
		`"exception":{"values":[{"type":"ActiveRecord::RecordNotFound","value":"Couldn't find Order with 'id'=48213","module":"ActiveRecord",` +
		`"stacktrace":{"frames":[` + strings.Join(frames, ",") + `]}}]},` +
		`"breadcrumbs":{"values":[` + strings.Join(crumbs, ",") + `]},` +
		`"request":{"url":"https://shop.example.com/api/v1/orders/48213","method":"GET","query_string":"ref=ab12cd34",` +
		`"headers":{"Accept":"application/json","User-Agent":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15"}},` +
		`"user":{"id":"8812","ip_address":"203.0.113.7"},` +
		`"contexts":{"os":{"name":"Linux","version":"6.14"},"runtime":{"name":"ruby","version":"3.4.2"},"trace":{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7"}},` +
		`"tags":{"controller":"orders","action":"show","region":"eu-west"},"extra":{"order_id":48213,"retries":0},` +
		`"sdk":{"name":"sentry.ruby.rails","version":"5.22.1"}}`)
}

// --- Log body: validate, split the array, decode each Log and its nested
// objects into map[string]RawMessage, as logObjects and flatten do.

type api struct {
	valid     func([]byte) bool
	unmarshal func([]byte, any) error
}

var (
	std       = api{json.Valid, json.Unmarshal}
	sonicStd  = api{sonic.ConfigStd.Valid, sonic.ConfigStd.Unmarshal}
	sonicDflt = api{sonic.ConfigDefault.Valid, sonic.ConfigDefault.Unmarshal}
)

func splitAndDecode(a api, body []byte) int {
	if !a.valid(body) {
		panic("invalid")
	}
	var elements []json.RawMessage
	if err := a.unmarshal(body, &elements); err != nil {
		panic(err)
	}
	n := 0
	for _, e := range elements {
		var object map[string]json.RawMessage
		if err := a.unmarshal(e, &object); err != nil {
			panic(err)
		}
		for _, v := range object {
			if len(v) > 0 && v[0] == '{' {
				var inner map[string]json.RawMessage
				_ = a.unmarshal(v, &inner)
				n += len(inner)
			}
		}
		n += len(object)
	}
	return n
}

func BenchmarkLogBody(b *testing.B) {
	body := arrayBody()
	for _, c := range []struct {
		name string
		a    api
	}{{"std", std}, {"sonicStd", sonicStd}, {"sonicDefault", sonicDflt}} {
		b.Run(c.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				splitAndDecode(c.a, body)
			}
		})
	}
}

// --- Event: parse into map[string]any, as parseEvent does (v2 API first).

func BenchmarkParseEvent(b *testing.B) {
	raw := benchEvent()
	run := func(name string, f func([]byte, any) error) {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			for b.Loop() {
				var p map[string]any
				if err := f(raw, &p); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	run("jsonv2", func(d []byte, v any) error { return jsonv2.Unmarshal(d, v) })
	run("sonicStd", sonic.ConfigStd.Unmarshal)
	run("sonicDefault", sonic.ConfigDefault.Unmarshal)
}

// --- Encode a page of 200 Logs. Attributes only implements v2's
// MarshalJSONTo, which sonic ignores, so sonic gets the same page with
// Attributes as one plain map[string]any (what it would need).

func page() (map[string]any, map[string]any) {
	logs := make([]models.Log, 200)
	plain := make([]map[string]any, 200)
	at := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for i := range logs {
		l := models.Log{
			ID: uuid.Must(uuid.NewV7()), Time: at.Add(-time.Duration(i) * time.Millisecond),
			Level: "info", Message: fmt.Sprintf("Completed 200 OK in %dms for GET /api/v1/products/search?q=shoes&page=%d \"café\"", 100+i%400, i%10),
			Service: "web", Environment: "production",
			TraceID: fmt.Sprintf("4bf92f3577b34da6a3ce929d0e0e%04x", i), SpanID: fmt.Sprintf("00f067aa0ba9%04x", i),
			Attributes: models.Attributes{
				String: map[string]string{"http.method": "GET", "http.path": "/api/v1/products/search",
					"http.request_id": fmt.Sprintf("9f8e7d6c-5b4a-4321-8fed-%012d", i),
					"http.user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
					"client.name": "Example Store", "client.plan": "enterprise", "client.region": "eu-west",
					"params.q": "shoes", "worker.host": fmt.Sprintf("web-7f9c-%d", i%4), "worker.thread": "puma 003"},
				Number: map[string]float64{"http.status": 200, "http.duration_ms": float64(100+i%400) + 0.4,
					"client.id": float64(1000 + i%50), "params.page": float64(i % 10), "db.queries": float64(i % 20),
					"db.time_ms": 97.8, "worker.pid": float64(4000 + i%100)},
				Bool: map[string]bool{"db.cached": false, "retry": false},
				JSON: map[string]string{"tags": `["search","api","v1"]`, "params.filters.color": `["red","blue"]`},
			},
		}
		logs[i] = l
		attrs := map[string]any{}
		for k, v := range l.Attributes.String {
			attrs[k] = v
		}
		for k, v := range l.Attributes.Number {
			attrs[k] = v
		}
		for k, v := range l.Attributes.Bool {
			attrs[k] = v
		}
		for k, v := range l.Attributes.JSON {
			attrs[k] = json.RawMessage(v)
		}
		plain[i] = map[string]any{"id": l.ID, "time": l.Time, "level": l.Level, "message": l.Message,
			"service": l.Service, "environment": l.Environment, "trace_id": l.TraceID, "span_id": l.SpanID, "attributes": attrs}
	}
	meta := func(v any) map[string]any {
		return map[string]any{"logs": v, "next_cursor": "MjAyNi0xMC0yMVQxMjowMDowMFo", "elapsed_ms": int64(52)}
	}
	return meta(logs), meta(plain)
}

func BenchmarkEncodeLogPage(b *testing.B) {
	real, plain := page()
	run := func(name string, v any, f func(any) ([]byte, error)) {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out, err := f(v)
				if err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(out)))
			}
		})
	}
	run("std_today", real, json.Marshal)
	run("sonicStd", plain, sonic.ConfigStd.Marshal)
	run("sonicDefault", plain, sonic.ConfigDefault.Marshal)
}

// --- Behaviour on the bodies issue 02 pinned down.

func TestBehaviour(t *testing.T) {
	cases := map[string][]byte{
		"duplicate name": []byte(`{"a":1,"a":2}`),
		"invalid UTF-8":  []byte("{\"a\":\"x\xffy\"}"),
		"big number":     []byte(`{"a":123456789012345678901234567890}`),
		"deep nesting":   []byte(strings.Repeat("[", 10001) + strings.Repeat("]", 10001)),
	}
	for _, name := range []string{"duplicate name", "invalid UTF-8", "big number", "deep nesting"} {
		in := cases[name]
		for _, c := range []struct {
			n string
			f func([]byte, any) error
		}{{"encoding/json", json.Unmarshal}, {"sonicStd", sonic.ConfigStd.Unmarshal}, {"sonicDefault", sonic.ConfigDefault.Unmarshal}} {
			var v any
			err := c.f(in, &v)
			s := fmt.Sprintf("%v", v)
			if len(s) > 40 {
				s = s[:40] + "…"
			}
			t.Logf("%-15s %-13s err=%v  value=%q", name, c.n, err, s)
		}
	}
}
