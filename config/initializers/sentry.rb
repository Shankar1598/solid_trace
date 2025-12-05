Sentry.init do |config|
  config.dsn = Rails.application.credentials[:sentry_dsn]

  config.dsn = ENV["SENTRY_DSN"] if ENV["SENTRY_DSN"]
  config.breadcrumbs_logger = [:active_support_logger, :http_logger]
  config.release = `git rev-parse HEAD`.strip

  config.sdk_logger = Logger.new("log/sentry.log")
  config.sdk_logger.level = Logger::INFO
end
