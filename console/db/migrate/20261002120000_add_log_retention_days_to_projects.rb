# frozen_string_literal: true

# A Project's own Log Retention in whole days; NULL means the instance default.
# EventStore reads it read-only (docs/adr/0001-eventstore-writes-console-tables.md
# covers the tables it writes; Logs add none).
class AddLogRetentionDaysToProjects < ActiveRecord::Migration[8.1]
  def change
    add_column :projects, :log_retention_days, :integer
  end
end
