# frozen_string_literal: true

# Evidence Retention: how many whole days the Agent Project's Organization
# keeps its Evidence files, from 1 to 3650. EventStore reads it
# (event_store/storage/console_settings.go, ADR 0001: change both in the same
# commit) and deletes the bytes of each older file, and a change applies to
# the files already there. Only the Agent Project uses it.
class AddEvidenceRetentionDaysToProjects < ActiveRecord::Migration[8.1]
  def change
    add_column :projects, :evidence_retention_days, :integer, default: 90, null: false
  end
end
