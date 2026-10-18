# frozen_string_literal: true

# Every minute (config/recurring.yml): enqueues a run of each enabled Monitor
# that is due.
class MonitorSchedulerJob < ApplicationJob
  queue_as :default

  def perform
    LogMonitor.due(Time.current).find_each do |monitor|
      MonitorEvaluationJob.perform_later(monitor.id)
    end
  end
end
