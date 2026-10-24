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
  SECTIONS = %i[range event_store empty ingest_counts band rates].freeze

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
    logs = if projects.empty? then true elsif answer then !answer["has_logs"] end
    { projects: projects.empty?, logs: logs }
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

  private

  def projects
    @projects ||= organization.projects.order(:created_at, :id).to_a
  end

  # EventStore's Overview of the Organization's Projects, or nil when it has
  # none or EventStore can't answer (@unavailable says why). Storage, the
  # whole install's, is asked for Instance admins only.
  def answer
    return @answer if defined?(@answer)

    @answer = projects.empty? ? nil : EventStore.overview!(
      project_ids: projects.map(&:id), from: @from, to: @to,
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
