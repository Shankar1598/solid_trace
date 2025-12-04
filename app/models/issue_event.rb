require 'msgpack'

class IssueEvent < ApplicationRecord
  belongs_to :issue
  serialize :event_data, coder: MessagePackCoder
end
