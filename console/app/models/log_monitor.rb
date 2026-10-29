# frozen_string_literal: true

# A Monitor: a saved Query on one of an Organization's Projects that runs
# every interval_minutes and tells the Monitor's Integrations what it finds.
# The class is LogMonitor because Ruby's standard library already has a
# Monitor; its model name, table and foreign keys are Monitor's.
#
# - A Threshold monitor's Query ends in a summarize of one number. Each row
#   of its table over the last range_minutes is a group, keyed by its other
#   columns; a group whose number passes the threshold is firing. It
#   compares either the number itself (compare value) or its percent change
#   from its Baseline, the number over the baseline range (compare change).
# - A Match monitor reads the Logs ingested since its last run, from
#   ingest_cursor, and sends one message when any match.
#
# MonitorEvaluation runs it; any member of the Organization can manage it.
class LogMonitor < ApplicationRecord
  self.table_name = "monitors"

  OPERATORS = %w[> >= < <=].freeze
  # A Monitor comparing the change fires when a group's number rises, falls
  # or changes either way by at least its threshold, a percent.
  CHANGE_OPERATORS = %w[rises falls changes].freeze
  # Where the Baseline range is: the range just before the current one, or
  # the same range 1 day or 1 week earlier.
  BASELINES = %w[previous day week].freeze
  BASELINE_OFFSETS = { "day" => 1.day, "week" => 1.week }.freeze
  MAX_INTERVAL_MINUTES = 24 * 60
  MAX_RANGE_MINUTES = 7 * 24 * 60
  # A tick comes about a minute after the last, give or take, so a Monitor
  # due a little after a tick runs at that tick rather than a minute late.
  SCHEDULE_SLACK = 30.seconds
  # How long EventStore may take over a run, as for a Dashboard's chart.
  RUN_TIMEOUT_SECONDS = 30
  # A Threshold monitor's range ends this long before its run, so Logs
  # still waiting for Log processing are not counted as missing.
  PROCESSING_LAG = 60.seconds

  def self.model_name
    @model_name ||= ActiveModel::Name.new(self, nil, "Monitor")
  end

  enum :kind, { threshold: "threshold", match: "match" }, prefix: true, validate: true
  enum :compare, { value: "value", change: "change" }, prefix: true, validate: true

  belongs_to :organization
  belongs_to :project
  has_many :monitor_integrations, foreign_key: :monitor_id, inverse_of: :monitor, dependent: :delete_all
  # An Integration unticked on the form destroys its link, so the link can
  # resolve what it was told (MonitorIntegration).
  has_many :integrations, through: :monitor_integrations, dependent: :destroy
  has_many :states, class_name: "MonitorState", foreign_key: :monitor_id, inverse_of: :monitor, dependent: :delete_all
  has_many :runs, -> { order(ran_at: :desc, id: :desc) }, class_name: "MonitorRun", foreign_key: :monitor_id,
    inverse_of: :monitor, dependent: :delete_all

  scope :enabled, -> { where(disabled_at: nil) }
  # The enabled Monitors the scheduler runs at now (due?).
  scope :due, ->(now) { enabled.where(next_run_at: nil).or(enabled.where(next_run_at: ..(now + SCHEDULE_SLACK))) }

  validates :name, presence: true, length: { maximum: 200 }
  validates :query, presence: true, length: { maximum: 20_000 }
  validates :interval_minutes, numericality: { only_integer: true, in: 1..MAX_INTERVAL_MINUTES }
  validates :range_minutes, numericality: { only_integer: true, in: 1..MAX_RANGE_MINUTES }
  validates :operator, inclusion: { in: OPERATORS, message: "must be >, >=, < or <=" }, if: :compares_the_value?
  validates :threshold, numericality: true, if: :compares_the_value?
  validates :operator, inclusion: { in: CHANGE_OPERATORS, message: "must be rises, falls or changes" }, if: :compares_the_change?
  validates :threshold, numericality: { greater_than: 0, message: "must be a percent greater than 0" }, if: :compares_the_change?
  validates :baseline, inclusion: { in: BASELINES, message: "must be previous, day or week" }, if: :compares_the_change?
  validates :minimum, numericality: { greater_than: 0 }, allow_nil: true, if: :compares_the_change?
  validate :baseline_apart_from_the_range, if: :compares_the_change?
  validate :baseline_within_retention, if: :baseline_check_needed?
  validate :project_of_the_organization

  before_validation :forget_the_baseline, unless: :compares_the_change?
  validate :query_runs_as_its_kind, if: :query_check_needed?

  before_save :run_at_the_next_tick, if: :schedule_changed?
  after_update :resolve_groups_on_change, if: :stopped_checking_the_threshold?
  # Before the dependent deletes, which take its states and Integrations.
  before_destroy :resolve_groups_on_delete, prepend: true

  def enabled?
    disabled_at.nil?
  end

  # Saves the Monitor with attributes and, when integration_ids is given,
  # those Integrations, all or nothing. They are looked up in the Monitor's
  # Organization: another Organization's Integration refuses the save.
  # Answers whether it saved; errors says why not.
  #
  # Validating may check the Query with EventStore, which can take as long
  # as a run, so it happens before the transaction opens: on SQLite a write
  # transaction holds the database's write lock, and every other writer
  # would wait on EventStore too.
  def save_with(attributes, integration_ids: nil)
    linked = organization_integrations(integration_ids) unless integration_ids.nil?
    assign_attributes(attributes)
    if linked == :foreign
      errors.add(:integrations, "must belong to the Monitor's Organization")
      return false
    end
    return false unless valid?

    LogMonitor.transaction do
      next false unless save(validate: false)

      self.integrations = linked unless linked.nil?
      true
    end
  end

  # Whether the scheduler should run it at now.
  def due?(now)
    enabled? && (next_run_at.nil? || next_run_at <= now + SCHEDULE_SLACK)
  end

  # Whether a Threshold monitor compares its groups' change from their
  # Baseline rather than their value.
  def compares_the_change?
    kind_threshold? && compare_change?
  end

  def compares_the_value?
    kind_threshold? && !compare_change?
  end

  # Whether a group's value passes the threshold, by the operator: the value
  # itself, or comparing the change, its change from baseline.
  # A group below the minimum is ok.
  def breached?(value, baseline = nil)
    return value.public_send(operator, threshold) unless compares_the_change?
    return false if below_minimum?(value, baseline)

    change = LogMonitor.change(value, baseline)
    case operator
    when "rises" then change >= threshold
    when "falls" then change <= -threshold
    else change.abs >= threshold
    end
  end

  # Whether, comparing the change, a group is too small to compare: neither
  # its number nor its Baseline reaches the minimum, so a drop from 1,000 to
  # 0 is still compared.
  def below_minimum?(value, baseline)
    compares_the_change? && !minimum.nil? && [ value.abs, baseline.abs ].max < minimum
  end

  # value's percent change from baseline. From a Baseline of 0 a number
  # above 0 is an infinite rise and one below an infinite fall, the biggest
  # change there is, and 0 then and now is no change. A negative Baseline
  # divides by its size, so -100 to -50 is up 50%.
  def self.change(value, baseline)
    return (value <=> 0) * Float::INFINITY if baseline.zero? && !value.zero?
    return 0.0 if baseline.zero?

    (value - baseline).fdiv(baseline.abs) * 100
  end

  # The change a Notification carries: nil for no data and for a rise or
  # fall from a Baseline of 0, which has no size.
  def self.change_fact(value, baseline)
    return if value.nil? || baseline.nil?

    change = change(value, baseline)
    change if change.finite?
  end

  # Of two numbers for one group, the one that passes the threshold sooner:
  # the larger for >, >=, rises and changes, the smaller for <, <= and falls.
  # Comparing the change, a and b are changes, and changes compares sizes.
  def worse(a, b)
    case operator
    when "<", "<=", "falls" then [ a, b ].min
    when "changes" then [ a, b ].max_by(&:abs)
    else [ a, b ].max
    end
  end

  # The [from, to) a Threshold monitor's run at now reads, with its range or
  # the range_minutes given.
  def window(now, range_minutes: self.range_minutes)
    to = now - PROCESSING_LAG
    [ to - range_minutes.minutes, to ]
  end

  # The [from, to) a run at now reads for its groups' Baseline: the current
  # range shifted back by baseline_offset.
  def baseline_window(now, range_minutes: self.range_minutes, baseline: self.baseline)
    offset = baseline_offset(range_minutes: range_minutes, baseline: baseline)
    window(now, range_minutes: range_minutes).map { |at| at - offset }
  end

  # How far back the Baseline range is from the current one: the range's
  # own length for previous, so the two meet, else 1 day or 1 week.
  def baseline_offset(range_minutes: self.range_minutes, baseline: self.baseline)
    BASELINE_OFFSETS.fetch(baseline) { range_minutes.minutes }
  end

  # Why the Baseline reaches past the Project's Log Retention, the Project's
  # own or else the instance's default, so it would have no Logs to compare
  # with; nil when it doesn't. It reads after "the Baseline".
  def baseline_past_retention
    retention = project.log_retention_days || InstanceSetting.current.default_log_retention_days
    reach = range_minutes.minutes + baseline_offset
    return if reach <= retention.days

    "reaches #{LogMonitor.duration_text(reach)} back, past the Project's Log Retention of " \
      "#{retention} #{'day'.pluralize(retention)}, so it would have no Logs to compare with: choose a nearer " \
      "Baseline or a shorter range, or keep the Project's Logs longer"
  end

  # A length of time in days, hours and minutes: 7 days and 5 minutes.
  def self.duration_text(duration)
    minutes = (duration / 60).round
    parts = { "day" => minutes / 1440, "hour" => minutes % 1440 / 60, "minute" => minutes % 60 }
    parts.filter_map { |unit, n| "#{n} #{unit.pluralize(n)}" if n.positive? }.to_sentence
  end

  # Whether a Match monitor saved now starts its runs from the newest Log
  # batch: when it has no ingest_cursor, reads another Project, has just
  # become a Match monitor or is turned back on, so it skips the Logs
  # ingested while it was off.
  def arm?
    kind_match? && (new_record? || ingest_cursor.nil? || will_save_change_to_project_id? ||
      will_save_change_to_kind? || turned_on?)
  end

  # Starts a Match monitor's runs from ingest_cursor, a Log batch id, kept
  # at now.
  def arm(cursor, now)
    self.ingest_cursor = cursor
    self.ingest_cursor_at = now
    self.timeouts_in_a_row = 0
  end

  # An Integration unlinked from the Monitor hears no more about it, and no
  # later run would resolve what it was told: each firing group sends it
  # monitor_resolved now, which closes its PagerDuty incident. The groups
  # stay firing for the Integrations still linked.
  def resolve_groups_for(integration)
    send_resolved("unlinked", integrations: Integration.where(id: integration.id))
  end

  private

  # The Organization's Integrations of ids, or :foreign when any isn't one.
  def organization_integrations(ids)
    ids = ids.map(&:to_i).uniq
    found = organization.integrations.where(id: ids).to_a
    found.size == ids.size ? found : :foreign
  end

  def project_of_the_organization
    return unless project && organization

    errors.add(:project, "must belong to the Monitor's Organization") unless project.organization_id == organization_id
  end

  # EventStore checks the Query when what it runs changes, and when the
  # Monitor is turned back on, which arms a Match monitor again.
  def query_check_needed?
    return false if errors.include?(:query) || errors.include?(:project) || project.nil? || kind.nil?

    new_record? || will_save_change_to_query? || will_save_change_to_kind? ||
      will_save_change_to_project_id? || (kind_threshold? && will_save_change_to_range_minutes?) || turned_on?
  end

  # A Baseline 1 day earlier would overlap a range over 1 day. One 1 week
  # earlier can't: no range is longer than a week.
  def baseline_apart_from_the_range
    return unless baseline == "day" && range_minutes.is_a?(Integer) && range_minutes > 1440

    errors.add(:baseline, "can't be 1 day earlier with a range over 1 day, as the two ranges would overlap: " \
      "choose 1 week earlier, or a range of at most 1 day")
  end

  # Retention is checked when how far back the Baseline reaches or the
  # Project changes, so a Monitor whose Project keeps less since can still
  # be edited and turned off; its runs say why they fail.
  def baseline_check_needed?
    return false unless compares_the_change? && project && BASELINES.include?(baseline)
    return false unless range_minutes.is_a?(Integer) && errors.exclude?(:range_minutes)

    new_record? || will_save_change_to_baseline? || will_save_change_to_range_minutes? ||
      will_save_change_to_compare? || will_save_change_to_kind? || will_save_change_to_project_id? || turned_on?
  end

  def baseline_within_retention
    problem = baseline_past_retention
    errors.add(:baseline, problem) if problem
  end

  def query_runs_as_its_kind
    MonitorQueryCheck.call(self)
  end

  def turned_on?
    persisted? && will_save_change_to_disabled_at? && disabled_at.nil?
  end

  # A changed interval, or a Monitor turned back on, runs at the next tick
  # instead of waiting out a schedule it no longer has.
  def schedule_changed?
    will_save_change_to_interval_minutes? || turned_on?
  end

  def run_at_the_next_tick
    self.next_run_at = nil
  end

  # A Match monitor, or a Threshold monitor comparing the value, has no
  # Baseline or minimum; a Match monitor compares nothing.
  def forget_the_baseline
    self.baseline = nil
    self.minimum = nil
    self.compare = "value" if kind_match?
  end

  def stopped_checking_the_threshold?
    no_longer_a_threshold_monitor? || turned_off? || switched_compare?
  end

  # A Threshold monitor that compared the value now compares the change, or
  # the other way: its groups' firing meant something else.
  def switched_compare?
    kind_threshold? && saved_change_to_compare?
  end

  def no_longer_a_threshold_monitor?
    saved_change_to_kind? && kind_before_last_save == "threshold"
  end

  def turned_off?
    saved_change_to_disabled_at? && disabled_at_before_last_save.nil?
  end

  def resolve_groups_on_change
    reason = if no_longer_a_threshold_monitor? then "kind_changed"
    elsif turned_off? then "turned_off"
    else "compare_changed"
    end
    resolve_groups(reason, was: saved_changes.transform_values(&:first))
  end

  def resolve_groups_on_delete
    resolve_groups("deleted")
  end

  # A Threshold monitor changed to a Match monitor, turned off, switched
  # between comparing the value and the change, or deleted no longer
  # compares its groups as they fired, and no run would resolve them: each
  # firing group sends monitor_resolved now, saying why, which closes its
  # PagerDuty incident, and its states go. A group still past the threshold
  # fires again at the next run. was holds the settings the save changed,
  # as they were.
  def resolve_groups(reason, was: {})
    send_resolved(reason, integrations: integrations, was: was)
    states.delete_all
  end

  # The Notifications are written in bulk, as a Monitor may have thousands
  # of firing groups and this runs in the save's transaction. They describe
  # the groups as they fired, by the Monitor's settings then (was, over its
  # own): comparing the change, with their Baseline and change, and the
  # ranges their last run read when that's known.
  def send_resolved(reason, integrations:, was: {})
    was = attributes.merge(was)
    change = was["compare"] == "change"
    facts = { "operator" => was["operator"], "threshold" => was["threshold"], reason => true }
    facts.merge!("compare" => "change", "baseline_setting" => was["baseline"]).merge!(last_ranges(was)) if change
    changes = states.reload.select(&:status_firing?).map do |state|
      payload = facts.merge("value" => state.last_value)
      if change
        payload.merge!("baseline" => state.last_baseline,
          "change" => LogMonitor.change_fact(state.last_value, state.last_baseline))
      end
      [ IntegrationNotification::MONITOR_RESOLVED, state.group_key, payload ]
    end
    IntegrationNotification.schedule_ticks(IntegrationNotification.monitor_changes(self, changes, integrations: integrations))
  end

  # The current and Baseline ranges the groups' states were last read over:
  # the last ok run's, by the settings in was, when the Monitor wasn't saved
  # after that run. After a save the run may have read other settings, so
  # none are known.
  def last_ranges(was)
    ran_at = runs.where(status: "ok").pick(:ran_at)
    return {} unless ran_at && ran_at >= was["updated_at"]

    now = window(ran_at, range_minutes: was["range_minutes"])
    before = baseline_window(ran_at, range_minutes: was["range_minutes"], baseline: was["baseline"])
    %w[from to baseline_from baseline_to].zip([ *now, *before ].map { |at| at.utc.iso8601 }).to_h
  end
end
