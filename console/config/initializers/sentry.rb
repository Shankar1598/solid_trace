# frozen_string_literal: true

# On an install, deploy/install sets SENTRY_DSN to the Meta Project's DSN and
# SENTRY_TRACES_SAMPLE_RATE to 0.05, so Console sends its Issues, and the
# Transactions of that share of its requests and jobs, to the Meta Project
# (docs/traces.md). Without a sample rate, nothing is traced.
Sentry.init do |config|
  config.dsn = ENV["SENTRY_DSN"].presence || Rails.application.credentials[:sentry_dsn]
  if ENV["SENTRY_TRACES_SAMPLE_RATE"].present?
    config.traces_sample_rate = Float(ENV["SENTRY_TRACES_SAMPLE_RATE"], exception: false)
  end
  config.before_send = ->(event, hint) { ConsoleSentry.before_send(event, hint) }
  config.breadcrumbs_logger = [ :active_support_logger, :http_logger ]
  # The Docker image has no git, but its build sets SOLIDTRACE_VERSION.
  commit = begin
    `git rev-parse HEAD 2>/dev/null`.strip
  rescue Errno::ENOENT
    nil
  end
  config.release = commit.presence || ENV["SOLIDTRACE_VERSION"]
end
