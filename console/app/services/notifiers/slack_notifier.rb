# frozen_string_literal: true

module Notifiers
  class SlackNotifier
    include HttpDelivery

    # +kind+ is the Notification kind and selects the message. A batched kind
    # lists every Issue in +issues+; the others describe +issues.first+.
    def initialize(integration, kind:, issues:, previous_assignee_name: nil, new_assignee_name: nil)
      @integration = integration
      @kind = kind
      @issues = issues
      @previous_assignee_name = previous_assignee_name
      @new_assignee_name = new_assignee_name
    end

    # Raises NotConfigured without a webhook URL, and DeliveryFailed when
    # the provider does not accept the message.
    def call
      raise NotConfigured, "No webhook URL" if webhook_url.blank?

      post_json(webhook_url, payload)
    end

    private

    attr_reader :integration, :kind, :issues, :previous_assignee_name, :new_assignee_name

    def issue
      issues.first
    end

    def webhook_url
      integration.webhook_url
    end

    def payload
      case kind
      when IntegrationNotification::ISSUE_CREATED then issue_created_payload
      when IntegrationNotification::THRESHOLD_REACHED then threshold_payload
      when IntegrationNotification::ASSIGNMENT_CHANGED then assignment_payload
      else raise ArgumentError, "Unknown notification kind: #{kind.inspect}"
      end
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
          issue_fields_block
        ],
      }
    end

    def assignment_payload
      previous = previous_assignee_name.presence || "Unassigned"
      current = new_assignee_name.presence || "Unassigned"

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
              { type: "mrkdwn", text: "*To:*\n#{current}" }
            ],
          }
        ],
      }
    end

    def issue_created_payload
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
          }
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
          { type: "mrkdwn", text: "*Project:*\n#{issue.project.name}" }
        ],
      }
    end
  end
end
