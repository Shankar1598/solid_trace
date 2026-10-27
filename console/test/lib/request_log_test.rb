# frozen_string_literal: true

require "test_helper"
require "request_log"

# Production's one JSON line per request, which Vector ships as a Log with
# Attributes (docs/adr/0011). Lograge isn't on in tests, so these build the
# line from what lograge hands RequestLog.
class RequestLogTest < ActiveSupport::TestCase
  test "a request's line has its time, Level, message, user, id and filtered params" do
    user = build(:user, id: 3, name: "Ana María", email: "ana@example.com")
    request = ActionDispatch::Request.new(Rack::MockRequest.env_for("/acme/logs", "REMOTE_ADDR" => "203.0.113.9",
      "HTTP_USER_AGENT" => "curl/8", "action_dispatch.request_id" => "req-1"))
    event = ActiveSupport::Notifications::Event.new("process_action.action_controller", 0, 0, "1", {
      request: request, params: { "controller" => "logs", "action" => "index", "q" => "level == \"error\"", "password" => "[FILTERED]" },
    })
    lograge = { method: "GET", path: "/acme/logs", format: "html", controller: "LogsController", action: "index",
      status: 200, allocations: 1200, duration: 41.234, view: 3.11, db: 1.44 }

    line = RequestLog.line(lograge.merge(RequestLog.request(event), RequestLog.user(user)), {})

    assert_equal({
      level: "info", message: "GET /acme/logs 200, 41.2 ms", kind: "request",
      method: "GET", path: "/acme/logs", format: "html", controller: "LogsController", action: "index", status: 200,
      request_id: "req-1", remote_ip: "203.0.113.9", user_agent: "curl/8",
      params: { "q" => "level == \"error\"", "password" => "[FILTERED]" }.to_json, version: Rails.configuration.x.solidtrace_version,
      user_id: 3, user_email: "ana@example.com", user_name: "Ana María",
      duration_ms: 41.2, view_ms: 3.1, db_ms: 1.4,
    }, line.except(:time))
    assert_match(/\A\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z\z/, line[:time])
  end

  test "a request Sentry traces, sampled or not, has its trace id, which EventStore reads as the Log's" do
    trace_id = "6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b10"
    request = ActionDispatch::Request.new(Rack::MockRequest.env_for("/acme/logs"))
    event = ActiveSupport::Notifications::Event.new("process_action.action_controller", 0, 0, "1", { request: request, params: {} })

    traced = Sentry.with_scope do |scope|
      scope.set_span(Sentry::Transaction.new(name: "LogsController#index", trace_id: trace_id, sampled: false))
      RequestLog.line({ status: 200, duration: 1.0 }.merge(RequestLog.request(event)), {})
    end
    untraced = RequestLog.line({ status: 200, duration: 1.0 }.merge(RequestLog.request(event)), {})

    assert_equal trace_id, traced[:trace_id]
    assert_not untraced.key?(:trace_id)
  end

  test "a failed request is an error with the exception, and a signed-out one names no user" do
    lograge = { method: "POST", path: "/mcp", status: 500, error: "RuntimeError: boom", duration: 2.0 }

    line = RequestLog.line(lograge.merge(RequestLog.user(nil)), {})

    assert_equal [ "error", "POST /mcp 500, 2.0 ms", "RuntimeError: boom" ], line.values_at(:level, :message, :error)
    assert_not line.key?(:user_id)
  end
end
