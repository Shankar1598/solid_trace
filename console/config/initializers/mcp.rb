# frozen_string_literal: true

# The MCP server's unexpected failures go to Rails' error reporter (and so to
# Sentry): the client is told only that the call failed.
MCP.configure do |config|
  config.exception_reporter = ->(error, _context) { Rails.error.report(error) }
end
