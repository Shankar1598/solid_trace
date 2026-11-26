# frozen_string_literal: true

# EventStore reads evidence_retention_days
# (event_store/storage/console_settings.go, ADR 0001: change both in the same
# commit).
class AddEvidenceRetentionDaysToProjects < ActiveRecord::Migration[8.1]
  def change
    add_column :projects, :evidence_retention_days, :integer, default: 90, null: false
  end
end
