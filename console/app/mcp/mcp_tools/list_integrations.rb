# frozen_string_literal: true

module McpTools
  # Lists an Organization's Integrations, without their settings, as the
  # Monitor form offers them.
  class ListIntegrations < Base
    description "Lists the Organization's Integrations, which Monitors tell what they find: id, name, provider and whether " \
                "it's enabled. create_monitor and update_monitor take their ids as integration_ids."
    read_only!
    takes_organization

    def run
      integrations = IntegrationOptionSerializer.list(organization.integrations.order(:name))

      json({ organization: organization.slug, integrations: integrations }, link: links.integrations(organization))
    end
  end
end
