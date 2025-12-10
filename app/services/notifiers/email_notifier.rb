module Notifiers
  class EmailNotifier
    def initialize(integration, issue)
      @integration = integration
      @issue = issue
    end

    def call
      recipients = @integration.recipients
      return if recipients.blank?

      IssueMailer.notify(@issue, recipients).deliver_later
    end
  end
end
