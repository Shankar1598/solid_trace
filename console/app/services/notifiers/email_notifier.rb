# frozen_string_literal: true

module Notifiers
  class EmailNotifier
    # +kind+ is the Notification kind and selects the mail. A batched kind
    # lists every Issue in +issues+; the others describe +issues.first+.
    def initialize(integration, kind:, issues:, previous_assignee_name: nil, new_assignee_name: nil)
      @integration = integration
      @kind = kind
      @issues = issues
      @previous_assignee_name = previous_assignee_name
      @new_assignee_name = new_assignee_name
    end

    def call
      recipients = @integration.recipients
      return if recipients.blank?

      mail(recipients).deliver_later
    end

    private

    def mail(recipients)
      case @kind
      when IntegrationNotification::ISSUE_CREATED
        IssueMailer.batch_notify(@issues, recipients)
      when IntegrationNotification::THRESHOLD_REACHED
        IssueMailer.notify(@issues.first, recipients)
      when IntegrationNotification::ASSIGNMENT_CHANGED
        IssueMailer.assignment_updated(
          @issues.first,
          recipients,
          previous_assignee_name: @previous_assignee_name,
          new_assignee_name: @new_assignee_name
        )
      else
        raise ArgumentError, "Unknown notification kind: #{@kind.inspect}"
      end
    end
  end
end
