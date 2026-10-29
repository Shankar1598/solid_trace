# frozen_string_literal: true

# Runs a Monitor once, at now: the one entry point for evaluating Monitors.
# It asks EventStore, moves the Monitor's states, writes the Notifications
# each change owes (IntegrationNotification.monitor_changes) and records the
# run, all in one transaction, then schedules each Integration's tick.
#
# - Threshold: runs the Query over [now - range - 60 s, now - 60 s). Each
#   row is a group keyed by its columns other than the number, or all rows
#   are one group (the worst number) when the Monitor doesn't notify by
#   group. A group whose number passes the threshold fires; ok to firing
#   sends monitor_firing, firing to ok sends monitor_resolved, and staying
#   firing sends nothing. A group with no row is ok, and no rows at all fire
#   the Monitor's one group only with alert_on_no_data. A table cut at
#   EventStore's row limit is an error (TABLE_CUT).
# - Threshold comparing the change: runs the Query again over the Baseline
#   range (LogMonitor#baseline_window) with what's left of the run's
#   timeout, and each group's change from its Baseline is compared instead:
#   a row with none in the Baseline rises from 0. Without notify by group
#   the worst row's change makes the one group. Either Query failing is an
#   error that changes no state, and no data looks at the current table.
# - Match: reads the Logs ingested after ingest_cursor and keeps the new
#   cursor; when any match, sends one monitor_matched with the newest
#   MATCHED_LOGS and how many matched in all. After TIMEOUTS_BEFORE_SKIPPING
#   timeouts in a row it starts again from the newest Log batch, skipping
#   the Logs in between, which its run records and its next message says.
#
# When EventStore is down, answers 5xx, times out or refuses the Query, the
# run records the error as last_error, changes no state, and the next tick
# runs the Monitor again.
#
# One run at a time per Monitor, whether the scheduler (MonitorEvaluationJob)
# or Run now started it: a run takes the Monitor's run_started_at as a lease
# and lets go when it ends, and a run that finds the lease taken does nothing.
# A run whose Monitor was changed, turned off or deleted while it ran writes
# nothing it found: the change may have resolved the Monitor's groups
# (LogMonitor#resolve_groups), and the run's states would undo that.
class MonitorEvaluation
  MATCHED_LOGS = 10
  TIMEOUTS_BEFORE_SKIPPING = 3
  # Characters of a matched Log's message kept for its Notification.
  MAX_LOG_MESSAGE = 1000
  # How long a run holds its Monitor. Runs end well within it, as EventStore
  # stops each at LogMonitor::RUN_TIMEOUT_SECONDS; a run whose process died
  # is taken over after it.
  RUN_LEASE = 5.minutes
  # The most states one statement writes.
  STATES_PER_WRITE = 1_000
  # EventStore answers at most 10,000 rows. A run that sees only some groups
  # would compare a different subset each time, firing and resolving groups
  # it merely missed, so a cut table is an error that changes no state.
  TABLE_CUT = "The Query gives more than 10,000 rows, the most EventStore answers, so a run can't compare every " \
    "group with the threshold: group by fewer columns, or filter the Logs before the summarize."
  CHANGED_DURING_RUN = "The Monitor was changed during this run, so the run changed nothing. The next run checks it again."

  OUT_OF_TIME = "The Query over the current range took the run's #{LogMonitor::RUN_TIMEOUT_SECONDS} s, leaving none " \
    "to read the Baseline's: filter the Logs before the summarize, or shorten the range."

  # The table EventStore answered can't be compared with the threshold.
  class Unusable < StandardError; end

  # A group's number this run and, comparing the change, its Baseline: its
  # number over the Baseline range, 0 when it had no row there. value is nil
  # for no data.
  Reading = Data.define(:value, :baseline)
  NO_DATA = Reading.new(nil, nil)

  # What a Threshold run reads: the number column's name, the current range
  # [from, to), the Baseline's [from, to) when comparing the change, and
  # each group's Reading by group key.
  Found = Data.define(:column, :from, :to, :baseline_range, :groups)

  # What a Threshold run at now would find, for the form's preview of a
  # Monitor not yet saved: the ranges it reads, and each group's columns,
  # Reading and whether it would fire, as the run decides it. It reads
  # EventStore as a run does but takes no lease and writes nothing. Without
  # a threshold only no data fires. Raises Unusable for a table a run can't
  # compare, and EventStore's errors.
  def self.preview(monitor, now)
    new(monitor, now).preview
  end

  def preview
    found = read_threshold
    found.with(groups: found.groups.map do |key, reading|
      firing = @monitor.threshold.nil? ? reading.value.nil? : fires?(reading)
      { group: key.empty? ? {} : JSON.parse(key), value: reading.value, baseline: reading.baseline, firing: firing }
    end)
  end

  # The MonitorRun recorded, or nil when another run of the Monitor is going.
  def self.run(monitor, now)
    started = take(monitor)
    return unless started

    begin
      new(monitor, now).run
    ensure
      LogMonitor.where(id: monitor.id, run_started_at: started).update_all(run_started_at: nil)
    end
  end

  # Takes the Monitor's lease, in one UPDATE so two runs can't both take it.
  # The time it was taken at, or nil when another run holds it.
  def self.take(monitor)
    started = Time.current
    free = LogMonitor.where(run_started_at: nil).or(LogMonitor.where(run_started_at: ...(started - RUN_LEASE)))
    started if free.where(id: monitor.id).update_all(run_started_at: started) == 1
  end
  private_class_method :take

  def initialize(monitor, now)
    @monitor = monitor
    @now = now
    # Which version of the Monitor the run reads: any save changes it.
    @version = monitor.updated_at
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
    found = read_threshold
    record do
      firing = move_states(found.groups, column: found.column, from: found.from, to: found.to,
        baseline_range: found.baseline_range)
      finish(firing: firing)
    end
  rescue EventStore::TimedOut => e
    failed(EventStore.error_text(e.body))
  rescue Unusable => e
    failed(e.message)
  end

  # What a Threshold run at now reads: the current range's table and,
  # comparing the change, the Baseline range's, as each group's Reading. A
  # Baseline older than the Project's Log Retention is Unusable.
  def read_threshold
    if @monitor.compares_the_change? && (problem = @monitor.baseline_past_retention)
      raise Unusable, "The Baseline #{problem}."
    end

    from, to = @monitor.window(@now)
    column, rows = rows_of(table_over(from, to, LogMonitor::RUN_TIMEOUT_SECONDS))
    readings = rows.transform_values { |value| Reading.new(value, nil) }
    if @monitor.compares_the_change?
      baseline_range = @monitor.baseline_window(@now)
      _, before = rows_of(table_over(*baseline_range, time_left))
      readings = rows.to_h { |key, value| [ key, Reading.new(value, before.fetch(key, 0)) ] }
    end
    groups = @monitor.notify_by_group ? readings : the_worst(readings)
    groups = { "" => NO_DATA } if groups.empty? && @monitor.alert_on_no_data
    Found.new(column: column, from: from, to: to, baseline_range: baseline_range, groups: groups)
  end

  # Whether a group whose reading this is fires: no data does, and a number
  # does when it, or comparing the change its change, passes the threshold.
  def fires?(reading)
    reading.value.nil? || @monitor.breached?(reading.value, reading.baseline)
  end

  # The table the Query gives over [from, to), stopped after timeout
  # seconds.
  def table_over(from, to, timeout)
    answer = EventStore.query_logs(
      project_id: @monitor.project_id, query: @monitor.query, from: from, to: to, timeout_seconds: timeout
    )
    table = answer["table"]
    raise Unusable, "The Query returns Logs, not a table: end it in a summarize of one number." unless table
    raise Unusable, TABLE_CUT if table["truncated"]

    table
  end

  # What's left of the run's LogMonitor::RUN_TIMEOUT_SECONDS, for the
  # Baseline's Query after the current range's.
  def time_left
    left = (LogMonitor::RUN_TIMEOUT_SECONDS - (Process.clock_gettime(Process::CLOCK_MONOTONIC) - @started)).floor(3)
    raise Unusable, OUT_OF_TIME if left <= 0

    left
  end

  # The number column's name, and each row's group key and number. Rows are
  # keyed by their columns other than the number whether or not the Monitor
  # notifies by group; a row whose number is null is left out, and rows
  # sharing a key keep the worse number.
  def rows_of(table)
    columns = table["columns"] || []
    numbers = columns.each_index.select { |i| columns[i]["type"] == "number" }
    unless numbers.size == 1
      raise Unusable, "The Query gives #{numbers.size} numbers per group, not one to compare with the threshold."
    end

    at = numbers.first
    rows = {}
    (table["rows"] || []).each do |row|
      value = row[at]
      next unless value.is_a?(Numeric)

      key = group_key(columns, row, at)
      rows[key] = rows.key?(key) ? @monitor.worse(rows[key], value) : value
    end
    [ columns[at]["name"], rows ]
  end

  # A Monitor that doesn't notify by group has one group, "", the worst
  # row's: the one whose number, or comparing the change whose change,
  # passes the threshold soonest, of those that reach the Monitor's minimum
  # when any do. None when there are no rows.
  def the_worst(readings)
    return {} if readings.empty?

    big = readings.values.reject { |reading| @monitor.below_minimum?(reading.value, reading.baseline) }
    compared = (big.presence || readings.values).map { |reading| [ reading, compared(reading) ] }
    worst = compared.map(&:last).reduce { |a, b| @monitor.worse(a, b) }
    { "" => compared.find { |_, number| number == worst }.first }
  end

  # The number a group's reading compares with the threshold.
  def compared(reading)
    @monitor.compares_the_change? ? LogMonitor.change(reading.value, reading.baseline) : reading.value
  end

  # A group's columns, name to value in the table's order, as JSON; "" when
  # the table has no column but the number.
  def group_key(columns, row, at)
    group = columns.each_with_index.filter_map { |column, i| [ column["name"], row[i] ] unless i == at }.to_h
    group.empty? ? "" : group.to_json
  end

  # Moves each group's state to what this run found, and writes the
  # Notification each change owes. A group with no row this run is ok: a
  # firing one resolves, and an ok one is forgotten. Returns how many groups
  # are firing.
  #
  # It runs in the transaction that holds SQLite's write lock, and a run may
  # have up to 10,000 groups, so the states are written in bulk: one upsert
  # of what changed and one delete of the ok groups that are gone. The
  # Notifications are written in bulk too, when the run is recorded.
  def move_states(groups, column:, from:, to:, baseline_range: nil)
    states = @monitor.states.index_by(&:group_key)
    facts = { "column" => column, "threshold" => @monitor.threshold, "operator" => @monitor.operator,
              "from" => from.utc.iso8601, "to" => to.utc.iso8601 }
    if baseline_range
      facts.merge!("compare" => "change", "baseline_from" => baseline_range.first.utc.iso8601,
        "baseline_to" => baseline_range.last.utc.iso8601)
    end
    changed = []
    firing_count = 0

    groups.each do |key, reading|
      value, baseline = reading.value, reading.baseline
      state = states.delete(key)
      status, since = state ? [ state.status, state.since ] : [ "ok", @now ]
      firing = fires?(reading)
      firing_count += 1 if firing
      if firing != (status == "firing")
        kind = firing ? IntegrationNotification::MONITOR_FIRING : IntegrationNotification::MONITOR_RESOLVED
        # Firing for no data, or resolving after it.
        no_data = firing ? value.nil? : state.last_value.nil?
        notify(kind, key, facts.merge("value" => value, "no_data" => no_data).merge(change_facts(reading)))
        status, since = (firing ? "firing" : "ok"), @now
      end
      next if state && state.status == status && state.last_value == value && state.last_baseline == baseline

      changed << { group_key: key, status: status, since: since, last_value: value, last_baseline: baseline }
    end

    gone = []
    states.each_value do |state|
      next gone << state.id if state.status_ok?

      notify(IntegrationNotification::MONITOR_RESOLVED, state.group_key,
        facts.merge("value" => nil, "absent" => true, "no_data" => state.last_value.nil?).merge(change_facts(NO_DATA)))
      changed << { group_key: state.group_key, status: "ok", since: @now, last_value: nil, last_baseline: nil }
    end

    @monitor.states.where(id: gone).delete_all if gone.any?
    changed.each_slice(STATES_PER_WRITE) do |rows|
      @monitor.states.upsert_all(rows, unique_by: %i[monitor_id group_key])
    end
    firing_count
  end

  # Comparing the change, what a group's Notification says beside its
  # value: its Baseline and its change, a percent. A rise or fall from a
  # Baseline of 0 has no size, so its change is nil; so is no data's.
  def change_facts(reading)
    return {} unless @monitor.compares_the_change?

    change = LogMonitor.change(reading.value, reading.baseline) unless reading.value.nil?
    { "baseline" => reading.baseline, "change" => change&.finite? ? change : nil }
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

    record do
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
    record do
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

  # Keeps a change for record to write with the run's others.
  def notify(kind, group_key, payload)
    @changes << [ kind, group_key, payload ]
  end

  def failed(error)
    record { finish(error: error) }
  end

  # Writes what the run found in one transaction, if the Monitor is still
  # the version the run read. SQLite transactions here begin IMMEDIATE, so
  # no save of the Monitor lands between the check and the writes. A
  # changed Monitor records the run as changing nothing, and a deleted one
  # records nothing (nil). The Notifications the run's changes owe
  # (notify) are written in the same transaction, and their Integrations'
  # ticks are scheduled, one each, once it commits.
  def record
    @changes = []
    notified = []
    run = ApplicationRecord.transaction do
      version = LogMonitor.where(id: @monitor.id).pick(:updated_at)
      if version.nil? then nil
      elsif version != @version then finish(error: CHANGED_DURING_RUN)
      else
        yield.tap { notified = IntegrationNotification.monitor_changes(@monitor, @changes) }
      end
    end
    IntegrationNotification.schedule_ticks(notified)
    run
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
