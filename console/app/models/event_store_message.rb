# frozen_string_literal: true

class EventStoreMessage < MessageQueueRecord
  enum :status, {
    pending: 0,
    processing: 1,
    processed: 2,
    failed: 3,
  }, prefix: true

  # Nothing sets `processing` now; older rows stuck in it are retried too.
  scope :processable, -> { where(status: [ :pending, :processing, :failed ]).where("attempts < 5") }

  serialize :payload, coder: MessagePacker::WithCompression
end
