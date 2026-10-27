# frozen_string_literal: true

# Trace Retention: how many whole days a Project keeps its Spans. The instance
# default starts at 14, and a Project's own, NULL meaning the default,
# overrides it, as Log Retention does. EventStore reads both read-only
# (event_store/storage/console_settings.go, ProjectTraceRetention; ADR 0001).
class AddTraceRetentionDays < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :default_trace_retention_days, :integer, default: 14, null: false
    add_column :projects, :trace_retention_days, :integer
  end
end
