# frozen_string_literal: true

class EventSerializer
  def initialize(event)
    @event = event
  end

  def as_json(*)
    {
      id: @event.id,
      issue_id: @event.issue_id,
      environment: @event.environment,
      event_data: @event.event_data,
      created_at: @event.created_at.iso8601,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
