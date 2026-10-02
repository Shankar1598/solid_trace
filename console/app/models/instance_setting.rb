# frozen_string_literal: true

# The install's settings: one record, set by the operator with
# `bin/rails instance_settings:set` (lib/tasks/instance_settings.rake) or
# `InstanceSetting.current.update!(...)` in a Rails console. There's no UI:
# Console has no instance-admin role. Until the operator first sets something
# there's no row and the column defaults apply.
#
# EventStore reads this table read-only, and assumes the same defaults when the
# row is missing (event_store/storage/console_settings.go).
class InstanceSetting < ApplicationRecord
  ID = 1

  # What turning SQL queries on allows (docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md).
  SQL_QUERIES_DESCRIPTION = <<~TEXT
    SQL queries let every member of every Organization run raw DuckDB SQL from the Logs explorer.
    They are not sandboxed or limited to a Project, so turning them on lets any member:
      - read every Organization's Logs and Events
      - read files on the server
      - write files on the server
      - drop tables
      - starve ingest with slow queries
    Turn them on only if you trust every member of every Organization.
  TEXT

  validates :sql_queries_enabled, inclusion: { in: [ true, false ] }
  validates :default_log_retention_days, numericality: { only_integer: true, greater_than: 0 }

  def self.current
    find_by(id: ID) || new(id: ID)
  end
end
