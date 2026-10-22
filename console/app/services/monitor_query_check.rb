# frozen_string_literal: true

# Checks, when a Monitor is saved, that EventStore can run its Query the way
# its kind needs, and adds to the Monitor's errors on :query when it can't.
#
# - Either kind: validate says the Query has no mistakes and names no
#   Dashboard filter ($service), which only a Dashboard's Charts fill in; a
#   Monitor would run it as All, a value nobody chose.
# - Threshold: the Query ends in a summarize (or count), and a dry run over
#   the Monitor's range gives a table with exactly one number column and no
#   time bins.
# - Match: arming it (after_batch "") checks the Query returns Logs, and
#   gives the ingest_cursor its runs start from, which the Monitor keeps when
#   it has to start again (#arm?).
class MonitorQueryCheck
  SUMMARIZE = "must end in a summarize that gives one number per group, such as " \
    "| summarize count() by service, for a Threshold monitor to compare with its threshold"

  def self.call(monitor, now: Time.current)
    new(monitor, now).call
  end

  def initialize(monitor, now)
    @monitor = monitor
    @now = now
  end

  def call
    validation = EventStore.validate_logs_query(project_id: @monitor.project_id, query: @monitor.query)
    return query_errors(validation) if Array(validation["errors"]).any?

    filters = Array(validation["parameters"])
    return add("can't name a filter. Filters (#{filters.map { |name| "$#{name}" }.join(', ')}) work only on Dashboard Charts") if filters.any?

    @monitor.kind_threshold? ? check_threshold(validation) : check_match
  rescue EventStore::Rejected => e
    query_errors(e.body)
  rescue EventStore::TimedOut => e
    add("took longer than #{LogMonitor::RUN_TIMEOUT_SECONDS} s to run: #{EventStore.error_text(e.body)}")
  rescue EventStore::Unavailable => e
    Rails.logger.error(e.message)
    add("could not be checked, as EventStore is unavailable. Try again shortly.")
  end

  private

  def check_threshold(validation)
    return add(SUMMARIZE) unless ends_in_summarize?(Array(validation["stages"]).last)

    from, to = @monitor.window(@now)
    answer = EventStore.query_logs(
      project_id: @monitor.project_id, query: @monitor.query, from: from, to: to,
      timeout_seconds: LogMonitor::RUN_TIMEOUT_SECONDS
    )
    return add(SUMMARIZE) unless answer["table"]

    check_columns(answer["table"]["columns"] || [])
  end

  def ends_in_summarize?(stage)
    return false unless stage

    lines = @monitor.query.split("\n", -1)
    offset = lines.first(stage["line"] - 1).sum { |line| line.length + 1 } + stage["column"] - 1
    @monitor.query[offset..].to_s.match?(/\A(summarize|count)\b/)
  end

  def check_columns(columns)
    numbers = columns.select { |column| column["type"] == "number" }.map { |column| column["name"] }
    bins = columns.select { |column| column["bin_seconds"].present? }.map { |column| column["name"] }
    if numbers.size != 1
      add("must give exactly one number per group to compare with the threshold, " \
        "but gives #{numbers.size}#{numbers.any? ? " (#{numbers.join(', ')})" : ''}. " \
        "A number to group by can be turned into text with tostring()")
    elsif bins.any?
      add("can't group by a time bin (#{bins.join(', ')}): a Threshold monitor compares one number " \
        "per group over its whole range. Remove the bin from the summarize's by")
    end
  end

  def check_match
    answer = EventStore.logs_ingested_after(
      project_id: @monitor.project_id, query: @monitor.query, after_batch: "",
      timeout_seconds: LogMonitor::RUN_TIMEOUT_SECONDS
    )
    @monitor.arm(answer["ingest_cursor"], @now) if @monitor.arm?
  end

  # EventStore's errors for the Query, each with where it starts.
  def query_errors(body)
    EventStore.mistakes(body).each { |mistake| add(mistake) }
  end

  def add(message)
    @monitor.errors.add(:query, message)
  end
end
