# frozen_string_literal: true

module McpTools
  # An Organization's members and their roles, as its Members page lists them.
  class ListMembers < Base
    description "Lists an Organization's members by name: each one's name, email and role (admin or member). " \
                "update_issue assigns Issues by these emails."
    read_only!
    takes_organization

    def run
      memberships = organization.organization_users.includes(:user).order("users.name").references(:user)

      json({ organization: organization.slug, members: memberships.map { |membership| member_json(membership) } },
        link: links.members(organization))
    end

    private

    def member_json(membership)
      { name: membership.user.name, email: membership.user.email, role: membership.role }
    end
  end
end
