# frozen_string_literal: true

Sentry.init do |config|
  config.dsn = Rails.application.credentials[:sentry_dsn]

  config.dsn = ENV["SENTRY_DSN"] if ENV["SENTRY_DSN"]
  config.breadcrumbs_logger = [ :active_support_logger, :http_logger ]
  config.release = `git rev-parse HEAD`.strip
end
