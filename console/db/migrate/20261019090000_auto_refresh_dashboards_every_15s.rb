# frozen_string_literal: true

# A Dashboard made from now on runs its charts again every 15 seconds. One
# made before keeps the auto-refresh it has: this changes only the default.
class AutoRefreshDashboardsEvery15s < ActiveRecord::Migration[8.1]
  def change
    change_column_default :dashboards, :auto_refresh, from: "off", to: "15s"
  end
end
