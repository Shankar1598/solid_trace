# frozen_string_literal: true

# A Monitor: a saved Query on one of an Organization's Projects that runs
# every interval_minutes and tells the Monitor's Integrations what it finds.
# The class is LogMonitor because Ruby's standard library already has a
# Monitor; its model name, table and foreign keys are Monitor's.
#
# - A Threshold monitor's Query ends in a summarize of one number. Each row
#   of its table over the last range_minutes is a group, keyed by its other
#   columns; a group whose number passes the threshold is firing.
# - A Match monitor reads the Logs ingested since its last run, from
#   ingest_cursor, and sends one message when any match.
#
# MonitorEvaluation runs it; any member of the Organization can manage it.
class LogMonitor < ApplicationRecord
  self.table_name = "monitors"

  OPERATORS = %w[> >= < <=].freeze
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

  belongs_to :organization
  belongs_to :project
  has_many :monitor_integrations, foreign_key: :monitor_id, inverse_of: :monitor, dependent: :delete_all
  has_many :integrations, through: :monitor_integrations
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
  validates :operator, inclusion: { in: OPERATORS, message: "must be >, >=, < or <=" }, if: :kind_threshold?
  validates :threshold, numericality: true, if: :kind_threshold?
  validate :project_of_the_organization
  validate :integrations_of_the_organization
  validate :query_runs_as_its_kind, if: :query_check_needed?

  before_save :run_at_the_next_tick, if: :schedule_changed?

  def enabled?
    disabled_at.nil?
  end

  # Whether the scheduler should run it at now.
  def due?(now)
    enabled? && (next_run_at.nil? || next_run_at <= now + SCHEDULE_SLACK)
  end

  # Whether value passes the threshold, by the operator.
  def breached?(value)
    value.public_send(operator, threshold)
  end

  # The [from, to) a Threshold monitor's run at now reads.
  def window(now)
    to = now - PROCESSING_LAG
    [ to - range_minutes.minutes, to ]
  end

  # Whether a Match monitor saved now starts its runs from the newest Log
  # batch: when it has no ingest_cursor, reads another Project, has just
  # become a Match monitor or is turned back on, so it skips the Logs
  # ingested while it was off.
  def arm?
    kind_match? && (new_record? || ingest_cursor.nil? || will_save_change_to_project_id? ||
      will_save_change_to_kind? || turned_on?)
  end

  # Starts a Match monitor's runs from ingest_cursor, a Log batch id.
  def arm(cursor)
    self.ingest_cursor = cursor
    self.timeouts_in_a_row = 0
  end

  private

  def project_of_the_organization
    return unless project && organization

    errors.add(:project, "must belong to the Monitor's Organization") unless project.organization_id == organization_id
  end

  def integrations_of_the_organization
    return unless organization

    foreign = integrations.reject { |integration| integration.organization_id == organization_id }
    errors.add(:integrations, "must belong to the Monitor's Organization") if foreign.any?
  end

  # EventStore checks the Query when what it runs changes, and when the
  # Monitor is turned back on, which arms a Match monitor again.
  def query_check_needed?
    return false if errors.include?(:query) || errors.include?(:project) || project.nil? || kind.nil?

    new_record? || will_save_change_to_query? || will_save_change_to_kind? ||
      will_save_change_to_project_id? || (kind_threshold? && will_save_change_to_range_minutes?) || turned_on?
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
end
