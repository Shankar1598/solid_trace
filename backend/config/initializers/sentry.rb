if Rails.env.production?
  Sentry.init do |config|
    config.dsn = 'http://testkey123@localhost:3000/1'
    config.breadcrumbs_logger = [:active_support_logger, :http_logger]

    # Set traces_sample_rate to 1.0 to capture 100%
    # of transactions for performance monitoring.
    # We recommend adjusting this value in production.
    config.traces_sample_rate = 1.0
  end
end
