# frozen_string_literal: true

class ConsoleMessage < MessageQueueRecord
  enum :status, {
    pending: 0,
    processing: 1,
    processed: 2,
    failed: 3,
  }, prefix: true

  scope :processable, -> { where(status: [ :pending, :failed ]).where("attempts < 5") }

  serialize :payload, coder: MessagePacker::WithCompression::VeryFast
end
