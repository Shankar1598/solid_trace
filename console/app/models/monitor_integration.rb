# frozen_string_literal: true

# An Integration a Monitor tells about what it finds. Both belong to the same
# Organization (LogMonitor checks it).
class MonitorIntegration < ApplicationRecord
  belongs_to :monitor, class_name: "LogMonitor", inverse_of: :monitor_integrations
  belongs_to :integration
end
