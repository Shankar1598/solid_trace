# frozen_string_literal: true

module McpTools
  # The user the API token acts as, and their Organizations with their role
  # in each.
  class Whoami < Base
    description "Who you are in SolidTrace: the user your API token acts as, and the Organizations you belong to, " \
                "with your role in each (admin or member). Other tools take these Organizations' slugs."
    read_only!

    def run
      memberships = user.organization_users.includes(:organization).sort_by { |membership| membership.organization.slug }

      json({
        user: { name: user.name, email: user.email },
        organizations: memberships.map do |membership|
          { slug: membership.organization.slug, name: membership.organization.name, role: membership.role }
        end,
      }, link: links.user_settings)
    end
  end
end
