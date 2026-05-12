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

  # Convenience alias for callers that only have an Issue. Infers
  # +newly_created+ from ActiveRecord's dirty tracking.
  def self.notify(issue)
    new(issue, issue.id_previously_changed?).check_and_notify
  end

  # Delivers a batch of pending Notification rows for the given integration.
  # Resolves Issues from notification payloads and calls the appropriate
  # provider notifier with an +issue_created_batch+ event.
  def self.deliver_batch(integration, row_ids)
    Notification.mark_rows(row_ids, :processing)

    notifications = Notification.where(id: row_ids).order(:created_at)
    issue_ids = notifications.map { |n| n.payload["issue_id"] }.compact.uniq
    issues = Issue.where(id: issue_ids).includes(:project).index_by(&:id)
    ordered_issues = notifications.map { |n| issues[n.payload["issue_id"]] }.compact

    if ordered_issues.any?
      notifier_class = notifier_for(integration.provider)
      if notifier_class
        notification_payload = { event: "issue_created_batch", issues: ordered_issues }
        notifier_class.new(integration, ordered_issues.first, notification: notification_payload).call
        Notification.mark_rows(row_ids, :sent)
      else
        Notification.mark_rows(row_ids, :sent, "Notifier unavailable")
      end
    else
      Notification.mark_rows(row_ids, :sent, "Issues missing")
    end
  rescue StandardError => e
    Notification.mark_rows(row_ids, :failed, e.message)
    raise e
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
      IntegrationNotificationProcessorJob.set(wait: Notification::GROUPING::INITIAL_DELAY).perform_later(integration.id)
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
