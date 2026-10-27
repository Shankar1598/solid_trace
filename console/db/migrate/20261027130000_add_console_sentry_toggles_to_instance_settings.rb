# frozen_string_literal: true

# Whether Console sends its own Issues, and traces its requests and jobs, to
# its Sentry DSN, the Meta Project on an install (docs/traces.md). Both on by
# default, as they were; turning them off needs no restart (ConsoleSentry).
# EventStore reads other columns of instance_settings by name, so they don't
# concern it (ADR 0001).
class AddConsoleSentryTogglesToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :send_console_issues, :boolean, default: true, null: false
    add_column :instance_settings, :trace_console, :boolean, default: true, null: false
  end
end
