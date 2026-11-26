# frozen_string_literal: true

# Every minute, at second 30 (config/recurring.yml): tells each Organization's Monitor
# status Integration which Monitors do not run, and which run again
# (IntegrationNotification.monitor_status). An Organization with no Monitor
# status Integration and Monitors told to one has their marks cleared.
class MonitorStatusJob < ApplicationJob
  queue_as :default

  def perform
    now = Time.current
    told = LogMonitor.where.not(not_running_told_integration_id: nil).select(:organization_id)
    Organization.where.not(monitor_status_integration_id: nil).or(Organization.where(id: told))
      .includes(:monitor_status_integration).find_each do |organization|
      IntegrationNotification.monitor_status(organization, now)
    end
  end
end
