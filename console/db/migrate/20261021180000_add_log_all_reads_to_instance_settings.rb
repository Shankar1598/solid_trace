# frozen_string_literal: true

# Whether every Log read is timed into the Meta Organization's Project with
# the SQL EventStore ran for it, not only the MCP server's
# (.scratch/call-timings/spec.md). Off by default: the explorer and
# Dashboards refresh every 15 seconds, and each read can be many SQL lines.
# EventStore reads other columns of instance_settings by name, so it doesn't
# concern it (ADR 0001).
class AddLogAllReadsToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :log_all_reads, :boolean, default: false, null: false
  end
end
