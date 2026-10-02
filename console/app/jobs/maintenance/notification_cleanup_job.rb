# frozen_string_literal: true

module Maintenance
  # Deletes outbox rows whose outcome is final: sent and skipped rows after a
  # day, and failed rows at the attempt cap after a week.
  class NotificationCleanupJob < ApplicationJob
    queue_as :default

    def perform
      Notification.where(status: [ :sent, :skipped ], updated_at: ...1.day.ago).delete_all
      Notification.exhausted.where(updated_at: ...7.days.ago).delete_all
    end
  end
end
