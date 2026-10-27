# Captures the envelopes sentry-ruby sends for two Transactions, with fixed
# ids and times so that each Span's self time is known. Run it with the
# sentry-ruby version to capture on the load path:
#
#   ruby capture.rb <output prefix> <trace id> <second trace id>
#
# It writes <prefix>-rails.envelope and <prefix>-url.envelope.
#
# The first is a Rails request at a 25% sample rate: an N+1 of three line
# items, and a view that calls a service over HTTP. The second is a Rack
# request named by its raw URL.
require "sentry-ruby"
require "json"

class Capture < Sentry::Transport
  OUT = []

  def send_data(data, options = {})
    OUT << data
  end
end

Sentry.init do |c|
  c.dsn = "http://public_key@localhost:4000/1"
  c.traces_sample_rate = 0.25
  c.transport.transport_class = Capture
  c.background_worker_threads = 0
  c.environment = "production"
  c.release = "api@1.4.0"
  c.server_name = "web-1"
  c.send_client_reports = false
end

T0 = Time.utc(2026, 10, 2, 11, 0, 0).to_f
def at(ms) = T0 + ms / 1000.0

out, trace, url_trace = ARGV

t = Sentry.start_transaction(name: "OrdersController#show", source: :view, op: "http.server",
  trace_id: trace, span_id: "a000000000000001", sample_rand: 0.1, start_timestamp: at(0))
raise "not sampled" unless t.sampled
Sentry.get_current_scope.set_span(t)
Sentry.set_tags(customer: "acme")
a = t.start_child(op: "db.sql.active_record", description: %q{SELECT "orders".* FROM "orders" WHERE "orders"."id" = 42 LIMIT 1},
  span_id: "a000000000000002", start_timestamp: at(5))
a.finish(end_timestamp: at(15))
[1, 2, 3].each_with_index do |id, i|
  b = t.start_child(op: "db.sql.active_record", description: %(SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = #{id}),
    span_id: "a00000000000001#{i}", start_timestamp: at(20 + 5 * i))
  b.finish(end_timestamp: at(25 + 5 * i))
end
c = t.start_child(op: "template.render", description: "orders/show.html.erb", span_id: "a000000000000003", start_timestamp: at(40))
d = c.start_child(op: "http.client", description: "GET https://rates.example.com/rates", span_id: "a000000000000004", start_timestamp: at(50))
d.set_data("http.response.status_code", 200)
d.set_http_status(200)
d.finish(end_timestamp: at(80))
c.finish(end_timestamp: at(100))
t.set_http_status(200)
t.finish(end_timestamp: at(120))

u = Sentry.start_transaction(name: "/orders/42", source: :url, op: "http.server",
  trace_id: url_trace, span_id: "b000000000000001", sample_rand: 0.1, start_timestamp: at(1000))
raise "not sampled" unless u.sampled
u.set_http_status(500)
u.finish(end_timestamp: at(1010))

Capture::OUT.zip(%w[rails url]).each { |data, name| File.write("#{out}-#{name}.envelope", data) }
