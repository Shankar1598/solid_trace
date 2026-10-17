# frozen_string_literal: true

# An Integration as a Monitor's form and page, and the MCP server, know it:
# without its settings, which hold webhook URLs and keys.
class IntegrationOptionSerializer
  def self.list(integrations)
    integrations.map { |integration| new(integration).as_json }
  end

  def initialize(integration)
    @integration = integration
  end

  def as_json(*)
    { id: @integration.id, name: @integration.name, provider: @integration.provider, enabled: @integration.active }
  end

  def to_json(*)
    as_json.to_json
  end
end
