# frozen_string_literal: true

# EventStore writes these (see docs/adr/0001-eventstore-writes-console-tables.md).
class AddSeenCountsToIssues < ActiveRecord::Migration[8.1]
  def change
    add_column :issues, :times_seen, :bigint, null: false, default: 0
    add_column :issues, :first_seen_at, :datetime
    add_column :issues, :last_seen_at, :datetime

    # Per Project, the newest Event whose Issue counts are recorded, so that
    # Event processing replaying a batch doesn't count an Event twice.
    create_table :project_seen_cursors, id: false do |t|
      t.references :project, null: false, foreign_key: true, primary_key: true
      t.string :event_uuid, null: false
    end
  end
end
