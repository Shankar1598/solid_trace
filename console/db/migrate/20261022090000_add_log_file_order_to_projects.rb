# frozen_string_literal: true

# A Project's Log file order: a JSON array of the Field names the hourly export
# sorts each Log file by, before time. NULL means the default, ["service",
# "file"]; [] means time only (.scratch/log-file-order/spec.md). EventStore
# reads it read-only (ADR 0001).
class AddLogFileOrderToProjects < ActiveRecord::Migration[8.1]
  def change
    add_column :projects, :log_file_order, :json
  end
end
