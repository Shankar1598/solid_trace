# frozen_string_literal: true

require "net/http"
require "uri"
require "json"

module Notifiers
  class PagerdutyNotifier
    EVENTS_API_URL = "https://events.pagerduty.com/v2/enqueue".freeze

    def initialize(integration, issue)
      @integration = integration
      @issue = issue
    end

    def call
      return unless routing_key.present?

      uri = URI.parse(EVENTS_API_URL)
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = true
      http.open_timeout = 5
      http.read_timeout = 5

      request = Net::HTTP::Post.new(uri.path)
      request["Content-Type"] = "application/json"
      request.body = payload.to_json

      response = http.request(request)

      unless response.is_a?(Net::HTTPSuccess)
        Rails.logger.warn("PagerDuty notification failed: #{response.code} #{response.body}")
      end

      response
    rescue StandardError => e
      Rails.logger.error("PagerDuty notification error: #{e.message}")
      nil
    end

    private

    attr_reader :integration, :issue

    def routing_key
      integration.routing_key
    end

    def severity
      integration.severity
    end

    def payload
      {
        routing_key: routing_key,
        event_action: "trigger",
        dedup_key: "garnet-issue-#{issue.id}",
        payload: {
          summary: "[#{issue.kind.upcase}] #{issue.title}",
          source: issue.project.name,
          severity: severity,
          custom_details: {
            issue_id: issue.id,
            issue_number: issue.number,
            culprit: issue.culprit,
            project: issue.project.name,
            organization: issue.project.organization.name
          }
        }
      }
    end
  end
end
