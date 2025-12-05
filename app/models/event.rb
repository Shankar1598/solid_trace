require 'msgpack'

class Event < ApplicationRecord
  belongs_to :issue
  serialize :event_data, coder: MessagePackCoder
end
