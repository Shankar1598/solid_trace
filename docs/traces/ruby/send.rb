# Sends one Transaction, and an Event in it, through DualTransport, as
# EventStore's test of docs/traces.md does (traces_docs_test.go):
#
#   ruby send.rb <sentry.io's DSN> <SolidTrace's DSN> <trace id>
require_relative "dual_transport"

sentry_dsn, solidtrace_dsn, trace_id = ARGV
ENV["SOLIDTRACE_DSN"] = solidtrace_dsn

Sentry.init do |config|
  config.dsn = sentry_dsn
  config.traces_sample_rate = 1.0
  config.transport.transport_class = DualTransport
  # Sends as it captures, so everything is sent before the script ends.
  config.background_worker_threads = 0
  config.send_client_reports = false
end

transaction = Sentry.start_transaction(name: "OrdersController#show", source: :view, op: "http.server", trace_id: trace_id)
Sentry.get_current_scope.set_span(transaction)
transaction.with_child_span(op: "db.sql.active_record", description: "SELECT 1") { sleep 0.01 }
Sentry.capture_message("Order 42 has no line items")
transaction.set_http_status(200)
transaction.finish
