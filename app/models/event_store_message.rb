# frozen_string_literal: true

class EventStoreMessage < MessageQueueRecord
  enum :status, {
    pending: 0,
    processing: 1,
    processed: 2,
    failed: 3,
  }

  scope :processable, -> { where(status: [ :pending, :failed ]).where("attempts < 5") }

  serialize :payload, MessagePacker::WithCompression
end
