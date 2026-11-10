# frozen_string_literal: true

# Console's URL and ingest's URL as people, links and SDKs reach them
# (ADR 0017). Each has one resolver, which every email, invite link, MCP link,
# DSN and Logs snippet Console builds goes through:
#
# - console_url: the Console URL instance setting, else CONSOLE_URL, else the
#   address the request reached Console at.
# - ingest_url: the Ingest URL instance setting, else PUBLIC_INGEST_URL, else
#   Console's host, as console_url gives it, at EventStore's ingest port.
#
# A blank setting is nil, so an install that never saves one keeps what
# deploy/.env and the request give. Jobs and mailers have no request, so
# without a setting or an env var they have no URL: ApplicationMailer and the
# Notifiers then use the environment's default_url_options.
#
# HTTPS isn't one of these settings: Console's HTTPS mode follows CONSOLE_URL's
# scheme at boot (config/environments/production.rb), and InstanceSetting
# refuses a Console URL with the other scheme from the page and MCP.
module PublicUrls
  # EventStore's ingest port, where ingest is on Console's host by default.
  INGEST_PORT = 4000

  module_function

  # Console's URL, or nil in a job or mailer with none saved or set.
  # request_url is the address the request reached Console at.
  def console_url(request_url = nil, settings: InstanceSetting.current, env: ENV)
    settings.console_url || console_url_fallback(request_url, env: env)&.fetch(:url)
  end

  # Ingest's URL as SDKs and shippers reach it, or nil when there's no Console
  # URL to take its host from.
  def ingest_url(request_url = nil, settings: InstanceSetting.current, env: ENV)
    settings.ingest_url || ingest_url_fallback(request_url, settings: settings, env: env)&.fetch(:url)
  end

  # What a blank Console URL setting uses, { url:, source: }, source being
  # "CONSOLE_URL" or "request"; nil when neither gives one.
  def console_url_fallback(request_url = nil, env: ENV)
    if (url = clean(env["CONSOLE_URL"]))
      { url: url, source: "CONSOLE_URL" }
    elsif (url = clean(request_url))
      { url: url, source: "request" }
    end
  end

  # What a blank Ingest URL setting uses, { url:, source: }, source being
  # "PUBLIC_INGEST_URL" or "console_url" (Console's host at INGEST_PORT); nil
  # when neither gives one.
  def ingest_url_fallback(request_url = nil, settings: InstanceSetting.current, env: ENV)
    if (url = clean(env["PUBLIC_INGEST_URL"]))
      { url: url, source: "PUBLIC_INGEST_URL" }
    elsif (console = console_url(request_url, settings: settings, env: env))
      uri = URI(console)
      { url: "#{uri.scheme}://#{uri.host}:#{INGEST_PORT}", source: "console_url" }
    end
  end

  # url_for's options for Console's URL, for links built with no request
  # (mailers, Notifiers): { host:, port:, protocol: }, or nil with none.
  def console_url_options(settings: InstanceSetting.current, env: ENV)
    url = console_url(settings: settings, env: env)
    return unless url

    uri = URI(url)
    { host: uri.host, port: uri.port, protocol: uri.scheme }
  end

  # Whether Console started with HTTPS on, from CONSOLE_URL's scheme
  # (config/environments/production.rb). It changes only with a restart.
  def https?
    Rails.application.config.force_ssl == true
  end

  # A URL from an env var or a request, without blanks or a trailing slash.
  def clean(url)
    url.to_s.strip.sub(%r{/+\z}, "").presence
  end
  private_class_method :clean
end
