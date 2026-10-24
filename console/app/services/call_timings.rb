# frozen_string_literal: true

# One JSON line for every MCP tool call and every Log read, with how long it
# took and the release that ran it (.scratch/call-timings/spec.md). EventStore
# logs the SQL each Log read ran with the same request_id. Every MCP tool call
# is timed. The MCP server's reads are timed while InstanceSetting#log_mcp_reads
# is on (the default); the rest, the explorer's, Dashboards', the chart
# editor's and Monitors', only while the operator has turned on
# InstanceSetting#log_all_reads, as they refresh every 15 seconds, and that
# times the MCP server's too. Production writes the lines to stdout,
# where Vector ships them to the Meta Organization's Project and their fields
# become Attributes (docs/adr/0011); development appends them to
# log/call_timings.log (config/initializers/call_timings.rb). A line that
# can't be written never fails the call.
module CallTimings
  # Who and what a Log read runs for, set by the controller, job or MCP tool
  # that starts it, and reset with the request or job.
  class Context < ActiveSupport::CurrentAttributes
    attribute :origin, :request_id, :user, :instance_settings

    # InstanceSetting#log_all_reads, read once per request or job.
    def log_all_reads?
      read_instance_settings.log_all_reads?
    end

    # InstanceSetting#log_mcp_reads, read once per request or job.
    def log_mcp_reads?
      read_instance_settings.log_mcp_reads?
    end

    private

    def read_instance_settings
      self.instance_settings ||= InstanceSetting.current
    end
  end

  # How each EventStore failure is told as an outcome.
  OUTCOMES = {
    "EventStore::Rejected" => "rejected",
    "EventStore::TimedOut" => "timed_out",
    "EventStore::Unavailable" => "unavailable",
    "EventStore::SQLQueriesOff" => "sql_off",
  }.freeze

  class << self
    # Times an MCP tool call, the block, as the user who made it: the line
    # has its tool and arguments, and the Log reads it makes have origin
    # mcp:<tool>. Arguments such as a password are filtered as request
    # parameters are (config/initializers/filter_parameter_logging.rb).
    # Answers what the block answers.
    def mcp_tool(tool, arguments, user)
      started = now
      arguments = ActiveSupport::ParameterFilter.new(Rails.application.config.filter_parameters).filter(arguments)
      response = Context.set(origin: "mcp:#{tool}", user: user) { yield }
      error = response.error? ? response.content.filter_map { |part| part[:text] }.join("\n") : nil
      write("mcp_tool", "MCP tool #{tool}", started, error ? "error" : "ok", error,
        user: user, tool: tool, arguments: arguments)
      response
    rescue StandardError => e
      write("mcp_tool", "MCP tool #{tool}", started, "exception", "#{e.class}: #{e.message}",
        user: user, tool: tool, arguments: arguments)
      raise
    end

    # Times a Log read, the block, which answers EventStore's answer: read
    # is query, histogram, sql or match, and query the Query or SQL it runs.
    # Answers what the block answers.
    def log_read(read, project_id:, query:, from: nil, to: nil)
      return yield unless logged_read?

      started = now
      fields = { read: read, origin: Context.origin, project_id: project_id, query: query, from: from&.utc&.iso8601(3), to: to&.utc&.iso8601(3) }
      answer = yield
      write("log_read", "Log read #{read}", started, "ok", nil, **fields, rows: rows(answer))
      answer
    rescue StandardError => e
      outcome = e.class.ancestors.lazy.filter_map { |kind| OUTCOMES[kind.name] }.first || "exception"
      write("log_read", "Log read #{read}", started, outcome, e.message, **fields)
      raise
    end

    # Whether the Log read running now is timed, and EventStore logs its SQL:
    # any while log_all_reads is on, and an MCP tool's while log_mcp_reads is.
    def logged_read?
      Context.log_all_reads? || (Context.origin.to_s.start_with?("mcp:") && Context.log_mcp_reads?)
    end

    # Who the call or read running now is for: an MCP tool's user, or the
    # signed-in user.
    def user
      Context.user || Current.user
    end

    private

    def now
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    # How many Logs, table rows or buckets an answer has.
    def rows(answer)
      return unless answer.is_a?(Hash)

      (answer["logs"] || answer.dig("table", "rows") || answer["buckets"])&.size
    end

    def write(kind, message, started, outcome, error, user: self.user, **fields)
      output = Rails.configuration.x.call_timings_output
      return unless output

      duration_ms = ((now - started) * 1000).round(1)
      line = {
        time: Time.current.utc.iso8601(3),
        level: outcome == "ok" ? "info" : "warn",
        message: "#{message}, #{duration_ms} ms",
        kind: kind,
        duration_ms: duration_ms,
        outcome: outcome,
        error: error,
        user_id: user&.id,
        user_email: user&.email,
        user_name: user&.name,
        request_id: Context.request_id,
        version: Rails.configuration.x.solidtrace_version,
        **fields,
      }.compact
      output.write("#{JSON.generate(line)}\n")
    rescue StandardError => e
      Rails.logger.warn("Call timing not written: #{e.class}: #{e.message}")
    end
  end
end
