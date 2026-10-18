package pebblevalsep

import (
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Vocabularies for the generated Events. They follow the load test's
// Sentry-style payloads (load_testing/lib/payloads.js), smaller: real SDK
// Events average about 2.2 KB.
var (
	filePaths = []string{
		"app/controllers/api/events_controller.rb", "app/controllers/api/v1/issues_controller.rb",
		"app/controllers/application_controller.rb", "app/models/event.rb", "app/models/issue.rb",
		"app/models/project.rb", "app/services/event_processor.rb", "app/services/fingerprint_calculator.rb",
		"app/jobs/process_event_job.rb", "app/jobs/notify_slack_job.rb", "lib/event_store/client.rb",
		"lib/utils/serialization.rb", "src/components/Checkout.tsx", "src/api/client.ts",
		"vendor/bundle/ruby/3.3.0/gems/actionpack-7.2.1/lib/action_controller/metal.rb",
		"vendor/bundle/ruby/3.3.0/gems/activesupport-7.2.1/lib/active_support/callbacks.rb",
		"vendor/bundle/ruby/3.3.0/gems/rack-3.1.7/lib/rack/builder.rb",
		"vendor/bundle/ruby/3.3.0/gems/puma-6.4.2/lib/puma/server.rb",
		"/usr/local/lib/ruby/3.3.0/net/http.rb", "/usr/local/lib/ruby/3.3.0/timeout.rb",
	}
	functions = []string{
		"call", "process", "handle", "execute", "perform", "dispatch", "render", "validate",
		"authenticate", "serialize", "fetch", "store", "index", "show", "create", "update",
		"before_action", "run_callbacks", "block in call", "with_retries",
	}
	errorTypes = []string{
		"NoMethodError", "ArgumentError", "RuntimeError", "TypeError", "Timeout::Error",
		"ActiveRecord::RecordNotFound", "ActiveRecord::RecordInvalid", "Net::ReadTimeout",
		"Redis::CannotConnectError", "TypeError: Cannot read properties of undefined",
	}
	errorValues = []string{
		"undefined method `name' for nil", "wrong number of arguments (given 2, expected 1)",
		"execution expired", "Couldn't find Issue with 'id'=%d", "Validation failed: Email has already been taken",
		"Net::ReadTimeout with #<TCPSocket:(closed)>", "Error connecting to Redis on 10.0.%d.4:6379 (ECONNREFUSED)",
		"reading 'items' (order %d)",
	}
	categories  = []string{"http", "navigation", "ui.click", "console", "query", "redis", "sidekiq"}
	httpMethods = []string{"GET", "POST", "PUT", "DELETE"}
	routes      = []string{"/checkout", "/api/v1/orders", "/api/v1/cart/items", "/login", "/settings/billing", "/search"}
	browsers    = []string{"Chrome", "Firefox", "Safari", "Edge"}
	osNames     = []string{"macOS", "Windows", "Linux", "iOS", "Android"}
	releases    = []string{"web@2.41.0", "web@2.41.1", "web@2.42.0", "api@7.3.2", "api@7.4.0"}
	envs        = []string{"production", "production", "production", "staging"}
)

// eventIDOffset is where the 32 hex digits of event_id start in every payload,
// so writes can give each Event its own id without rebuilding the JSON.
const eventIDOffset = len(`{"event_id":"`)

// newPayloads returns n Sentry-style Event JSON payloads, ≈2.2 KB on average
// (0.9–6 KB), deterministic for a seed.
func newPayloads(n int, seed uint64) [][]byte {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte(eventJSON(r))
	}
	return out
}

func pick[T any](r *rand.Rand, s []T) T { return s[r.IntN(len(s))] }

func randHex(r *rand.Rand, n int) string {
	b := make([]byte, (n+1)/2)
	for i := range b {
		b[i] = byte(r.UintN(256))
	}
	return hex.EncodeToString(b)[:n]
}

