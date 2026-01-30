# frozen_string_literal: true

class IntegrationSerializer
  def initialize(integration)
    @integration = integration
  end

  def as_json(*)
    {
      id: @integration.id,
      name: @integration.name,
      provider: @integration.provider,
      enabled: @integration.active,
      settings: @integration.settings,
      created_at: @integration.created_at.iso8601,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
