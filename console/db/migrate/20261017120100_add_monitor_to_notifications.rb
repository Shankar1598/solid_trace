# frozen_string_literal: true

# A Notification is about an Issue or about one group of a Monitor, never
# both. Like issue_id, monitor_id has no foreign key: a Monitor may be deleted
# while its rows wait, and delivery then records them as skipped. A Monitor's
# rows carry the group they are about ("" for a Monitor of one group), and
# are delivered in order per group.
class AddMonitorToNotifications < ActiveRecord::Migration[8.1]
  def change
    change_column_null :notifications, :issue_id, true
    add_reference :notifications, :monitor, foreign_key: false, index: false
    add_column :notifications, :group_key, :string
    add_index :notifications, [ :monitor_id, :group_key, :created_at ],
      name: "index_notifications_on_monitor_group_created_at"
    add_check_constraint :notifications,
      "(issue_id IS NOT NULL AND monitor_id IS NULL AND group_key IS NULL) OR " \
      "(issue_id IS NULL AND monitor_id IS NOT NULL AND group_key IS NOT NULL)",
      name: "notifications_one_subject"
  end
end
