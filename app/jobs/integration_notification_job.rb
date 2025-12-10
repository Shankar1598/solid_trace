# frozen_string_literal: true

class IntegrationNotificationJob < ApplicationJob
  queue_as :default

  retry_on Net::OpenTimeout, Net::ReadTimeout, wait: :exponentially_longer, attempts: 3

  def perform(issue, newly_created)
    IntegrationNotifier.new(issue, newly_created).notify
  end
end
