require "json"
require "active_support"
require "active_support/logger"
require "active_support/tagged_logging"

# Production's log lines start with their Level, `INFO message`, so Vector can
# read it when it ships them as Logs (docs/adr/0011).
#
# Vector reads Docker's output a line at a time, so an entry of several lines,
# such as an exception and its backtrace, is written as one JSON line instead,
# `{"level":"FATAL","message":"…"}`, which Vector reads whole. Rails pads an
# exception with blank lines; those are trimmed first, so a routing error is
# one ordinary line.
#
# A line written in a request or job Sentry traces, sampled or not, carries
# its trace id: `INFO [trace=<id>] message`, or a JSON line's trace_id field.
# The Meta Project's Log transform moves the tag into the Log's trace id
# (docs/traces.md).
class LevelLogFormatter < ActiveSupport::Logger::SimpleFormatter
  def self.logger(io)
    logger = ActiveSupport::Logger.new(io)
    logger.formatter = new
    ActiveSupport::TaggedLogging.new(logger)
  end

  def call(severity, time, progname, msg)
    lines = super.lines(chomp: true)
    lines.shift while lines.first&.strip&.empty?
    lines.pop while lines.last&.strip&.empty?

    trace_id = self.trace_id
    if lines.size > 1
      "#{JSON.generate({ level: severity, message: lines.join("\n"), trace_id: trace_id }.compact)}\n"
    else
      "#{severity} #{"[trace=#{trace_id}] " if trace_id}#{lines.first}\n"
    end
  end

  # The trace id of the request or job writing the line, if Sentry traces it.
  def trace_id
    Sentry.get_current_scope&.get_span&.trace_id if defined?(Sentry)
  end
end
