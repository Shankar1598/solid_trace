# frozen_string_literal: true

# When the run going now on a Monitor started, nil when none is: the lease
# MonitorEvaluation.run takes, so a scheduled run and Run now never overlap.
class AddRunStartedAtToMonitors < ActiveRecord::Migration[8.1]
  def change
    add_column :monitors, :run_started_at, :datetime
  end
end
