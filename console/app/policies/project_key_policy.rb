# frozen_string_literal: true

# A Project's keys send Logs and Events to it, so only the Organization's
# admins make, rotate and delete them, in the UI and the MCP server alike.
class ProjectKeyPolicy < ApplicationPolicy
  def create?
    record.project.organization.organization_users.find_by(user: user)&.admin? || false
  end

  alias_method :rotate?, :create?
  alias_method :destroy?, :create?
end
