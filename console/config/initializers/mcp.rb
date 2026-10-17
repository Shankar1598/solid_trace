# frozen_string_literal: true

# The MCP server's unexpected failures go to the log and to Sentry: the client
# is told only that the call failed. The gem hands over a tool's failure as the
# tool's own exception, and a client's mistake, such as an unknown tool, as a
# RequestHandlerError that isn't an internal error, which isn't reported.
MCP.configure do |config|
  config.exception_reporter = lambda do |error, _context|
    next if error.is_a?(MCP::Server::RequestHandlerError) && error.error_type != :internal_error

    backtrace = Rails.backtrace_cleaner.clean(error.backtrace.to_a)
    Rails.logger.error([ "MCP: #{error.class}: #{error.message}", *backtrace ].join("\n"))
    Sentry.capture_exception(error)
  end
end
