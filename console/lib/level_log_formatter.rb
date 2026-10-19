require "active_support"
require "active_support/logger"
require "active_support/tagged_logging"

# Production's log lines start with their Level, `INFO [request-id] message`,
# so Vector can read it when it ships them as Logs (docs/adr/0011).
class LevelLogFormatter < ActiveSupport::Logger::SimpleFormatter
  def self.logger(io)
    logger = ActiveSupport::Logger.new(io)
    logger.formatter = new
    ActiveSupport::TaggedLogging.new(logger)
  end

  def call(severity, time, progname, msg)
    "#{severity} #{super}"
  end
end
