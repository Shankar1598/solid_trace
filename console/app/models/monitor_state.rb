# frozen_string_literal: true

# Where one group of a Threshold monitor stands: ok or firing since a time,
# with the number its last run gave (nil for no data). group_key is the
# group's columns as a JSON object in the table's order, or "" when the
# Monitor has one group. A change of status is what sends a firing or
# resolved Notification, so a group that stays firing sends nothing more.
class MonitorState < ApplicationRecord
  enum :status, { ok: "ok", firing: "firing" }, prefix: true, validate: true

  belongs_to :monitor, class_name: "LogMonitor", inverse_of: :states

  validates :since, presence: true

  # The group's columns, name to value, or {} for a Monitor of one group.
  def group
    MonitorState.group_of(group_key)
  end

  def self.group_of(group_key)
    group_key.blank? ? {} : JSON.parse(group_key)
  end
end
