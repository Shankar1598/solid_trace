# frozen_string_literal: true

# The Issue a Notification is about moves from the serialized payload to its own
# column, so the threshold dedup check can find an Issue's rows in SQL. No
# foreign key: an Issue may be deleted while its rows wait, and delivery then
# records them as skipped.
class AddIssueToNotifications < ActiveRecord::Migration[8.1]
  class Row < ActiveRecord::Base
    self.table_name = "notifications"
    serialize :payload, coder: MessagePackCoder
  end

  def up
    add_reference :notifications, :issue, foreign_key: false, index: false
    add_index :notifications, [ :integration_id, :issue_id, :kind, :created_at ],
      name: "index_notifications_on_integration_issue_kind_created_at"

    Row.reset_column_information
    Row.find_each do |row|
      row.issue_id = row.payload["issue_id"]
      row.payload = row.payload.except("issue_id")
      row.save!(touch: false)
    end

    change_column_null :notifications, :issue_id, false
  end

  def down
    Row.find_each do |row|
      row.payload = row.payload.merge("issue_id" => row.issue_id)
      row.save!(touch: false)
    end

    remove_index :notifications, name: "index_notifications_on_integration_issue_kind_created_at"
    remove_reference :notifications, :issue
  end
end
