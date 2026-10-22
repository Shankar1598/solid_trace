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
    SQL queries let every member of every Organization run raw DuckDB SQL from the Logs explorer and the MCP server.
    They are read-only: anything but one SELECT is refused, so they can't change data, settings or files.
    They read only the logs and events views of the Project they run on: tables, files, URLs and the catalogue are refused.
    But they are not sandboxed, so turning them on lets any member:
      - read every Log and Event of their own Organization's Projects
      - starve ingest with slow queries
    They are on by default. Turn them off if you don't trust every member of every Organization not to slow the install down.
  TEXT

  # The Meta Organization (ADR 0011), until it is deleted.
  belongs_to :meta_organization, class_name: "Organization", optional: true

  # Encrypted with keys derived from SECRET_KEY_BASE (ADR 0015).
  encrypts :smtp_password

  validates :sql_queries_enabled, inclusion: { in: [ true, false ] }
  validates :sign_up_open, inclusion: { in: [ true, false ] }
  validates :log_all_reads, inclusion: { in: [ true, false ] }
  validates :default_log_retention_days, numericality: { only_integer: true, greater_than: 0 }

  def self.current
    find_by(id: ID) || new(id: ID)
  end

  # Whether someone may sign up: while the install has no user, so whoever set
  # it up can make the first account, or when the operator has opened sign-up.
  # Otherwise people join by Invite.
  def sign_up_allowed?
    sign_up_open? || !User.exists?
  end
end
