# frozen_string_literal: true

class IntegrationNotifier
  def initialize(issue, newly_created)
    @issue = issue
    @newly_created = newly_created
  end

  def check_and_notify
    organization = issue.project.organization
    organization.integrations.active.find_each do |integration|
      notify_if_conditions_met(integration)
    rescue StandardError => e
      Rails.logger.error("Integration notification failed for #{integration.provider}: #{e.message}")
    end
  end

  def self.notify_assignment(issue, previous_assignee_id:, new_assignee_id:)
    organization = issue.project.organization
    organization_users = organization.organization_users.includes(:user)
    previous_assignee_name = previous_assignee_id ? organization_users.find_by(id: previous_assignee_id)&.user&.name : nil
    new_assignee_name = new_assignee_id ? organization_users.find_by(id: new_assignee_id)&.user&.name : nil

    notification = {
      event: "issue_assignment_updated",
      previous_assignee_id: previous_assignee_id,
      new_assignee_id: new_assignee_id,
      previous_assignee_name: previous_assignee_name,
      new_assignee_name: new_assignee_name,
    }

    organization.integrations.active.find_each do |integration|
      next unless integration.notify_on_assignment

      notifier_class = notifier_for(integration.provider)
      next unless notifier_class

      notifier_class.new(integration, issue, notification: notification).call
    rescue StandardError => e
      Rails.logger.error("Integration assignment notification failed for #{integration.provider}: #{e.message}")
    end
  end

  def self.notify(issue)
    # Determine if newly created based on some logic or assume false if not passed?
    # Original usage in tests: IntegrationNotifier.notify(issue)
    # The listener likely calls this.
    # We should match what EventIngestor does or what tests expect.
    # Tests pass just issue.
    # Let's assume newly created is true for now or check issue status?
    # Actually, the listener `IntegrationNotificationJob` likely calls `new(issue, newly_created).check_and_notify`.
    # Tests are calling static method. Let's redirect static to instance with default.
    new(issue, issue.id_previously_changed?).check_and_notify
  end

  private

  attr_reader :issue, :newly_created

  def notify_if_conditions_met(integration)
    return unless should_notify?(integration)

    event = event_name_for(integration)
    if event == "issue_created"
      Notification.enqueue!(
        integration: integration,
        event_type: event,
        payload: { "issue_id" => issue.id }
      )
      return
    end

    notifier_class = notifier_for(integration.provider)
    return unless notifier_class

    notifier_class.new(integration, issue, notification: { event: event }).call
  end

  def should_notify?(integration)
    # Rule 1: Always notify for new issues (if enabled)
    if integration.notify_on_new_issue && @newly_created
      return true
    end

    # Rule 2: Notify if X events occurred within Y minutes (if enabled)
    if integration.notify_on_event_threshold && event_threshold_exceeded?(integration)
      return true
    end

    false
  end

  def event_threshold_exceeded?(integration)
    threshold = integration.event_threshold
    time_window = integration.time_window_minutes

    recent_event_count = issue.events.newer_than(time_window.minutes.ago).count

    # Notify only when we hit exactly the threshold (not every event after)
    recent_event_count == threshold
  end

  def event_name_for(integration)
    return "issue_created" if integration.notify_on_new_issue && newly_created
    return "event_threshold_reached" if integration.notify_on_event_threshold

    "issue_notification"
  end

  def self.notifier_for(provider)
    case provider
    when "slack"
      Notifiers::SlackNotifier
    when "pagerduty"
      Notifiers::PagerdutyNotifier
    when "email"
      Notifiers::EmailNotifier
    end
  end

  def notifier_for(provider)
    self.class.notifier_for(provider)
  end
end
