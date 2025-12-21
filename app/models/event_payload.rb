# frozen_string_literal: true

class EventPayload < ApplicationRecord
  belongs_to :event
  serialize :payload, coder: MessagePackCoder::Compressed
end
