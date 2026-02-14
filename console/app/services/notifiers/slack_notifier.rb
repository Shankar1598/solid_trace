# frozen_string_literal: true

require "net/http"
require "uri"
require "json"

module Notifiers
  class SlackNotifier
    def initialize(integration, issue, notification: nil)
      @integration = integration
      @issue = issue
      @notification = notification || {}
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
    attr_reader :notification

    def webhook_url
      integration.webhook_url
    end

    def payload
      case notification[:event]
      when "issue_assignment_updated"
        assignment_payload
      when "event_threshold_reached"
        threshold_payload
      when "issue_created_batch"
        issue_created_batch_payload
      else
        issue_created_payload
      end
    end

    def issue_created_payload
      {
        text: "🚨 New Issue: #{issue.title}",
        blocks: [
          {
            type: "header",
            text: {
              type: "plain_text",
              text: "🚨 New Issue Created",
              emoji: true,
            },
          },
          issue_fields_block,
        ],
      }
    end

    def threshold_payload
      {
        text: "📈 Event threshold reached: #{issue.title}",
        blocks: [
          {
            type: "header",
            text: {
              type: "plain_text",
              text: "📈 Event Threshold Reached",
              emoji: true,
            },
          },
          issue_fields_block,
        ],
      }
    end

    def assignment_payload
      previous = notification[:previous_assignee_name].presence || "Unassigned"
      current = notification[:new_assignee_name].presence || "Unassigned"

      {
        text: "👤 Issue assignment updated: #{issue.title}",
        blocks: [
          {
            type: "header",
            text: {
              type: "plain_text",
              text: "👤 Issue Assignment Updated",
              emoji: true,
            },
          },
          {
            type: "section",
            fields: [
              { type: "mrkdwn", text: "*Project:*\n#{issue.project.name}" },
              { type: "mrkdwn", text: "*Issue:*\n##{issue.number} #{issue.title}" },
              { type: "mrkdwn", text: "*From:*\n#{previous}" },
              { type: "mrkdwn", text: "*To:*\n#{current}" },
            ],
          },
        ],
      }
    end

    def issue_created_batch_payload
      issues = batch_issues
      count = issues.count
      display = issues.first(10)
      lines = display.map do |item|
        "• <#{issue_url(item)}|##{item.number} #{item.title}> (#{item.project.name})"
      end
      if count > display.count
        lines << "_and #{count - display.count} more_"
      end
      lines << "<#{issues_url(issues.first)}|View all issues>"

      {
        text: "🚨 #{count} New Issues Detected",
        blocks: [
          {
            type: "header",
            text: {
              type: "plain_text",
              text: "🚨 #{count} New Issues Detected",
              emoji: true,
            },
          },
          {
            type: "section",
            text: {
              type: "mrkdwn",
              text: lines.join("\n"),
            },
          },
        ],
      }
    end

    def issue_fields_block
      {
        type: "section",
        fields: [
          { type: "mrkdwn", text: "*Title:*\n#{issue.title}" },
          { type: "mrkdwn", text: "*Kind:*\n#{issue.kind}" },
          { type: "mrkdwn", text: "*Culprit:*\n#{issue.culprit || 'N/A'}" },
          { type: "mrkdwn", text: "*Project:*\n#{issue.project.name}" },
        ],
      }
    end

    def batch_issues
      issues = Array(notification[:issues]).compact
      issues = [ issue ] if issues.empty? && issue.present?
      issues
    end

    def issue_url(target_issue)
      url_helpers.project_issue_url(
        target_issue.project,
        target_issue,
        org_slug: target_issue.project.organization.slug,
        host: host
      )
    end

    def issues_url(target_issue)
      url_helpers.issues_url(
        org_slug: target_issue.project.organization.slug,
        host: host
      )
    end

    def url_helpers
      Rails.application.routes.url_helpers
    end

    def host
      SolidTrace::Application.config.action_mailer.default_url_options[:host] || "localhost:3000"
    end
  end
end
