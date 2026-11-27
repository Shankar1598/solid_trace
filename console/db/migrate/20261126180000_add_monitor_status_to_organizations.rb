# frozen_string_literal: true

# An Organization can choose one Integration that is told when its Monitors
# are not running, and when they run again. Each Monitor keeps since when it
# has not run, and the Integration that was told about it. These messages are
# about an Organization's Monitors, not about one Issue or one Monitor
# group. So a Notification of those two kinds has neither subject.
#
# SQLite adds a foreign key or a check constraint by making the table again:
# it copies the rows, drops the table and makes it from the copy. The drop
# runs the ON DELETE actions of the tables that point at it, and so deletes
# their rows, unless foreign keys are off. SQLite does not turn them off in a
# transaction. So this migration turns them off before it opens its own.
class AddMonitorStatusToOrganizations < ActiveRecord::Migration[8.1]
  disable_ddl_transaction!

  ONE_SUBJECT = "(issue_id IS NOT NULL AND monitor_id IS NULL AND group_key IS NULL) OR " \
    "(issue_id IS NULL AND monitor_id IS NOT NULL AND group_key IS NOT NULL)"
  STATUS_SUBJECT = "(issue_id IS NULL AND monitor_id IS NULL AND group_key IS NULL AND " \
    "kind IN ('monitors_not_running', 'monitors_running_again'))"

  def exec_migration(connection, direction)
    connection.disable_referential_integrity { connection.transaction { super } }
  end

  def change
    add_reference :organizations, :monitor_status_integration,
      foreign_key: { to_table: :integrations, on_delete: :nullify }, index: true
    add_column :monitors, :not_running_since, :datetime
    add_reference :monitors, :not_running_told_integration,
      foreign_key: { to_table: :integrations, on_delete: :nullify }, index: true

    remove_check_constraint :notifications, ONE_SUBJECT, name: "notifications_one_subject"
    add_check_constraint :notifications, "#{ONE_SUBJECT} OR #{STATUS_SUBJECT}", name: "notifications_one_subject"
  end
end
