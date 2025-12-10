class Event < ApplicationRecord
  belongs_to :issue
  serialize :event_data, coder: MessagePackCoder
  self.attributes_for_inspect = [ :id, :issue_id, :environment ]
end
