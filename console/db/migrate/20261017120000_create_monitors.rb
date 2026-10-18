# frozen_string_literal: true

# Monitors belong to an Organization and run a saved Query on one of its
# Projects every interval_minutes. A Threshold monitor compares the number its
# Query's summarize gives, per group, with a threshold over the last
# range_minutes; a Match monitor reads the Logs ingested since its last run,
# from ingest_cursor, the Log batch it has read up to.
#
# next_run_at is when the scheduler next runs it: nil runs it at the next
# tick. timeouts_in_a_row counts a Match monitor's runs that EventStore
# stopped at their timeout; at three it starts again from the newest Log
# batch, and logs_skipped_at says when, until a message has said so.
class CreateMonitors < ActiveRecord::Migration[8.1]
  def change
    create_table :monitors do |t|
      t.references :organization, null: false, foreign_key: true
      t.references :project, null: false, foreign_key: true
      t.string :name, null: false
      t.string :kind, null: false
      t.text :query, null: false
      t.integer :interval_minutes, null: false, default: 1
      t.integer :range_minutes, null: false, default: 5
      t.string :operator
      t.float :threshold
      t.boolean :notify_by_group, null: false, default: false
      t.boolean :alert_on_no_data, null: false, default: false
      t.datetime :disabled_at
      t.datetime :last_run_at
      t.datetime :next_run_at
      t.text :last_error
      t.string :ingest_cursor
      t.integer :timeouts_in_a_row, null: false, default: 0
      t.datetime :logs_skipped_at
      t.timestamps
    end

    create_table :monitor_integrations do |t|
      t.references :monitor, null: false, foreign_key: { on_delete: :cascade }, index: false
      t.references :integration, null: false, foreign_key: { on_delete: :cascade }
      t.timestamps
    end
    add_index :monitor_integrations, [ :monitor_id, :integration_id ], unique: true

    create_table :monitor_states do |t|
      t.references :monitor, null: false, foreign_key: { on_delete: :cascade }, index: false
      t.string :group_key, null: false, default: ""
      t.string :status, null: false, default: "ok"
      t.datetime :since, null: false
      t.float :last_value
      t.timestamps
    end
    add_index :monitor_states, [ :monitor_id, :group_key ], unique: true

    create_table :monitor_runs do |t|
      t.references :monitor, null: false, foreign_key: { on_delete: :cascade }, index: false
      t.datetime :ran_at, null: false
      t.string :status, null: false
      t.text :error
      t.integer :elapsed_ms
      t.integer :firing
      t.integer :matched
      t.boolean :logs_skipped, null: false, default: false
      t.datetime :created_at, null: false
    end
    add_index :monitor_runs, [ :monitor_id, :ran_at ]
  end
end
