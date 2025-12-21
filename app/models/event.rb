# frozen_string_literal: true

class Event < ApplicationRecord
  belongs_to :issue
  has_one :event_payload, dependent: :destroy
  delegate :payload, to: :event_payload

  self.attributes_for_inspect = [ :id, :issue_id, :environment ]
end
