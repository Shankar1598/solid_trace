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

    if lines.size > 1
      "#{JSON.generate(level: severity, message: lines.join("\n"))}\n"
    else
      "#{severity} #{lines.first}\n"
    end
  end
end
