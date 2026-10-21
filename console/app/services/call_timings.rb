# frozen_string_literal: true

# One JSON line for every MCP tool call and every Log read, with how long it
# took (.scratch/call-timings/spec.md). Production writes the lines to stdout,
# where Vector ships them to the Meta Organization's Project and their fields
# become Attributes (docs/adr/0011); development appends them to
# log/call_timings.log (config/initializers/call_timings.rb). A line that
# can't be written never fails the call.
module CallTimings
  # Who and what a Log read runs for, set by the controller, job or MCP tool
  # that starts it, and reset with the request or job.
  class Context < ActiveSupport::CurrentAttributes
    attribute :origin, :request_id, :user
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
    # mcp:<tool>. Answers what the block answers.
    def mcp_tool(tool, arguments, user)
      started = now
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

    private

    def now
      Process.clock_gettime(Process::CLOCK_MONOTONIC)
    end

    # How many Logs, table rows or buckets an answer has.
    def rows(answer)
      return unless answer.is_a?(Hash)

      (answer["logs"] || answer.dig("table", "rows") || answer["buckets"])&.size
    end

    def write(kind, message, started, outcome, error, user: Context.user || Current.user, **fields)
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
        request_id: Context.request_id,
        **fields,
      }.compact
      output.write("#{JSON.generate(line)}\n")
    rescue StandardError => e
      Rails.logger.warn("Call timing not written: #{e.class}: #{e.message}")
    end
  end
end
