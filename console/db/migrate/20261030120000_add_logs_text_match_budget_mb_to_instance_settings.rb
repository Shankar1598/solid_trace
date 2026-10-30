# frozen_string_literal: true

# The most a Query that matches text may read, in MiB of kept bytes, before
# the Logs explorer and the MCP server refuse it; 0 refuses none. It was
# EventStore's logs_text_match_budget_mb, which took a restart to change.
# EventStore reads it read-only on each sized read, and assumes 4096 when
# the row is missing (event_store/storage/console_settings.go; ADR 0001).
#
# On production in October 2026, text-match reads under about 4 GB of
# ClickHouse's read_bytes finished within 7 s, and every one of 13 GB or more
# ran past the 30 s timeout. A full scan of message and search_text reads
# about 0.8 times the kept bytes, so 4 GiB kept is about 3.2 GiB read.
class AddLogsTextMatchBudgetMbToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :logs_text_match_budget_mb, :integer, default: 4096, null: false
  end
end
