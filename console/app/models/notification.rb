# frozen_string_literal: true

class Notification < ApplicationRecord
  belongs_to :integration

  serialize :payload, coder: MessagePackCoder

  module GROUPING
    INITIAL_DELAY = 10.seconds
    DEBOUNCE_WINDOW = 1.minute
  end

  enum :status, {
    pending: 0,
    processing: 1,
    sent: 2,
    failed: 3,
  }, prefix: true

  def self.enqueue!(integration:, event_type:, payload: {})
    record = create!(
      integration: integration,
      event_type: event_type,
      payload: payload,
      status: :pending
    )

    IntegrationNotificationProcessorJob.set(wait: GROUPING::INITIAL_DELAY).perform_later(integration.id)

    record
  end

  def self.pending_for(integration_id, cutoff_at = Time.current)
    where(integration_id: integration_id, status: [ :pending, :failed ])
      .where("created_at <= ?", cutoff_at)
      .order(:created_at)
  end
end
