package processing

import (
	"fmt"
	"strings"
	"testing"
)

// benchEvent is a Sentry error Event of about 4 KB: an exception with eight
// frames, breadcrumbs, a request, contexts and tags.
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

// BenchmarkParseEvent times what issueWork does with an Event's JSON before
// it looks up the Issue: parse it, and find its fingerprint and title.
//
//	go test -run '^$' -bench ParseEvent -benchmem -count 6 ./processing/
func BenchmarkParseEvent(b *testing.B) {
	raw := benchEvent()
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for b.Loop() {
		payload, err := parseEvent(raw)
		if err != nil {
			b.Fatal(err)
		}
		classifyIssue(payload)
	}
}
