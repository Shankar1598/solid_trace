# frozen_string_literal: true

# A Threshold monitor's run is held while EventStore says that its Logs are
# not complete. The run keeps why, and the Monitor keeps when its runs
# started to be held.
class AddHoldsToMonitors < ActiveRecord::Migration[8.1]
  def change
    add_column :monitors, :held_since, :datetime
    add_column :monitor_runs, :held_reason, :text
  end
end