func eventJSON(r *rand.Rand) string {
	ts := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC).Add(time.Duration(r.Int64N(int64(24 * time.Hour))))
	var b strings.Builder
	fmt.Fprintf(&b, `{"event_id":"%s","timestamp":"%s","platform":"ruby","level":"error",`, randHex(r, 32), ts.Format(time.RFC3339Nano))
	fmt.Fprintf(&b, `"server_name":"web-%d","release":"%s","environment":"%s",`, r.IntN(12), pick(r, releases), pick(r, envs))

	errValue := pick(r, errorValues)
	if strings.Contains(errValue, "%d") {
		errValue = fmt.Sprintf(errValue, r.IntN(100000))
	}
	fmt.Fprintf(&b, `"exception":{"values":[{"type":"%s","value":"%s","stacktrace":{"frames":[`, pick(r, errorTypes), errValue)
	// Frames: 1–7, most 2–4.
	frames := 1 + r.IntN(3) + r.IntN(3) + r.IntN(2)*r.IntN(3)
	for f := range frames {
		path := pick(r, filePaths)
		inApp := !strings.HasPrefix(path, "vendor/") && !strings.HasPrefix(path, "/usr/")
		fn := pick(r, functions)
		if f > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"filename":"%s","abs_path":"/srv/app/%s","function":"%s","lineno":%d,"colno":%d,"in_app":%t`,
			path, path, fn, 1+r.IntN(600), 1+r.IntN(80), inApp)
		if inApp {
			fmt.Fprintf(&b, `,"context_line":"      result = %s(params[:id], user: current_user)"`, fn)
		}
		b.WriteByte('}')
	}
	b.WriteString(`]}}]},`)

	// Breadcrumbs: 0–4, most 1–2.
	b.WriteString(`"breadcrumbs":{"values":[`)
	crumbs := r.IntN(3) + r.IntN(2)*r.IntN(3)
	for c := range crumbs {
		if c > 0 {
			b.WriteByte(',')
		}
		cat := pick(r, categories)
		fmt.Fprintf(&b, `{"timestamp":"%s","category":"%s","level":"info","message":"%s %s %d",`,
			ts.Add(-time.Duration(crumbs-c)*time.Second).Format(time.RFC3339Nano), cat, pick(r, httpMethods), pick(r, routes), 200)
		fmt.Fprintf(&b, `"data":{"url":"https://shop.example.com%s/%d?ref=%s","status_code":200,"duration_ms":%.1f}}`,
			pick(r, routes), r.IntN(1_000_000), randHex(r, 8), 5+900*r.Float64())
	}
	b.WriteString(`]},`)

	fmt.Fprintf(&b, `"tags":{"browser":"%s %d","os":"%s","url":"%s","customer_tier":"%s"},`,
		pick(r, browsers), 100+r.IntN(30), pick(r, osNames), pick(r, routes), pick(r, []string{"free", "pro", "enterprise"}))
	fmt.Fprintf(&b, `"user":{"id":"u_%d","email":"user%d@example.com","ip_address":"203.0.%d.%d"},`, r.IntN(5_000_000), r.IntN(5_000_000), r.IntN(256), r.IntN(256))
	fmt.Fprintf(&b, `"extra":{"request_id":"%s","order_id":%d,"cart_total":%.2f,"queue_latency_ms":%.3f},`,
		randHex(r, 24), r.IntN(90_000_000), 1000*r.Float64(), 50*r.Float64())
	fmt.Fprintf(&b, `"request":{"url":"https://shop.example.com%s","method":"%s","headers":{"User-Agent":"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) %s/%d.0 Safari/537.36","Accept":"application/json"}},`,
		pick(r, routes), pick(r, httpMethods), pick(r, osNames), pick(r, browsers), 100+r.IntN(30))
	fmt.Fprintf(&b, `"contexts":{"os":{"name":"Linux","version":"6.%d"},"runtime":{"name":"ruby","version":"3.3.%d"},"trace":{"trace_id":"%s","span_id":"%s","op":"http.server"}},`,
		r.IntN(12), r.IntN(6), randHex(r, 32), randHex(r, 16))
	b.WriteString(`"sdk":{"name":"sentry.ruby.rails","version":"5.21.0"}}`)
	return b.String()
}
