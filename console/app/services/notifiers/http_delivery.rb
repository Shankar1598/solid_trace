# frozen_string_literal: true

require "net/http"
require "uri"
require "json"

module Notifiers
  # The HTTP transport and Console URLs shared by the Slack and PagerDuty
  # adapters. Only the adapters include it.
  module HttpDelivery
    TIMEOUT_SECONDS = 5

    private

    # POSTs +payload+ as JSON to +url+ and returns the response. Anything other
    # than a 2xx response raises DeliveryFailed.
    def post_json(url, payload, operator_set: false)
      response = post(url, payload.to_json, operator_set: operator_set)
      raise DeliveryFailed, "HTTP #{response.code}: #{response.body.to_s.truncate(500)}" unless response.is_a?(Net::HTTPSuccess)

      response
    end

    # POSTs the JSON text +body+ to +url+, with +headers+, and returns the
    # response, whatever its status. A connection error or a timeout raises
    # DeliveryFailed.
    #
    # Only https URLs are sent to, as a member sets an Integration's webhook
    # URL and must not make Console POST to plain-http hosts it reaches, such
    # as EventStore's query API. A plain http URL is sent without TLS only in
    # development and test, where stubs stand in for the providers, or when
    # the operator set it (+operator_set+, PAGERDUTY_EVENTS_URL and the
    # Dispatcher's URLs).
    def post(url, body, operator_set: false, headers: {})
      uri = URI.parse(url)
      plain_http = uri.scheme == "http" && (operator_set || Rails.env.local?)
      unless uri.scheme == "https" || plain_http
        raise DeliveryFailed, "The URL doesn't start with https://, and Console sends notifications over https only."
      end

      http = Net::HTTP.new(uri.hostname, uri.port)
      http.use_ssl = !plain_http
      http.open_timeout = TIMEOUT_SECONDS
      http.read_timeout = TIMEOUT_SECONDS

      request = Net::HTTP::Post.new(uri.request_uri)
      request["Content-Type"] = "application/json"
      headers.each { |name, value| request[name] = value }
      request.body = body

      http.request(request)
    rescue Net::OpenTimeout, Net::ReadTimeout, SocketError, SystemCallError, IOError, OpenSSL::SSL::SSLError => e
      raise DeliveryFailed, "#{e.class}: #{e.message}"
    end

    def issue_url(issue)
      url_helpers.project_issue_url(issue.project, issue, org_slug: issue.project.organization.slug, **PublicUrls.link_options)
    end

    def issues_url(issue)
      url_helpers.issues_url(org_slug: issue.project.organization.slug, **PublicUrls.link_options)
    end

    def url_helpers
      Rails.application.routes.url_helpers
    end
  end
end
