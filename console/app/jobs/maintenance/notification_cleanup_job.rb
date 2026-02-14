# frozen_string_literal: true

module Maintenance
  class NotificationCleanupJob < ApplicationJob
    queue_as :default

    def perform
      Notification.where(status: :sent)
        .where("sent_at < ?", 1.day.ago)
        .delete_all
    end
  end
end
