# frozen_string_literal: true

# An Organization can choose one Integration that is told when its Monitors
# are not running, and when they run again. Each Monitor keeps since when it
# has not run, and the Integration that was told about it. These messages are
# about an Organization's Monitors, not about one Issue or one Monitor
# group. So a Notification of those two kinds has neither subject.
class AddMonitorStatusToOrganizations < ActiveRecord::Migration[8.1]
  ONE_SUBJECT = "(issue_id IS NOT NULL AND monitor_id IS NULL AND group_key IS NULL) OR " \
    "(issue_id IS NULL AND monitor_id IS NOT NULL AND group_key IS NOT NULL)"
  STATUS_SUBJECT = "(issue_id IS NULL AND monitor_id IS NULL AND group_key IS NULL AND " \
    "kind IN ('monitors_not_running', 'monitors_running_again'))"

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
