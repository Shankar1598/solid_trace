# frozen_string_literal: true

Sentry.init do |config|
  config.dsn = Rails.application.credentials[:sentry_dsn]

  config.dsn = ENV["SENTRY_DSN"] if ENV["SENTRY_DSN"]
  config.breadcrumbs_logger = [ :active_support_logger, :http_logger ]
  # The Docker image has no git, so there's no release there.
  config.release = begin
    `git rev-parse HEAD`.strip
  rescue Errno::ENOENT
    nil
  end
end
