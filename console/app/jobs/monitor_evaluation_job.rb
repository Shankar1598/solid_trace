# frozen_string_literal: true

# Runs one Monitor (MonitorEvaluation) if it is still due and enabled. One
# run at a time per Monitor: MonitorEvaluation.run does nothing while another
# run of the Monitor is going, Run now's too, and the next tick enqueues it
# again if it is still due.
class MonitorEvaluationJob < ApplicationJob
  queue_as :default

  def perform(monitor_id)
    monitor = LogMonitor.find_by(id: monitor_id)
    now = Time.current
    return unless monitor&.due?(now)

    MonitorEvaluation.run(monitor, now)
  end
end
