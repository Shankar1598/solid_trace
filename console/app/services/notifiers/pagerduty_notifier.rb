# frozen_string_literal: true

require "net/http"
require "uri"
require "json"

module Notifiers
  class PagerdutyNotifier
    EVENTS_API_URL = "https://events.pagerduty.com/v2/enqueue".freeze

    # +kind+ is the Notification kind and selects the message. A batched kind
    # lists every Issue in +issues+; the others describe +issues.first+.
    def initialize(integration, kind:, issues:, previous_assignee_name: nil, new_assignee_name: nil)
      @integration = integration
      @kind = kind
      @issues = issues
      @previous_assignee_name = previous_assignee_name
      @new_assignee_name = new_assignee_name
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

    attr_reader :integration, :kind, :issues, :previous_assignee_name, :new_assignee_name

    def issue
      issues.first
    end

    def routing_key
      integration.routing_key
    end

    def severity
      integration.severity
    end

    def payload
      case kind
      when IntegrationNotification::ISSUE_CREATED then issue_created_payload
      when IntegrationNotification::THRESHOLD_REACHED then issue_payload("Event threshold reached")
      when IntegrationNotification::ASSIGNMENT_CHANGED then issue_payload("Issue assignment updated")
      else raise ArgumentError, "Unknown notification kind: #{kind.inspect}"
      end
    end

    def issue_payload(summary_prefix)
      {
        routing_key: routing_key,
        event_action: "trigger",
        dedup_key: "solid-trace-issue-#{issue.id}",
        payload: {
          summary: "[#{issue.kind.upcase}] #{summary_prefix}: #{issue.title}",
          source: issue.project.name,
          severity: severity,
          custom_details: {
            issue_id: issue.id,
            issue_number: issue.number,
            culprit: issue.culprit,
            project: issue.project.name,
            organization: issue.project.organization.name,
            previous_assignee: previous_assignee_name,
            new_assignee: new_assignee_name,
          },
        },
      }
    end

    def issue_created_payload
      count = issues.count
      display = issues.first(10)
      more_count = count - display.count

      {
        routing_key: routing_key,
        event_action: "trigger",
        dedup_key: "solid-trace-issues-batch-#{integration.id}",
        payload: {
          summary: "#{count} new issues detected",
          source: issues.first.project.organization.name,
          severity: severity,
          custom_details: {
            organization: issues.first.project.organization.name,
            count: count,
            issues: display.map do |item|
              {
                id: item.id,
                number: item.number,
                title: item.title,
                project: item.project.name,
                url: issue_url(item),
              }
            end,
            more_count: more_count.positive? ? more_count : 0,
            issues_url: issues_url(issues.first),
          },
        },
      }
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
