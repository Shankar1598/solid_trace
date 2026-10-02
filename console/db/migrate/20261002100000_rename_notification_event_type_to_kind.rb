# frozen_string_literal: true

# A Notification row holds a notification kind, not an Event ("Event" is a
# Sentry payload). +attempts+ counts delivery attempts for bounded retries.
class RenameNotificationEventTypeToKind < ActiveRecord::Migration[8.1]
  def change
    rename_column :notifications, :event_type, :kind
    add_column :notifications, :attempts, :integer, default: 0, null: false
  end
end
