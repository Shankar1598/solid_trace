# frozen_string_literal: true

require "active_support/core_ext/integer/time"
require_relative "../../lib/level_log_formatter"

Rails.application.configure do
  # Settings specified here will take precedence over those in config/application.rb.

  # Code is not reloaded between requests.
  config.enable_reloading = false

  # Eager load code on boot for better performance and memory savings (ignored by Rake tasks).
  config.eager_load = true

  # Full error reports are disabled.
  config.consider_all_requests_local = false

  # Turn on fragment caching in view templates.
  config.action_controller.perform_caching = true

  # Cache assets for far-future expiry since they are all digest stamped.
  config.public_file_server.headers = { "cache-control" => "public, max-age=#{1.year.to_i}" }

  # Enable serving of images, stylesheets, and JavaScripts from an asset server.
  # config.asset_host = "http://assets.example.com"

  # Store uploaded files on the local file system (see config/storage.yml for options).
  config.active_storage.service = :local

  # With HTTPS on in deploy/setup-gcp, CONSOLE_URL is https:// and Caddy terminates
  # TLS in front of Thruster. Without it, Thruster serves plain HTTP on port 3000.
  console_url = URI(ENV["CONSOLE_URL"].presence || "http://example.com")
  if console_url.scheme == "https"
    # Every request came through Caddy, so URLs and redirects are https://.
    config.assume_ssl = true
    # Secure cookies and the http-to-https redirect. HSTS stays off until the
    # install has a domain: browsers ignore it for an IP, and on a test hostname
    # it would refuse plain HTTP for years after HTTPS was turned off.
    config.force_ssl = true
    config.ssl_options = { hsts: false, redirect: { exclude: ->(request) { request.path == "/up" } } }
  end

  # Log to STDOUT with the current request id as a default log tag, each line
  # starting with its Level: `INFO [request-id] message`.
  config.log_tags = [ :request_id ]
  config.logger   = LevelLogFormatter.logger(STDOUT)

  # Change to "debug" to log everything (including potentially personally-identifiable information!).
  config.log_level = ENV.fetch("RAILS_LOG_LEVEL", "info")

  # Prevent health checks from clogging up the logs.
  config.silence_healthcheck_path = "/up"

  # Don't log any deprecations.
  config.active_support.report_deprecations = false

  # Replace the default in-process memory cache store with a durable alternative.
  config.cache_store = :solid_cache_store

  # Replace the default in-process and non-durable queuing backend for Active Job.
  config.active_job.queue_adapter = :solid_queue
  config.solid_queue.connects_to = { database: { writing: :queue } }

  # Ignore bad email addresses and do not raise email delivery errors.
  # Set this to true and configure the email server for immediate delivery to raise delivery errors.
  # config.action_mailer.raise_delivery_errors = false

  # Links in emails point at Console's public URL. deploy/setup-gcp writes it, and
  # the SMTP settings below, to deploy/.env.
  config.action_mailer.default_url_options = { host: console_url.host, port: console_url.port, protocol: console_url.scheme }

  if ENV["SMTP_ADDRESS"].present?
    config.action_mailer.smtp_settings = {
      address: ENV["SMTP_ADDRESS"],
      port: (ENV["SMTP_PORT"].presence || 587).to_i,
      user_name: ENV["SMTP_USERNAME"].presence,
      password: ENV["SMTP_PASSWORD"].presence,
      authentication: (:plain if ENV["SMTP_USERNAME"].present?),
    }.compact
  end

  # Enable locale fallbacks for I18n (makes lookups for any locale fall back to
  # the I18n.default_locale when a translation cannot be found).
  config.i18n.fallbacks = true

  # Do not dump schema after migrations.
  config.active_record.dump_schema_after_migration = false

  # Only use :id for inspections in production.
  config.active_record.attributes_for_inspect = [ :id ]

  # Enable DNS rebinding protection and other `Host` header attacks.
  # config.hosts = [
  #   "example.com",     # Allow requests from example.com
  #   /.*\.example\.com/ # Allow requests from subdomains like `www.example.com`
  # ]
  #
  # Skip DNS rebinding protection for the default health check endpoint.
  # config.host_authorization = { exclude: ->(request) { request.path == "/up" } }
end
