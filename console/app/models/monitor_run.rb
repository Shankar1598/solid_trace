# frozen_string_literal: true

# One run of a Monitor: when it ran (ran_at, the run's now), whether
# EventStore answered (ok) or not (error, with why), and what it found: how
# many groups were firing after it, or how many Logs matched. logs_skipped
# marks the run of a Match monitor that started again from the newest Log
# batch after its runs kept timing out, leaving the Logs in between unread.
# The cleanup job keeps the last KEEP per Monitor.
class MonitorRun < ApplicationRecord
  KEEP = 100

  enum :status, { ok: "ok", error: "error" }, prefix: true, validate: true

  belongs_to :monitor, class_name: "LogMonitor", inverse_of: :runs

  validates :ran_at, presence: true

  # Deletes each Monitor's runs older than its newest KEEP.
  def self.prune
    ranked = select(:id, "ROW_NUMBER() OVER (PARTITION BY monitor_id ORDER BY ran_at DESC, id DESC) AS position")
    where(id: unscoped.from(ranked, :ranked).where("position > ?", KEEP).select(:id)).delete_all
  end
end
