# frozen_string_literal: true

# The install's one settings record, set by the operator (there's no
# instance-admin role, so no UI). No row means these column defaults.
# EventStore reads it read-only and assumes the same defaults when the row is
# missing (event_store/storage/console_settings.go).
class CreateInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    create_table :instance_settings do |t|
      t.boolean :sql_queries_enabled, null: false, default: false
      t.integer :default_log_retention_days, null: false, default: 30
      t.timestamps
    end
  end
end
