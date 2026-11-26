# frozen_string_literal: true

# Every minute, at second 30 (config/recurring.yml): tells each Organization's Monitor
# status Integration which Monitors do not run, and which run again
# (IntegrationNotification.monitor_status).
class MonitorStatusJob < ApplicationJob
  queue_as :default

  def perform
    now = Time.current
    Organization.where.not(monitor_status_integration_id: nil).includes(:monitor_status_integration).find_each do |organization|
      IntegrationNotification.monitor_status(organization, now)
    end
  end
end
