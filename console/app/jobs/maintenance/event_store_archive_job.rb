# frozen_string_literal: true

module Maintenance
  class EventStoreArchiveJob < ApplicationJob
    queue_as :default

    def perform
      # Archive yesterday's events
      yesterday = Date.yesterday.to_s

      Rails.logger.info("EventStoreArchiveJob: Enqueueing archive for #{yesterday}")

      ConsoleMessage.create!(
        message_type: "archive_events",
        payload: { date: yesterday }
      )
    end
  end
end
