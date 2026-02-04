# frozen_string_literal: true

module Notifiers
  class EmailNotifier
    def initialize(integration, issue, notification: nil)
      @integration = integration
      @issue = issue
      @notification = notification || {}
    end

    def call
      recipients = @integration.recipients
      return if recipients.blank?

      case @notification[:event]
      when "issue_assignment_updated"
        IssueMailer.assignment_updated(
          @issue,
          recipients,
          previous_assignee_name: @notification[:previous_assignee_name],
          new_assignee_name: @notification[:new_assignee_name]
        ).deliver_later
      else
        IssueMailer.notify(@issue, recipients).deliver_later
      end
    end
  end
end
