# frozen_string_literal: true

# When a Match monitor's ingest_cursor was kept, so the explorer link of its
# next message can start from there: the Logs it reads are those processed
# since. Monitors already armed take their last run, or their last save.
class AddIngestCursorAtToMonitors < ActiveRecord::Migration[8.1]
  def change
    add_column :monitors, :ingest_cursor_at, :datetime

    up_only do
      execute "UPDATE monitors SET ingest_cursor_at = COALESCE(last_run_at, updated_at) WHERE ingest_cursor IS NOT NULL"
    end
  end
end
