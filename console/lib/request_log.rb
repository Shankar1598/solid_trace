# frozen_string_literal: true

require "active_support"
require "active_support/logger"
require "lograge"

# Production's one log line per request, a JSON line on stdout, as the call
# timings are, which Vector ships to the Meta Organization's Project, its
# fields becoming Attributes (docs/adr/0011). Lograge writes it in place of
# Rails' Started, Processing, Parameters and Completed lines.
#
# {"time":…,"level":"info","message":"GET /acme/logs 200, 41.2 ms","kind":"request",
#  "method":"GET","path":"/acme/logs","controller":"LogsController","action":"index",
#  "status":200,"duration_ms":41.2,"view_ms":3.1,"db_ms":1.4,"user_id":3,…}
module RequestLog
  # Params every request has, which say nothing the line doesn't.
  ROUTE_PARAMS = %w[controller action format].freeze

  class << self
    def configure(lograge)
      lograge.enabled = true
      lograge.base_controller_class = %w[ActionController::Base ActionController::API]
      lograge.logger = ActiveSupport::Logger.new($stdout)
      lograge.formatter = Lograge::Formatters::Json.new
      # Rails silences /up, but not in lograge's logger.
      lograge.ignore_actions = [ "Rails::HealthController#show" ]
      lograge.custom_payload { |controller| user(controller.try(:current_user)) }
      lograge.custom_options = method(:request)
      lograge.before_format = method(:line)
    end

    # The request's user, from the controller.
    def user(user)
      { user_id: user&.id, user_email: user&.email, user_name: user&.name }
    end

    # What the request itself says: its id, which other lines it causes,
    # such as its call timings, also carry, where it came from, and its
    # params, filtered as Rails filters them, as one JSON string, not an
    # Attribute for every param.
    def request(event)
      request = event.payload[:request]
      params = event.payload[:params].except(*ROUTE_PARAMS)
      {
        request_id: request.request_id,
        remote_ip: request.remote_ip,
        user_agent: request.user_agent,
        params: params.presence&.to_json,
        version: Rails.configuration.x.solidtrace_version,
      }
    end

    # The line, with the time, Level and message every Log has, and its
    # times named as the call timings name theirs.
    def line(data, _payload)
      duration_ms = data[:duration]&.round(1)
      {
        time: Time.current.utc.iso8601(3),
        level: data[:status].to_i >= 500 ? "error" : "info",
        message: "#{data[:method]} #{data[:path]} #{data[:status]}, #{duration_ms} ms",
        kind: "request",
        **data.except(:duration, :view, :db, :allocations),
        duration_ms: duration_ms,
        view_ms: data[:view]&.round(1),
        db_ms: data[:db]&.round(1),
      }.compact
    end
  end
end
