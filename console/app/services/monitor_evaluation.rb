# frozen_string_literal: true

# Runs a Monitor once, at now: the one entry point for evaluating Monitors.
# It asks EventStore, moves the Monitor's states, writes the Notifications
# each change owes (IntegrationNotification.monitor_changed) and records the
# run, all in one transaction.
#
# - Threshold: runs the Query over [now - range - 60 s, now - 60 s). Each
#   row is a group keyed by its columns other than the number, or all rows
#   are one group (the worst number) when the Monitor doesn't notify by
#   group. A group whose number passes the threshold fires; ok to firing
#   sends monitor_firing, firing to ok sends monitor_resolved, and staying
#   firing sends nothing. A group with no row is ok, and no rows at all fire
#   the Monitor's one group only with alert_on_no_data.
# - Match: reads the Logs ingested after ingest_cursor and keeps the new
#   cursor; when any match, sends one monitor_matched with the newest
#   MATCHED_LOGS and how many matched in all. After TIMEOUTS_BEFORE_SKIPPING
#   timeouts in a row it starts again from the newest Log batch, skipping
#   the Logs in between, which its run records and its next message says.
#
# When EventStore is down, answers 5xx, times out or refuses the Query, the
# run records the error as last_error, changes no state, and the next tick
# runs the Monitor again.
class MonitorEvaluation
  MATCHED_LOGS = 10
  TIMEOUTS_BEFORE_SKIPPING = 3
  # Characters of a matched Log's message kept for its Notification.
  MAX_LOG_MESSAGE = 1000

  # The table EventStore answered can't be compared with the threshold.
  class Unusable < StandardError; end

  def self.run(monitor, now)
    new(monitor, now).run
  end

  def initialize(monitor, now)
    @monitor = monitor
    @now = now
    @started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
  end

  # The MonitorRun it recorded.
  def run
    @monitor.kind_threshold? ? run_threshold : run_match
  rescue EventStore::Rejected => e
    failed(EventStore.error_text(e.body))
  rescue EventStore::Unavailable => e
    failed(e.message)
  end

  private

  # ---------------------------------------------------------------------------
  # Threshold
  # ---------------------------------------------------------------------------

  def run_threshold
    from, to = @monitor.window(@now)
    answer = EventStore.query_logs(
      project_id: @monitor.project_id, query: @monitor.query, from: from, to: to,
      timeout_seconds: LogMonitor::RUN_TIMEOUT_SECONDS
    )
    table = answer["table"]
    return failed("The Query returns Logs, not a table: end it in a summarize of one number.") unless table

    column, groups = groups_of(table)
    groups = { "" => nil } if groups.empty? && @monitor.alert_on_no_data

    ApplicationRecord.transaction do
      firing = move_states(groups, column: column, from: from, to: to)
      finish(firing: firing)
    end
  rescue EventStore::TimedOut => e
    failed(EventStore.error_text(e.body))
  rescue Unusable => e
    failed(e.message)
  end

  # The number column's name, and each group's key and number: the worst of
  # its rows', since a row's number may be null.
  def groups_of(table)
    columns = table["columns"] || []
    numbers = columns.each_index.select { |i| columns[i]["type"] == "number" }
    unless numbers.size == 1
      raise Unusable, "The Query gives #{numbers.size} numbers per group, not one to compare with the threshold."
    end

    at = numbers.first
    groups = {}
    (table["rows"] || []).each do |row|
      value = row[at]
      next unless value.is_a?(Numeric)

      key = @monitor.notify_by_group ? group_key(columns, row, at) : ""
      groups[key] = groups.key?(key) ? worst(groups[key], value) : value
    end
    [ columns[at]["name"], groups ]
  end

  # A group's columns, name to value in the table's order, as JSON; "" when
  # the table has no column but the number.
  def group_key(columns, row, at)
    group = columns.each_with_index.filter_map { |column, i| [ column["name"], row[i] ] unless i == at }.to_h
    group.empty? ? "" : group.to_json
  end

  def worst(a, b)
    @monitor.operator.start_with?(">") ? [ a, b ].max : [ a, b ].min
  end

  # Moves each group's state to what this run found, and writes the
  # Notification each change owes. A group with no row this run is ok: a
  # firing one resolves, and an ok one is forgotten. Returns how many groups
  # are firing.
  def move_states(groups, column:, from:, to:)
    states = @monitor.states.index_by(&:group_key)
    facts = { "column" => column, "threshold" => @monitor.threshold, "operator" => @monitor.operator,
              "from" => from.utc.iso8601, "to" => to.utc.iso8601 }

    groups.each do |key, value|
      state = states.delete(key) || @monitor.states.build(group_key: key, status: "ok", since: @now)
      firing = value.nil? || @monitor.breached?(value)
      if firing != state.status_firing?
        kind = firing ? IntegrationNotification::MONITOR_FIRING : IntegrationNotification::MONITOR_RESOLVED
        # Firing for no data, or resolving after it.
        no_data = firing ? value.nil? : state.last_value.nil?
        notify(kind, key, facts.merge("value" => value, "no_data" => no_data))
        state.assign_attributes(status: firing ? "firing" : "ok", since: @now)
      end
      state.update!(last_value: value)
    end

    states.each_value do |state|
      next state.destroy! if state.status_ok?

      notify(IntegrationNotification::MONITOR_RESOLVED, state.group_key,
        facts.merge("value" => nil, "absent" => true, "no_data" => state.last_value.nil?))
      state.update!(status: "ok", since: @now, last_value: nil)
    end

    @monitor.states.reload.count(&:status_firing?)
  end

  # ---------------------------------------------------------------------------
  # Match
  # ---------------------------------------------------------------------------

  def run_match
    return start_again if @monitor.ingest_cursor.nil?
    return start_again(skipping: true) if @monitor.timeouts_in_a_row >= TIMEOUTS_BEFORE_SKIPPING

    answer = EventStore.logs_ingested_after(
      project_id: @monitor.project_id, query: @monitor.query, after_batch: @monitor.ingest_cursor,
      limit: MATCHED_LOGS, timeout_seconds: LogMonitor::RUN_TIMEOUT_SECONDS
    )
    logs = Array(answer["logs"]).first(MATCHED_LOGS)
    total = [ answer["total"].to_i, logs.size ].max

    ApplicationRecord.transaction do
      notify_matched(logs, total) if total.positive?
      @monitor.update_columns(ingest_cursor: answer.fetch("ingest_cursor"), ingest_cursor_at: @now, timeouts_in_a_row: 0)
      finish(matched: total)
    end
  rescue EventStore::TimedOut => e
    timed_out(EventStore.error_text(e.body))
  end

  def notify_matched(logs, total)
    payload = {
      "logs" => logs.map { |log| slim(log) },
      "total" => total,
      "from" => matched_from(logs),
      "to" => @now.utc.iso8601,
      "logs_skipped_at" => @monitor.logs_skipped_at&.utc&.iso8601,
    }
    notify(IntegrationNotification::MONITOR_MATCHED, "", payload)
    @monitor.update_columns(logs_skipped_at: nil)
  end

  # Where the message's explorer link starts. The Logs listed are the
  # newest, so the others are older: the link starts a processing lag
  # before the cursor was kept, as the Logs read were processed since, or
  # at the oldest Log listed when that came in later than its time says.
  def matched_from(logs)
    times = logs.filter_map { |log| Time.iso8601(log["time"].to_s) rescue nil }
    times << @monitor.ingest_cursor_at - LogMonitor::PROCESSING_LAG if @monitor.ingest_cursor_at
    times.min&.utc&.iso8601
  end

  def slim(log)
    log.slice("id", "time", "level", "service").merge("message" => log["message"].to_s.truncate(MAX_LOG_MESSAGE))
  end

  # A Match run EventStore stopped at its timeout moves no cursor. The
  # third in a row starts the Monitor again from the newest Log batch.
  def timed_out(error)
    @monitor.timeouts_in_a_row += 1
    if @monitor.timeouts_in_a_row < TIMEOUTS_BEFORE_SKIPPING
      @monitor.update_columns(timeouts_in_a_row: @monitor.timeouts_in_a_row)
      return failed(error)
    end

    start_again(skipping: true, error: error)
  end

  # Arms the Monitor at the newest Log batch. skipping says the Logs since
  # its cursor are left unread, which its run and next message record. When
  # EventStore can't arm it, the next run tries again.
  def start_again(skipping: false, error: nil)
    answer = EventStore.logs_ingested_after(
      project_id: @monitor.project_id, query: @monitor.query, after_batch: "",
      timeout_seconds: LogMonitor::RUN_TIMEOUT_SECONDS
    )
    ApplicationRecord.transaction do
      @monitor.arm(answer.fetch("ingest_cursor"), @now)
      @monitor.logs_skipped_at = @now if skipping
      @monitor.update_columns(@monitor.slice(:ingest_cursor, :ingest_cursor_at, :timeouts_in_a_row, :logs_skipped_at))
      note = "#{error} Runs timed out #{TIMEOUTS_BEFORE_SKIPPING} times in a row, so the Monitor started again " \
        "from the newest Logs and skipped those ingested since its last run." if error
      finish(matched: 0, logs_skipped: skipping, error: note)
    end
  rescue EventStore::Unavailable, EventStore::Rejected => e
    @monitor.update_columns(timeouts_in_a_row: @monitor.timeouts_in_a_row) if @monitor.timeouts_in_a_row_changed?
    # A connection error or a 5xx has no body EventStore answered with.
    why = e.body ? EventStore.error_text(e.body) : e.message
    failed(error ? "#{error} Starting again from the newest Logs failed: #{why}" : why)
  end

  # ---------------------------------------------------------------------------
  # Recording the run
  # ---------------------------------------------------------------------------

  def notify(kind, group_key, payload)
    IntegrationNotification.monitor_changed(@monitor, kind: kind, group_key: group_key, payload: payload)
  end

  def failed(error)
    finish(error: error)
  end

  # Records the run. An ok run is next due after the Monitor's interval; one
  # with an error at the next tick.
  def finish(error: nil, firing: nil, matched: nil, logs_skipped: false)
    next_run_in = error ? 1.minute : @monitor.interval_minutes.minutes
    @monitor.update_columns(last_run_at: @now, last_error: error, next_run_at: @now + next_run_in)
    @monitor.runs.create!(
      ran_at: @now, status: error ? "error" : "ok", error: error, firing: firing, matched: matched,
      logs_skipped: logs_skipped,
      elapsed_ms: ((Process.clock_gettime(Process::CLOCK_MONOTONIC) - @started) * 1000).round
    )
  end
end
