require "net/http"
require "uri"
require "json"

module Notifiers
  class SlackNotifier
    def initialize(integration, issue)
      @integration = integration
      @issue = issue
    end

    def call
      return unless webhook_url.present?

      uri = URI.parse(webhook_url)
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = true
      http.open_timeout = 5
      http.read_timeout = 5

      request = Net::HTTP::Post.new(uri.path)
      request["Content-Type"] = "application/json"
      request.body = payload.to_json

      response = http.request(request)

      unless response.is_a?(Net::HTTPSuccess)
        Rails.logger.warn("Slack notification failed: #{response.code} #{response.body}")
      end

      response
    rescue StandardError => e
      Rails.logger.error("Slack notification error: #{e.message}")
      nil
    end

    private

    attr_reader :integration, :issue

    def webhook_url
      integration.webhook_url
    end

    def payload
      {
        text: "🚨 New Issue: #{issue.title}",
        blocks: [
          {
            type: "header",
            text: {
              type: "plain_text",
              text: "🚨 New Issue Created",
              emoji: true
            }
          },
          {
            type: "section",
            fields: [
              {
                type: "mrkdwn",
                text: "*Title:*\n#{issue.title}"
              },
              {
                type: "mrkdwn",
                text: "*Kind:*\n#{issue.kind}"
              },
              {
                type: "mrkdwn",
                text: "*Culprit:*\n#{issue.culprit || 'N/A'}"
              },
              {
                type: "mrkdwn",
                text: "*Project:*\n#{issue.project.name}"
              }
            ]
          }
        ]
      }
    end
  end
end
