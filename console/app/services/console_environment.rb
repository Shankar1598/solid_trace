# frozen_string_literal: true

# What Console read from its environment when it started: each setting's
# label, the env var it comes from, the value in effect, whether that's the
# default, and whether it's a secret. A secret's value is only whether it's
# set. Changing any of them takes a restart (deploy/.env).
class ConsoleEnvironment
  # The defaults config/puma.rb and config/queue.yml fall back to. Both read
  # ENV inline as they load, with nothing Console can ask afterwards, so a
  # change to either default must be copied here.
  DEFAULT_WEB_THREADS = 10
  DEFAULT_JOB_PROCESSES = "1"

  # The secrets, whose values never leave the server.
  SECRETS = { "SECRET_KEY_BASE" => "Secret key base", "SENTRY_DSN" => "Sentry DSN" }.freeze

  # console_url is the URL this request reached Console at, which ingest's
  # public URL falls back to (EventStore.public_ingest_url).
  def initialize(console_url:, env: ENV)
    @console_url = console_url
    @env = env
  end

  def settings
    [
      row("Console URL", "CONSOLE_URL", @env["CONSOLE_URL"].presence),
      row("Public ingest URL", "PUBLIC_INGEST_URL", EventStore.public_ingest_url(@console_url)),
      row("EventStore query API", "EVENT_STORE_URL", EventStore.base_url),
      row("EventStore ingest", "EVENT_STORE_INGEST_URL", EventStore.ingest_url),
      row("Version", "SOLIDTRACE_VERSION", Rails.application.config.x.solidtrace_version),
      row("Web threads", "RAILS_MAX_THREADS", web_threads.to_s),
      row("Job processes", "JOB_CONCURRENCY", @env.fetch("JOB_CONCURRENCY", DEFAULT_JOB_PROCESSES)),
      row("Log level", "RAILS_LOG_LEVEL", Rails.application.config.log_level.to_s),
      *SECRETS.map { |source, label| secret_row(label, source) },
    ]
  end

  private

  def row(label, source, value)
    { label: label, source: source, value: value, default: @env[source].blank?, secret: false }
  end

  def secret_row(label, source)
    { label: label, source: source, value: @env[source].present?, default: @env[source].blank?, secret: true }
  end

  # The threads of the Puma serving this request, or, outside Puma, the count
  # config/puma.rb gives production.
  def web_threads
    (defined?(Puma::Server) && Puma::Server.current&.max_threads) || @env.fetch("RAILS_MAX_THREADS", DEFAULT_WEB_THREADS).to_i
  end
end
