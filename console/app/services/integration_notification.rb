# frozen_string_literal: true

# Integration notification: the one place that decides when and how an
# Organization's Integrations are told about its Issues.
#
# Entry points:
# - issue_received_event: an Issue received an Event (and may be new)
# - issue_assignment_changed: an Issue's assignee changed
# - deliver_pending: the per-Integration processor tick
#
# Triggers write Notification rows (the outbox) for every kind whose rule
# applies and schedule a tick. The tick applies the per-Integration rate limit
# and delivers what is due through the provider adapter. The stored kind
# decides the message the adapter formats.
#
# Every row ends in a recorded outcome: sent; failed with the provider's error
# and retried by later ticks up to Notification::MAX_ATTEMPTS; or skipped with
# a reason when it can never be delivered.
module IntegrationNotification
  ISSUE_CREATED = "issue_created"
  THRESHOLD_REACHED = "threshold_reached"
  ASSIGNMENT_CHANGED = "assignment_changed"

  # Delay between the first row for an Integration and its tick, so that Issues
  # arriving together share one message.
  INITIAL_DELAY = 10.seconds

  # At most one tick delivers per Integration within this window. It is also the
  # debounce before a follow-up tick picks up rows that arrived meanwhile.
  RATE_LIMIT_WINDOW = 1.minute

  # The rule table: which kinds each trigger can produce, and when each applies
  # to an Integration. Every rule is evaluated independently, so one trigger may
  # produce several kinds.
  RULES = {
    issue_received_event: {
      ISSUE_CREATED => ->(integration, _issue, context) {
        context[:newly_created] && integration.notify_on_new_issue
      },
      # Until threshold moves to the outbox, a new Issue that gets an issue
      # created Notification is not also checked for the threshold, as before.
      THRESHOLD_REACHED => ->(integration, issue, context) {
        next false if context[:newly_created] && integration.notify_on_new_issue

        integration.notify_on_event_threshold && event_threshold_reached?(integration, issue)
      },
    },
    issue_assignment_changed: {
      ASSIGNMENT_CHANGED => ->(integration, _issue, _context) { integration.notify_on_assignment },
    },
  }.freeze

  # Kinds delivered together as one message per tick. Every other kind is
  # delivered as one message per row.
  BATCHED_KINDS = [ ISSUE_CREATED ].freeze

  # Kinds still sent synchronously from the trigger instead of through the
  # outbox, as before this module existed.
  SENT_FROM_TRIGGER = [ THRESHOLD_REACHED, ASSIGNMENT_CHANGED ].freeze

  ADAPTERS = {
    "slack" => "Notifiers::SlackNotifier",
    "pagerduty" => "Notifiers::PagerdutyNotifier",
    "email" => "Notifiers::EmailNotifier",
  }.freeze

  class << self
    def issue_received_event(issue, newly_created:)
      apply_rules(:issue_received_event, issue, newly_created: newly_created)
    end

    def issue_assignment_changed(issue, previous_assignee_id:, new_assignee_id:)
      apply_rules(
        :issue_assignment_changed,
        issue,
        previous_assignee_id: previous_assignee_id,
        new_assignee_id: new_assignee_id
      )
    end

    def deliver_pending(integration_id)
      row_ids = Notification.pending_for(integration_id, Time.current).pluck(:id)
      return if row_ids.empty?

      integration = Integration.find_by(id: integration_id)
      return Notification.mark_rows(row_ids, :skipped, "Integration missing") if integration.nil?
      return Notification.mark_rows(row_ids, :skipped, "Integration inactive") unless integration.active

      window_opens_at = rate_limit_window_opens_at(integration)
      if window_opens_at
        schedule_tick(integration, wait_until: window_opens_at)
        return
      end

      deliver_rows(integration, row_ids)

      if Notification.deliverable.where(integration: integration).exists?
        schedule_tick(integration, wait: RATE_LIMIT_WINDOW)
      end
    end

    private

    def apply_rules(trigger, issue, **context)
      issue.project.organization.integrations.active.find_each do |integration|
        RULES.fetch(trigger).each do |kind, applies|
          next unless applies.call(integration, issue, context)

          if SENT_FROM_TRIGGER.include?(kind)
            send_from_trigger(integration, kind, issue, context)
          else
            record(integration, kind, issue, context)
          end
        end
      rescue StandardError => e
        Rails.logger.error("Integration notification failed for #{integration.provider}: #{e.message}")
      end
    end

    def record(integration, kind, issue, context)
      Notification.enqueue!(
        integration: integration,
        kind: kind,
        payload: { "issue_id" => issue.id }.merge(context.except(:newly_created).stringify_keys)
      )
      schedule_tick(integration, wait: INITIAL_DELAY)
    end

    def schedule_tick(integration, **timing)
      IntegrationNotificationProcessorJob.set(**timing).perform_later(integration.id)
    end

    # When the rate-limit window opens again, or nil if it is open now.
    def rate_limit_window_opens_at(integration)
      last_sent_at = Notification.where(integration: integration, status: :sent).maximum(:sent_at)
      return unless last_sent_at

      opens_at = last_sent_at + RATE_LIMIT_WINDOW
      opens_at if Time.current < opens_at
    end

    def deliver_rows(integration, row_ids)
      rows = Notification.where(id: row_ids).order(:created_at).to_a
      issues = Issue.where(id: rows.map { |row| row.payload["issue_id"] }).includes(project: :organization).index_by(&:id)
      issue_for = ->(row) { issues[row.payload["issue_id"]] }

      rows_without_issue, rows = rows.partition { |row| issue_for.(row).nil? }
      Notification.mark_rows(rows_without_issue.map(&:id), :skipped, "Issue missing") if rows_without_issue.any?
      Notification.mark_rows(rows.map(&:id), :processing)

      rows.group_by(&:kind).each do |kind, kind_rows|
        if BATCHED_KINDS.include?(kind)
          deliver_message(integration, kind, kind_rows, kind_rows.map(&issue_for))
        else
          kind_rows.each { |row| deliver_message(integration, kind, [ row ], [ issue_for.(row) ], row.payload) }
        end
      end
    end

    # Delivers one message and records its outcome on every row it carries.
    def deliver_message(integration, kind, rows, issues, payload = {})
      row_ids = rows.map(&:id)
      adapter_for(integration).new(integration, kind: kind, issues: issues, **assignee_names(issues.first, payload)).call
      Notification.mark_rows(row_ids, :sent)
    rescue Notifiers::NotConfigured => e
      Notification.mark_rows(row_ids, :skipped, e.message)
    rescue StandardError => e
      Rails.logger.warn("Integration notification to #{integration.provider} failed: #{e.message}")
      Notification.mark_rows(row_ids, :failed, e.message)
    end

    def send_from_trigger(integration, kind, issue, context)
      adapter_for(integration).new(integration, kind: kind, issues: [ issue ], **assignee_names(issue, context)).call
    rescue Notifiers::NotConfigured
      nil
    end

    def adapter_for(integration)
      ADAPTERS.fetch(integration.provider).constantize
    end

    def assignee_names(issue, context)
      context = context.symbolize_keys
      return {} unless context.key?(:previous_assignee_id) || context.key?(:new_assignee_id)

      members = issue.project.organization.organization_users.includes(:user)
      {
        previous_assignee_name: member_name(members, context[:previous_assignee_id]),
        new_assignee_name: member_name(members, context[:new_assignee_id]),
      }
    end

    def member_name(members, organization_user_id)
      return unless organization_user_id

      members.find_by(id: organization_user_id)&.user&.name
    end

    def event_threshold_reached?(integration, issue)
      issue.events.newer_than(integration.time_window_minutes.minutes.ago).count == integration.event_threshold
    end
  end
end
