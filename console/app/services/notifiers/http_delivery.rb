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
    def post_json(url, payload)
      uri = URI.parse(url)
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = true
      http.open_timeout = TIMEOUT_SECONDS
      http.read_timeout = TIMEOUT_SECONDS

      request = Net::HTTP::Post.new(uri.path)
      request["Content-Type"] = "application/json"
      request.body = payload.to_json

      response = http.request(request)
      raise DeliveryFailed, "HTTP #{response.code}: #{response.body.to_s.truncate(500)}" unless response.is_a?(Net::HTTPSuccess)

      response
    rescue Net::OpenTimeout, Net::ReadTimeout, SocketError, SystemCallError, IOError, OpenSSL::SSL::SSLError => e
      raise DeliveryFailed, "#{e.class}: #{e.message}"
    end

    def issue_url(issue)
      url_helpers.project_issue_url(issue.project, issue, org_slug: issue.project.organization.slug, host: host)
    end

    def issues_url(issue)
      url_helpers.issues_url(org_slug: issue.project.organization.slug, host: host)
    end

    def url_helpers
      Rails.application.routes.url_helpers
    end

    def host
      SolidTrace::Application.config.action_mailer.default_url_options[:host] || "localhost:3000"
    end
  end
end
