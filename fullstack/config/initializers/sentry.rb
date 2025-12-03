Sentry.init do |config|
  config.dsn = 'http://testkey123@localhost:3000/1'
  config.breadcrumbs_logger = [:active_support_logger, :http_logger]
  # config.release = `git rev-parse HEAD`.strip

  config.sdk_logger = Logger.new("log/sentry.log")
  config.sdk_logger.level = Logger::INFO
end
