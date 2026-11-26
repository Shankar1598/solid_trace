# frozen_string_literal: true

# Integration notification: the one place that decides when and how an
# Organization's Integrations are told about its Issues and Monitors.
#
# Entry points:
# - issue_received_event: an Issue received an Event (and may be new)
# - check_event_threshold: the same, for the rule that reads EventStore
# - issue_assignment_changed: an Issue's assignee changed
# - monitor_changed: a Monitor's group started or stopped firing, or Logs
#   matched a Match monitor
# - monitor_status: an Organization's Monitors stopped running, or run
#   again (MonitorStatusJob)
# - deliver_pending: the per-Integration processor tick
#
# Triggers write Notification rows (the outbox) for every kind whose rule
# applies and schedule a tick. The tick applies the per-Integration rate limit
# and delivers what is due through the provider adapter. The stored kind
# decides the message the adapter formats.
#
# Every row ends in a recorded outcome: sent; failed with the provider's error
# and retried by later ticks, up to Notification::MAX_ATTEMPTS for an Issue
# row and for Notification::MONITOR_RETRY_FOR for a Monitor row; refused
# with the receiver's reason, and not retried, when the receiver will never
# take it (the Factory provider's 4xx other than 429); or skipped with a
# reason when it can never be delivered.
module IntegrationNotification
  ISSUE_CREATED = "issue_created"
  THRESHOLD_REACHED = "threshold_reached"
  ASSIGNMENT_CHANGED = "assignment_changed"
  MONITOR_FIRING = "monitor_firing"
  MONITOR_RESOLVED = "monitor_resolved"
  MONITOR_MATCHED = "monitor_matched"
  MONITOR_KINDS = [ MONITOR_FIRING, MONITOR_RESOLVED, MONITOR_MATCHED ].freeze
  # About an Organization's Monitors, to its Monitor status Integration.
  MONITORS_NOT_RUNNING = "monitors_not_running"
  MONITORS_RUNNING_AGAIN = "monitors_running_again"
  MONITOR_STATUS_KINDS = [ MONITORS_NOT_RUNNING, MONITORS_RUNNING_AGAIN ].freeze
  # The kinds whose adapter sends a Notifiers::Message.
  MESSAGE_KINDS = (MONITOR_KINDS + MONITOR_STATUS_KINDS).freeze

  # How long a Monitor is not running before monitor_status tells the team.
  NOT_RUNNING_AFTER = 2.minutes
  # How far back a "not running" row is read to see whether it was
  # delivered. A newer row that told a Monitor again is younger, and a
  # sent row is deleted only a day after it was sent
  # (Maintenance::NotificationCleanupJob), so the newest row that names
  # a Monitor is always read.
  UNDELIVERED_LOOKBACK = 1.day

  # Delay between the first row for an Integration and its tick, so that Issues
  # arriving together share one message.
  INITIAL_DELAY = 10.seconds

  # The most Monitor rows one statement writes.
  ROWS_PER_WRITE = 1_000

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
    },
    # Apart from issue_received_event because it reads EventStore, which may
    # be unable to count Events for a long time.
    event_threshold_check: {
      THRESHOLD_REACHED => ->(integration, issue, _context) {
        integration.notify_on_event_threshold && event_threshold_reached?(integration, issue)
      },
    },
    issue_assignment_changed: {
      ASSIGNMENT_CHANGED => ->(integration, _issue, _context) { integration.notify_on_assignment },
    },
  }.freeze

  # Kinds delivered together as one message per tick. Every other kind is
  # delivered as one message per row. Monitor kinds never are: each row is
  # one step of its group's chain. The Factory provider batches none: each
  # Issue is its own delivery, which can start its own Run.
  BATCHED_KINDS = [ ISSUE_CREATED ].freeze

  ADAPTERS = {
    "slack" => "Notifiers::SlackNotifier",
    "pagerduty" => "Notifiers::PagerdutyNotifier",
    "email" => "Notifiers::EmailNotifier",
    "factory" => "Notifiers::FactoryNotifier",
  }.freeze

  class << self
    def issue_received_event(issue, newly_created:)
      apply_rules(:issue_received_event, issue, newly_created: newly_created)
    end

    # Raises EventStore::Unavailable, before writing anything, when it cannot
    # get the windowed Event count; the caller retries.
    def check_event_threshold(issue)
      apply_rules(:event_threshold_check, issue)
    end

    def issue_assignment_changed(issue, previous_assignee_id:, new_assignee_id:)
      apply_rules(
        :issue_assignment_changed,
        issue,
        previous_assignee_id: previous_assignee_id,
        new_assignee_id: new_assignee_id
      )
    end

    # Writes a row of +kind+ about one group of a Monitor for each of its
    # active Integrations, or of +integrations+ when given, and schedules
    # their ticks.
    def monitor_changed(monitor, kind:, group_key:, payload:, integrations: monitor.integrations)
      schedule_ticks(monitor_changes(monitor, [ [ kind, group_key, payload ] ], integrations: integrations))
    end

    # Writes the rows of many changes of a Monitor's groups, each
    # [kind, group_key, payload], for each of its active Integrations, or of
    # +integrations+ when given, ROWS_PER_WRITE rows a statement. Returns the
    # Integrations written to, whose ticks the caller schedules
    # (schedule_ticks) once the rows are committed.
    #
    # MonitorEvaluation calls it once a run, with each change of a group's
    # status, in the transaction that records the changes and holds SQLite's
    # write lock, so the rows follow the Monitor's states: a group that
    # stays firing writes nothing more. A run may change thousands of
    # groups, hence the bulk write, and the ticks wait for the commit.
    # +payload+ is what the run saw, for the message
    # (Notifiers::MonitorMessage). Each row also keeps what the message says
    # about the Monitor, so it is delivered even when the Monitor is deleted
    # first.
    def monitor_changes(monitor, changes, integrations: monitor.integrations)
      unknown = changes.map(&:first).find { |kind| !MONITOR_KINDS.include?(kind) }
      raise ArgumentError, "Unknown Monitor notification kind: #{unknown.inspect}" if unknown
      return [] if changes.empty?

      integrations = integrations.active.to_a
      about = Notifiers::MonitorMessage.about(monitor)
      now = Time.current
      rows = changes.flat_map do |kind, group_key, payload|
        payload = payload.merge("monitor" => about)
        integrations.map do |integration|
          { integration_id: integration.id, monitor_id: monitor.id, group_key: group_key, kind: kind,
            payload: payload, status: :pending, created_at: now, updated_at: now }
        end
      end
      rows.each_slice(ROWS_PER_WRITE) { |slice| Notification.insert_all!(slice) }
      integrations
    end

    # One tick for each Integration, however many rows it was written.
    def schedule_ticks(integrations)
      integrations.uniq.each { |integration| schedule_tick(integration, wait: INITIAL_DELAY) }
    end

    # Tells the Organization's Monitor status Integration I which enabled
    # Monitors have not run for NOT_RUNNING_AFTER (monitors_not_running), and
    # which of those it was told about run again or were turned off
    # (monitors_running_again): at most one row of each kind. Each Monitor's
    # not_running_told_integration_id names the Integration that was told:
    # - a Monitor that is not running and was not told to I goes into
    #   "not running", and is told to I
    # - a Monitor told to I that runs again, or is off, goes into
    #   "running again", and is told to none
    # - a Monitor told to another Integration, or to I by a "not running"
    #   that was never delivered (forget_undelivered), is told to none with
    #   no message. If it is still not running, the next call tells I.
    # With no Monitor status Integration, every mark is cleared. With an
    # inactive one, nothing changes.
    #
    # The rows and the marks are written in one transaction, so a job that
    # stops at any point and runs again sends each message once. Once it
    # commits, a tick is scheduled while I has status rows to deliver, also
    # when this call wrote none: so a tick that was not scheduled is.
    def monitor_status(organization, now = Time.current)
      integration = organization.monitor_status_integration
      return if integration && !integration.active

      ApplicationRecord.transaction do
        tell_monitor_status(organization, integration, now) if integration
        forget_other_marks(organization.monitors, integration)
      end
      return unless integration

      schedule_ticks([ integration ]) if Notification.deliverable.where(integration: integration, kind: MONITOR_STATUS_KINDS).exists?
    end

    # A deleted Monitor that was told to the Monitor status Integration, the
    # last one told: a "running again" that lists it as deleted, so the
    # PagerDuty incident is resolved. While others told are still not
    # running, it leaves the set with no message. It reads the mark in the
    # delete's transaction, as the job may have changed it.
    def monitor_deleted(monitor)
      organization = monitor.organization
      integration = organization.monitor_status_integration
      return unless integration&.active

      told = organization.monitors.where(not_running_told_integration_id: integration.id)
      return unless told.exists?(id: monitor.id) && !told.where.not(id: monitor.id).exists?

      enabled = organization.monitors.enabled.where.not(id: monitor.id).count
      status_row(integration, MONITORS_RUNNING_AGAIN, Notifiers::MonitorStatusMessage.facts(organization,
        enabled: enabled, all: false, monitors: [ listed(monitor, left: Notifiers::MonitorStatusMessage::DELETED) ]))
      schedule_ticks([ integration ])
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

    # Every rule is evaluated before anything is written, so EventStore being
    # unavailable leaves nothing behind and a retried trigger does not write
    # rows twice. Any other error skips only that Integration.
    def apply_rules(trigger, issue, **context)
      due = []
      issue.project.organization.integrations.active.find_each do |integration|
        RULES.fetch(trigger).each do |kind, applies|
          due << [ integration, kind ] if applies.call(integration, issue, context)
        end
      rescue EventStore::Unavailable
        raise
      rescue StandardError => e
        Rails.logger.error("Integration notification rules failed for #{integration.provider}: #{e.message}")
      end

      due.each do |integration, kind|
        record(integration, kind, issue, context)
      rescue StandardError => e
        Rails.logger.error("Integration notification failed for #{integration.provider}: #{e.message}")
      end
    end

    def record(integration, kind, issue, context)
      Notification.enqueue!(
        integration: integration,
        issue: issue,
        kind: kind,
        payload: context.except(:newly_created).stringify_keys
      )
      schedule_tick(integration, wait: INITIAL_DELAY)
    end

    def tell_monitor_status(organization, integration, now)
      monitors = organization.monitors
      forget_undelivered(integration, monitors)
      told = monitors.where(not_running_told_integration_id: integration.id)
      # where.not leaves out NULL, so the Monitors told to none are asked for apart.
      untold = monitors.where(not_running_told_integration_id: nil)
        .or(monitors.where.not(not_running_told_integration_id: integration.id))
      stopped = untold.enabled.where(not_running_since: ..(now - NOT_RUNNING_AFTER))
        .includes(:project).order(:not_running_since, :id).to_a
      back = told.where(not_running_since: nil).or(told.where.not(disabled_at: nil)).includes(:project).order(:name, :id).to_a

      LogMonitor.where(id: stopped.map(&:id)).update_all(not_running_told_integration_id: integration.id)
      LogMonitor.where(id: back.map(&:id)).update_all(not_running_told_integration_id: nil)
      LogMonitor.where(id: back.map(&:id)).where.not(disabled_at: nil).update_all(not_running_since: nil)
      enabled = monitors.enabled.count

      if stopped.any?
        status_row(integration, MONITORS_NOT_RUNNING, Notifiers::MonitorStatusMessage.facts(organization,
          enabled: enabled, all: enabled > 1 && stopped.size == enabled,
          monitors: stopped.map { |monitor| listed(monitor, not_running: true) }))
      end
      if back.any?
        left = Notifiers::MonitorStatusMessage::TURNED_OFF
        status_row(integration, MONITORS_RUNNING_AGAIN, Notifiers::MonitorStatusMessage.facts(organization,
          enabled: enabled, all: enabled > 1 && back.size == enabled && back.all?(&:enabled?),
          still_not_running: monitors.enabled.where(not_running_told_integration_id: integration.id).count,
          monitors: back.map { |monitor| listed(monitor, left: (left unless monitor.enabled?)) }))
      end
    end

    # A Monitor told to the Integration by a "not running" that ended
    # skipped, refused or exhausted was never told: it is told to none, so
    # that the next call tells it again while it still does not run.
    def forget_undelivered(integration, monitors)
      told = monitors.where(not_running_told_integration_id: integration.id).pluck(:id)
      return if told.empty?

      rows = Notification.where(integration: integration, kind: MONITORS_NOT_RUNNING,
        created_at: UNDELIVERED_LOOKBACK.ago..).order(:created_at, :id).to_a
      undelivered = Notification.undelivered.where(id: rows.map(&:id)).pluck(:id).to_set
      newest = {}
      rows.each { |row| Notifiers::MonitorStatusMessage.read(row.payload).monitors.each { |m| newest[m.id] = row.id } }
      lost = told.select { |id| undelivered.include?(newest[id]) }
      LogMonitor.where(id: lost).update_all(not_running_told_integration_id: nil) if lost.any?
    end

    # The marks of an Integration that is no longer the Monitor status
    # Integration, with no message: the Integration that is tells the
    # Monitors that still do not run.
    def forget_other_marks(monitors, integration)
      others = monitors.where.not(not_running_told_integration_id: nil)
      others = others.where.not(not_running_told_integration_id: integration.id) if integration
      others.where.not(disabled_at: nil).update_all(not_running_since: nil, not_running_told_integration_id: nil)
      others.update_all(not_running_told_integration_id: nil)
    end

    def status_row(integration, kind, facts)
      Notification.create!(integration: integration, kind: kind, payload: Notifiers::MonitorStatusMessage.payload(facts),
        status: :pending)
    end

    # A Monitor as a status row lists it. One that is not running has since
    # when, and why: its last run's error, or the reason that its runs are
    # held.
    def listed(monitor, not_running: false, left: nil)
      facts = { id: monitor.id, name: monitor.name, project: monitor.project.name, left: left }
      if not_running
        held = monitor.runs.first&.held_reason if monitor.last_error.blank?
        facts.merge!(since: monitor.not_running_since.utc.iso8601, error: monitor.last_error.presence || (held && "Held: #{held}"))
      end
      Notifiers::MonitorStatusMessage::Listed.new(**facts)
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

    # Rows about Issues and the other rows are delivered their own ways. A
    # row with no Issue is delivered as a Monitor row, and so is a Monitor
    # status row, which has no Monitor either.
    def deliver_rows(integration, row_ids)
      deliver_issue_rows(integration, row_ids)
      deliver_monitor_rows(integration, row_ids)
    end

    def deliver_issue_rows(integration, row_ids)
      rows = Notification.where(id: row_ids).where.not(issue_id: nil).includes(issue: { project: :organization })
        .order(:created_at).to_a

      rows_without_issue, rows = rows.partition { |row| row.issue.nil? }
      Notification.mark_rows(rows_without_issue.map(&:id), :skipped, "Issue missing") if rows_without_issue.any?
      Notification.mark_rows(rows.map(&:id), :processing)

      rows.group_by(&:kind).each do |kind, kind_rows|
        if BATCHED_KINDS.include?(kind) && !integration.factory?
          deliver_message(integration, kind, kind_rows, kind_rows.map(&:issue))
        else
          kind_rows.each { |row| deliver_message(integration, kind, [ row ], [ row.issue ], row.payload) }
        end
      end
    end

    # Each group of a Monitor is a chain: its rows go one at a time, oldest
    # first, and the chain stops at the first that fails, leaving the rest
    # for the retry. So a resolve never goes before the trigger it closes.
    # A deleted Monitor's rows go too, from what each keeps of it, so the
    # resolve written when it was deleted closes its PagerDuty incident.
    # The Integration's Monitor status rows are one chain too, so "running
    # again" never goes before the "not running" that it follows.
    def deliver_monitor_rows(integration, row_ids)
      rows = Notification.where(id: row_ids, issue_id: nil)
        .includes(monitor: [ :organization, :project ]).order(:created_at, :id).to_a

      rows_without_monitor, rows = rows.partition do |row|
        MONITOR_STATUS_KINDS.exclude?(row.kind) && row.monitor.nil? && row.payload&.dig("monitor").nil?
      end
      Notification.mark_rows(rows_without_monitor.map(&:id), :skipped, "Monitor missing") if rows_without_monitor.any?

      rows.group_by { |row| [ row.monitor_id, row.group_key ] }.each_value do |chain|
        chain.each do |row|
          Notification.mark_rows([ row.id ], :processing)
          break unless deliver_monitor_message(integration, row)
        end
      end
    end

    # Delivers one Monitor row; false when it failed and may be retried.
    def deliver_monitor_message(integration, row)
      message = if MONITOR_STATUS_KINDS.include?(row.kind)
        Notifiers::MonitorStatusMessage.build(row)
      else
        Notifiers::MonitorMessage.build(row)
      end
      adapter_for(integration).new(integration, kind: row.kind, message: message, **row_of(integration, [ row ])).call
      Notification.mark_rows([ row.id ], :sent)
      true
    rescue Notifiers::NotConfigured, Notifiers::Skipped => e
      Notification.mark_rows([ row.id ], :skipped, e.message)
      true
    rescue Notifiers::Refused => e
      Notification.mark_rows([ row.id ], :refused, e.message)
      true
    rescue StandardError => e
      Rails.logger.warn("Integration notification to #{integration.provider} failed: #{e.message}")
      Notification.mark_rows([ row.id ], :failed, e.message)
      false
    end

    # Delivers one message and records its outcome on every row it carries.
    def deliver_message(integration, kind, rows, issues, payload = {})
      row_ids = rows.map(&:id)
      adapter_for(integration).new(integration, kind: kind, issues: issues, **assignee_names(issues.first, payload),
                                   **row_of(integration, rows)).call
      Notification.mark_rows(row_ids, :sent)
    rescue Notifiers::NotConfigured, Notifiers::Skipped => e
      Notification.mark_rows(row_ids, :skipped, e.message)
    rescue Notifiers::Refused => e
      Notification.mark_rows(row_ids, :refused, e.message)
    rescue StandardError => e
      Rails.logger.warn("Integration notification to #{integration.provider} failed: #{e.message}")
      Notification.mark_rows(row_ids, :failed, e.message)
    end

    def adapter_for(integration)
      ADAPTERS.fetch(integration.provider).constantize
    end

    # The Factory adapter gets its one row, whose id is the delivery id.
    def row_of(integration, rows)
      integration.factory? ? { notification: rows.sole } : {}
    end

    def assignee_names(issue, context)
      context = context.symbolize_keys
      return {} unless context.key?(:previous_assignee_id) || context.key?(:new_assignee_id)

      # Includes archived (discarded) members, so a former assignee is named.
      members = issue.project.organization.all_organization_users
      {
        previous_assignee_name: member_name(members, context[:previous_assignee_id]),
        new_assignee_name: member_name(members, context[:new_assignee_id]),
      }
    end

    def member_name(members, organization_user_id)
      return unless organization_user_id

      members.find_by(id: organization_user_id)&.user&.name
    end

    # At least event_threshold Events within the window, and no threshold
    # reached row for this Issue and Integration in that window: the outbox is
    # the dedup record.
    def event_threshold_reached?(integration, issue)
      window_start = integration.time_window_minutes.minutes.ago
      return false if threshold_reached_since?(integration, issue, window_start)

      issue.events.newer_than(window_start).count! >= integration.event_threshold
    end

    def threshold_reached_since?(integration, issue, window_start)
      Notification.exists?(integration: integration, issue: issue, kind: THRESHOLD_REACHED, created_at: window_start..)
    end
  end
end
