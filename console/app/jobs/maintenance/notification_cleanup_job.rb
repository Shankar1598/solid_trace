# frozen_string_literal: true

module Maintenance
  # Deletes outbox rows whose outcome is final: sent and skipped rows after a
  # day, and failed rows at the attempt cap after a week. Also keeps only
  # each Monitor's last MonitorRun::KEEP runs, and deletes the message
  # queue's rows after MESSAGE_RETENTION, whatever their status: a message
  # that old is no use even if it was never handled.
  class NotificationCleanupJob < ApplicationJob
    queue_as :default

    MESSAGE_RETENTION = 30.days

    def perform
      Notification.where(status: [ :sent, :skipped ], updated_at: ...1.day.ago).delete_all
      Notification.exhausted.where(updated_at: ...7.days.ago).delete_all
      MonitorRun.prune
      [ EventStoreMessage, ConsoleMessage ].each do |model|
        model.where(created_at: ...MESSAGE_RETENTION.ago).delete_all
      end
    end
  end
end
