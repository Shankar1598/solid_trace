# frozen_string_literal: true

# Every Project's Logs are sorted by Service and file in ClickHouse, so a
# Project no longer has a Log file order of its own (ADR 0016).
class RemoveLogFileOrderFromProjects < ActiveRecord::Migration[8.1]
  def change
    remove_column :projects, :log_file_order, :json
  end
end
