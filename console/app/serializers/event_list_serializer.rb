# frozen_string_literal: true

class EventListSerializer
  def initialize(event)
    @event = event
  end

  def as_json(*)
    {
      id: @event.uuid,
      environment: @event.environment,
      created_at: @event.created_at.iso8601,
    }
  end
end
