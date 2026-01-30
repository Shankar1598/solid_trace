# frozen_string_literal: true

class MessageQueueRecord < ApplicationRecord
  self.abstract_class = true
  connects_to database: { writing: :message_queue, reading: :message_queue }
end
