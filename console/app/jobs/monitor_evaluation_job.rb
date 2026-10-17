# frozen_string_literal: true

# Runs one Monitor (MonitorEvaluation) if it is still due and enabled. One
# run at a time per Monitor: a run enqueued while another is going is
# dropped, and the next tick enqueues it again if it is still due.
class MonitorEvaluationJob < ApplicationJob
  queue_as :default

  limits_concurrency to: 1,
    key: ->(monitor_id, *_args) { monitor_id },
    duration: 5.minutes,
    on_conflict: :discard

  def perform(monitor_id)
    monitor = LogMonitor.find_by(id: monitor_id)
    now = Time.current
    return unless monitor&.due?(now)

    MonitorEvaluation.run(monitor, now)
  end
end
