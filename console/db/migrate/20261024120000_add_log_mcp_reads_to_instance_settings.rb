# frozen_string_literal: true

# Whether the MCP server's Log reads are timed into the Meta Organization's
# Project with the SQL EventStore ran for them, while log_all_reads is off.
# On by default, as they always were; an operator turns it off when agents'
# reads add too many Logs. log_all_reads on times them whatever this says.
# EventStore reads other columns of instance_settings by name, so it doesn't
# concern it (ADR 0001).
class AddLogMcpReadsToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :log_mcp_reads, :boolean, default: true, null: false
  end
end
