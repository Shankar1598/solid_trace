# frozen_string_literal: true

# An Organization's Overview over a range of whole UTC days, as a user sees
# it: the sections the Overview page shows and get_overview answers, made
# here once so the two can't drift.
#
# EventStore answers what arrived (Ingest counts) and what is stored, in one
# call. When it can't, its sections are nil and event_store says why; the
# sections Console holds itself still answer. Each section is a method named
# in SECTIONS, which #to_h answers in that order.
class Overview
  # The ranges by name, with how many UTC days each covers, ending today.
  RANGES = { "today" => 1, "7d" => 7, "30d" => 30 }.freeze
  DEFAULT_RANGE = "today"

  # How long Console waits for EventStore's Overview, in seconds.
  TIMEOUT_SECONDS = 3

  # The sections, in the order they're answered.
  SECTIONS = %i[range event_store empty ingest_counts band rates daily projects health].freeze

  # The Projects that wear a Chart colour of their own, the first by
  # creation; the rest fold into Other, which wears the next. Never cycled.
  COLORED_PROJECTS = 7
  OTHER = "other"

  # A Project whose last Log arrived longer ago than this is stale.
  STALE_AFTER = 1.hour

  attr_reader :organization, :user, :range_name

  # The range a name names, the default for any other.
  def self.range_name(name)
    RANGES.key?(name.to_s) ? name.to_s : DEFAULT_RANGE
  end

  def initialize(organization:, user:, range: DEFAULT_RANGE, today: Time.now.utc.to_date)
    @organization = organization
    @user = user
    @range_name = self.class.range_name(range)
    @to = today
    @from = today - (RANGES.fetch(@range_name) - 1)
  end

  def to_h
    SECTIONS.index_with { |section| send(section) }
  end

  # The range's name and its first and last UTC day.
  def range
    { name: range_name, from: @from.iso8601, to: @to.iso8601, days: RANGES.fetch(range_name), time_zone: "UTC" }
  end

  # Whether EventStore answered, and if not, why.
  def event_store
    answer
    @unavailable ? { available: false, error: @unavailable } : { available: true }
  end

  # What the page says when there's nothing to show: no Projects yet, or
  # Projects with no Logs yet. Logs is nil when EventStore didn't answer.
  def empty
    logs = if org_projects.empty? then true elsif answer then !answer["has_logs"] end
    { projects: org_projects.empty?, logs: logs }
  end

  # When Ingest counts begin, and whether that's after the range starts, so
  # a short Received step isn't read as a drop.
  def ingest_counts
    return unless answer

    since = answer["ingest_counts_since"]&.then { |time| Time.iso8601(time) }
    { since: since&.utc&.iso8601, starts_after_range: since.nil? || since > @from.to_time(:utc) }
  end

  # Received → Stored: the bytes and Logs that arrived and what ClickHouse
  # holds of the range's days, with the compression ratio, received bytes
  # over stored bytes; the unreadable Logs and the requests turned away beside
  # it.
  def band
    return unless answer

    band = answer["band"]
    received, stored = band["received"], band["stored"]
    {
      received: received.slice("logs", "bytes", "bytes_on_wire", "requests"),
      stored: stored.slice("logs", "bytes", "local_bytes", "bucket_bytes"),
      ratio: stored["bytes"].to_i.positive? ? (received["bytes"].to_f / stored["bytes"]).round(1) : nil,
      unreadable_logs: band["unreadable_logs"],
      overloaded_requests: band["overloaded_requests"],
    }
  end

  # Logs, bytes and requests a second by arrival, each an average over the
  # range so far and the peak second of the Projects together; Today also
  # the last five minutes' Logs a second.
  def rates
    return unless answer

    rates = answer["rates"]
    rate = ->(r) { { average: r["average"], peak: r["peak"], peak_at: r["peak_at"] } }
    {
      seconds: rates["seconds"],
      logs: rate.(rates["logs"]),
      bytes: rate.(rates["bytes"]),
      requests: rate.(rates["requests"]),
      last_five_minutes: range_name == "today" ? rates["last_five_minutes"] : nil,
    }
  end

  # The daily Charts' series and days: the Logs each series received each
  # day, a series a Project or Other (by its key), the bytes received and
  # stored, and the Logs a second, average and peak. Each Project's series
  # keeps its colour whatever the range, as it follows creation order alone.
  def daily
    return unless answer

    days = Array(answer["days"]).map do |day|
      logs = series.to_h { |s| [ s[:key], 0 ] }
      Array(day["projects"]).each do |p|
        key = series_key(p["project_id"])
        logs[key] += p["received_logs"].to_i if key
      end
      {
        day: day["day"], logs: logs,
        received_bytes: day.dig("received", "bytes").to_i, stored_bytes: day.dig("stored", "bytes").to_i,
        logs_per_second: rate_of(day["logs_per_second"]),
      }
    end
    { series: series, days: days }
  end

  # One row per Project, in creation order: what it received and stored of
  # the range, the ratio (received bytes over stored), its share of the
  # bytes received, its Logs a second, its Retention (the instance's when it
  # has none) and the Logs it keeps of every day, and when its last Log
  # arrived, stale when over an hour ago.
  def projects
    return unless answer

    by_id = Array(answer["projects"]).index_by { |p| p["project_id"] }
    total = by_id.values.sum { |p| p.dig("received", "bytes").to_i }
    default_retention = InstanceSetting.current.default_log_retention_days
    org_projects.map do |project|
      got = by_id[project.id] || {}
      received, stored = got.dig("received", "bytes").to_i, got.dig("stored", "bytes").to_i
      all_days = got["all_days"] || {}
      last = got["last_log_at"]&.then { |time| Time.iso8601(time) }
      {
        id: project.id, name: project.name, slug: project.slug, color: color_of(project.id),
        logs: got.dig("received", "logs").to_i, received_bytes: received, stored_bytes: stored,
        ratio: stored.positive? ? (received.to_f / stored).round(1) : nil,
        share: total.positive? ? (received.to_f / total).round(4) : nil,
        logs_per_second: rate_of(got["logs_per_second"]),
        retention_days: project.log_retention_days || default_retention,
        retention_is_default: project.log_retention_days.nil?,
        oldest_day: all_days["oldest_day"],
        all_days: { logs: all_days["logs"].to_i, bytes: all_days["bytes"].to_i },
        last_log_at: last&.utc&.iso8601,
        stale: last && last < Time.now - STALE_AFTER,
        has_logs: all_days["logs"].to_i.positive? || last.present?,
      }
    end
  end

  # Log processing's waits and Log reads' times per Project, failing and
  # Firing Monitors, and the range's new Issues and Events (Overview::Health).
  # Monitors and Issues are Console's, so they answer without EventStore.
  def health
    Health.new(organization: organization, projects: org_projects, from: @from, to: @to, answer: answer).to_h
  end

  private

  def org_projects
    @org_projects ||= organization.projects.order(:created_at, :id).to_a
  end

  # The daily Charts' series: a Project each for the first COLORED_PROJECTS
  # by creation, then Other for the rest, with the Projects each holds.
  def series
    @series ||= begin
      own, rest = org_projects.take(COLORED_PROJECTS), org_projects.drop(COLORED_PROJECTS)
      list = own.each_with_index.map do |project, i|
        { key: "project-#{project.id}", name: project.name, color: i + 1, project_ids: [ project.id ] }
      end
      list << { key: OTHER, name: "Other", color: COLORED_PROJECTS + 1, project_ids: rest.map(&:id) } if rest.any?
      list
    end
  end

  def series_key(project_id)
    series.find { |s| s[:project_ids].include?(project_id) }&.dig(:key)
  end

  def color_of(project_id)
    series.find { |s| s[:project_ids].include?(project_id) }&.dig(:color)
  end

  def rate_of(rate)
    rate ||= {}
    { average: rate["average"].to_f, peak: rate["peak"].to_i, peak_at: rate["peak_at"] }
  end

  # EventStore's Overview of the Organization's Projects, or nil when it has
  # none or EventStore can't answer (@unavailable says why). Storage, the
  # whole install's, is asked for Instance admins only.
  def answer
    return @answer if defined?(@answer)

    @answer = org_projects.empty? ? nil : EventStore.overview!(
      project_ids: org_projects.map(&:id), from: @from, to: @to,
      include_storage: user.instance_admin?, read_timeout: TIMEOUT_SECONDS
    )
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error("Overview: #{e.message}")
    @unavailable = "EventStore's numbers are unavailable: #{unavailable_reason(e)}"
    @answer = nil
  end

  def unavailable_reason(error)
    body = error.respond_to?(:body) ? error.body : nil
    return EventStore.error_text(body) if body.present?

    error.message.include?("connection error") ? "EventStore can't be reached." : error.message
  end
end
